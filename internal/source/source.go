// Package source provides pluggable artist-discovery sources for release-radar.
// Each Source fetches a list of RawArtist entries from a different Apple Music
// data source. The Aggregator combines them, resolves names to catalog IDs,
// and applies dedup rules.
package source

import (
	"context"
	"fmt"

	"github.com/harrisonoest/release-radar/pkg/api"
)

// RawArtist is one artist discovered by a Source, before catalog resolution.
type RawArtist struct {
	// Name is the artist name as it appears in the source. Used for catalog
	// resolution when CatalogID is empty.
	Name string
	// CatalogID is the Apple Music catalog ID for the artist, if the source
	// provides it directly. Empty means the aggregator must resolve via search.
	CatalogID string
}

// Source is a single artist-discovery mechanism.
type Source interface {
	// Type returns the source_type identifier stored in artist_sources.
	// One of: 'library_artists', 'library_albums', 'library_songs', 'liked_songs', 'playlist'.
	Type() string
	// ID returns the playlist ID for source_type='playlist'; empty otherwise.
	ID() string
	// DisplayName is human-readable, e.g. "Library" or "Playlist: My Artists".
	DisplayName() string
	// Fetch retrieves raw artists from the source. Errors are returned, not swallowed.
	Fetch(ctx context.Context, c Fetcher) ([]RawArtist, error)
}

// Fetcher is the subset of the api.Client the source package needs.
// The return types are defined in pkg/api (see Task 4.3) and imported here.
// Using api.* types directly avoids a circular import: api cannot depend on
// internal/source, so the source package adapts to the api types.
type Fetcher interface {
	GetStorefront(ctx context.Context) (string, error)
	GetAllLibraryArtists(ctx context.Context, limit int, onProgress func(page, total int)) (*api.LibraryArtists, error)
	GetAllLibraryAlbums(ctx context.Context, limit int, onProgress func(page, total int)) (*api.LibraryAlbumsResult, error)
	GetAllLibrarySongs(ctx context.Context, limit int, onProgress func(page, total int)) (*api.LibrarySongsResult, error)
	GetLibraryPlaylistCatalogTracks(ctx context.Context, playlistID string, limit int) ([]api.PlaylistTrackResult, error)
	GetAllLibraryPlaylists(ctx context.Context) ([]api.PlaylistSummary, error)
	SearchArtists(ctx context.Context, storefront, name string) (*api.ArtistSearchResult, error)
}

// Source implementations import "github.com/harrisonoest/release-radar/pkg/api"
// and convert api.* values into []RawArtist in their Fetch method.

// Build constructs a Source from a type+id spec.
func Build(sourceType, sourceID string) (Source, error) {
	switch sourceType {
	case "library_artists":
		return &LibraryArtists{}, nil
	case "library_albums":
		return &LibraryAlbums{}, nil
	case "library_songs":
		return &LibrarySongs{}, nil
	case "liked_songs":
		return &LikedSongs{}, nil
	case "playlist":
		if sourceID == "" {
			return nil, fmt.Errorf("playlist source requires an id")
		}
		return &PlaylistSource{playlistID: sourceID}, nil
	default:
		return nil, fmt.Errorf("unknown source type: %s", sourceType)
	}
}
