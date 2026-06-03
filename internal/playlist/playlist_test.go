package playlist

import (
	"context"
	"errors"
	"testing"

	"github.com/harrisonoest/release-radar/internal/scanner"
	"github.com/harrisonoest/release-radar/pkg/config"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{}
	m := New(cfg, nil)

	if m == nil {
		t.Fatal("expected Manager, got nil")
	}
}

func TestManager_SetStorefront(t *testing.T) {
	cfg := &config.Config{}
	m := New(cfg, nil)

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
	m := New(cfg, nil)
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
	m := New(cfg, nil)
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
