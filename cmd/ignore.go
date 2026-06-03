package cmd

import (
	"fmt"
	"regexp"

	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
)

var ignoreCmd = &cobra.Command{
	Use:   "ignore",
	Short: "Manage ignored artists",
	Long:  `Add or remove artists from the ignore list. Ignored artists are excluded from scans.`,
}

var ignoreAddCmd = &cobra.Command{
	Use:   "add <catalog_id> <name>",
	Short: "Ignore an artist by catalog ID",
	Long:  `Add an artist to the ignore list so they are excluded from future scans. Use the catalog ID from Apple Music.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer store.Close()

		catalogID := args[0]
		name := args[1]

		if err := validateCatalogID(catalogID); err != nil {
			return err
		}

		if err := store.IgnoreArtist(catalogID, name); err != nil {
			return fmt.Errorf("failed to ignore artist: %w", err)
		}

		fmt.Printf("Ignored artist: %s (%s)\n", name, catalogID)
		return nil
	},
}

var ignoreRemoveCmd = &cobra.Command{
	Use:   "remove <catalog_id>",
	Short: "Remove an artist from the ignore list",
	Long:  `Remove an artist from the ignore list so they are included in future scans again.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer store.Close()

		catalogID := args[0]

		if err := validateCatalogID(catalogID); err != nil {
			return err
		}

		ignored, err := store.IsArtistIgnored(catalogID)
		if err != nil {
			return fmt.Errorf("failed to check ignored status: %w", err)
		}
		if !ignored {
			fmt.Printf("Artist %s is not in the ignore list.\n", catalogID)
			return nil
		}

		if err := store.UnignoreArtist(catalogID); err != nil {
			return fmt.Errorf("failed to unignore artist: %w", err)
		}

		fmt.Printf("Removed %s from ignore list.\n", catalogID)
		return nil
	},
}

var ignoreListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all ignored artists",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer store.Close()

		ignored, err := store.ListIgnoredArtists()
		if err != nil {
			return fmt.Errorf("failed to list ignored artists: %w", err)
		}

		if len(ignored) == 0 {
			fmt.Println("No ignored artists.")
			return nil
		}

		fmt.Printf("Ignored artists (%d):\n", len(ignored))
		for _, a := range ignored {
			fmt.Printf("  %s — %s\n", a.CatalogID, a.Name)
		}
		return nil
	},
}

func validateCatalogID(catalogID string) error {
	if catalogID == "" {
		return fmt.Errorf("catalog_id cannot be empty")
	}
	// Catalog IDs are numeric strings (e.g., "1068300376")
	// Library IDs are prefixed (e.g., "r.xUjxaAb")
	validFormat := regexp.MustCompile(`^[0-9]+$|^[a-z]\.[a-zA-Z0-9]+$`)
	if !validFormat.MatchString(catalogID) {
		return fmt.Errorf("invalid catalog_id format: %s (expected numeric ID or prefixed library ID like 'r.xUjxaAb')", catalogID)
	}
	return nil
}

func init() {
	ignoreCmd.AddCommand(ignoreAddCmd)
	ignoreCmd.AddCommand(ignoreRemoveCmd)
	ignoreCmd.AddCommand(ignoreListCmd)
}
