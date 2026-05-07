package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/harrisonoest/release-radar/pkg/config"
)

type Artist struct {
	ID        string   `json:"id"`
	CatalogID string   `json:"catalog_id"`
	Name      string   `json:"name"`
	Href      string   `json:"href"`
	Genres    []string `json:"genres,omitempty"`
	LastSeen  string   `json:"last_seen"`
}

type ArtistCache struct {
	Artists []Artist `json:"artists"`
}

func LoadArtists() (*ArtistCache, error) {
	path, err := artistsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ArtistCache{}, nil
		}
		return nil, fmt.Errorf("cannot read artists cache: %w", err)
	}

	var cache ArtistCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("cannot parse artists cache: %w", err)
	}
	return &cache, nil
}

func SaveArtists(cache *ArtistCache) error {
	path, err := artistsPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func artistsPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create cache directory: %w", err)
	}
	return filepath.Join(dir, "artists.json"), nil
}
