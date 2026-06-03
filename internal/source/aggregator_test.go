package source

import (
	"context"
	"testing"

	"github.com/harrisonoest/release-radar/pkg/api"
	"github.com/harrisonoest/release-radar/pkg/db"
)

type mockStore struct {
	artists    []db.Artist
	sources    []db.ArtistSource
	replaceErr error
	upsertErr  error
}

func (m *mockStore) ReplaceArtists(artists []db.Artist) error {
	if m.replaceErr != nil {
		return m.replaceErr
	}
	m.artists = artists
	return nil
}

func (m *mockStore) UpsertArtistSource(src db.ArtistSource) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.sources = append(m.sources, src)
	return nil
}

func TestAggregator_DedupesAndResolves(t *testing.T) {
	m := &mockFetcher{
		libraryArtists: &api.LibraryArtists{
			Data: []api.LibraryArtist{
				{ID: "lib-1", Attributes: api.LibraryArtistAttributes{Name: "Artist 1"}, Relationships: api.LibraryArtistRelationships{Catalog: api.LibraryArtistCatalog{Data: []api.LibraryArtistCatalogData{{ID: "c1", Type: "artists"}}}}},
				{ID: "lib-2", Attributes: api.LibraryArtistAttributes{Name: "Artist 2"}, Relationships: api.LibraryArtistRelationships{Catalog: api.LibraryArtistCatalog{Data: []api.LibraryArtistCatalogData{{ID: "c2", Type: "artists"}}}}},
			},
		},
		librarySongs: &api.LibrarySongsResult{
			Songs: []api.LibrarySong{
				{ID: "s1", ArtistName: "Artist 1"},
				{ID: "s2", ArtistName: "Artist 3"},
			},
		},
	}
	m.searchResults = map[string]*api.ArtistSearchResult{
		"Artist 1": {Artists: []api.ArtistSearchEntry{{ID: "c1", Name: "Artist 1"}}},
		"Artist 3": {Artists: []api.ArtistSearchEntry{{ID: "c3", Name: "Artist 3"}}},
	}

	store := &mockStore{}
	agg := &Aggregator{
		Store:      store,
		Fetcher:    m,
		Storefront: "us",
		Logger:     func(f string, args ...interface{}) {},
	}
	sources := []Source{
		&LibraryArtists{},
		&LibrarySongs{},
	}

	if err := agg.Aggregate(context.Background(), sources); err != nil {
		t.Fatal(err)
	}

	if len(store.artists) != 3 {
		t.Errorf("expected 3 artists, got %d", len(store.artists))
	}
	if len(store.sources) < 4 {
		t.Errorf("expected >= 4 source rows, got %d", len(store.sources))
	}
}
