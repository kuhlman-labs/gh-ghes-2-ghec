package cmd

import (
	"fmt"
	"os"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  `Manage configuration settings for the GitHub repository migration tool.`,
	// This prevents the config command from inheriting PersistentPreRun from parent
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		fmt.Println("Config command running with no configuration initialization")
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration file",
	Long: `Initialize a configuration file with default values.
The configuration file will be created in the current directory.
You can then edit this file to customize your settings.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Starting config init command...")

		// Get config path
		configPath, err := config.GetConfigPath()
		if err != nil {
			return fmt.Errorf("failed to get config path: %w", err)
		}

		// Check if file already exists
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("config file already exists at %s", configPath)
		}

		// Create default config
		defaultConfig := config.CreateDefaultConfig()

		// Update config with flag values if provided
		if webhookURL != "" {
			defaultConfig.Webhook.URL = webhookURL
		}
		if port != 8080 {
			defaultConfig.Server.Port = port
		}

		// Create file
		file, err := os.Create(configPath)
		if err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
		defer file.Close()

		// Write config
		if err := config.WriteConfig(defaultConfig, file); err != nil {
			// Clean up the file if we failed to write
			os.Remove(configPath)
			return fmt.Errorf("failed to write config: %w", err)
		}

		fmt.Printf("Configuration file created at: %s\n", configPath)
		fmt.Println("You can now edit this file to customize your settings.")
		return nil
	},
}

func init() {
	// Add config commands
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
}
