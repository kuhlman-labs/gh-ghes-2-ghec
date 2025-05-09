package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/server"
	"github.com/spf13/cobra"
)

var (
	// Flag variables
	ghesToken    string
	ghCloudToken string
	webhookURL   string
	port         int
)

var rootCmd = &cobra.Command{
	Use:   "ghes-2-ghec",
	Short: "GitHub repository migration tool",
	Long:  `A tool for migrating repositories from GitHub Enterprise Server to GitHub Enterprise Cloud.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize and validate configuration
		if err := initializeConfig(); err != nil {
			return err
		}

		// Create migrator and server
		s, err := setupServer()
		if err != nil {
			return err
		}

		// Run server with graceful shutdown
		return runServerWithGracefulShutdown(s)
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
	rootCmd.Flags().StringVar(&ghesToken, "ghes-token", "", "GitHub Enterprise Server token")
	rootCmd.Flags().StringVar(&ghCloudToken, "gh-cloud-token", "", "GitHub Enterprise Cloud token")
	rootCmd.Flags().StringVar(&webhookURL, "webhook-url", "", "Webhook URL for notifications")
	rootCmd.Flags().IntVar(&port, "port", 8080, "Port to listen on")

	// Remove required flags to allow config init to work
	// We'll validate these in the RunE functions where needed
}

// initializeConfig initializes and validates the configuration
func initializeConfig() error {
	// Initialize configuration
	if err := config.Init(); err != nil {
		return fmt.Errorf("failed to initialize configuration: %w", err)
	}
	cfg := config.Get()

	// Update config with flag values
	cfg.GitHub.GHESToken = ghesToken
	cfg.GitHub.GHCloudToken = ghCloudToken
	cfg.Webhook.URL = webhookURL
	cfg.Server.Port = port

	// Validate configuration
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize clients
	if err := config.InitClients(cfg.GitHub.GHESToken, cfg.GitHub.GHCloudToken); err != nil {
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	return nil
}

// setupServer creates and configures the server
func setupServer() (*server.Server, error) {
	cfg := config.Get()

	// Create migrator
	m := migrator.New(cfg.Webhook.URL)

	// Create server
	s := server.New(cfg, m)

	return s, nil
}

// runServerWithGracefulShutdown starts the server and handles graceful shutdown
func runServerWithGracefulShutdown(s *server.Server) error {
	logger := logging.Get()
	cfg := config.Get()

	// Create context that listens for the interrupt signal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start server in a goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Starting server...")
		if err := s.Start(); err != nil && err != http.ErrServerClosed {
			serverErrors <- fmt.Errorf("server error: %w", err)
		}
	}()

	// Wait for interrupt signal or server error
	select {
	case err := <-serverErrors:
		return err
	case <-ctx.Done():
		logger.Info("Shutdown signal received")
	}

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	// Shutdown server
	if err := s.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	logger.Info("Server shutdown complete")
	return nil
}
