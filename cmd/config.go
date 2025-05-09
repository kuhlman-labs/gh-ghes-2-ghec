package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

		// Get current directory
		currentDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Create config file in current directory
		configFile := filepath.Join(currentDir, "config.yaml")

		// Check if file already exists
		if _, err := os.Stat(configFile); err == nil {
			return fmt.Errorf("config file already exists at %s", configFile)
		}

		// Create default config
		type ServerConfig struct {
			Port            int `yaml:"port"`
			ShutdownTimeout int `yaml:"shutdown_timeout"`
			ReadTimeout     int `yaml:"read_timeout"`
			WriteTimeout    int `yaml:"write_timeout"`
		}

		type GitHubConfig struct {
			GHESToken    string `yaml:"ghes_token"`
			GHCloudToken string `yaml:"gh_cloud_token"`
		}

		type WebhookConfig struct {
			URL string `yaml:"url"`
		}

		type Config struct {
			Server  ServerConfig  `yaml:"server"`
			GitHub  GitHubConfig  `yaml:"github"`
			Webhook WebhookConfig `yaml:"webhook"`
		}

		defaultConfig := Config{
			Server: ServerConfig{
				Port:            8080,
				ShutdownTimeout: 30,
				ReadTimeout:     15,
				WriteTimeout:    15,
			},
			GitHub: GitHubConfig{
				GHESToken:    "", // Will be filled from flag if provided
				GHCloudToken: "", // Will be filled from flag if provided
			},
			Webhook: WebhookConfig{
				URL: "", // Will be filled from flag if provided
			},
		}

		// Update config with flag values if provided
		if ghesToken != "" {
			defaultConfig.GitHub.GHESToken = ghesToken
		}
		if ghCloudToken != "" {
			defaultConfig.GitHub.GHCloudToken = ghCloudToken
		}
		if webhookURL != "" {
			defaultConfig.Webhook.URL = webhookURL
		}
		if port != 8080 {
			defaultConfig.Server.Port = port
		}

		// Create file
		file, err := os.Create(configFile)
		if err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
		defer file.Close()

		// Create YAML encoder
		encoder := yaml.NewEncoder(file)
		encoder.SetIndent(2)

		// Write config
		if err := encoder.Encode(defaultConfig); err != nil {
			// Clean up the file if we failed to write
			os.Remove(configFile)
			return fmt.Errorf("failed to encode config: %w", err)
		}

		fmt.Printf("Configuration file created at: %s\n", configFile)
		fmt.Println("You can now edit this file to customize your settings.")
		return nil
	},
}

func init() {
	// Add config commands
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
}
