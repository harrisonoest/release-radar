package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/internal/source"
	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var initAll bool
var initFromPlaylist string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Pull artists from your Apple Music library",
	Long: `Fetches all artists from your Apple Music library and saves them
to a local cache. Sets the baseline timestamp for future release scans.

	Use --all to additionally pull artists from library albums, library songs,
and liked songs.

Use --from-playlist to additionally merge artists from a single playlist
into the artists table without saving it as a permanent source. Accepts
'name:NAME' or a playlist ID.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer store.Close()

		authenticator, err := auth.NewAuthenticatorWithStore(cfg, store)
		if err != nil {
			return fmt.Errorf("failed to initialize authenticator: %w", err)
		}

		client, err := api.NewClient(cfg, authenticator)
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
		defer cancel()

		storefront, err := client.GetStorefront(ctx)
		if err != nil {
			return fmt.Errorf("failed to get storefront: %w", err)
		}

		agg := &source.Aggregator{
			Store:      store,
			Fetcher:    client,
			Storefront: storefront,
			Logger: func(format string, args ...interface{}) {
				// Resolution/coverage warnings must not hide behind -v:
				// every dropped name is an artist the tool will miss.
				fmt.Printf("  "+format+"\n", args...)
			},
		}

		sources := []source.Source{
			&source.LibraryArtists{},
		}
		if initAll {
			sources = append(sources,
				&source.LibraryAlbums{},
				&source.LibrarySongs{},
				&source.LikedSongs{},
			)
		} else {
			fmt.Println("NOTE: bare 'init' tracks library ARTISTS only. Artists you know through")
			fmt.Println("albums, songs, liked songs, or playlists are missed — run 'init --all' for full coverage.")
		}

		runAgg := func(agg *source.Aggregator, srcs []source.Source, total int) error {
			var srcName atomic.Value
			var srcArtists atomic.Int64
			srcName.Store("")

			p := mpb.New(mpb.WithWidth(70))
			bar := p.AddBar(int64(total),
				mpb.BarFillerClearOnComplete(),
				mpb.PrependDecorators(
					decor.Any(func(decor.Statistics) string {
						n := srcName.Load()
						if s, ok := n.(string); ok && s != "" {
							return fmt.Sprintf("Fetching %s", s)
						}
						return "Fetching sources"
					}, decor.WCSyncSpace),
				),
				mpb.AppendDecorators(
					decor.CountersNoUnit("%d / %d"),
					decor.Name(" "),
					decor.Any(func(decor.Statistics) string {
						return fmt.Sprintf("artists: %d", srcArtists.Load())
					}),
				),
			)

			agg.OnSourceDone = func(sourceNum, totalSources int, name string, artistCount int) {
				srcName.Store(name)
				srcArtists.Store(int64(artistCount))
				bar.SetCurrent(int64(sourceNum))
			}

			err := agg.Aggregate(ctx, srcs)
			bar.SetTotal(int64(total), true)
			p.Wait()
			return err
		}

		if err := runAgg(agg, sources, len(sources)); err != nil {
			return fmt.Errorf("aggregation failed: %w", err)
		}

		if initFromPlaylist != "" {
			sourceID := initFromPlaylist
			if playlistName, isName := strings.CutPrefix(sourceID, "name:"); isName {
				playlists, err := client.GetAllLibraryPlaylists(ctx)
				if err != nil {
					return fmt.Errorf("failed to list playlists: %w", err)
				}
				found := false
				for _, p := range playlists {
					if p.Name == playlistName {
						sourceID = p.ID
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("playlist named %q not found in your library", playlistName)
				}
			}
			ps, err := source.Build("playlist", sourceID)
			if err != nil {
				return fmt.Errorf("invalid playlist source: %w", err)
			}
			oneShotAgg := &source.Aggregator{
				Store:          store,
				Fetcher:        client,
				Storefront:     storefront,
				SkipSourceRows: true,
				Logger: func(format string, args ...interface{}) {
					if verbose {
						fmt.Printf("  "+format+"\n", args...)
					}
				},
			}
			if err := runAgg(oneShotAgg, []source.Source{ps}, 1); err != nil {
				return fmt.Errorf("playlist aggregation failed: %w", err)
			}
			fmt.Println("Merged artists from playlist (one-shot).")
		}

		if err := store.MarkScanned(0, 0); err != nil {
			return fmt.Errorf("failed to save scan state: %w", err)
		}

		count, err := store.CountArtists()
		if err != nil {
			return fmt.Errorf("failed to count artists: %w", err)
		}

		fmt.Printf("Cached %d artists. Ready to scan!\n", count)
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&initAll, "all", false, "pull artists from all library sources (albums, songs, liked)")
	initCmd.Flags().StringVar(&initFromPlaylist, "from-playlist", "", "fetch artists from this playlist once (does not save as a source). Accepts 'name:NAME' or a playlist ID.")
}
