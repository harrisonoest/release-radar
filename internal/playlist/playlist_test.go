package playlist

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/harrisonoest/release-radar/internal/scanner"
	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
	applemusic "github.com/minchao/go-apple-music"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{}
	m := New(cfg, nil, nil)

	if m == nil {
		t.Fatal("expected Manager, got nil")
	}
}

func TestManager_SetStorefront(t *testing.T) {
	cfg := &config.Config{}
	m := New(cfg, nil, nil)

	m.SetStorefront("gb")

	if m.storefront != "gb" {
		t.Errorf("storefront = %q, want %q", m.storefront, "gb")
	}
}

func TestIs404(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "404 error",
			err:      errors.New("404 not found"),
			expected: true,
		},
		{
			name:     "not found error",
			err:      errors.New("resource not found"),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("some other error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := is404(tt.err)
			if result != tt.expected {
				t.Errorf("is404(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestManager_AddReleases_EmptyReleases(t *testing.T) {
	cfg := &config.Config{
		Playlist: config.PlaylistConfig{
			Name:       "Release Radar",
			AutoCreate: true,
		},
	}
	m := New(cfg, nil, nil)
	m.SetStorefront("us")
	m.getExistingCatalogIDsFunc = func(ctx context.Context, playlistID string) (map[string]bool, error) {
		return map[string]bool{}, nil
	}

	ctx := context.Background()
	added, err := m.AddReleases(ctx, "pl1", []scanner.Release{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 0 {
		t.Errorf("added = %d, want 0", added)
	}
}

func TestManager_AddReleases_WarnsOnMissingAlbum(t *testing.T) {
	cfg := &config.Config{
		Playlist: config.PlaylistConfig{
			Name:       "Release Radar",
			AutoCreate: true,
		},
	}
	m := New(cfg, nil, nil)
	m.SetStorefront("us")
	m.getAlbumCatalogTrackIDsFunc = func(ctx context.Context, albumID string) ([]songID, error) {
		return nil, errors.New("album not found")
	}
	m.getExistingCatalogIDsFunc = func(ctx context.Context, playlistID string) (map[string]bool, error) {
		return map[string]bool{}, nil
	}

	ctx := context.Background()
	added, err := m.AddReleases(ctx, "pl1", []scanner.Release{
		{
			ArtistID:    "artist1",
			ArtistName:  "Artist 1",
			AlbumID:     "album1",
			AlbumName:   "Album 1",
			ReleaseDate: "2024-01-01",
			TrackCount:  1,
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 0 {
		t.Errorf("added = %d, want 0 (should skip album with error)", added)
	}
}

func newTestClient(t *testing.T) *api.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/playlists/pl1/tracks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	baseURL, _ := url.Parse(server.URL)
	am := applemusic.NewClient(nil)
	am.BaseURL = baseURL
	return &api.Client{Client: am}
}

func newTestManagerWithStore(cfg *config.Config, store *db.Store, trackIDs []string, existingCatalogIDs map[string]bool) *Manager {
	client := newTestClient(&testing.T{})
	m := New(cfg, client, store)
	m.SetStorefront("us")
	m.getAlbumCatalogTrackIDsFunc = func(ctx context.Context, albumID string) ([]songID, error) {
		out := make([]songID, len(trackIDs))
		for i, id := range trackIDs {
			out[i] = songID{ID: id}
		}
		return out, nil
	}
	m.getExistingCatalogIDsFunc = func(ctx context.Context, playlistID string) (map[string]bool, error) {
		return existingCatalogIDs, nil
	}
	return m
}

func TestAddReleases_UpdatesReleasesTable(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := db.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{Playlist: config.PlaylistConfig{Name: "Test", AutoCreate: true}}
	m := newTestManagerWithStore(cfg, store, []string{"t1", "t2"}, map[string]bool{})

	releases := []scanner.Release{
		{AlbumID: "album-1", ArtistID: "a1", ArtistName: "A1", AlbumName: "Album 1", ReleaseDate: "2026-06-01", TrackCount: 2},
	}
	added, err := m.AddReleases(context.Background(), "pl1", releases)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}

	rel, err := store.GetRelease("album-1")
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil {
		t.Fatal("expected release row, got nil")
	}
	if rel.State != "added" {
		t.Errorf("expected state 'added', got %q", rel.State)
	}
	if rel.AddedAt == "" {
		t.Error("expected non-empty added_at")
	}
}

func newTestManagerWithBackfillMock(t *testing.T, cfg *config.Config, store *db.Store, tracks []BackfillTrack) *Manager {
	t.Helper()
	m := New(cfg, nil, store)
	saved := tracks
	backfillFetchTracksFunc = func(ctx context.Context, playlistID string) ([]BackfillTrack, error) {
		return saved, nil
	}
	t.Cleanup(func() { backfillFetchTracksFunc = nil })
	return m
}

func TestBackfillFromPlaylist_InsertsAllAsAdded(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := db.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	skipped, _ := store.SkippedReleaseIDs()
	if len(skipped) != 0 {
		t.Fatalf("expected empty releases table, got %d entries", len(skipped))
	}

	cfg := &config.Config{Playlist: config.PlaylistConfig{Name: "Test", AutoCreate: true}}
	m := newTestManagerWithBackfillMock(t, cfg, store, []BackfillTrack{
		{TrackID: "t1", AlbumID: "album-x", AlbumName: "Album X", ArtistName: "Artist X", ReleaseDate: "2026-01-01"},
		{TrackID: "t2", AlbumID: "album-y", AlbumName: "Album Y", ArtistName: "Artist Y", ReleaseDate: "2026-02-01"},
	})

	n, err := m.BackfillFromPlaylist(context.Background(), "playlist-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("expected 2 backfilled, got %d", n)
	}

	relX, _ := store.GetRelease("album-x")
	if relX == nil || relX.State != "added" {
		t.Errorf("expected album-x in added state, got %+v", relX)
	}
	relY, _ := store.GetRelease("album-y")
	if relY == nil || relY.State != "added" {
		t.Errorf("expected album-y in added state, got %+v", relY)
	}
}

func TestBackfillFromPlaylist_SkipsIfNotEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := db.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.UpsertRelease(db.Release{AlbumID: "existing", State: "seen", FirstSeenAt: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Playlist: config.PlaylistConfig{Name: "Test"}}
	m := newTestManagerWithBackfillMock(t, cfg, store, []BackfillTrack{
		{TrackID: "t1", AlbumID: "should-skip", AlbumName: "X"},
	})

	n, err := m.BackfillFromPlaylist(context.Background(), "playlist-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 (table non-empty), got %d", n)
	}
	rel, _ := store.GetRelease("should-skip")
	if rel != nil {
		t.Error("backfill should not have run when table was non-empty")
	}
}
