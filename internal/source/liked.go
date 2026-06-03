package source

import (
	"context"
	"fmt"
)

const likedSongsName = "Liked Songs"

// LikedSongs fetches artists from the user's Liked Songs playlist.
// It uses the Liked Songs playlist (resolved by name from the user's library
// playlists) and the same track-fetching path as PlaylistSource.
type LikedSongs struct{}

func (s *LikedSongs) Type() string        { return "liked_songs" }
func (s *LikedSongs) ID() string          { return "" } // resolved at Fetch time
func (s *LikedSongs) DisplayName() string { return likedSongsName }

func (s *LikedSongs) Fetch(ctx context.Context, c Fetcher) ([]RawArtist, error) {
	playlists, err := c.GetAllLibraryPlaylists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list playlists: %w", err)
	}
	var likedID string
	for _, p := range playlists {
		if p.Name == likedSongsName {
			likedID = p.ID
			break
		}
	}
	if likedID == "" {
		return nil, fmt.Errorf("Liked Songs playlist not found")
	}
	// Delegate to playlist fetch logic.
	ps := &PlaylistSource{playlistID: likedID}
	return ps.Fetch(ctx, c)
}
