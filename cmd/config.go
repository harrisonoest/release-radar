package cmd

import (
	"fmt"

	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		path, _ := config.ConfigFilePath()
		if path != "" {
			fmt.Printf("Config file: %s\n\n", path)
		}

		fmt.Printf("Apple:\n")
		fmt.Printf("  Team ID:         %s\n", cfg.Apple.TeamID)
		fmt.Printf("  MusicKit Key ID: %s\n", cfg.Apple.MusicKitKeyID)
		fmt.Printf("  Key Path:        %s\n", cfg.Apple.MusicKitKeyPath)
		fmt.Printf("\nPlaylist:\n")
		fmt.Printf("  Name:       %s\n", cfg.Playlist.Name)
		fmt.Printf("  Auto-create: %v\n", cfg.Playlist.AutoCreate)
		if cfg.Playlist.ID != "" {
			fmt.Printf("  ID: %s\n", cfg.Playlist.ID)
		}
		fmt.Printf("\nScan:\n")
		fmt.Printf("  Concurrency:         %d\n", cfg.Scan.Concurrency)
		fmt.Printf("  Max albums/artist:   %d\n", cfg.Scan.MaxAlbumsPerArtist)
		if len(cfg.Scan.IgnoredArtists) > 0 {
			fmt.Printf("  Ignored artists:     %v\n", cfg.Scan.IgnoredArtists)
		}

		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd)
}
