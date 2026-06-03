package source

import (
	"context"
	"testing"

	"github.com/harrisonoest/release-radar/pkg/api"
)

// mockFetcher implements source.Fetcher for tests. The libraryArtists field
// is *api.LibraryArtists (the existing type from pkg/api). Other fields are
// the narrow types defined in pkg/api.
type mockFetcher struct {
	libraryArtists    *api.LibraryArtists
	libraryArtistsErr error
	libraryAlbums     *api.LibraryAlbumsResult
	libraryAlbumsErr  error
	librarySongs      *api.LibrarySongsResult
	librarySongsErr   error
	playlists         []api.PlaylistSummary
	playlistsErr      error
	playlistTracks    []api.PlaylistTrackResult
	playlistTracksErr error
	searchResults     map[string]*api.ArtistSearchResult
}

func (m *mockFetcher) GetStorefront(ctx context.Context) (string, error) {
	return "us", nil
}
func (m *mockFetcher) GetAllLibraryArtists(ctx context.Context, limit int, onProgress func(page, total int)) (*api.LibraryArtists, error) {
	return m.libraryArtists, m.libraryArtistsErr
}
func (m *mockFetcher) GetAllLibraryAlbums(ctx context.Context, limit int, onProgress func(page, total int)) (*api.LibraryAlbumsResult, error) {
	return m.libraryAlbums, m.libraryAlbumsErr
}
func (m *mockFetcher) GetAllLibrarySongs(ctx context.Context, limit int, onProgress func(page, total int)) (*api.LibrarySongsResult, error) {
	return m.librarySongs, m.librarySongsErr
}
func (m *mockFetcher) GetLibraryPlaylistCatalogTracks(ctx context.Context, playlistID string, limit int) ([]api.PlaylistTrackResult, error) {
	return m.playlistTracks, m.playlistTracksErr
}
func (m *mockFetcher) GetAllLibraryPlaylists(ctx context.Context) ([]api.PlaylistSummary, error) {
	return m.playlists, m.playlistsErr
}
func (m *mockFetcher) SearchArtists(ctx context.Context, storefront, name string) (*api.ArtistSearchResult, error) {
	if m.searchResults == nil {
		return &api.ArtistSearchResult{}, nil
	}
	if r, ok := m.searchResults[name]; ok {
		return r, nil
	}
	return &api.ArtistSearchResult{}, nil
}

func TestLibraryArtists_Fetch(t *testing.T) {
	m := &mockFetcher{
		libraryArtists: &api.LibraryArtists{
			Data: []api.LibraryArtist{
				{ID: "lib-1", Attributes: api.LibraryArtistAttributes{Name: "Artist 1"}, Relationships: api.LibraryArtistRelationships{Catalog: api.LibraryArtistCatalog{Data: []api.LibraryArtistCatalogData{{ID: "c1", Type: "artists"}}}}},
				{ID: "lib-2", Attributes: api.LibraryArtistAttributes{Name: "Artist 2"}, Relationships: api.LibraryArtistRelationships{Catalog: api.LibraryArtistCatalog{Data: []api.LibraryArtistCatalogData{{ID: "c2", Type: "artists"}}}}},
				{ID: "lib-3", Attributes: api.LibraryArtistAttributes{Name: "No Catalog"}, Relationships: api.LibraryArtistRelationships{Catalog: api.LibraryArtistCatalog{Data: nil}}},
			},
		},
	}
	s := &LibraryArtists{}
	got, err := s.Fetch(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 artists, got %d", len(got))
	}
	for _, a := range got {
		if a.CatalogID == "" {
			t.Error("expected non-empty CatalogID")
		}
	}
}
