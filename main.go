// Package main is the entry point for the GitHub Enterprise Server to GitHub Enterprise Cloud
// migration tool. It initializes logging and configuration before executing the command-line interface.
package main

import (
	"github.com/kuhlman-labs/gh-ghes-2-ghec/cmd"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
)

// main initializes the application and runs the root command.
// It sets up logging and configuration before delegating to the cmd package
// to handle command-line parsing and execution.
func main() {
	// Initialize logging
	if err := logging.Init(); err != nil {
		// If logging initialization fails, we'll still have stdout logging
		logging.Get().Error("Failed to initialize file logging", "error", err)
	}

	// Initialize configuration
	if err := config.Init(); err != nil {
		logging.Get().Error("Failed to initialize configuration", "error", err)
		return
	}

	// Execute root command
	cmd.Execute()
}
