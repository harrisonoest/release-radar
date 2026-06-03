package source

import "context"

// LibraryArtists fetches artists from the user's library via
// GET /v1/me/library/artists?include=catalog. The catalog relationship gives
// us the catalog ID directly.
type LibraryArtists struct{}

func (s *LibraryArtists) Type() string        { return "library_artists" }
func (s *LibraryArtists) ID() string          { return "" }
func (s *LibraryArtists) DisplayName() string { return "Library artists" }

func (s *LibraryArtists) Fetch(ctx context.Context, c Fetcher) ([]RawArtist, error) {
	res, err := c.GetAllLibraryArtists(ctx, 25, nil)
	if err != nil {
		return nil, err
	}
	out := make([]RawArtist, 0, len(res.Data))
	for _, a := range res.Data {
		catID := ""
		if len(a.Relationships.Catalog.Data) > 0 {
			catID = a.Relationships.Catalog.Data[0].ID
		}
		if catID == "" {
			continue
		}
		out = append(out, RawArtist{
			Name:      a.Attributes.Name,
			CatalogID: catID,
		})
	}
	return out, nil
}
