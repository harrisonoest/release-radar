package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/pkg/config"
	applemusic "github.com/minchao/go-apple-music"
)

type ProgressCallback func(done, total int)

type LibraryArtists struct {
	Data []LibraryArtist `json:"data"`
	Href string          `json:"href,omitempty"`
	Next string          `json:"next,omitempty"`
	Meta struct {
		Total int `json:"total"`
	} `json:"meta,omitempty"`
}

type LibraryArtist struct {
	ID            string                     `json:"id"`
	Type          string                     `json:"type"`
	Href          string                     `json:"href,omitempty"`
	Attributes    LibraryArtistAttributes    `json:"attributes,omitempty"`
	Relationships LibraryArtistRelationships `json:"relationships,omitempty"`
}

type LibraryArtistAttributes struct {
	Name string `json:"name"`
}

type LibraryArtistRelationships struct {
	Catalog LibraryArtistCatalog `json:"catalog,omitempty"`
}

type LibraryArtistCatalog struct {
	Data []LibraryArtistCatalogData `json:"data"`
}

type LibraryArtistCatalogData struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type ArtistAlbumsResult struct {
	Albums []applemusic.Album
	Total  int
}

type Client struct {
	*applemusic.Client
	storefront string
}

type ClientInterface interface {
	GetStorefront(ctx context.Context) (string, error)
	GetArtistAlbums(ctx context.Context, storefront, artistID string, since time.Time) (*ArtistAlbumsResult, error)
}

func NewClient(cfg *config.Config, authenticator *auth.Authenticator) (*Client, error) {
	devToken, err := authenticator.DeveloperToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get developer token: %w", err)
	}

	musicUserToken, err := authenticator.MusicUserToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get music user token: %w", err)
	}

	tp := applemusic.Transport{
		Token:          devToken,
		MusicUserToken: musicUserToken,
	}
	return &Client{Client: applemusic.NewClient(tp.Client())}, nil
}

func (c *Client) GetStorefront(ctx context.Context) (string, error) {
	if c.storefront != "" {
		return c.storefront, nil
	}

	result, _, err := c.Me.GetStorefront(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get storefront: %w", err)
	}
	if len(result.Data) == 0 {
		return "", fmt.Errorf("no storefront returned")
	}

	c.storefront = result.Data[0].Id
	return c.storefront, nil
}

func (c *Client) GetArtistAlbums(ctx context.Context, storefront, artistID string, since time.Time) (*ArtistAlbumsResult, error) {
	watermark := since.AddDate(0, 0, -30)
	pageSize := c.scanPageSize()
	filter := "albums,singles,eps,compilations,live-albums"

	var all []applemusic.Album
	offset := 0

	type albumResponse struct {
		Data []applemusic.Album `json:"data"`
		Next string             `json:"next,omitempty"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}

	for {
		u := fmt.Sprintf("v1/catalog/%s/artists/%s/albums?limit=%d&offset=%d&filter[albums]=%s",
			storefront, artistID, pageSize, offset, filter)
		req, err := c.NewRequest("GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		result := &albumResponse{}
		resp, err := c.Do(ctx, req, result)

		if resp != nil {
			if resp.StatusCode == http.StatusNotFound {
				return &ArtistAlbumsResult{}, nil
			}
			if resp.StatusCode == http.StatusTooManyRequests {
				return nil, fmt.Errorf("rate limited (429)")
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("API request failed: %w", err)
		}

		all = append(all, result.Data...)

		hitWatermark := false
		for _, album := range result.Data {
			if album.Attributes.ReleaseDate == "" {
				continue
			}
			t, parseErr := time.Parse("2006-01-02", album.Attributes.ReleaseDate)
			if parseErr == nil {
				if t.Before(watermark) {
					hitWatermark = true
					break
				}
				continue
			}
			for _, layout := range []string{"2006-01", "2006"} {
				if pt, perr := time.Parse(layout, album.Attributes.ReleaseDate); perr == nil {
					if pt.Before(watermark) {
						hitWatermark = true
						break
					}
				}
			}
			if hitWatermark {
				break
			}
		}

		if hitWatermark || len(result.Data) < pageSize || (result.Meta.Total > 0 && len(all) >= result.Meta.Total) {
			return &ArtistAlbumsResult{Albums: all, Total: result.Meta.Total}, nil
		}
		offset += pageSize
	}
}

func (c *Client) scanPageSize() int {
	return 25
}

// PlaylistTrackResult is a single track returned by GetLibraryPlaylistCatalogTracks
// with album and artist metadata attached.
type PlaylistTrackResult struct {
	TrackID     string
	AlbumID     string
	AlbumName   string
	ArtistName  string
	ReleaseDate string
}

// GetLibraryPlaylistCatalogTracks fetches the catalog tracks of a library playlist
// with album metadata for backfill.
func (c *Client) GetLibraryPlaylistCatalogTracks(ctx context.Context, playlistID string, limit int) ([]PlaylistTrackResult, error) {
	u := fmt.Sprintf("v1/me/library/playlists/%s/tracks?include=albums&limit=%d", playlistID, limit)
	req, err := c.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	type trackResp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name       string `json:"name"`
				ArtistName string `json:"artistName"`
				AlbumName  string `json:"albumName"`
			} `json:"attributes"`
			Relationships struct {
				Album struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				} `json:"album"`
			} `json:"relationships"`
		} `json:"data"`
	}
	var result trackResp
	resp, err := c.Do(ctx, req, &result)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("playlist tracks returned %d", resp.StatusCode)
	}
	out := make([]PlaylistTrackResult, 0, len(result.Data))
	for _, t := range result.Data {
		p := PlaylistTrackResult{
			TrackID:    t.ID,
			AlbumName:  t.Attributes.AlbumName,
			ArtistName: t.Attributes.ArtistName,
		}
		if len(t.Relationships.Album.Data) > 0 {
			p.AlbumID = t.Relationships.Album.Data[0].ID
		}
		out = append(out, p)
	}
	return out, nil
}

// LibraryAlbumsResult is the paginated result of GetAllLibraryAlbums.
type LibraryAlbumsResult struct {
	Albums []LibraryAlbum
}

// LibraryAlbum is a single album from the user's library with its primary artist.
type LibraryAlbum struct {
	ID         string
	ArtistID   string
	ArtistName string
	Name       string
}

// LibrarySongsResult is the paginated result of GetAllLibrarySongs.
type LibrarySongsResult struct {
	Songs []LibrarySong
}

// LibrarySong is a single song from the user's library with album and artist metadata.
type LibrarySong struct {
	ID         string
	AlbumID    string
	AlbumName  string
	ArtistName string
}

// PlaylistSummary is a minimal playlist record used to list and locate
// the user's library playlists (including "Liked Songs").
type PlaylistSummary struct {
	ID   string
	Name string
}

// ArtistSearchResult is the result of SearchArtists, narrowed to catalog
// artist entries.
type ArtistSearchResult struct {
	Artists []ArtistSearchEntry
}

// ArtistSearchEntry is a single artist hit from a catalog search.
type ArtistSearchEntry struct {
	ID   string
	Name string
}

// GetAllLibraryAlbums pages through the user's library albums.
func (c *Client) GetAllLibraryAlbums(ctx context.Context, limit int, onProgress func(page, total int)) (*LibraryAlbumsResult, error) {
	var all []LibraryAlbum
	offset := 0
	page := 0
	total := 0

	type albumResponse struct {
		Data []struct {
			ID            string `json:"id"`
			Attributes    struct {
				Name       string `json:"name"`
				ArtistName string `json:"artistName"`
			} `json:"attributes"`
			Relationships struct {
				Artists struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				} `json:"artists"`
			} `json:"relationships"`
		} `json:"data"`
		Next string `json:"next,omitempty"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}

	for {
		u := fmt.Sprintf("v1/me/library/albums?limit=%d&offset=%d&include=artists", limit, offset)
		var result *albumResponse
		var resp *applemusic.Response
		var err error

		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
				}
			}
			req, reqErr := c.NewRequest("GET", u, nil)
			if reqErr != nil {
				return nil, fmt.Errorf("failed to create request: %w", reqErr)
			}
			result = &albumResponse{}
			resp, err = c.Do(ctx, req, result)
			if err == nil && resp.StatusCode == http.StatusOK {
				break
			}
			if resp != nil && resp.StatusCode >= 500 {
				continue
			}
			if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("API request failed: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		for _, item := range result.Data {
			la := LibraryAlbum{
				ID:         item.ID,
				Name:       item.Attributes.Name,
				ArtistName: item.Attributes.ArtistName,
			}
			if len(item.Relationships.Artists.Data) > 0 {
				la.ArtistID = item.Relationships.Artists.Data[0].ID
			}
			all = append(all, la)
		}
		page++
		if total == 0 && result.Meta.Total > 0 {
			total = (result.Meta.Total + limit - 1) / limit
		}
		if onProgress != nil && total > 0 {
			onProgress(page, total)
		} else if onProgress != nil {
			onProgress(page, 0)
		}
		if len(result.Data) < limit {
			break
		}
		offset += limit
	}

	return &LibraryAlbumsResult{Albums: all}, nil
}

// GetAllLibrarySongs pages through the user's library songs.
func (c *Client) GetAllLibrarySongs(ctx context.Context, limit int, onProgress func(page, total int)) (*LibrarySongsResult, error) {
	var all []LibrarySong
	offset := 0
	page := 0
	total := 0

	type songResponse struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name       string `json:"name"`
				ArtistName string `json:"artistName"`
				AlbumName  string `json:"albumName"`
			} `json:"attributes"`
		} `json:"data"`
		Next string `json:"next,omitempty"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}

	for {
		u := fmt.Sprintf("v1/me/library/songs?limit=%d&offset=%d", limit, offset)
		var result *songResponse
		var resp *applemusic.Response
		var err error

		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
				}
			}
			req, reqErr := c.NewRequest("GET", u, nil)
			if reqErr != nil {
				return nil, fmt.Errorf("failed to create request: %w", reqErr)
			}
			result = &songResponse{}
			resp, err = c.Do(ctx, req, result)
			if err == nil && resp.StatusCode == http.StatusOK {
				break
			}
			if resp != nil && resp.StatusCode >= 500 {
				continue
			}
			if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("API request failed: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		for _, item := range result.Data {
			all = append(all, LibrarySong{
				ID:         item.ID,
				ArtistName: item.Attributes.ArtistName,
				AlbumName:  item.Attributes.AlbumName,
			})
		}
		page++
		if total == 0 && result.Meta.Total > 0 {
			total = (result.Meta.Total + limit - 1) / limit
		}
		if onProgress != nil && total > 0 {
			onProgress(page, total)
		} else if onProgress != nil {
			onProgress(page, 0)
		}
		if len(result.Data) < limit {
			break
		}
		offset += limit
	}

	return &LibrarySongsResult{Songs: all}, nil
}

// GetAllLibraryPlaylists returns every playlist in the user's library.
func (c *Client) GetAllLibraryPlaylists(ctx context.Context) ([]PlaylistSummary, error) {
	all, _, err := c.Me.GetAllLibraryPlaylists(ctx, &applemusic.PageOptions{Limit: 100})
	if err != nil {
		return nil, fmt.Errorf("failed to get library playlists: %w", err)
	}
	out := make([]PlaylistSummary, 0, len(all.Data))
	for _, p := range all.Data {
		out = append(out, PlaylistSummary{ID: p.Id, Name: p.Attributes.Name})
	}
	return out, nil
}

// SearchArtists queries the catalog for artists matching name on the given
// storefront.
func (c *Client) SearchArtists(ctx context.Context, storefront, name string) (*ArtistSearchResult, error) {
	u := fmt.Sprintf("v1/catalog/%s/search?types=artists&term=%s&limit=1", storefront, url.QueryEscape(name))
	req, err := c.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	var result struct {
		Results struct {
			Artists struct {
				Data []struct {
					ID         string `json:"id"`
					Attributes struct {
						Name string `json:"name"`
					} `json:"attributes"`
				} `json:"data"`
			} `json:"artists"`
		} `json:"results"`
	}
	resp, err := c.Do(ctx, req, &result)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned %d", resp.StatusCode)
	}
	out := &ArtistSearchResult{}
	for _, a := range result.Results.Artists.Data {
		out.Artists = append(out.Artists, ArtistSearchEntry{ID: a.ID, Name: a.Attributes.Name})
	}
	return out, nil
}

func (c *Client) GetAllLibraryArtists(ctx context.Context, limit int, onProgress func(page, total int)) (*LibraryArtists, error) {
	var all []LibraryArtist
	offset := 0
	page := 0
	total := 0

	for {
		u := fmt.Sprintf("v1/me/library/artists?limit=%d&offset=%d&include=catalog", limit, offset)

		var result *LibraryArtists
		var resp *applemusic.Response
		var err error

		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
				}
			}
			req, reqErr := c.NewRequest("GET", u, nil)
			if reqErr != nil {
				return nil, fmt.Errorf("failed to create request: %w", reqErr)
			}
			result = &LibraryArtists{}
			resp, err = c.Do(ctx, req, result)
			if err == nil && resp.StatusCode == http.StatusOK {
				break
			}
			if resp != nil && resp.StatusCode >= 500 {
				continue
			}
			if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("API request failed: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
		}

		all = append(all, result.Data...)
		page++

		if total == 0 && result.Meta.Total > 0 {
			total = (result.Meta.Total + limit - 1) / limit
		}

		if onProgress != nil && total > 0 {
			onProgress(page, total)
		} else if onProgress != nil {
			onProgress(page, 0)
		}

		if len(result.Data) < limit {
			break
		}
		offset += limit
	}

	return &LibraryArtists{Data: all}, nil
}
