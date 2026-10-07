package cmd

import (
	"fmt"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Apple Music",
	Long: `Starts an OAuth proxy flow to obtain a Music User Token from Apple Music.
Opens a browser window for you to sign in with your Apple ID.
Tokens are stored in the local database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		store, err := db.Open("")
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer store.Close()

		am, err := auth.NewAuthenticatorWithStore(cfg, store)
		if err != nil {
			return fmt.Errorf("failed to initialize authenticator: %w", err)
		}

		if err := am.Authenticate(cmd.Context()); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		fmt.Println("Authentication successful! Tokens cached.")
		return nil
	},
}
