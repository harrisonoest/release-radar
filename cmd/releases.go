package cmd

import (
	"fmt"

	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
)

var releasesCmd = &cobra.Command{
	Use:   "releases",
	Short: "Manage tracked releases",
	Long:  `List, show, ignore, unignore, and remove releases tracked by release-radar.`,
}

var releasesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tracked releases",
	RunE: func(cmd *cobra.Command, args []string) error {
		state, _ := cmd.Flags().GetString("state")
		limit, _ := cmd.Flags().GetInt("limit")

		store, err := db.Open("")
		if err != nil {
			return err
		}
		defer store.Close()

		if state != "" {
			releases, err := store.ListReleasesByState(state, limit)
			if err != nil {
				return err
			}
			printReleasesList(releases)
			return nil
		}

		counts, err := store.CountReleasesByState()
		if err != nil {
			return err
		}
		fmt.Println("Tracked releases by state:")
		for _, s := range []string{"added", "ignored", "seen"} {
			fmt.Printf("  %-10s %d\n", s, counts[s])
		}
		return nil
	},
}

func printReleasesList(releases []db.Release) {
	if len(releases) == 0 {
		fmt.Println("No releases.")
		return
	}
	for _, r := range releases {
		fmt.Printf("  %s  %-30s  %-20s  %s\n", r.ReleaseDate, truncate(r.Name, 30), truncate(r.ArtistName, 20), r.AlbumID)
	}
}

var releasesShowCmd = &cobra.Command{
	Use:   "show <album_id>",
	Short: "Show details of a release",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return err
		}
		defer store.Close()

		rel, err := store.GetRelease(args[0])
		if err != nil {
			return err
		}
		if rel == nil {
			return fmt.Errorf("release not found: %s", args[0])
		}
		fmt.Printf("Album ID:    %s\n", rel.AlbumID)
		fmt.Printf("Name:        %s\n", rel.Name)
		fmt.Printf("Artist:      %s (%s)\n", rel.ArtistName, rel.CatalogArtistID)
		fmt.Printf("Release:     %s\n", rel.ReleaseDate)
		fmt.Printf("Tracks:      %d\n", rel.TrackCount)
		fmt.Printf("State:       %s\n", rel.State)
		fmt.Printf("First seen:  %s\n", rel.FirstSeenAt)
		if rel.AddedAt != "" {
			fmt.Printf("Added at:    %s\n", rel.AddedAt)
		}
		return nil
	},
}

var releasesIgnoreCmd = &cobra.Command{
	Use:   "ignore <album_id>",
	Short: "Mark a release as ignored (will never be suggested)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return updateState(args[0], "ignored", "Ignored")
	},
}

var releasesUnignoreCmd = &cobra.Command{
	Use:   "unignore <album_id>",
	Short: "Bring an ignored release back into consideration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return updateState(args[0], "seen", "Unignored")
	},
}

var releasesRemoveCmd = &cobra.Command{
	Use:   "remove <album_id>",
	Short: "Delete a release entirely",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return err
		}
		defer store.Close()
		if err := store.DeleteRelease(args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed release: %s\n", args[0])
		return nil
	},
}

func updateState(albumID, state, label string) error {
	store, err := db.Open("")
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.UpdateReleaseState(albumID, state); err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", label, albumID)
	return nil
}

func init() {
	releasesListCmd.Flags().String("state", "", "filter by state: seen, added, ignored, upcoming")
	releasesListCmd.Flags().Int("limit", 50, "max releases to show (0 for no limit)")

	releasesCmd.AddCommand(releasesListCmd)
	releasesCmd.AddCommand(releasesShowCmd)
	releasesCmd.AddCommand(releasesIgnoreCmd)
	releasesCmd.AddCommand(releasesUnignoreCmd)
	releasesCmd.AddCommand(releasesRemoveCmd)
}
