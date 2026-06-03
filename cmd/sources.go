package cmd

import (
	"fmt"
	"strings"

	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
)

var sourcesCmd = &cobra.Command{
	Use:   "sources",
	Short: "Manage artist sources",
	Long:  `List, add, and remove artist sources. Sources populate the artists table when 'release-radar init --all' is run.`,
}

var sourcesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer store.Close()

		sources, err := store.ListArtistSources()
		if err != nil {
			return fmt.Errorf("failed to list artist sources: %w", err)
		}

		if len(sources) == 0 {
			fmt.Println("No artist sources configured. Run 'release-radar init' first.")
			return nil
		}

		counts := make(map[string]int)
		for _, s := range sources {
			key := s.SourceType + "|" + s.SourceID
			counts[key]++
		}

		fmt.Printf("Configured sources (%d):\n", len(counts))
		for key, n := range counts {
			parts := strings.SplitN(key, "|", 2)
			sourceType := parts[0]
			sourceID := ""
			if len(parts) == 2 {
				sourceID = parts[1]
			}
			switch sourceType {
			case "library_artists":
				fmt.Printf("  library_artists            %d artists\n", n)
			case "library_albums":
				fmt.Printf("  library_albums             %d artists\n", n)
			case "library_songs":
				fmt.Printf("  library_songs              %d artists\n", n)
			case "liked_songs":
				fmt.Printf("  liked_songs                %d artists\n", n)
			case "playlist":
				fmt.Printf("  playlist %-17s %d artists\n", sourceID, n)
			}
		}
		return nil
	},
}

func init() {
	sourcesCmd.AddCommand(sourcesListCmd)
}
