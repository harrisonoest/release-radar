package source

import (
	"context"
	"testing"

	"github.com/harrisonoest/release-radar/pkg/api"
)

func TestLibrarySongs_Fetch(t *testing.T) {
	m := &mockFetcher{
		librarySongs: &api.LibrarySongsResult{
			Songs: []api.LibrarySong{
				{ID: "s1", ArtistName: "Artist 1", AlbumName: "Album 1"},
				{ID: "s2", ArtistName: "Artist 1", AlbumName: "Album 1"}, // duplicate name — dedup
				{ID: "s3", ArtistName: "Artist 2", AlbumName: "Album 2"},
				{ID: "s4", ArtistName: "", AlbumName: "Album 3"}, // empty name — skip
			},
		},
	}
	src := &LibrarySongs{}
	got, err := src.Fetch(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 unique artists, got %d", len(got))
	}
	for _, a := range got {
		if a.Name == "" {
			t.Error("expected non-empty Name")
		}
	}
}
