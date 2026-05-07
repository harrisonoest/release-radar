package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"time"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/internal/playlist"
	"github.com/harrisonoest/release-radar/internal/scanner"
	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var (
	scanSince        string
	scanConcurrency  int
	scanLimitArtists int

	scanCmd = &cobra.Command{
		Use:   "scan",
		Short: "Check for new releases and add to playlist",
		Long: `Scans all cached artists for new album releases since the last scan.
New releases are added to your Release Radar playlist (or configured playlist).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if scanConcurrency > 0 {
				cfg.Scan.Concurrency = scanConcurrency
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

			dbArtists, err := store.ListArtists()
			if err != nil {
				return fmt.Errorf("failed to load artists: %w\nRun 'release-radar init' first.", err)
			}

			if len(dbArtists) == 0 {
				fmt.Println("No artists cached. Run 'release-radar init' first.")
				return nil
			}

			dbIgnored, err := store.GetIgnoredCatalogIDs()
			if err != nil {
				return fmt.Errorf("failed to load ignored artists: %w", err)
			}

			ignored := make(map[string]bool)
			for id := range dbIgnored {
				ignored[id] = true
			}
			for _, id := range cfg.Scan.IgnoredArtists {
				ignored[id] = true
			}

			if len(ignored) > 0 {
				filtered := make([]db.Artist, 0, len(dbArtists))
				for _, a := range dbArtists {
					if !ignored[a.CatalogID] {
						filtered = append(filtered, a)
					}
				}
				if verbose {
					fmt.Printf("Excluding %d ignored artists (remaining: %d).\n", len(dbArtists)-len(filtered), len(filtered))
				}
				dbArtists = filtered
			}

			var since time.Time
			if scanSince != "" {
				since, err = time.Parse("2006-01-02", scanSince)
				if err != nil {
					return fmt.Errorf("invalid --since date: %w", err)
				}
			} else {
				state, err := store.GetScanState()
				if err != nil {
					return fmt.Errorf("failed to load scan state: %w", err)
				}
				if state.LastScan != "" {
					since, err = time.Parse(time.RFC3339, state.LastScan)
					if err != nil {
						since = time.Now().AddDate(0, 0, -30)
					}
				} else {
					since = time.Now().AddDate(0, 0, -30)
				}
			}

			artists := dbArtists
			if scanLimitArtists > 0 && scanLimitArtists < len(artists) {
				artists = artists[:scanLimitArtists]
				if verbose {
					fmt.Printf("Limited to %d artists (--limit-artists flag)\n", scanLimitArtists)
				}
			}

			scan := scanner.New(cfg, client, verbose)

			var foundCount atomic.Int64
			var errCount atomic.Int64

			p := mpb.New(mpb.WithWidth(60))
			bar := p.AddBar(int64(len(artists)),
				mpb.BarFillerClearOnComplete(),
				mpb.PrependDecorators(decor.Name("Scanning artists", decor.WCSyncSpace)),
				mpb.AppendDecorators(
					decor.CountersNoUnit("%d / %d"),
					decor.Name(" | "),
					decor.Any(func(decor.Statistics) string { return fmt.Sprintf("found: %d", foundCount.Load()) }),
					decor.Name(" "),
					decor.Any(func(decor.Statistics) string { return fmt.Sprintf("err: %d", errCount.Load()) }),
				),
			)

			scan.SetProgressCallback(func(checked, found, errors int64) {
				bar.SetCurrent(checked)
				foundCount.Store(found)
				errCount.Store(errors)
			})

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()

			releases, err := scan.Scan(ctx, artists, since)
			p.Wait()
			fmt.Println()
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			if len(releases) == 0 {
				fmt.Printf("No new releases found since %s.\n", since.Format("2006-01-02"))
				if err := store.MarkScanned(0, 0); err != nil {
					return fmt.Errorf("failed to update scan state: %w", err)
				}
				return nil
			}

			sort.Slice(releases, func(i, j int) bool {
				return releases[i].ReleaseDate > releases[j].ReleaseDate
			})

			printReleases(releases)

			if dryRun {
				fmt.Printf("\nDry run — %d releases would be added (not saved).\n", len(releases))
				return nil
			}

			pm := playlist.New(cfg, client)
			storefront, err := client.GetStorefront(ctx)
			if err != nil {
				return fmt.Errorf("failed to get storefront: %w", err)
			}
			pm.SetStorefront(storefront)

			playlistID, err := pm.EnsurePlaylist(ctx)
			if err != nil {
				return fmt.Errorf("failed to ensure playlist: %w", err)
			}

			fmt.Printf("\nAdding %d new releases to playlist…\n", len(releases))
			added, err := pm.AddReleases(ctx, playlistID, releases)
			if err != nil {
				return fmt.Errorf("failed to add releases: %w", err)
			}

			if err := store.MarkScanned(len(releases), added); err != nil {
				return fmt.Errorf("failed to update scan state: %w", err)
			}

			fmt.Printf("Added %d albums to playlist.\n", added)

			return nil
		},
	}
)

func init() {
	scanCmd.Flags().StringVar(&scanSince, "since", "", "check releases since date (YYYY-MM-DD)")
	scanCmd.Flags().IntVarP(&scanConcurrency, "concurrency", "c", 0, "number of concurrent artist queries (default from config)")
	scanCmd.Flags().IntVar(&scanLimitArtists, "limit-artists", 0, "limit to first N artists (for testing)")
}

func printReleases(releases []scanner.Release) {
	fmt.Printf("\n")
	fmt.Printf("┌──────────────────────────────────────────────────────────────────────────────┐\n")
	fmt.Printf("│  %-3d new releases                                                           │\n", len(releases))
	fmt.Printf("├──────────────────────────────────────────────────────────────────────────────┤\n")

	for _, r := range releases {
		prefix := "+ "
		if r.TrackCount == 0 {
			prefix = "  "
		}
		fmt.Fprintf(os.Stdout, "│ %s%-12s %-40s %-20s │\n",
			prefix, r.ReleaseDate, truncate(r.AlbumName, 40), truncate(r.ArtistName, 20))
	}

	fmt.Printf("└──────────────────────────────────────────────────────────────────────────────┘\n")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
