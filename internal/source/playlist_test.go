package source

import (
	"context"
	"testing"

	"github.com/harrisonoest/release-radar/pkg/api"
)

func TestPlaylistSource_Fetch(t *testing.T) {
	m := &mockFetcher{
		playlistTracks: []api.PlaylistTrackResult{
			{TrackID: "t1", ArtistName: "Artist 1", AlbumID: "a1", AlbumName: "Album 1"},
			{TrackID: "t2", ArtistName: "Artist 2", AlbumID: "a2", AlbumName: "Album 2"},
			{TrackID: "t3", ArtistName: "Artist 1", AlbumID: "a1", AlbumName: "Album 1"},
		},
	}
	s := &PlaylistSource{playlistID: "playlist-123"}
	got, err := s.Fetch(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 unique artists, got %d", len(got))
	}
}
