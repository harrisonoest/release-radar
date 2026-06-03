package source

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/harrisonoest/release-radar/pkg/db"
)

// Aggregator combines results from multiple Sources, resolves names to catalog
// IDs, applies dedup rules, and writes to the store.
type Aggregator struct {
	Store          StoreWriter
	Fetcher        Fetcher
	Storefront     string
	Logger         func(format string, args ...interface{})
	SkipSourceRows bool
}

// StoreWriter is the subset of *db.Store the Aggregator needs.
type StoreWriter interface {
	ReplaceArtists(artists []db.Artist) error
	UpsertArtistSource(src db.ArtistSource) error
}

// Aggregate runs all given Sources, dedupes the results, resolves names to
// catalog IDs (where needed), and writes everything to the store.
func (a *Aggregator) Aggregate(ctx context.Context, sources []Source) error {
	now := time.Now().UTC().Format(time.RFC3339)

	rawBySource := make(map[string][]RawArtist, len(sources))
	for _, src := range sources {
		raw, err := src.Fetch(ctx, a.Fetcher)
		if err != nil {
			a.log("source %s fetch failed: %v", src.DisplayName(), err)
			continue
		}
		rawBySource[src.Type()+"|"+src.ID()] = raw
	}

	nameToID, err := a.resolveNames(ctx, rawBySource)
	if err != nil {
		return fmt.Errorf("name resolution failed: %w", err)
	}

	bestName := make(map[string]string)
	for srcKey, raws := range rawBySource {
		for _, r := range raws {
			catalogID := r.CatalogID
			if catalogID == "" {
				if id, ok := nameToID[r.Name]; ok {
					catalogID = id
				} else {
					a.log("could not resolve name: %q (source %s)", r.Name, srcKey)
					continue
				}
			}
			if existing, ok := bestName[catalogID]; !ok || len(r.Name) < len(existing) {
				bestName[catalogID] = r.Name
			}
		}
	}

	var finalCatalogIDs []string
	for catalogID, name := range bestName {
		if isCollaboration(name) {
			continue
		}
		finalCatalogIDs = append(finalCatalogIDs, catalogID)
	}

	var rows []db.Artist
	for _, id := range finalCatalogIDs {
		rows = append(rows, db.Artist{
			CatalogID: id,
			Name:      bestName[id],
			LastSeen:  now,
		})
	}
	if err := a.Store.ReplaceArtists(rows); err != nil {
		return fmt.Errorf("ReplaceArtists failed: %w", err)
	}

	if a.SkipSourceRows {
		return nil
	}
	for srcKey, raws := range rawBySource {
		parts := strings.SplitN(srcKey, "|", 2)
		sourceType, sourceID := parts[0], parts[1]
		for _, r := range raws {
			catalogID := r.CatalogID
			if catalogID == "" {
				if id, ok := nameToID[r.Name]; ok {
					catalogID = id
				} else {
					continue
				}
			}
			_ = a.Store.UpsertArtistSource(db.ArtistSource{
				CatalogID:  catalogID,
				SourceType: sourceType,
				SourceID:   sourceID,
				AddedAt:    now,
			})
		}
	}

	return nil
}

func (a *Aggregator) resolveNames(ctx context.Context, rawBySource map[string][]RawArtist) (map[string]string, error) {
	nameToID := make(map[string]string)
	for _, raws := range rawBySource {
		for _, r := range raws {
			if r.CatalogID != "" {
				continue
			}
			if _, seen := nameToID[r.Name]; seen {
				continue
			}
			result, err := a.Fetcher.SearchArtists(ctx, a.Storefront, r.Name)
			if err != nil {
				a.log("search failed for %q: %v", r.Name, err)
				continue
			}
			if result == nil || len(result.Artists) == 0 {
				continue
			}
			nameToID[r.Name] = result.Artists[0].ID
		}
	}
	return nameToID, nil
}

func (a *Aggregator) log(format string, args ...interface{}) {
	if a.Logger != nil {
		a.Logger(format, args...)
	}
}

// isCollaboration mirrors cmd/init.go's logic.
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
