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

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Pull artists from your Apple Music library",
	Long: `Fetches all artists from your Apple Music library and saves them
to a local cache. Sets the baseline timestamp for future release scans.

Use --all to additionally pull artists from library albums, library songs,
and liked songs.`,
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
}
