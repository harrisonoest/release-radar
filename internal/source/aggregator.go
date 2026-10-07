package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/harrisonoest/release-radar/pkg/api"
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
	OnSourceDone   func(sourceNum, totalSources int, name string, artistCount int)
}

// StoreWriter is the subset of *db.Store the Aggregator needs.
type StoreWriter interface {
	ReplaceArtists(artists []db.Artist) error
	UpsertArtistSources(sources []db.ArtistSource) error
}

// Aggregate runs all given Sources, dedupes the results, resolves names to
// catalog IDs (where needed), and writes everything to the store.
func (a *Aggregator) Aggregate(ctx context.Context, sources []Source) error {
	now := time.Now().UTC().Format(time.RFC3339)

	rawBySource := make(map[string][]RawArtist, len(sources))
	completed := 0
	var sourceErrs []error
	for _, src := range sources {
		raw, err := src.Fetch(ctx, a.Fetcher)
		if err != nil {
			a.log("source %s fetch failed: %v", src.DisplayName(), err)
			sourceErrs = append(sourceErrs, fmt.Errorf("source %s: %w", src.DisplayName(), err))
			if a.OnSourceDone != nil {
				a.OnSourceDone(completed, len(sources), src.DisplayName()+" (failed)", 0)
			}
			continue
		}
		rawBySource[src.Type()+"|"+src.ID()] = raw
		completed++
		if a.OnSourceDone != nil {
			a.OnSourceDone(completed, len(sources), src.DisplayName(), len(raw))
		}
	}

	nameToID, err := a.resolveNames(ctx, rawBySource)
	if err != nil {
		return fmt.Errorf("name resolution failed: %w", err)
	}
	// A failed source must abort the run: ReplaceArtists wipes the artists
	// table, so continuing would silently shrink the tracked set to whatever
	// the surviving sources produced.
	if len(sourceErrs) > 0 {
		return fmt.Errorf("aborting: %d source(s) failed (tracked set NOT updated): %w",
			len(sourceErrs), errors.Join(sourceErrs...))
	}

	// bestName tracks the display name per catalog ID. resolved marks IDs that
	// came from name→ID search resolution rather than a catalog-backed source
	// entry; only resolved IDs are subject to the collaboration filter, since
	// name heuristics on real catalog artists kill bands like
	// "Mumford & Sons" or "Earth, Wind & Fire".
	bestName := make(map[string]string)
	resolved := make(map[string]bool)
	for srcKey, raws := range rawBySource {
		for _, r := range raws {
			catalogID := r.CatalogID
			if catalogID == "" {
				if id, ok := nameToID[r.Name]; ok {
					catalogID = id
					resolved[catalogID] = true
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

	// Build artist rows directly, applying the collaboration filter inline.
	rows := make([]db.Artist, 0, len(bestName))
	for catalogID, name := range bestName {
		if resolved[catalogID] && isCollaboration(name) {
			continue
		}
		rows = append(rows, db.Artist{
			CatalogID: catalogID,
			Name:      name,
			LastSeen:  now,
		})
	}
	if err := a.Store.ReplaceArtists(rows); err != nil {
		return fmt.Errorf("ReplaceArtists failed: %w", err)
	}

	if a.SkipSourceRows {
		return nil
	}
	sourceRows := make([]db.ArtistSource, 0)
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
			sourceRows = append(sourceRows, db.ArtistSource{
				CatalogID:  catalogID,
				SourceType: sourceType,
				SourceID:   sourceID,
				AddedAt:    now,
			})
		}
	}
	if err := a.Store.UpsertArtistSources(sourceRows); err != nil {
		return fmt.Errorf("UpsertArtistSources failed: %w", err)
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
			if id, ok := a.searchArtistWithRetry(ctx, r.Name); ok {
				nameToID[r.Name] = id
			} else {
				a.log("could not resolve artist name via search: %q", r.Name)
			}
		}
	}
	return nameToID, nil
}

// searchArtistWithRetry queries the catalog search endpoint with exponential
// backoff on 429/5xx and accepts the top hit only when its normalized name
// matches the query — search with limit=1 otherwise happily returns an
// unrelated artist for collab-style or misspelled names.
func (a *Aggregator) searchArtistWithRetry(ctx context.Context, name string) (string, bool) {
	const attempts = 3
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", false
			case <-time.After(time.Duration(1<<uint(attempt-1)) * time.Second):
			}
		}
		result, err := a.Fetcher.SearchArtists(ctx, a.Storefront, name)
		if err != nil {
			a.log("search failed for %q (attempt %d): %v", name, attempt+1, err)
			// 4xx errors are deterministic — retrying only burns rate
			// limit. 429 is the exception: it recovers after backoff.
			var statusErr *api.StatusError
			if errors.As(err, &statusErr) &&
				statusErr.StatusCode >= 400 && statusErr.StatusCode < 500 &&
				statusErr.StatusCode != http.StatusTooManyRequests {
				return "", false
			}
			continue
		}
		if result == nil || len(result.Artists) == 0 {
			return "", false
		}
		hit := result.Artists[0]
		if !strings.EqualFold(strings.TrimSpace(hit.Name), strings.TrimSpace(name)) {
			a.log("search for %q returned non-matching artist %q; skipping", name, hit.Name)
			return "", false
		}
		return hit.ID, true
	}
	return "", false
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
