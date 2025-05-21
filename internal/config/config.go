// Package config handles loading, validating, and managing application configuration.
// It supports configuration from files, environment variables, and default values.
package config

import (
	"fmt"
	"os"
	"sync"
	"time"

	"errors"

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
	GitHub  GitHubConfig  `mapstructure:"github"`
	Clients *Clients      // GitHub API clients
	Tracing struct {
		Enabled     bool    `mapstructure:"enabled"`
		Endpoint    string  `mapstructure:"endpoint"`
		ServiceName string  `mapstructure:"service_name"`
		SampleRate  float64 `mapstructure:"sample_rate"`
	} `mapstructure:"tracing"`
	Metrics struct {
		Enabled     bool   `mapstructure:"enabled"`
		Port        int    `mapstructure:"port"`
		Path        string `mapstructure:"path"`
		ServiceName string `mapstructure:"service_name"`
	} `mapstructure:"metrics"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Queue     QueueConfig     `mapstructure:"queue"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
}

// ServerConfig holds server-specific configuration options.
// It defines network settings, timeouts, and rate limits for the HTTP server.
type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	RateLimit       int           `mapstructure:"rate_limit"` // Requests per minute, 0 means unlimited
	Dashboard       bool          `mapstructure:"dashboard"`  // Whether to enable the dashboard UI
}

// GitHubConfig holds GitHub-specific configuration.
// Contains configuration for proxy servers and other GitHub API-related settings.
type GitHubConfig struct {
	// Proxy contains configuration for HTTP proxy servers
	Proxy ProxyConfig `mapstructure:"proxy"`
}

// WebhookConfig holds webhook-specific configuration.
// It defines the URL for webhook notifications about migration status changes.
type WebhookConfig struct {
	URL string `mapstructure:"url"`
}

// LoggingConfig holds logging-specific configuration.
// It specifies the log format, level, and output destination.
type LoggingConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	TimeFormat string `mapstructure:"time_format"`
	FilePath   string `mapstructure:"file_path"`
	MaxSize    int    `mapstructure:"max_size"`    // Maximum size in MB
	MaxBackups int    `mapstructure:"max_backups"` // Maximum number of backup files
	MaxAge     int    `mapstructure:"max_age"`     // Maximum age in days
	Compress   bool   `mapstructure:"compress"`    // Compress rotated files
	Pretty     bool   `mapstructure:"pretty"`      // Use pretty output for human readability
	Color      bool   `mapstructure:"color"`       // Use colors
	WithCaller bool   `mapstructure:"with_caller"` // Include caller information
	ShowSource bool   `mapstructure:"show_source"` // Show source code location
	JSON       bool   `mapstructure:"json"`        // Output logs as JSON
	File       bool   `mapstructure:"file"`        // Output logs to file
	Console    bool   `mapstructure:"console"`     // Output logs to console
}

// StorageConfig holds storage-specific configuration.
// It defines how migration state data is persisted.
type StorageConfig struct {
	Enabled          bool   `mapstructure:"enabled"`           // Whether persistent storage is enabled
	Type             string `mapstructure:"type"`              // Storage type: sqlite, mysql, postgres
	ConnectionString string `mapstructure:"connection_string"` // Connection string or file path
	TablePrefix      string `mapstructure:"table_prefix"`      // Optional prefix for table names
	Timeout          int    `mapstructure:"timeout"`           // Timeout in seconds for database operations (0 means use default)
}

// QueueConfig holds queue-specific configuration.
// It defines parameters for the repository migration queue.
type QueueConfig struct {
	Enabled             bool `mapstructure:"enabled"`               // Whether intelligent queueing is enabled
	MaxQueueSize        int  `mapstructure:"max_queue_size"`        // Maximum number of jobs that can be queued
	MaxArchiveThreads   int  `mapstructure:"max_archive_threads"`   // Maximum number of concurrent archive generations
	MaxMigrationThreads int  `mapstructure:"max_migration_threads"` // Maximum number of concurrent migrations
	DefaultPriority     int  `mapstructure:"default_priority"`      // Default priority for migrations
	QueueStatsInterval  int  `mapstructure:"queue_stats_interval"`  // Interval in seconds for logging queue stats
}

// SchedulerConfig holds scheduler-specific configuration.
// It defines parameters for the repository migration scheduler.
type SchedulerConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	Interval   time.Duration `mapstructure:"interval"`
	MaxWorkers int           `mapstructure:"max_workers"`
}

// ConfigForWriting is used to serialize config to YAML.
// It contains a simplified representation of the Config struct
// suitable for writing to a configuration file.
type ConfigForWriting struct {
	Server struct {
		Port            int  `yaml:"port"`
		ShutdownTimeout int  `yaml:"shutdown_timeout"`
		ReadTimeout     int  `yaml:"read_timeout"`
		WriteTimeout    int  `yaml:"write_timeout"`
		RateLimit       int  `yaml:"rate_limit"` // Requests per minute, 0 means unlimited
		Dashboard       bool `yaml:"dashboard"`  // Whether to enable the dashboard UI
	} `yaml:"server"`
	Webhook struct {
		URL string `yaml:"url"`
	} `yaml:"webhook"`
	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
	GitHub struct {
		Proxy struct {
			Enabled     bool   `yaml:"enabled"`
			URL         string `yaml:"url"`
			Username    string `yaml:"username,omitempty"`
			Password    string `yaml:"password,omitempty"`
			NoProxyList string `yaml:"no_proxy_list,omitempty"`
		} `yaml:"proxy"`
	} `yaml:"github"`
	Tracing struct {
		Enabled     bool    `yaml:"enabled"`
		Endpoint    string  `yaml:"endpoint"`
		ServiceName string  `yaml:"service_name"`
		SampleRate  float64 `yaml:"sample_rate"`
	} `yaml:"tracing"`
	Storage struct {
		Enabled          bool   `yaml:"enabled"`
		Type             string `yaml:"type"`
		ConnectionString string `yaml:"connection_string"`
		TablePrefix      string `yaml:"table_prefix"`
		Timeout          int    `yaml:"timeout"`
	} `yaml:"storage"`
	Queue struct {
		Enabled             bool `yaml:"enabled"`
		MaxQueueSize        int  `yaml:"max_queue_size"`
		MaxArchiveThreads   int  `yaml:"max_archive_threads"`
		MaxMigrationThreads int  `yaml:"max_migration_threads"`
		DefaultPriority     int  `yaml:"default_priority"`
		QueueStatsInterval  int  `yaml:"queue_stats_interval"`
	} `yaml:"queue"`
	Scheduler struct {
		Enabled    bool `yaml:"enabled"`
		Interval   int  `yaml:"interval"`
		MaxWorkers int  `yaml:"max_workers"`
	} `yaml:"scheduler"`
}

// Default configuration constants
const (
	configFileName     = "config.yaml"
	defaultPort        = 8080
	defaultTimeout     = 120 * time.Second // Increased from 60 to 120 seconds
	defaultIOTimeout   = 60 * time.Second  // Increased from 30 to 60 seconds
	defaultRateLimit   = 60                // 60 requests per minute
	defaultStorageType = "sqlite"
	defaultStoragePath = "migrations.db"
	defaultDbTimeout   = 120 // Default timeout for database operations (2 minutes)

	// Queue configuration defaults
	defaultMaxQueueSize        = 1000 // Maximum queue size
	defaultMaxArchiveThreads   = 5    // GitHub's limit for archive generation
	defaultMaxMigrationThreads = 10   // GitHub's limit for concurrent migrations
	defaultQueuePriority       = 50   // Default priority for migrations
	defaultQueueStatsInterval  = 300  // Log queue stats every 5 minutes
)

var (
	cfg  *Config
	once sync.Once
)

// GetConfigPath returns the path to the configuration file.
// It uses the current working directory to determine where the config file should be.
func GetConfigPath() (string, error) {
	return configFileName, nil
}

// Init initializes the configuration by setting up default values
// and loading values from config file and environment variables.
// It ensures the configuration is only initialized once.
func Init() error {
	var err error
	once.Do(func() {
		cfg = CreateDefaultConfig()
		err = loadConfig()
		if err != nil {
			var notFoundErr viper.ConfigFileNotFoundError
			if errors.As(err, &notFoundErr) {
				err = nil
			} else {
				err = fmt.Errorf("failed to load config: %w", err)
			}
		}
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
	viper.SetDefault("server.dashboard", true) // Default to enabled
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("storage.enabled", false)
	viper.SetDefault("storage.type", defaultStorageType)
	viper.SetDefault("storage.connection_string", defaultStoragePath)
	viper.SetDefault("storage.table_prefix", "")
	viper.SetDefault("storage.timeout", defaultDbTimeout)

	// Set default values for queue configuration
	viper.SetDefault("queue.enabled", true) // Enable smart queueing by default
	viper.SetDefault("queue.max_queue_size", defaultMaxQueueSize)
	viper.SetDefault("queue.max_archive_threads", defaultMaxArchiveThreads)
	viper.SetDefault("queue.max_migration_threads", defaultMaxMigrationThreads)
	viper.SetDefault("queue.default_priority", defaultQueuePriority)
	viper.SetDefault("queue.queue_stats_interval", defaultQueueStatsInterval)

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
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && pathErr.Err != nil && pathErr.Err.Error() == "no such file or directory" {
			// If config doesn't exist, keep using defaults that were set in Init()
			return nil
		}
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
			Dashboard:       true, // Default to enabled
		},
		Webhook: WebhookConfig{},
		Logging: LoggingConfig{
			Level: "info",
		},
		Storage: StorageConfig{
			Enabled:          false,
			Type:             defaultStorageType,
			ConnectionString: defaultStoragePath,
			TablePrefix:      "",
			Timeout:          defaultDbTimeout,
		},
		Queue: QueueConfig{
			Enabled:             true,
			MaxQueueSize:        defaultMaxQueueSize,
			MaxArchiveThreads:   defaultMaxArchiveThreads,
			MaxMigrationThreads: defaultMaxMigrationThreads,
			DefaultPriority:     defaultQueuePriority,
			QueueStatsInterval:  defaultQueueStatsInterval,
		},
		Scheduler: SchedulerConfig{
			Enabled:    true,
			Interval:   defaultTimeout,
			MaxWorkers: 10,
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
	writeCfg.Server.Dashboard = cfg.Server.Dashboard
	writeCfg.Webhook.URL = cfg.Webhook.URL
	writeCfg.Logging.Level = cfg.Logging.Level
	writeCfg.GitHub.Proxy.Enabled = cfg.GitHub.Proxy.Enabled
	writeCfg.GitHub.Proxy.URL = cfg.GitHub.Proxy.URL
	writeCfg.GitHub.Proxy.Username = cfg.GitHub.Proxy.Username
	writeCfg.GitHub.Proxy.Password = cfg.GitHub.Proxy.Password
	writeCfg.GitHub.Proxy.NoProxyList = cfg.GitHub.Proxy.NoProxyList
	writeCfg.Tracing.Enabled = cfg.Tracing.Enabled
	writeCfg.Tracing.Endpoint = cfg.Tracing.Endpoint
	writeCfg.Tracing.ServiceName = cfg.Tracing.ServiceName
	writeCfg.Tracing.SampleRate = cfg.Tracing.SampleRate
	writeCfg.Storage.Enabled = cfg.Storage.Enabled
	writeCfg.Storage.Type = cfg.Storage.Type
	writeCfg.Storage.ConnectionString = cfg.Storage.ConnectionString
	writeCfg.Storage.TablePrefix = cfg.Storage.TablePrefix
	writeCfg.Storage.Timeout = cfg.Storage.Timeout
	writeCfg.Queue.Enabled = cfg.Queue.Enabled
	writeCfg.Queue.MaxQueueSize = cfg.Queue.MaxQueueSize
	writeCfg.Queue.MaxArchiveThreads = cfg.Queue.MaxArchiveThreads
	writeCfg.Queue.MaxMigrationThreads = cfg.Queue.MaxMigrationThreads
	writeCfg.Queue.DefaultPriority = cfg.Queue.DefaultPriority
	writeCfg.Queue.QueueStatsInterval = cfg.Queue.QueueStatsInterval
	writeCfg.Scheduler.Enabled = cfg.Scheduler.Enabled
	writeCfg.Scheduler.Interval = int(cfg.Scheduler.Interval.Seconds())
	writeCfg.Scheduler.MaxWorkers = cfg.Scheduler.MaxWorkers

	return writeCfg
}
