package cmd

import (
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
	dryRun  bool

	rootCmd = &cobra.Command{
		Use:   "release-radar",
		Short: "Track new music releases from artists in your Apple Music library",
		Long: `Release Radar scans the artists in your Apple Music library,
checks for new album releases since your last scan, and adds them
to a dedicated playlist.`,
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.config/release-radar/config.toml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview changes without modifying playlists")

	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(ignoreCmd)
	rootCmd.AddCommand(sourcesCmd)
}
