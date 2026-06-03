package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/internal/source"
	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
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
				if verbose {
					fmt.Printf("  "+format+"\n", args...)
				}
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
		}

		if err := agg.Aggregate(ctx, sources); err != nil {
			return fmt.Errorf("aggregation failed: %w", err)
		}

		if initFromPlaylist != "" {
			sourceID := initFromPlaylist
			if len(sourceID) > 5 && sourceID[:5] == "name:" {
				playlists, err := client.GetAllLibraryPlaylists(ctx)
				if err != nil {
					return fmt.Errorf("failed to list playlists: %w", err)
				}
				found := false
				for _, p := range playlists {
					if p.Name == sourceID[5:] {
						sourceID = p.ID
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("playlist named %q not found in your library", sourceID[5:])
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
			if err := oneShotAgg.Aggregate(ctx, []source.Source{ps}); err != nil {
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
