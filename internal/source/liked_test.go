package source

import (
	"context"
	"errors"
	"testing"

	"github.com/harrisonoest/release-radar/pkg/api"
)

func TestLikedSongs_Fetch(t *testing.T) {
	m := &mockFetcher{
		playlists: []api.PlaylistSummary{
			{ID: "p1", Name: "My Mix"},
			{ID: "p2", Name: "Liked Songs"},
			{ID: "p3", Name: "Workout"},
		},
	}
	// ... we need to also mock playlistTracks for p2; we'll set it here
	m.playlistTracks = []api.PlaylistTrackResult{
		{TrackID: "t1", AlbumID: "album-x", AlbumName: "Album X", ArtistName: "Artist X"},
	}
	src := &LikedSongs{}
	got, err := src.Fetch(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Artist X" {
		t.Errorf("expected 1 artist (Artist X), got %+v", got)
	}
}

func TestLikedSongs_NotFound(t *testing.T) {
	m := &mockFetcher{
		playlists: []api.PlaylistSummary{
			{ID: "p1", Name: "My Mix"},
		},
	}
	src := &LikedSongs{}
	_, err := src.Fetch(context.Background(), m)
	if err == nil {
		t.Error("expected error when Liked Songs playlist not found")
	}
}

// silence unused import warning
var _ = errors.New
