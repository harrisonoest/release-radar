package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/internal/source"
	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
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

var sourcesAddCmd = &cobra.Command{
	Use:   "add <type> [id]",
	Short: "Add a new artist source",
	Long: `Add a new artist source. Type is one of: library_artists, library_albums, library_songs, liked_songs, playlist.
For 'playlist', the id is the playlist's Apple Music ID (or use 'name:NAME' to resolve by name).`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceType := args[0]
		sourceID := ""
		if len(args) > 1 {
			sourceID = args[1]
		}

		valid := map[string]bool{
			"library_artists": true, "library_albums": true,
			"library_songs": true, "liked_songs": true, "playlist": true,
		}
		if !valid[sourceType] {
			return fmt.Errorf("invalid source type: %s (valid: library_artists, library_albums, library_songs, liked_songs, playlist)", sourceType)
		}
		if sourceType == "playlist" && sourceID == "" {
			return fmt.Errorf("playlist source requires an id (or 'name:PLAYLIST NAME')")
		}

		if sourceType == "playlist" && len(sourceID) > 5 && sourceID[:5] == "name:" {
			playlistName := sourceID[5:]
			client, err := apiClientForSource()
			if err != nil {
				return err
			}
			playlists, err := client.GetAllLibraryPlaylists(cmd.Context())
			if err != nil {
				return err
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

		src, err := source.Build(sourceType, sourceID)
		if err != nil {
			return err
		}

		store, err := db.Open("")
		if err != nil {
			return err
		}
		defer store.Close()
		client, err := apiClientForSource()
		if err != nil {
			return err
		}
		storefront, err := client.GetStorefront(cmd.Context())
		if err != nil {
			return err
		}

		agg := &source.Aggregator{
			Store:      store,
			Fetcher:    client,
			Storefront: storefront,
			Logger:     func(f string, args ...interface{}) { fmt.Fprintf(os.Stderr, f+"\n", args...) },
		}
		if err := agg.Aggregate(cmd.Context(), []source.Source{src}); err != nil {
			return err
		}

		fmt.Printf("Added source: %s\n", src.DisplayName())
		return nil
	},
}

var sourcesRemoveCmd = &cobra.Command{
	Use:   "remove <type> [id]",
	Short: "Remove a source (artists from other sources remain)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceType := args[0]
		sourceID := ""
		if len(args) > 1 {
			sourceID = args[1]
		}

		store, err := db.Open("")
		if err != nil {
			return err
		}
		defer store.Close()

		if _, err := store.DeleteArtistSource(sourceType, sourceID); err != nil {
			return err
		}
		fmt.Printf("Removed source: %s (id=%s)\n", sourceType, sourceID)
		return nil
	},
}

var sourcesScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Re-fetch all enabled sources and update the artist set",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}
		store, err := db.Open("")
		if err != nil {
			return err
		}
		defer store.Close()
		authenticator, err := auth.NewAuthenticatorWithStore(cfg, store)
		if err != nil {
			return err
		}
		client, err := api.NewClient(cfg, authenticator)
		if err != nil {
			return err
		}
		storefront, err := client.GetStorefront(cmd.Context())
		if err != nil {
			return err
		}
		sources := []source.Source{
			&source.LibraryArtists{},
			&source.LibraryAlbums{},
			&source.LibrarySongs{},
			&source.LikedSongs{},
		}
		playlistIDs, _ := store.PlaylistSourceIDs()
		for _, pid := range playlistIDs {
			ps, _ := source.Build("playlist", pid)
			if ps != nil {
				sources = append(sources, ps)
			}
		}
		agg := &source.Aggregator{
			Store:      store,
			Fetcher:    client,
			Storefront: storefront,
			Logger:     func(f string, args ...interface{}) { fmt.Fprintf(os.Stderr, f+"\n", args...) },
		}
		if err := agg.Aggregate(cmd.Context(), sources); err != nil {
			return err
		}
		fmt.Println("Sources refreshed.")
		return nil
	},
}

func apiClientForSource() (*api.Client, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, err
	}
	store, err := db.Open("")
	if err != nil {
		return nil, err
	}
	defer store.Close()
	authenticator, err := auth.NewAuthenticatorWithStore(cfg, store)
	if err != nil {
		return nil, err
	}
	return api.NewClient(cfg, authenticator)
}

func init() {
	sourcesCmd.AddCommand(sourcesListCmd)
	sourcesCmd.AddCommand(sourcesAddCmd)
	sourcesCmd.AddCommand(sourcesRemoveCmd)
	sourcesCmd.AddCommand(sourcesScanCmd)
}
