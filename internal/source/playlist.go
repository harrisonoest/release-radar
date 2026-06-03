package source

import (
	"context"
	"fmt"
)

// PlaylistSource fetches artists from a user-specified library playlist.
type PlaylistSource struct {
	playlistID string
}

func (s *PlaylistSource) Type() string { return "playlist" }
func (s *PlaylistSource) ID() string   { return s.playlistID }
func (s *PlaylistSource) DisplayName() string {
	return fmt.Sprintf("Playlist %s", s.playlistID)
}

func (s *PlaylistSource) Fetch(ctx context.Context, c Fetcher) ([]RawArtist, error) {
	tracks, err := c.GetLibraryPlaylistCatalogTracks(ctx, s.playlistID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]RawArtist, 0, len(tracks))
	seen := make(map[string]bool)
	for _, t := range tracks {
		if t.ArtistName == "" || seen[t.ArtistName] {
			continue
		}
		seen[t.ArtistName] = true
		out = append(out, RawArtist{Name: t.ArtistName})
	}
	return out, nil
}
