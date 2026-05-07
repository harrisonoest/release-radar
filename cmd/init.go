package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Pull artists from your Apple Music library",
	Long: `Fetches all artists from your Apple Music library and saves them
to a local cache. Sets the baseline timestamp for future release scans.`,
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

		p := mpb.New(mpb.WithWidth(60))
		bar := p.AddBar(0,
			mpb.BarFillerClearOnComplete(),
			mpb.PrependDecorators(decor.Name("Fetching artists", decor.WCSyncSpace)),
			mpb.AppendDecorators(decor.CountersNoUnit("pages: %d / %d")),
		)

		libraryArtists, err := client.GetAllLibraryArtists(ctx, 25, func(page, total int) {
			if total > 0 {
				bar.SetTotal(int64(total), false)
			}
			bar.SetCurrent(int64(page))
		})
		p.Wait()

		if err != nil {
			return fmt.Errorf("failed to fetch artists: %w", err)
		}

		if len(libraryArtists.Data) == 0 {
			fmt.Println("No artists found in your library.")
			return nil
		}

		if verbose {
			fmt.Printf("Fetched %d library artist entries.\n", len(libraryArtists.Data))
		}

		now := time.Now().UTC().Format(time.RFC3339)

		type libEntry struct {
			libraryID string
			catalogID string
			name      string
			href      string
		}
		entries := make([]libEntry, 0, len(libraryArtists.Data))
		standaloneCatalogs := make(map[string]bool)

		for _, a := range libraryArtists.Data {
			catalogID := ""
			if len(a.Relationships.Catalog.Data) > 0 {
				catalogID = a.Relationships.Catalog.Data[0].ID
			}
			if catalogID == "" {
				continue
			}

			entries = append(entries, libEntry{
				libraryID: a.ID,
				catalogID: catalogID,
				name:      a.Attributes.Name,
				href:      a.Href,
			})

			if !isCollaboration(a.Attributes.Name) {
				standaloneCatalogs[catalogID] = true
			}
		}

		artists := make([]db.Artist, 0, len(standaloneCatalogs))
		seenCatalog := make(map[string]int)

		for _, e := range entries {
			if !standaloneCatalogs[e.catalogID] {
				continue
			}

			artist := db.Artist{
				CatalogID: e.catalogID,
				LibraryID: e.libraryID,
				Name:      e.name,
				Href:      e.href,
				LastSeen:  now,
			}

			if idx, exists := seenCatalog[e.catalogID]; exists {
				existing := &artists[idx]
				if len(e.name) < len(existing.Name) {
					*existing = artist
				}
				continue
			}
			seenCatalog[e.catalogID] = len(artists)
			artists = append(artists, artist)
		}

		if verbose {
			skipped := len(libraryArtists.Data) - len(entries)
			collabDropped := len(entries) - len(artists)
			fmt.Printf("Dropped %d without catalog, %d collaborator-only, cached %d artists.\n",
				skipped, collabDropped, len(artists))
		}

		if err := store.ReplaceArtists(artists); err != nil {
			return fmt.Errorf("failed to save artists: %w", err)
		}

		if err := store.MarkScanned(0, 0); err != nil {
			return fmt.Errorf("failed to save scan state: %w", err)
		}

		fmt.Printf("Cached %d artists. Ready to scan!\n", len(artists))
		return nil
	},
}

var collabSeps = []string{" & ", ", ", " X "}

func isCollaboration(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, " feat. ") || strings.Contains(lower, " ft. ") {
		return true
	}
	for _, sep := range collabSeps {
		if strings.Contains(name, sep) {
			return true
		}
	}
	return false
}
