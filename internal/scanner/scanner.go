package scanner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
)

type Release struct {
	ArtistID    string
	ArtistName  string
	AlbumID     string
	AlbumName   string
	ReleaseDate string
	TrackCount  int
	// Upcoming is true when the release date is in the future (announced but
	// not yet out, including placeholder dates). Upcoming releases are
	// recorded but not added to the playlist.
	Upcoming bool
}

type Scanner struct {
	cfg         *config.Config
	client      api.ClientInterface
	store       *db.Store
	concurrency int
	storefront  string
	verbose     bool

	// mu guards the counters and failedNames below; checkArtist runs on
	// worker goroutines and aggregates through it.
	mu          sync.Mutex
	checked     int64
	errors      int64
	permFails   int64
	pruned      int64
	zeroAlbums  int64
	failedNames []string
	onProgress  func(checked, found, errors int64)
}

func New(cfg *config.Config, client api.ClientInterface, store *db.Store, verbose bool) *Scanner {
	concurrency := cfg.Scan.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}
	return &Scanner{
		cfg:         cfg,
		client:      client,
		store:       store,
		concurrency: concurrency,
		verbose:     verbose,
	}
}

func (s *Scanner) SetProgressCallback(fn func(checked, found, errors int64)) {
	s.onProgress = fn
}

func (s *Scanner) PermanentFailures() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.permFails
}

func (s *Scanner) Pruned() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pruned
}

func (s *Scanner) ZeroAlbumArtists() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zeroAlbums
}

// FailedArtists returns the names of artists whose queries failed this scan.
// Their releases in this window are unknown, so callers must not advance the
// scan watermark past them.
func (s *Scanner) FailedArtists() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.failedNames))
	copy(out, s.failedNames)
	return out
}

// scanResult is one artist's outcome, sent from a worker to the aggregating
// loop in Scan.
type scanResult struct {
	artist db.Artist
	found  []Release
	err    error
}

func (s *Scanner) Scan(ctx context.Context, artists []db.Artist, since time.Time) ([]Release, error) {
	storefront, err := s.client.GetStorefront(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get storefront: %w", err)
	}
	s.storefront = storefront

	skipped := make(map[string]struct{})
	if s.store != nil {
		ids, err := s.store.SkippedReleaseIDs()
		if err != nil {
			return nil, fmt.Errorf("failed to load skipped releases: %w", err)
		}
		for id := range ids {
			skipped[id] = struct{}{}
		}
	}

	total := int64(len(artists))
	if s.verbose {
		fmt.Printf("Scanning %d artists (concurrency: %d, storefront: %s, since: %s)…\n",
			total, s.concurrency, storefront, since.Format("2006-01-02"))
	}

	// Fixed worker pool: s.concurrency goroutines range over the artist
	// channel and ship results back; Scan aggregates single-threaded below.
	artistsCh := make(chan db.Artist)
	results := make(chan scanResult)
	var workers sync.WaitGroup
	workers.Add(s.concurrency)
	for i := 0; i < s.concurrency; i++ {
		go func() {
			defer workers.Done()
			for a := range artistsCh {
				found, err := s.checkArtist(ctx, a, since)
				if err == nil {
					found = s.pruneSkipped(found, skipped)
				}
				results <- scanResult{artist: a, found: found, err: err}
			}
		}()
	}

	go func() {
		defer close(artistsCh)
		for _, a := range artists {
			artistsCh <- a
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	var releases []Release
	seen := make(map[string]struct{})
	var errs []error

	for res := range results {
		if res.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.artist.Name, res.err))
		} else {
			for _, r := range res.found {
				if _, dup := seen[r.AlbumID]; !dup {
					seen[r.AlbumID] = struct{}{}
					releases = append(releases, r)
				}
			}
		}
		s.mu.Lock()
		s.checked++
		if res.err != nil {
			s.errors++
			s.failedNames = append(s.failedNames, res.artist.Name)
		}
		checked, scanErrs := s.checked, s.errors
		s.mu.Unlock()
		if s.onProgress != nil {
			s.onProgress(checked, int64(len(releases)), scanErrs)
		}
	}

	s.mu.Lock()
	permFails, scanErrs := s.permFails, s.errors
	s.mu.Unlock()
	if s.verbose && (scanErrs > 0 || permFails > 0) {
		fmt.Printf("  Scan complete with %d errors, %d permanent failures (first: %v)\n", scanErrs, permFails, errs[0])
	}

	nonPermErrs := int64(len(errs)) - permFails
	if nonPermErrs > 0 && len(releases) == 0 {
		return nil, fmt.Errorf("all artist queries failed: %v", errs[0])
	}

	return releases, nil
}

// pruneSkipped drops releases whose album IDs are already skipped/ignored in
// the store, counting each dropped release.
func (s *Scanner) pruneSkipped(found []Release, skipped map[string]struct{}) []Release {
	if len(skipped) == 0 {
		return found
	}
	filtered := found[:0]
	pruned := 0
	for _, r := range found {
		if _, skip := skipped[r.AlbumID]; skip {
			pruned++
			continue
		}
		filtered = append(filtered, r)
	}
	if pruned > 0 {
		s.mu.Lock()
		s.pruned += int64(pruned)
		s.mu.Unlock()
	}
	return filtered
}

func (s *Scanner) checkArtist(ctx context.Context, a db.Artist, since time.Time) ([]Release, error) {
	if a.CatalogID == "" {
		return nil, fmt.Errorf("no catalog ID")
	}

	const maxAttempts = 5
	var result *api.ArtistAlbumsResult
	var err error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result, err = s.client.GetArtistAlbums(ctx, s.storefront, a.CatalogID, since)
		if err == nil {
			break
		}
		if errors.Is(err, api.ErrRateLimited) {
			// Exponential backoff, but never sleep after the final
			// attempt: each sleep must be followed by a retry.
			if attempt < maxAttempts-1 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(1<<attempt) * time.Second):
				}
			}
			continue
		}
		return nil, err
	}
	if err != nil {
		s.mu.Lock()
		s.permFails++
		s.mu.Unlock()
		return nil, err
	}

	now := time.Now()
	var releases []Release
	for _, album := range result.Albums {
		if album.Attributes.ReleaseDate == "" {
			continue
		}

		releaseTime, err := parseReleaseDate(album.Attributes.ReleaseDate)
		if err != nil {
			continue
		}

		if releaseTime.After(since) {
			releases = append(releases, Release{
				ArtistID:    a.CatalogID,
				ArtistName:  a.Name,
				AlbumID:     album.Id,
				AlbumName:   album.Attributes.Name,
				ReleaseDate: album.Attributes.ReleaseDate,
				TrackCount:  int(album.Attributes.TrackCount),
				Upcoming:    releaseTime.After(now),
			})
		}
	}

	if len(result.Albums) == 0 {
		s.mu.Lock()
		s.zeroAlbums++
		s.mu.Unlock()
	}

	return releases, nil
}

// releaseDateFormats are the date granularities the API may return for a
// release: full date, month, or year only.
var releaseDateFormats = []string{
	"2006-01-02",
	"2006-01",
	"2006",
}

func parseReleaseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	for _, f := range releaseDateFormats {
		t, err := time.Parse(f, dateStr)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %s", dateStr)
}
