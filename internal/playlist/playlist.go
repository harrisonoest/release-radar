package playlist

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/harrisonoest/release-radar/internal/scanner"
	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/minchao/go-apple-music"
)

type Manager struct {
	cfg        *config.Config
	client     *api.Client
	storefront string

	getAlbumCatalogTrackIDsFunc func(ctx context.Context, albumID string) ([]songID, error)
	getExistingCatalogIDsFunc   func(ctx context.Context, playlistID string) (map[string]bool, error)
}

func New(cfg *config.Config, client *api.Client) *Manager {
	m := &Manager{cfg: cfg, client: client}
	m.getAlbumCatalogTrackIDsFunc = m.getAlbumCatalogTrackIDs
	m.getExistingCatalogIDsFunc = m.getExistingCatalogIDs
	return m
}

func (m *Manager) SetStorefront(storefront string) {
	m.storefront = storefront
}

func (m *Manager) EnsurePlaylist(ctx context.Context) (string, error) {
	all, _, err := m.client.Me.GetAllLibraryPlaylists(ctx, &applemusic.PageOptions{
		Limit: 100,
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch playlists: %w", err)
	}

	desired := m.cfg.Playlist.Name

	for _, p := range all.Data {
		if p.Attributes.Name == desired {
			return p.Id, nil
		}
	}

	if !m.cfg.Playlist.AutoCreate {
		return "", fmt.Errorf("playlist %q not found and auto_create is disabled", desired)
	}

	created, _, err := m.client.Me.CreateLibraryPlaylist(ctx, applemusic.CreateLibraryPlaylist{
		Attributes: applemusic.CreateLibraryPlaylistAttributes{
			Name:        desired,
			Description: "Managed by Release Radar",
		},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create playlist: %w", err)
	}

	if len(created.Data) == 0 {
		return "", fmt.Errorf("playlist created but no data returned")
	}

	return created.Data[0].Id, nil
}

func (m *Manager) AddReleases(ctx context.Context, playlistID string, releases []scanner.Release) (int, error) {
	existing, err := m.getExistingCatalogIDsFunc(ctx, playlistID)
	if err != nil {
		return 0, fmt.Errorf("failed to get existing tracks: %w", err)
	}

	added := 0
	for _, rel := range releases {
		tracks, err := m.getAlbumCatalogTrackIDsFunc(ctx, rel.AlbumID)
		if err != nil {
			fmt.Printf("  [warn] %s — %s: %v\n", rel.ArtistName, rel.AlbumName, err)
			continue
		}

		var newTracks []applemusic.CreateLibraryPlaylistTrack
		for _, t := range tracks {
			if !existing[t.ID] {
				newTracks = append(newTracks, applemusic.CreateLibraryPlaylistTrack{
					Id:   t.ID,
					Type: "songs",
				})
				existing[t.ID] = true
			}
		}

		if len(newTracks) == 0 {
			continue
		}

		_, err = m.client.Me.AddLibraryTracksToPlaylist(ctx, playlistID,
			applemusic.CreateLibraryPlaylistTrackData{Data: newTracks})
		if err != nil {
			return added, fmt.Errorf("failed to add tracks to playlist: %w", err)
		}
		added++

		fmt.Printf("  + %s — %s (%d tracks)\n", rel.ArtistName, rel.AlbumName, len(newTracks))
	}

	return added, nil
}

func (m *Manager) getExistingCatalogIDs(ctx context.Context, playlistID string) (map[string]bool, error) {
	ids := make(map[string]bool)

	tracks, _, err := m.client.Me.GetLibraryPlaylistTracks(ctx, playlistID, &applemusic.PageOptions{Limit: 100})
	if err != nil {
		if !is404(err) {
			return ids, fmt.Errorf("failed to get playlist tracks: %w", err)
		}
	} else {
		for _, t := range tracks {
			ids[t.Id] = true
		}
	}

	catalogTracks, err := m.client.Me.GetLibraryPlaylistCatalogTracks(ctx, playlistID, 100)
	if err != nil {
		if !is404(err) {
			return ids, nil
		}
	} else {
		for _, t := range catalogTracks {
			ids[t.Id] = true
		}
	}

	return ids, nil
}

func is404(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found"))
}

type songID struct {
	ID string
}

func (m *Manager) getAlbumCatalogTrackIDs(ctx context.Context, albumID string) ([]songID, error) {
	u := fmt.Sprintf("v1/catalog/%s/albums/%s", m.storefront, albumID)
	req, err := m.client.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	type albumResponse struct {
		Data []struct {
			Relationships struct {
				Tracks struct {
					Data []songID `json:"data"`
				} `json:"tracks"`
			} `json:"relationships"`
		} `json:"data"`
	}

	result := &albumResponse{}
	resp, err := m.client.Do(ctx, req, result)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("album not found")
	}

	return result.Data[0].Relationships.Tracks.Data, nil
}
