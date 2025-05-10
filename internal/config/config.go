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

// Config holds all configuration for the application
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Webhook WebhookConfig `mapstructure:"webhook"`
	Logging LoggingConfig `mapstructure:"logging"`
	Clients *Clients      // GitHub API clients
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	RateLimit       int           `mapstructure:"rate_limit"` // Requests per minute, 0 means unlimited
}

// GitHubConfig holds GitHub-specific configuration
// Not used for tokens anymore as they come from payload
type GitHubConfig struct {
}

// WebhookConfig holds webhook-specific configuration
type WebhookConfig struct {
	URL string `mapstructure:"url"`
}

// LoggingConfig holds logging-specific configuration
type LoggingConfig struct {
	Level string `mapstructure:"level"`
}

// ConfigForWriting is used to serialize config to YAML
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

// GetConfigPath returns the path to the configuration file
func GetConfigPath() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return filepath.Join(currentDir, configFileName), nil
}

// Init initializes the configuration
func Init() error {
	var err error
	once.Do(func() {
		cfg = CreateDefaultConfig()
		err = loadConfig()
	})
	return err
}

// Get returns the configuration instance
func Get() *Config {
	if cfg == nil {
		panic("config not initialized")
	}
	return cfg
}

// Validate checks if the configuration is valid for running the application
func Validate() error {
	if cfg.Server.Port <= 0 {
		return fmt.Errorf("invalid port number")
	}
	return nil
}

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

// WriteConfig writes the configuration to a file
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

// CreateDefaultConfig creates a new Config instance with default values
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

// convertToWritable converts a Config to ConfigForWriting
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
