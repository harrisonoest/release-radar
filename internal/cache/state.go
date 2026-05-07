package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/harrisonoest/release-radar/pkg/config"
)

type ScanState struct {
	LastScan    string `json:"last_scan"`
	AlbumsFound int    `json:"albums_found"`
	AlbumsAdded int    `json:"albums_added"`
}

func LoadScanState() (*ScanState, error) {
	path, err := scanStatePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ScanState{}, nil
		}
		return nil, fmt.Errorf("cannot read scan state: %w", err)
	}

	var state ScanState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("cannot parse scan state: %w", err)
	}
	return &state, nil
}

func SaveScanState(state *ScanState) error {
	path, err := scanStatePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func MarkScanned(found, added int) error {
	state, err := LoadScanState()
	if err != nil {
		return err
	}

	state.LastScan = time.Now().UTC().Format(time.RFC3339)
	state.AlbumsFound = found
	state.AlbumsAdded = added

	return SaveScanState(state)
}

func scanStatePath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create cache directory: %w", err)
	}
	return filepath.Join(dir, "scan_state.json"), nil
}
