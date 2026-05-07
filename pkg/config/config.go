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
	Concurrency        int      `mapstructure:"concurrency"`
	MaxAlbumsPerArtist int      `mapstructure:"max_albums_per_artist"`
	IgnoredArtists     []string `mapstructure:"ignored_artists"`
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
	viper.SetConfigName("config")
	viper.SetConfigType("toml")

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		dir, err := ConfigDir()
		if err != nil {
			return nil, err
		}
		viper.AddConfigPath(dir)
	}

	viper.SetDefault("playlist.name", "Release Radar")
	viper.SetDefault("playlist.auto_create", true)
	viper.SetDefault("scan.concurrency", 5)
	viper.SetDefault("scan.max_albums_per_artist", 10)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// No config yet; proceed with defaults
		} else {
			return nil, fmt.Errorf("error reading config: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return &cfg, nil
}

func Save(cfg *Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	viper.Set("apple", cfg.Apple)
	viper.Set("playlist", cfg.Playlist)
	viper.Set("scan", cfg.Scan)

	path := filepath.Join(dir, "config.toml")
	if err := viper.WriteConfigAs(path); err != nil {
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
