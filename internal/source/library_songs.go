package source

import "context"

// LibrarySongs fetches artists from the user's library songs.
// Catalog IDs are resolved via search, since songs only carry ArtistName (a string).
type LibrarySongs struct{}

func (src *LibrarySongs) Type() string        { return "library_songs" }
func (src *LibrarySongs) ID() string          { return "" }
func (src *LibrarySongs) DisplayName() string { return "Library songs" }

func (src *LibrarySongs) Fetch(ctx context.Context, c Fetcher) ([]RawArtist, error) {
	res, err := c.GetAllLibrarySongs(ctx, 100, nil)
	if err != nil {
		return nil, err
	}
	out := make([]RawArtist, 0)
	seen := make(map[string]bool)
	for _, song := range res.Songs {
		if song.ArtistName == "" || seen[song.ArtistName] {
			continue
		}
		seen[song.ArtistName] = true
		out = append(out, RawArtist{Name: song.ArtistName})
	}
	return out, nil
}
