package source

import "context"

// LibraryAlbums fetches artists from the user's library albums.
type LibraryAlbums struct{}

func (s *LibraryAlbums) Type() string        { return "library_albums" }
func (s *LibraryAlbums) ID() string          { return "" }
func (s *LibraryAlbums) DisplayName() string { return "Library albums" }

func (s *LibraryAlbums) Fetch(ctx context.Context, c Fetcher) ([]RawArtist, error) {
	res, err := c.GetAllLibraryAlbums(ctx, 100, nil)
	if err != nil {
		return nil, err
	}
	out := make([]RawArtist, 0, len(res.Albums))
	seen := make(map[string]bool)
	for _, a := range res.Albums {
		if a.ArtistID == "" {
			continue
		}
		if seen[a.ArtistID] {
			continue
		}
		seen[a.ArtistID] = true
		out = append(out, RawArtist{Name: a.ArtistName, CatalogID: a.ArtistID})
	}
	return out, nil
}
