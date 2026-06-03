package source

import (
	"context"
	"testing"

	"github.com/harrisonoest/release-radar/pkg/api"
)

func TestLibraryAlbums_Fetch(t *testing.T) {
	m := &mockFetcher{
		libraryAlbums: &api.LibraryAlbumsResult{
			Albums: []api.LibraryAlbum{
				{ID: "al1", ArtistID: "a1", ArtistName: "Artist 1", Name: "Album 1"},
				{ID: "al2", ArtistID: "a1", ArtistName: "Artist 1", Name: "Album 2"}, // duplicate artist
				{ID: "al3", ArtistID: "a2", ArtistName: "Artist 2", Name: "Album 3"},
				{ID: "al4", ArtistID: "", ArtistName: "Unknown", Name: "Album 4"}, // no artist ID — skip
			},
		},
	}
	s := &LibraryAlbums{}
	got, err := s.Fetch(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 unique artists, got %d", len(got))
	}
	for _, a := range got {
		if a.CatalogID == "" {
			t.Error("expected non-empty CatalogID")
		}
	}
}
