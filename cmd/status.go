package cmd

import (
	"fmt"

	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show tracked artists and last scan info",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		if err := store.Close(); err != nil {
			return fmt.Errorf("failed to close store: %w", err)
		}

		count, err := store.CountArtists()
		if err != nil {
			return fmt.Errorf("failed to count artists: %w", err)
		}

		state, err := store.GetScanState()
		if err != nil {
			return fmt.Errorf("failed to load scan state: %w", err)
		}

		auth, _ := store.GetAuth()
		authenticated := auth != nil && auth.MusicUserToken != ""

		ignoredCount, _ := store.CountIgnoredArtists()

		fmt.Printf("Artists tracked: %d\n", count)
		if ignoredCount > 0 {
			fmt.Printf("Artists ignored: %d\n", ignoredCount)
		}
		if state.LastScan != "" {
			fmt.Printf("Last scan:       %s\n", state.LastScan)
			fmt.Printf("Albums found:    %d\n", state.AlbumsFound)
			fmt.Printf("Albums added:    %d\n", state.AlbumsAdded)
		} else {
			fmt.Println("Last scan:       never")
		}
		sources, err := store.ListArtistSources()
		if err != nil {
			return fmt.Errorf("failed to list sources: %w", err)
		}
		uniqueSources := map[string]bool{}
		for _, s := range sources {
			uniqueSources[s.SourceType+"|"+s.SourceID] = true
		}
		fmt.Printf("Sources:        %d\n", len(uniqueSources))

		counts, err := store.CountReleasesByState()
		if err != nil {
			return fmt.Errorf("failed to count releases: %w", err)
		}
		if len(counts) > 0 {
			total := 0
			for _, n := range counts {
				total += n
			}
			fmt.Printf("Releases:       %d total", total)
			for _, state := range []string{"added", "ignored", "seen"} {
				if n, ok := counts[state]; ok {
					fmt.Printf(" (%s: %d)", state, n)
				}
			}
			fmt.Println()
		}
		fmt.Printf("Authenticated:   %v\n", authenticated)
		return nil
	},
}
