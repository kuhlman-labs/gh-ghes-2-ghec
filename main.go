package main

import (
	"github.com/kuhlman-labs/gh-ghes-2-ghec/cmd"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
)

func main() {
	// Initialize logging
	logging.Init()

	// Initialize configuration
	if err := config.Init(); err != nil {
		logging.Get().Error("Failed to initialize configuration", "error", err)
		return
	}

	// Execute root command
	cmd.Execute()
}
