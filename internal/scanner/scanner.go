package scanner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
}

type APIClient interface {
	GetStorefront(ctx context.Context) (string, error)
	GetArtistAlbums(ctx context.Context, storefront, artistID string, since time.Time) (*api.ArtistAlbumsResult, error)
}

type Scanner struct {
	cfg         *config.Config
	client      api.ClientInterface
	store       *db.Store
	concurrency int
	storefront  string
	verbose     bool
	includeSeen bool
	checked     atomic.Int64
	errors      atomic.Int64
	permFails   atomic.Int64
	pruned      atomic.Int64
	zeroAlbums  atomic.Int64
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

func (s *Scanner) SetIncludeSeen(v bool) {
	s.includeSeen = v
}

func (s *Scanner) PermanentFailures() int64 {
	return s.permFails.Load()
}

func (s *Scanner) Pruned() int64 {
	return s.pruned.Load()
}

func (s *Scanner) ZeroAlbumArtists() int64 {
	return s.zeroAlbums.Load()
}

func (s *Scanner) Scan(ctx context.Context, artists []db.Artist, since time.Time) ([]Release, error) {
	storefront, err := s.client.GetStorefront(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get storefront: %w", err)
	}
	s.storefront = storefront

	sem := make(chan struct{}, s.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var releases []Release
	var errs []error
	seen := make(map[string]bool)

	var skipped map[string]bool
	if s.store != nil {
		skipped, err = s.store.SkippedReleaseIDs()
		if err != nil {
			return nil, fmt.Errorf("failed to load skipped releases: %w", err)
		}
	}

	total := int64(len(artists))
	if s.verbose {
		fmt.Printf("Scanning %d artists (concurrency: %d, storefront: %s, since: %s)…\n",
			total, s.concurrency, storefront, since.Format("2006-01-02"))
	}

	for _, artist := range artists {
		wg.Add(1)
		go func(a db.Artist) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			found, err := s.checkArtist(ctx, a, since)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", a.Name, err))
				mu.Unlock()
				s.errors.Add(1)
			}

			if skipped != nil {
				filtered := found[:0]
				for _, r := range found {
					if skipped[r.AlbumID] {
						s.pruned.Add(1)
						continue
					}
					filtered = append(filtered, r)
				}
				found = filtered
			}

			if len(found) > 0 {
				mu.Lock()
				for _, r := range found {
					if !seen[r.AlbumID] {
						seen[r.AlbumID] = true
						releases = append(releases, r)
					}
				}
				releaseCount := int64(len(releases))
				mu.Unlock()

				checked := s.checked.Add(1)

				if s.onProgress != nil {
					s.onProgress(checked, releaseCount, s.errors.Load())
				}
			} else {
				checked := s.checked.Add(1)

				if s.onProgress != nil {
					s.onProgress(checked, int64(len(releases)), s.errors.Load())
				}
			}
		}(artist)
	}

	wg.Wait()

	permFails := s.permFails.Load()
	if s.verbose && (s.errors.Load() > 0 || permFails > 0) {
		fmt.Printf("  Scan complete with %d errors, %d permanent failures (first: %v)\n", s.errors.Load(), permFails, errs[0])
	}

	nonPermErrs := int64(len(errs)) - permFails
	if nonPermErrs > 0 && len(releases) == 0 {
		return nil, fmt.Errorf("all artist queries failed: %v", errs[0])
	}

	return releases, nil
}

func (s *Scanner) checkArtist(ctx context.Context, a db.Artist, since time.Time) ([]Release, error) {
	if a.CatalogID == "" {
		return nil, fmt.Errorf("no catalog ID")
	}

	var result *api.ArtistAlbumsResult
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result, err = s.client.GetArtistAlbums(ctx, s.storefront, a.CatalogID, since)
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "rate limited") || strings.Contains(err.Error(), "429") {
			backoff := time.Duration(1<<attempt) * time.Second
			time.Sleep(backoff)
			continue
		}
		return nil, err
	}
	if err != nil {
		s.permFails.Add(1)
		return nil, err
	}

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
			})
		}
	}

	if len(result.Albums) == 0 {
		s.zeroAlbums.Add(1)
	}

	return releases, nil
}

func parseReleaseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)

	formats := []string{
		"2006-01-02",
		"2006-01",
		"2006",
	}
	for _, f := range formats {
		t, err := time.Parse(f, dateStr)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %s", dateStr)
}
