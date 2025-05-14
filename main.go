// Package main is the entry point for the GitHub Enterprise Server to GitHub Enterprise Cloud
// migration tool. It initializes logging and configuration before executing the command-line interface.
package main

import (
	"context"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/cmd"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/tracing"
)

// main initializes the application and runs the root command.
// It sets up logging, configuration, and tracing before delegating
// to the cmd package to handle command-line parsing and execution.
func main() {
	// Initialize logging first
	if err := logging.Init(); err != nil {
		// If logging initialization fails, we'll still have stdout logging
		logging.Get().Error("Failed to initialize file logging", "error", err)
	}

	// Initialize configuration
	if err := config.Init(); err != nil {
		logging.Get().Error("Failed to initialize configuration", "error", err)
		return
	}

	// Get configuration
	cfg := config.Get()

	// Initialize tracing if enabled
	if cfg.Tracing.Enabled {
		tracingCfg := tracing.Config{
			Enabled:     cfg.Tracing.Enabled,
			Endpoint:    cfg.Tracing.Endpoint,
			ServiceName: cfg.Tracing.ServiceName,
			SampleRate:  cfg.Tracing.SampleRate,
		}

		if err := tracing.Init(tracingCfg); err != nil {
			logging.Get().Error("Failed to initialize tracing", "error", err)
			// Continue execution even if tracing setup fails
		}

		// Ensure tracing is shutdown gracefully
		defer func() {
			if err := tracing.Shutdown(context.Background()); err != nil {
				logging.Get().Error("Error shutting down tracer provider", "error", err)
			}
		}()
	}

	// Execute root command
	cmd.Execute()
}
