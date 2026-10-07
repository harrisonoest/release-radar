package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Apple    AppleConfig    `mapstructure:"apple"`
	Playlist PlaylistConfig `mapstructure:"playlist"`
	Scan     ScanConfig     `mapstructure:"scan"`
}

type AppleConfig struct {
	TeamID          string `mapstructure:"team_id"`
	MusicKitKeyID   string `mapstructure:"musickit_key_id"`
	MusicKitKeyPath string `mapstructure:"musickit_key_path"`
}

type PlaylistConfig struct {
	Name       string `mapstructure:"name"`
	AutoCreate bool   `mapstructure:"auto_create"`
	ID         string `mapstructure:"id"`
}

type ScanConfig struct {
	Concurrency    int      `mapstructure:"concurrency"`
	IgnoredArtists []string `mapstructure:"ignored_artists"`
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	dir := filepath.Join(home, ".config", "release-radar")
	return dir, nil
}

func Load(cfgFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		dir, err := ConfigDir()
		if err != nil {
			return nil, err
		}
		v.AddConfigPath(dir)
	}

	v.SetDefault("playlist.name", "Release Radar")
	v.SetDefault("playlist.auto_create", true)
	v.SetDefault("scan.concurrency", 5)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// No config yet; proceed with defaults
		} else {
			return nil, fmt.Errorf("error reading config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return &cfg, nil
}

// Save writes cfg to the config file. When cfgFile is set (e.g. via
// --config) it is written there; otherwise the default config.toml in
// ConfigDir is used.
func Save(cfg *Config, cfgFile string) error {
	path := cfgFile
	if path == "" {
		dir, err := ConfigDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("cannot create config directory: %w", err)
		}
		path = filepath.Join(dir, "config.toml")
	}

	v := viper.New()
	v.SetConfigType("toml")
	v.Set("apple", cfg.Apple)
	v.Set("playlist", cfg.Playlist)
	v.Set("scan", cfg.Scan)

	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}
	return nil
}

func ConfigFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}
