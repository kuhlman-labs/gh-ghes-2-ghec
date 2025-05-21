// Package cmd provides the command-line interface for the GHES to GHEC migration tool.
// It defines the root command and subcommands for migration operations, configuration,
// and utility functions.
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/server"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
	"github.com/spf13/cobra"
)

var (
	// Flag variables
	webhookURL    string
	port          int
	logLevel      string
	dashboardFlag bool

	// Flag state tracking
	logLevelFlagSet bool
)

// rootCmd is the root command for the CLI application.
// It runs the migration server and provides an HTTP API for repository migrations.
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
		if err := initializeConfig(cmd); err != nil {
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

// Execute adds all child commands to the root command and runs the CLI application.
// It handles errors and exits with a non-zero code if the command fails.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// init sets up the command-line flags and adds subcommands to the root command.
// This is called automatically by the Go runtime during package initialization.
func init() {
	// Add flags
	rootCmd.Flags().StringVar(&webhookURL, "webhook-url", "", "Global webhook URL for all migration notifications")
	rootCmd.Flags().IntVar(&port, "port", 8080, "Port to listen on")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "Logging level (debug, info, warn, error)")
	rootCmd.Flags().BoolVar(&dashboardFlag, "dashboard", true, "Enable the web dashboard UI")

	// Remove required flags to allow config init to work
	// We'll validate these in the RunE functions where needed
}

// initializeLogging sets up the logging system with the specified level.
// It initializes basic configuration to get the log level from config if not set by flag.
// Returns an error if logging initialization fails.
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

// initializeConfig initializes and validates the configuration.
// It updates the configuration with command-line flag values if provided.
// Returns an error if configuration validation fails.
func initializeConfig(cmd *cobra.Command) error {
	// Get the logger
	logger := logging.Get()

	// Configuration is already initialized in initializeLogging
	cfg := config.Get()

	// Update config with flag values if provided
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
	// Check if dashboard flag was explicitly set
	if cmd.Flags().Changed("dashboard") {
		cfg.Server.Dashboard = dashboardFlag
	}

	// Log config
	logger.Debug("Configuration loaded",
		"port", cfg.Server.Port,
		"webhook_configured", cfg.Webhook.URL != "",
		"log_level", cfg.Logging.Level,
		"dashboard", cfg.Server.Dashboard,
	)

	// Validate configuration
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	return nil
}

// setupServer creates and configures the HTTP server for handling migration requests.
// It creates a migrator instance and a server instance with the current configuration.
// Returns the configured server or an error if setup fails.
func setupServer() (*server.Server, error) {
	cfg := config.Get()
	logger := logging.Get()

	// Create HTTP client with default timeouts for various operations
	httpClient := &http.Client{
		Timeout: 30 * time.Second, // Default timeout
	}

	// Create a no-op GitHub API implementation
	// In real API calls, the migrator will create a proper API client using tokens from the request
	githubAPI := github.NewNoopAPI(logger)

	// Setup storage provider based on configuration
	var storageProvider storage.MigrationStorage
	var err error
	if cfg.Storage.Enabled {
		// Convert app config to storage config
		storageConfig := storage.NewStorageConfigFromConfig(&cfg.Storage)
		storageProvider, err = storage.NewStorageProvider(storageConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create storage provider: %w", err)
		}

		// Initialize storage
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := storageProvider.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize storage: %w", err)
		}
		logger.Info("Storage initialized successfully", "type", cfg.Storage.Type)
	} else {
		logger.Info("Storage is disabled, using in-memory storage only")
		storageProvider = &storage.NoopStorage{}
	}

	// Create migrator with dependencies
	m := migrator.NewMigrator(
		logger,          // Logger
		githubAPI,       // GitHub API client (no-op implementation)
		storageProvider, // Storage provider
		cfg.Webhook.URL, // Webhook URL
		cfg,             // Full config
		httpClient,      // HTTP client
		nil,             // Tracing provider (nil is acceptable)
	)

	// Create server
	s, err := server.New(cfg, m)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	return s, nil
}

// runServerWithGracefulShutdown starts the server and handles graceful shutdown.
// It sets up signal handling for clean termination on SIGINT or SIGTERM.
// Returns an error if the server encounters an error during startup or shutdown.
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
