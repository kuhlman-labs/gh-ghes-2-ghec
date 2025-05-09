package main

import (
	"github.com/kuhlman-labs/gh-ghes-2-ghec/cmd"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
)

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
