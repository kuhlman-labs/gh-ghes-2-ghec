// Package config handles loading, validating, and managing application configuration.
// It supports configuration from files, environment variables, and default values.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the application.
// It contains server settings, webhook configuration, logging preferences,
// and client configurations for connecting to GitHub APIs.
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Webhook WebhookConfig `mapstructure:"webhook"`
	Logging LoggingConfig `mapstructure:"logging"`
	Clients *Clients      // GitHub API clients
}

// ServerConfig holds server-specific configuration options.
// It defines network settings, timeouts, and rate limits for the HTTP server.
type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	RateLimit       int           `mapstructure:"rate_limit"` // Requests per minute, 0 means unlimited
}

// GitHubConfig holds GitHub-specific configuration.
// Not used for tokens anymore as they come from payload.
type GitHubConfig struct {
}

// WebhookConfig holds webhook-specific configuration.
// It defines the URL for webhook notifications about migration status changes.
type WebhookConfig struct {
	URL string `mapstructure:"url"`
}

// LoggingConfig holds logging-specific configuration.
// It defines the verbosity level for application logs.
type LoggingConfig struct {
	Level string `mapstructure:"level"`
}

// ConfigForWriting is used to serialize config to YAML.
// It contains a simplified representation of the Config struct
// suitable for writing to a configuration file.
type ConfigForWriting struct {
	Server struct {
		Port            int `yaml:"port"`
		ShutdownTimeout int `yaml:"shutdown_timeout"`
		ReadTimeout     int `yaml:"read_timeout"`
		WriteTimeout    int `yaml:"write_timeout"`
		RateLimit       int `yaml:"rate_limit"` // Requests per minute, 0 means unlimited
	} `yaml:"server"`
	Webhook struct {
		URL string `yaml:"url"`
	} `yaml:"webhook"`
	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

// Default configuration constants
const (
	configFileName   = "config.yaml"
	defaultPort      = 8080
	defaultTimeout   = 30 * time.Second
	defaultIOTimeout = 15 * time.Second
	defaultRateLimit = 60 // 60 requests per minute
)

var (
	cfg  *Config
	once sync.Once
)

// GetConfigPath returns the path to the configuration file.
// It uses the current working directory to determine where the config file should be.
func GetConfigPath() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return filepath.Join(currentDir, configFileName), nil
}

// Init initializes the configuration by setting up default values
// and loading values from config file and environment variables.
// It ensures the configuration is only initialized once.
func Init() error {
	var err error
	once.Do(func() {
		cfg = CreateDefaultConfig()
		err = loadConfig()
	})
	return err
}

// Get returns the configuration instance.
// It panics if the configuration has not been initialized.
func Get() *Config {
	if cfg == nil {
		panic("config not initialized")
	}
	return cfg
}

// Validate checks if the configuration is valid for running the application.
// It verifies that required fields have appropriate values.
func Validate() error {
	if cfg.Server.Port <= 0 {
		return fmt.Errorf("invalid port number")
	}
	return nil
}

// loadConfig loads configuration from environment variables and config file.
// It sets default values, then overrides them with configuration from the file
// and environment variables. If the config file doesn't exist, it uses the defaults.
func loadConfig() error {
	// Set default values
	viper.SetDefault("server.port", defaultPort)
	viper.SetDefault("server.shutdown_timeout", defaultTimeout)
	viper.SetDefault("server.read_timeout", defaultIOTimeout)
	viper.SetDefault("server.write_timeout", defaultIOTimeout)
	viper.SetDefault("server.rate_limit", defaultRateLimit)
	viper.SetDefault("logging.level", "info")

	// Read from environment variables
	viper.SetEnvPrefix("GH_REPO_MIGRATE")
	viper.AutomaticEnv()

	// Get config path
	configPath, err := GetConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	// Set up viper
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Try to read config, but don't error if it doesn't exist
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		// If config doesn't exist, keep using defaults that were set in Init()
		return nil
	}

	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return nil
}

// WriteConfig writes the configuration to a file.
// It converts the Config to a format suitable for writing to YAML
// and writes it to the provided file with proper indentation.
func WriteConfig(cfg *Config, file *os.File) error {
	// Convert config to writable format
	writeCfg := convertToWritable(cfg)

	// Create YAML encoder
	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)

	// Write config
	if err := encoder.Encode(writeCfg); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	return nil
}

// CreateDefaultConfig creates a new Config instance with default values.
// This provides a baseline configuration that can be used when no
// configuration file exists or when creating a new configuration.
func CreateDefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            defaultPort,
			ShutdownTimeout: defaultTimeout,
			ReadTimeout:     defaultIOTimeout,
			WriteTimeout:    defaultIOTimeout,
			RateLimit:       defaultRateLimit,
		},
		Webhook: WebhookConfig{},
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

// convertToWritable converts a Config to ConfigForWriting format.
// This transformation is needed to properly serialize the configuration
// to YAML with the correct field names and types.
func convertToWritable(cfg *Config) ConfigForWriting {
	writeCfg := ConfigForWriting{}
	writeCfg.Server.Port = cfg.Server.Port
	writeCfg.Server.ShutdownTimeout = int(cfg.Server.ShutdownTimeout.Seconds())
	writeCfg.Server.ReadTimeout = int(cfg.Server.ReadTimeout.Seconds())
	writeCfg.Server.WriteTimeout = int(cfg.Server.WriteTimeout.Seconds())
	writeCfg.Server.RateLimit = cfg.Server.RateLimit
	writeCfg.Webhook.URL = cfg.Webhook.URL
	writeCfg.Logging.Level = cfg.Logging.Level

	return writeCfg
}
