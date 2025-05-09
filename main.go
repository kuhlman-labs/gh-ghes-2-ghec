package main

import (
	"os"

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

	// Skip config initialization for "config init" command
	args := os.Args
	skipConfig := len(args) >= 3 && args[1] == "config" && args[2] == "init"

	if !skipConfig {
		// Initialize configuration
		if err := config.Init(); err != nil {
			logging.Get().Error("Failed to initialize configuration", "error", err)
			return
		}
	}

	// Execute root command
	cmd.Execute()
}
