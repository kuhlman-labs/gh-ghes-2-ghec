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
	logLevel     string

	// Flag state tracking
	logLevelFlagSet bool
)

var rootCmd = &cobra.Command{
	Use:   "ghes-2-ghec",
	Short: "GitHub repository migration tool",
	Long:  `A tool for migrating repositories from GitHub Enterprise Server to GitHub Enterprise Cloud.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Track flag state
		logLevelFlagSet = cmd.Flags().Changed("log-level")

		// Initialize logging with the specified level
		if err := initializeLogging(); err != nil {
			return err
		}

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
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "Logging level (debug, info, warn, error)")

	// Remove required flags to allow config init to work
	// We'll validate these in the RunE functions where needed
}

// initializeLogging sets up the logging system with the specified level
func initializeLogging() error {
	// Initialize logging
	if err := logging.Init(); err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}

	// Initialize basic configuration to get the log level from config if not set by flag
	if err := config.Init(); err != nil {
		return fmt.Errorf("failed to initialize configuration: %w", err)
	}

	// If log level is not set via flag, use it from config
	if logLevelFlagSet {
		// Flag was explicitly set, use that value
		level := logging.ParseLevel(logLevel)
		logging.SetLevel(level)
	} else {
		// Flag was not set, use config value
		cfg := config.Get()
		level := logging.ParseLevel(cfg.Logging.Level)
		logging.SetLevel(level)
		// Update the flag value to match config
		logLevel = cfg.Logging.Level
	}

	// Get the logger
	logger := logging.Get()

	// Log the selected level
	logger.Info("Logging initialized", "level", logLevel)

	return nil
}

// initializeConfig initializes and validates the configuration
func initializeConfig() error {
	// Get the logger
	logger := logging.Get()

	// Configuration is already initialized in initializeLogging
	cfg := config.Get()

	// Update config with flag values if provided
	if ghesToken != "" {
		cfg.GitHub.GHESToken = ghesToken
	}
	if ghCloudToken != "" {
		cfg.GitHub.GHCloudToken = ghCloudToken
	}
	if webhookURL != "" {
		cfg.Webhook.URL = webhookURL
	}
	if port != 8080 {
		cfg.Server.Port = port
	}
	// Update the logging level in config if set via flag
	if logLevelFlagSet {
		cfg.Logging.Level = logLevel
	}

	// Log config (without tokens)
	logger.Debug("Configuration loaded",
		"port", cfg.Server.Port,
		"webhook_configured", cfg.Webhook.URL != "",
		"log_level", cfg.Logging.Level,
	)

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
