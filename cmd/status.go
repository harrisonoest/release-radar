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
		defer store.Close()

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
		fmt.Printf("Authenticated:   %v\n", authenticated)
		return nil
	},
}
