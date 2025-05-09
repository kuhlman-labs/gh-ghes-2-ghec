package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	Port            int           `mapstructure:"port"`
	WebhookURL      string        `mapstructure:"webhook_url"`
	GHESToken       string        `mapstructure:"ghes_token"`
	GHCloudToken    string        `mapstructure:"gh_cloud_token"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	Clients         *Clients      // GitHub API clients
}

var (
	cfg  *Config
	once sync.Once
)

// Init initializes the configuration
func Init() error {
	var err error
	once.Do(func() {
		cfg = &Config{}
		err = loadConfig()
	})
	return err
}

// Get returns the configuration instance
func Get() *Config {
	return cfg
}

func loadConfig() error {
	// Set default values
	viper.SetDefault("port", 8080)
	viper.SetDefault("shutdown_timeout", 30*time.Second)

	// Read from environment variables
	viper.SetEnvPrefix("GH_REPO_MIGRATE")
	viper.AutomaticEnv()

	// Read from config file if it exists
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get user config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "gh-repo-migrate")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configPath)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	}

	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return nil
}
