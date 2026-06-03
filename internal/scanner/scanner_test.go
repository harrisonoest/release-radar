package scanner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applemusic "github.com/minchao/go-apple-music"

	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
)

type mockAPIClient struct {
	getStorefrontFn   func(ctx context.Context) (string, error)
	getArtistAlbumsFn func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error)
}

func (m *mockAPIClient) GetStorefront(ctx context.Context) (string, error) {
	return m.getStorefrontFn(ctx)
}

func (m *mockAPIClient) GetArtistAlbums(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
	return m.getArtistAlbumsFn(ctx, storefront, artistID, limit)
}

func newMockAPIClient() *mockAPIClient {
	return &mockAPIClient{}
}

func TestNew(t *testing.T) {
	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency: 5,
		},
	}
	client := newMockAPIClient()

	s := New(cfg, client, nil, false)

	if s.cfg != cfg {
		t.Errorf("config = %v, want %v", s.cfg, cfg)
	}
	if s.client != client {
		t.Errorf("client = %v, want %v", s.client, client)
	}
	if s.concurrency != 5 {
		t.Errorf("concurrency = %d, want 5", s.concurrency)
	}
	if s.verbose != false {
		t.Errorf("verbose = %v, want false", s.verbose)
	}
}

func TestNew_DefaultConcurrency(t *testing.T) {
	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency: 0,
		},
	}
	client := newMockAPIClient()

	s := New(cfg, client, nil, false)

	if s.concurrency != 10 {
		t.Errorf("concurrency = %d, want 10 (default)", s.concurrency)
	}
}

func TestSetProgressCallback(t *testing.T) {
	cfg := &config.Config{}
	client := newMockAPIClient()
	s := New(cfg, client, nil, false)

	var called int64
	s.SetProgressCallback(func(checked, found, errors int64) {
		atomic.AddInt64(&called, 1)
	})

	if s.onProgress == nil {
		t.Fatal("expected progress callback to be set")
	}

	s.onProgress(1, 2, 0)

	if atomic.LoadInt64(&called) != 1 {
		t.Errorf("callback called %d times, want 1", called)
	}
}

func TestParseReleaseDate(t *testing.T) {
	tests := []struct {
		name     string
		dateStr  string
		wantErr  bool
		expected string
	}{
		{
			name:     "full date",
			dateStr:  "2023-01-15",
			wantErr:  false,
			expected: "2023-01-15",
		},
		{
			name:     "year-month",
			dateStr:  "2023-01",
			wantErr:  false,
			expected: "2023-01-01",
		},
		{
			name:     "year only",
			dateStr:  "2023",
			wantErr:  false,
			expected: "2023-01-01",
		},
		{
			name:    "invalid date",
			dateStr: "not-a-date",
			wantErr: true,
		},
		{
			name:     "with whitespace",
			dateStr:  "  2023-01-15  ",
			wantErr:  false,
			expected: "2023-01-15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReleaseDate(tt.dateStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseReleaseDate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Format("2006-01-02") != tt.expected {
					t.Errorf("parseReleaseDate() = %v, want %v", got.Format("2006-01-02"), tt.expected)
				}
			}
		})
	}
}

func TestScanner_Scan(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		storefront    string
		storefrontErr error
		artists       []db.Artist
		albumResult   *api.ArtistAlbumsResult
		albumErr      error
		wantReleases  int
		wantErr       bool
	}{
		{
			name:         "empty artist list",
			storefront:   "us",
			artists:      []db.Artist{},
			albumResult:  &api.ArtistAlbumsResult{},
			wantReleases: 0,
			wantErr:      false,
		},
		{
			name:         "artist with no albums",
			storefront:   "us",
			artists:      []db.Artist{{CatalogID: "123", Name: "Artist"}},
			albumResult:  &api.ArtistAlbumsResult{},
			wantReleases: 0,
			wantErr:      false,
		},
		{
			name:       "artist with old album (before since)",
			storefront: "us",
			artists:    []db.Artist{{CatalogID: "123", Name: "Artist"}},
			albumResult: &api.ArtistAlbumsResult{
				Albums: []applemusic.Album{
					{
						Id: "album1",
						Attributes: applemusic.AlbumAttributes{
							Name:        "Old Album",
							ReleaseDate: "2022-01-01",
							TrackCount:  10,
						},
					},
				},
			},
			wantReleases: 0,
			wantErr:      false,
		},
		{
			name:       "artist with new album (after since)",
			storefront: "us",
			artists:    []db.Artist{{CatalogID: "123", Name: "Artist"}},
			albumResult: &api.ArtistAlbumsResult{
				Albums: []applemusic.Album{
					{
						Id: "album1",
						Attributes: applemusic.AlbumAttributes{
							Name:        "New Album",
							ReleaseDate: "2023-06-15",
							TrackCount:  12,
						},
					},
				},
			},
			wantReleases: 1,
			wantErr:      false,
		},
		{
			name:          "storefront error",
			storefrontErr: errors.New("failed to get storefront"),
			artists:       []db.Artist{{CatalogID: "123", Name: "Artist"}},
			wantReleases:  0,
			wantErr:       true,
		},
		{
			name:         "artist with empty catalog ID",
			storefront:   "us",
			artists:      []db.Artist{{CatalogID: "", Name: "Artist"}},
			albumResult:  &api.ArtistAlbumsResult{},
			wantReleases: 0,
			wantErr:      true,
		},
		{
			name:       "multiple artists with albums",
			storefront: "us",
			artists: []db.Artist{
				{CatalogID: "1", Name: "Artist1"},
				{CatalogID: "2", Name: "Artist2"},
			},
			albumResult: &api.ArtistAlbumsResult{
				Albums: []applemusic.Album{
					{
						Id: "album1",
						Attributes: applemusic.AlbumAttributes{
							Name:        "Album1",
							ReleaseDate: "2023-06-15",
							TrackCount:  10,
						},
					},
				},
			},
			wantReleases: 1,
			wantErr:      false,
		},
		{
			name:       "album with missing release date",
			storefront: "us",
			artists:    []db.Artist{{CatalogID: "123", Name: "Artist"}},
			albumResult: &api.ArtistAlbumsResult{
				Albums: []applemusic.Album{
					{
						Id: "album1",
						Attributes: applemusic.AlbumAttributes{
							Name:        "No Date Album",
							ReleaseDate: "",
							TrackCount:  5,
						},
					},
					{
						Id: "album2",
						Attributes: applemusic.AlbumAttributes{
							Name:        "Date Album",
							ReleaseDate: "2023-06-15",
							TrackCount:  10,
						},
					},
				},
			},
			wantReleases: 1,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockAPIClient()
			client.getStorefrontFn = func(ctx context.Context) (string, error) {
				if tt.storefrontErr != nil {
					return "", tt.storefrontErr
				}
				return tt.storefront, nil
			}
			client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
				if tt.albumErr != nil {
					return nil, tt.albumErr
				}
				return tt.albumResult, nil
			}

			cfg := &config.Config{
				Scan: config.ScanConfig{
					MaxAlbumsPerArtist: 10,
				},
			}

			s := New(cfg, client, nil, false)
			releases, err := s.Scan(context.Background(), tt.artists, since)

			if (err != nil) != tt.wantErr {
				t.Errorf("Scan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(releases) != tt.wantReleases {
				t.Errorf("Scan() releases = %d, want %d", len(releases), tt.wantReleases)
			}

			if tt.wantReleases > 0 && len(releases) > 0 {
				r := releases[0]
				if r.ReleaseDate == "" {
					t.Error("release date should not be empty")
				}
			}
		})
	}
}

func TestScanner_Scan_Deduplication(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	album := applemusic.Album{
		Id: "same-album-id",
		Attributes: applemusic.AlbumAttributes{
			Name:        "Same Album",
			ReleaseDate: "2023-06-15",
			TrackCount:  10,
		},
	}

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{album},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artists := []db.Artist{
		{CatalogID: "1", Name: "Artist1"},
		{CatalogID: "2", Name: "Artist2"},
		{CatalogID: "3", Name: "Artist3"},
	}

	releases, err := s.Scan(context.Background(), artists, since)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(releases) != 1 {
		t.Errorf("releases = %d, want 1 (deduplicated)", len(releases))
	}

	if releases[0].AlbumID != "same-album-id" {
		t.Errorf("album ID = %s, want same-album-id", releases[0].AlbumID)
	}
}

func TestScanner_Scan_Concurrency(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	var checkCount int64
	var mu sync.Mutex
	checkedArtists := make(map[string]bool)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		mu.Lock()
		checkedArtists[artistID] = true
		atomic.AddInt64(&checkCount, 1)
		mu.Unlock()

		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: "album-" + artistID,
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album " + artistID,
						ReleaseDate: "2023-06-15",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        5,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artists := make([]db.Artist, 10)
	for i := 0; i < 10; i++ {
		artists[i] = db.Artist{CatalogID: fmt.Sprintf("%d", i+1), Name: fmt.Sprintf("Artist%d", i+1)}
	}

	releases, err := s.Scan(context.Background(), artists, since)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(releases) != 10 {
		t.Errorf("releases = %d, want 10", len(releases))
	}

	mu.Lock()
	if len(checkedArtists) != 10 {
		t.Errorf("checked artists = %d, want 10", len(checkedArtists))
	}
	mu.Unlock()
}

func TestScanner_Scan_ContextCancellation(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	ctx, cancel := context.WithCancel(context.Background())

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
			return &api.ArtistAlbumsResult{
				Albums: []applemusic.Album{
					{
						Id: "album1",
						Attributes: applemusic.AlbumAttributes{
							Name:        "Album",
							ReleaseDate: "2023-06-15",
							TrackCount:  10,
						},
					},
				},
			}, nil
		}
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        1,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artists := []db.Artist{{CatalogID: "1", Name: "Artist"}}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	releases, err := s.Scan(ctx, artists, since)

	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
	if len(releases) != 0 {
		t.Errorf("releases = %d, want 0 on cancellation", len(releases))
	}
}

func TestScanner_Scan_ProgressCallback(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	var progressCalls []struct {
		checked int64
		found   int64
		errors  int64
	}

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		albumID := "album1"
		if artistID == "2" {
			albumID = "album2"
		}
		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: albumID,
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album " + artistID,
						ReleaseDate: "2023-06-15",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        2,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	var progressMu sync.Mutex
	s.SetProgressCallback(func(checked, found, errors int64) {
		progressMu.Lock()
		defer progressMu.Unlock()
		progressCalls = append(progressCalls, struct {
			checked int64
			found   int64
			errors  int64
		}{checked, found, errors})
	})

	artists := []db.Artist{
		{CatalogID: "1", Name: "Artist1"},
		{CatalogID: "2", Name: "Artist2"},
	}

	releases, err := s.Scan(context.Background(), artists, since)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(releases) != 2 {
		t.Errorf("releases = %d, want 2", len(releases))
	}

	if len(progressCalls) < 2 {
		t.Errorf("progress calls = %d, want at least 2", len(progressCalls))
	}

	progressMu.Lock()
	defer progressMu.Unlock()
	if progressCalls[0].checked != 1 && progressCalls[0].checked != 2 {
		t.Errorf("first progress checked = %d, want 1 or 2", progressCalls[0].checked)
	}
	if progressCalls[len(progressCalls)-1].checked != 2 {
		t.Errorf("last progress checked = %d, want 2", progressCalls[len(progressCalls)-1].checked)
	}
}

func TestScanner_Scan_ErrorHandling(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		if artistID == "error" {
			return nil, errors.New("API error")
		}
		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: "album1",
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album",
						ReleaseDate: "2023-06-15",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        2,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, true)

	artists := []db.Artist{
		{CatalogID: "1", Name: "WorkingArtist"},
		{CatalogID: "error", Name: "ErrorArtist"},
	}

	releases, err := s.Scan(context.Background(), artists, since)

	if err != nil {
		t.Errorf("Scan() error = %v, want nil (partial success)", err)
	}

	if len(releases) != 1 {
		t.Errorf("releases = %d, want 1 (only successful artist)", len(releases))
	}
}

func TestScanner_checkArtist(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		artist       db.Artist
		storefront   string
		albumResult  *api.ArtistAlbumsResult
		albumErr     error
		wantReleases int
		wantErr      bool
	}{
		{
			name:         "artist with no catalog ID",
			artist:       db.Artist{CatalogID: "", Name: "Artist"},
			storefront:   "us",
			wantReleases: 0,
			wantErr:      true,
		},
		{
			name:       "artist with albums after since date",
			artist:     db.Artist{CatalogID: "123", Name: "Artist"},
			storefront: "us",
			albumResult: &api.ArtistAlbumsResult{
				Albums: []applemusic.Album{
					{
						Id: "album1",
						Attributes: applemusic.AlbumAttributes{
							Name:        "New Album",
							ReleaseDate: "2023-06-15",
							TrackCount:  10,
						},
					},
				},
			},
			wantReleases: 1,
			wantErr:      false,
		},
		{
			name:       "artist with albums before since date",
			artist:     db.Artist{CatalogID: "123", Name: "Artist"},
			storefront: "us",
			albumResult: &api.ArtistAlbumsResult{
				Albums: []applemusic.Album{
					{
						Id: "album1",
						Attributes: applemusic.AlbumAttributes{
							Name:        "Old Album",
							ReleaseDate: "2022-06-15",
							TrackCount:  10,
						},
					},
				},
			},
			wantReleases: 0,
			wantErr:      false,
		},
		{
			name:         "artist with no albums (404 case)",
			artist:       db.Artist{CatalogID: "123", Name: "Artist"},
			storefront:   "us",
			albumResult:  &api.ArtistAlbumsResult{},
			wantReleases: 0,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockAPIClient()
			client.getStorefrontFn = func(ctx context.Context) (string, error) {
				return tt.storefront, nil
			}
			client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
				return tt.albumResult, tt.albumErr
			}

			cfg := &config.Config{
				Scan: config.ScanConfig{
					MaxAlbumsPerArtist: 10,
				},
			}

			s := New(cfg, client, nil, false)
			releases, err := s.checkArtist(context.Background(), tt.artist, since, 10)

			if (err != nil) != tt.wantErr {
				t.Errorf("checkArtist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(releases) != tt.wantReleases {
				t.Errorf("checkArtist() releases = %d, want %d", len(releases), tt.wantReleases)
			}
		})
	}
}

func TestScanner_checkArtist_RateLimitRetry(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	var attemptCount int
	var mu sync.Mutex

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		mu.Lock()
		attemptCount++
		mu.Unlock()

		if attemptCount < 3 {
			return nil, fmt.Errorf("rate limited (429)")
		}

		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: "album1",
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album",
						ReleaseDate: "2023-06-15",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artist := db.Artist{CatalogID: "123", Name: "Artist"}

	releases, err := s.checkArtist(context.Background(), artist, since, 10)

	if err != nil {
		t.Fatalf("checkArtist() error = %v", err)
	}

	if len(releases) != 1 {
		t.Errorf("releases = %d, want 1", len(releases))
	}

	mu.Lock()
	if attemptCount != 3 {
		t.Errorf("attemptCount = %d, want 3 (2 retries)", attemptCount)
	}
	mu.Unlock()
}

func TestScanner_checkArtist_RateLimitExhausted(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		return nil, fmt.Errorf("rate limited (429)")
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artist := db.Artist{CatalogID: "123", Name: "Artist"}

	releases, err := s.checkArtist(context.Background(), artist, since, 10)

	if err == nil {
		t.Error("expected error after retries exhausted")
	}

	if len(releases) != 0 {
		t.Errorf("releases = %d, want 0", len(releases))
	}
}

func TestScanner_checkArtist_ContextCancellation(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	ctx, cancel := context.WithCancel(context.Background())

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return &api.ArtistAlbumsResult{}, nil
		}
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artist := db.Artist{CatalogID: "123", Name: "Artist"}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	releases, err := s.checkArtist(ctx, artist, since, 10)

	if err == nil {
		t.Error("expected context cancellation error")
	}

	if len(releases) != 0 {
		t.Errorf("releases = %d, want 0", len(releases))
	}
}

func TestScanner_checkArtist_ParseError(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: "album1",
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album",
						ReleaseDate: "invalid-date",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artist := db.Artist{CatalogID: "123", Name: "Artist"}

	releases, err := s.checkArtist(context.Background(), artist, since, 10)

	if err != nil {
		t.Fatalf("checkArtist() error = %v", err)
	}

	if len(releases) != 0 {
		t.Errorf("releases = %d, want 0 (invalid date skipped)", len(releases))
	}
}

func TestScanner_checkArtist_AlbumLimit(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		if limit != 5 {
			t.Errorf("expected limit 5, got %d", limit)
		}
		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: "album1",
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album1",
						ReleaseDate: "2023-06-15",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			MaxAlbumsPerArtist: 5,
		},
	}

	s := New(cfg, client, nil, false)

	artist := db.Artist{CatalogID: "123", Name: "Artist"}

	releases, err := s.checkArtist(context.Background(), artist, since, 5)

	if err != nil {
		t.Fatalf("checkArtist() error = %v", err)
	}

	if len(releases) != 1 {
		t.Errorf("releases = %d, want 1", len(releases))
	}
}

func TestScanner_Scan_EmptyArtists(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		return &api.ArtistAlbumsResult{}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        2,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	releases, err := s.Scan(context.Background(), []db.Artist{}, since)

	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(releases) != 0 {
		t.Errorf("releases = %d, want 0", len(releases))
	}
}

func TestScanner_Scan_AllFailures(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		return nil, errors.New("API error")
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        2,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, true)

	artists := []db.Artist{
		{CatalogID: "1", Name: "Artist1"},
		{CatalogID: "2", Name: "Artist2"},
	}

	releases, err := s.Scan(context.Background(), artists, since)

	if err == nil {
		t.Error("expected error when all artists fail")
	}

	if len(releases) != 0 {
		t.Errorf("releases = %d, want 0", len(releases))
	}
}

func TestScanner_Scan_PartialSuccess(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		if artistID == "error" {
			return nil, errors.New("API error")
		}
		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: "album1",
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album",
						ReleaseDate: "2023-06-15",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        2,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, true)

	artists := []db.Artist{
		{CatalogID: "1", Name: "WorkingArtist"},
		{CatalogID: "error", Name: "ErrorArtist"},
	}

	releases, err := s.Scan(context.Background(), artists, since)

	if err != nil {
		t.Errorf("Scan() error = %v, want nil (partial success)", err)
	}

	if len(releases) != 1 {
		t.Errorf("releases = %d, want 1 (partial success)", len(releases))
	}
}

func TestScanner_Scan_DateFormats(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		releaseDate string
		wantRelease bool
	}{
		{
			name:        "full date after since",
			releaseDate: "2023-06-15",
			wantRelease: true,
		},
		{
			name:        "year-month after since",
			releaseDate: "2023-06",
			wantRelease: true,
		},
		{
			name:        "year after since",
			releaseDate: "2024",
			wantRelease: true,
		},
		{
			name:        "full date before since",
			releaseDate: "2022-12-31",
			wantRelease: false,
		},
		{
			name:        "year before since",
			releaseDate: "2022",
			wantRelease: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockAPIClient()
			client.getStorefrontFn = func(ctx context.Context) (string, error) {
				return "us", nil
			}
			client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
				return &api.ArtistAlbumsResult{
					Albums: []applemusic.Album{
						{
							Id: "album1",
							Attributes: applemusic.AlbumAttributes{
								Name:        "Album",
								ReleaseDate: tt.releaseDate,
								TrackCount:  10,
							},
						},
					},
				}, nil
			}

			cfg := &config.Config{
				Scan: config.ScanConfig{
					MaxAlbumsPerArtist: 10,
				},
			}

			s := New(cfg, client, nil, false)
			releases, err := s.Scan(context.Background(), []db.Artist{{CatalogID: "1", Name: "Artist"}}, since)

			if err != nil {
				t.Fatalf("Scan() error = %v", err)
			}

			if tt.wantRelease && len(releases) != 1 {
				t.Errorf("releases = %d, want 1", len(releases))
			}
			if !tt.wantRelease && len(releases) != 0 {
				t.Errorf("releases = %d, want 0", len(releases))
			}
		})
	}
}

func TestScanner_Scan_Parallel(t *testing.T) {
	since := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) {
		return "us", nil
	}
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		time.Sleep(10 * time.Millisecond)

		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{
					Id: "album-" + artistID,
					Attributes: applemusic.AlbumAttributes{
						Name:        "Album " + artistID,
						ReleaseDate: "2023-06-15",
						TrackCount:  10,
					},
				},
			},
		}, nil
	}

	cfg := &config.Config{
		Scan: config.ScanConfig{
			Concurrency:        10,
			MaxAlbumsPerArtist: 10,
		},
	}

	s := New(cfg, client, nil, false)

	artistCount := 20
	artists := make([]db.Artist, artistCount)
	for i := 0; i < artistCount; i++ {
		artists[i] = db.Artist{CatalogID: fmt.Sprintf("%d", i+1), Name: fmt.Sprintf("Artist%d", i+1)}
	}

	releases, err := s.Scan(context.Background(), artists, since)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(releases) != artistCount {
		t.Errorf("releases = %d, want %d", len(releases), artistCount)
	}
}

func TestScan_SkipsKnownReleases(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := db.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().Format(time.RFC3339)
	for _, r := range []db.Release{
		{AlbumID: "added-1", CatalogArtistID: "a1", Name: "n", State: "added", FirstSeenAt: now, ReleaseDate: "2026-01-01"},
		{AlbumID: "ignored-1", CatalogArtistID: "a1", Name: "n", State: "ignored", FirstSeenAt: now, ReleaseDate: "2026-01-01"},
	} {
		if err := store.UpsertRelease(r); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{Scan: config.ScanConfig{Concurrency: 1, MaxAlbumsPerArtist: 10}}
	client := newMockAPIClient()
	client.getStorefrontFn = func(ctx context.Context) (string, error) { return "us", nil }
	client.getArtistAlbumsFn = func(ctx context.Context, storefront, artistID string, limit int) (*api.ArtistAlbumsResult, error) {
		return &api.ArtistAlbumsResult{
			Albums: []applemusic.Album{
				{Id: "added-1", Attributes: applemusic.AlbumAttributes{Name: "Added", ReleaseDate: "2026-06-01", TrackCount: 5}},
				{Id: "ignored-1", Attributes: applemusic.AlbumAttributes{Name: "Ignored", ReleaseDate: "2026-06-01", TrackCount: 5}},
				{Id: "fresh-1", Attributes: applemusic.AlbumAttributes{Name: "Fresh", ReleaseDate: "2026-06-01", TrackCount: 5}},
			},
		}, nil
	}
	s := New(cfg, client, store, false)

	releases, err := s.Scan(context.Background(), []db.Artist{{CatalogID: "a1", Name: "A1"}}, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].AlbumID != "fresh-1" {
		t.Errorf("expected only 'fresh-1', got %+v", releases)
	}
}
