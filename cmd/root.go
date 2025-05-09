package cmd

import (
	"fmt"
	"os"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/server"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ghes-2-ghec",
	Short: "GitHub repository migration tool",
	Long:  `A tool for migrating repositories from GitHub Enterprise Server to GitHub Enterprise Cloud.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()

		// Initialize clients
		if err := config.InitClients(cfg.GHESToken, cfg.GHCloudToken); err != nil {
			return fmt.Errorf("failed to initialize clients: %w", err)
		}

		// Create and start server
		s := server.New(cfg.WebhookURL)
		return s.Start(cfg.Port)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// Add flags
	rootCmd.Flags().StringVar(&config.Get().GHESToken, "ghes-token", "", "GitHub Enterprise Server token")
	rootCmd.Flags().StringVar(&config.Get().GHCloudToken, "gh-cloud-token", "", "GitHub Enterprise Cloud token")
	rootCmd.Flags().StringVar(&config.Get().WebhookURL, "webhook-url", "", "Webhook URL for notifications")
	rootCmd.Flags().IntVar(&config.Get().Port, "port", 8080, "Port to listen on")

	// Mark required flags
	rootCmd.MarkFlagRequired("ghes-token")
	rootCmd.MarkFlagRequired("gh-cloud-token")
}
