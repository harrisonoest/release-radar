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

func TestAggregator_KeepsBandsWithAmpersand(t *testing.T) {
	m := &mockFetcher{
		libraryArtists: &api.LibraryArtists{
			Data: []api.LibraryArtist{
				// A real band whose catalog name contains " & " and ", ":
				// must NOT be dropped by the collaboration heuristic.
				{ID: "lib-1", Attributes: api.LibraryArtistAttributes{Name: "Earth, Wind & Fire"}, Relationships: api.LibraryArtistRelationships{Catalog: api.LibraryArtistCatalog{Data: []api.LibraryArtistCatalogData{{ID: "c1", Type: "artists"}}}}},
			},
		},
	}
	store := &mockStore{}
	agg := &Aggregator{Store: store, Fetcher: m, Storefront: "us"}
	if err := agg.Aggregate(context.Background(), []Source{&LibraryArtists{}}); err != nil {
		t.Fatal(err)
	}
	if len(store.artists) != 1 || store.artists[0].CatalogID != "c1" {
		t.Errorf("expected band c1 to be kept, got %+v", store.artists)
	}
}

func TestAggregator_DropsSearchResolvedCollaborations(t *testing.T) {
	m := &mockFetcher{
		librarySongs: &api.LibrarySongsResult{
			Songs: []api.LibrarySong{{ID: "s1", ArtistName: "A & B"}},
		},
		// Search resolves the collab string to a matching-name artist.
		searchResults: map[string]*api.ArtistSearchResult{
			"A & B": {Artists: []api.ArtistSearchEntry{{ID: "cX", Name: "A & B"}}},
		},
	}
	store := &mockStore{}
	agg := &Aggregator{Store: store, Fetcher: m, Storefront: "us"}
	if err := agg.Aggregate(context.Background(), []Source{&LibrarySongs{}}); err != nil {
		t.Fatal(err)
	}
	for _, a := range store.artists {
		if a.CatalogID == "cX" {
			t.Errorf("search-resolved collaboration cX should be dropped, got %+v", store.artists)
		}
	}
}

func TestAggregator_RejectsNonMatchingSearchHit(t *testing.T) {
	m := &mockFetcher{
		librarySongs: &api.LibrarySongsResult{
			Songs: []api.LibrarySong{{ID: "s1", ArtistName: "Obscure Artist"}},
		},
		// Search returns an unrelated artist for the name: must not adopt it.
		searchResults: map[string]*api.ArtistSearchResult{
			"Obscure Artist": {Artists: []api.ArtistSearchEntry{{ID: "cWrong", Name: "Totally Different"}}},
		},
	}
	store := &mockStore{}
	agg := &Aggregator{Store: store, Fetcher: m, Storefront: "us"}
	if err := agg.Aggregate(context.Background(), []Source{&LibrarySongs{}}); err != nil {
		t.Fatal(err)
	}
	if len(store.artists) != 0 {
		t.Errorf("expected no artists (non-matching hit rejected), got %+v", store.artists)
	}
}

func TestAggregator_AbortsOnSourceFailure(t *testing.T) {
	m := &mockFetcher{
		libraryArtistsErr: context.DeadlineExceeded,
		librarySongs: &api.LibrarySongsResult{
			Songs: []api.LibrarySong{{ID: "s1", ArtistName: "Artist 1"}},
		},
	}
	store := &mockStore{}
	agg := &Aggregator{Store: store, Fetcher: m, Storefront: "us"}
	if err := agg.Aggregate(context.Background(), []Source{&LibraryArtists{}, &LibrarySongs{}}); err == nil {
		t.Fatal("expected error when a source fails")
	}
	if len(store.artists) != 0 {
		t.Errorf("tracked set must not be updated when a source fails, got %+v", store.artists)
	}
}
