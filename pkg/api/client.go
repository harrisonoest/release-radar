package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/minchao/go-apple-music"
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

func (c *Client) GetArtistAlbums(ctx context.Context, storefront, artistID string, limit int) (*ArtistAlbumsResult, error) {
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
		u := fmt.Sprintf("v1/catalog/%s/artists/%s/albums?limit=%d&offset=%d", storefront, artistID, limit, offset)
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
		}

		if err != nil {
			return nil, fmt.Errorf("API request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
		}

		all = append(all, result.Data...)

		if len(result.Data) < limit || len(all) >= result.Meta.Total {
			return &ArtistAlbumsResult{Albums: all, Total: result.Meta.Total}, nil
		}
		offset += limit
	}
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
