package api

import (
	"context"
	"fmt"
	"net/http"
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
	storefront       string
	scanPageSizeHint int
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
	if pageSize <= 0 {
		pageSize = 25
	}
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
	if c.scanPageSizeHint > 0 {
		return c.scanPageSizeHint
	}
	return 25
}

type LibraryArtistAlbumsResult struct {
	Albums []applemusic.LibraryAlbum
	Total  int
}

func (c *Client) GetLibraryArtistAlbums(ctx context.Context, libraryArtistID string, limit int) (*LibraryArtistAlbumsResult, error) {
	var all []applemusic.LibraryAlbum
	offset := 0

	type albumResponse struct {
		Data []applemusic.LibraryAlbum `json:"data"`
		Next string                    `json:"next,omitempty"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}

	for {
		u := fmt.Sprintf("v1/me/library/artists/%s/albums?include=catalog&limit=%d&offset=%d", libraryArtistID, limit, offset)
		req, err := c.NewRequest("GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		result := &albumResponse{}
		resp, err := c.Do(ctx, req, result)

		if resp != nil {
			if resp.StatusCode == http.StatusNotFound {
				return &LibraryArtistAlbumsResult{}, nil
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

		if len(result.Data) < limit || len(all) >= result.Meta.Total {
			return &LibraryArtistAlbumsResult{Albums: all, Total: result.Meta.Total}, nil
		}
		offset += limit
	}
}

// PlaylistTrackResult is a single track returned by GetLibraryPlaylistCatalogTracks
// with album and artist metadata attached. Stub — fully implemented in Task 4.3.
type PlaylistTrackResult struct {
	TrackID     string
	AlbumID     string
	AlbumName   string
	ArtistName  string
	ReleaseDate string
}

// GetLibraryPlaylistCatalogTracks fetches the catalog tracks of a library playlist
// with album metadata for backfill. Stub — fully implemented in Task 4.3.
func (c *Client) GetLibraryPlaylistCatalogTracks(ctx context.Context, playlistID string, limit int) ([]PlaylistTrackResult, error) {
	return nil, nil
}

func (c *Client) GetAllLibraryArtists(ctx context.Context, limit int, onProgress ProgressCallback) (*LibraryArtists, error) {
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
