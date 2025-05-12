package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetConfigPath(t *testing.T) {
	path, err := GetConfigPath()
	require.NoError(t, err)
	expectedPath := filepath.Join(".", configFileName)
	assert.Equal(t, expectedPath, path)
}

func TestCreateDefaultConfig(t *testing.T) {
	cfg := CreateDefaultConfig()
	assert.NotNil(t, cfg)
	assert.Equal(t, defaultPort, cfg.Server.Port)
	assert.Equal(t, defaultTimeout, cfg.Server.ShutdownTimeout)
	assert.Equal(t, defaultIOTimeout, cfg.Server.ReadTimeout)
	assert.Equal(t, defaultIOTimeout, cfg.Server.WriteTimeout)
	assert.Equal(t, defaultRateLimit, cfg.Server.RateLimit)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid port",
			config: &Config{
				Server: ServerConfig{
					Port: -1,
				},
			},
			wantErr: true,
		},
		{
			name: "zero port",
			config: &Config{
				Server: ServerConfig{
					Port: 0,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = tt.config
			err := Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConvertToWritable(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            8080,
			ShutdownTimeout: 30 * time.Second,
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    15 * time.Second,
			RateLimit:       60,
		},
		Webhook: WebhookConfig{
			URL: "http://example.com/webhook",
		},
		Logging: LoggingConfig{
			Level: "debug",
		},
	}

	writeCfg := convertToWritable(cfg)
	assert.Equal(t, 8080, writeCfg.Server.Port)
	assert.Equal(t, 30, writeCfg.Server.ShutdownTimeout)
	assert.Equal(t, 15, writeCfg.Server.ReadTimeout)
	assert.Equal(t, 15, writeCfg.Server.WriteTimeout)
	assert.Equal(t, 60, writeCfg.Server.RateLimit)
	assert.Equal(t, "http://example.com/webhook", writeCfg.Webhook.URL)
	assert.Equal(t, "debug", writeCfg.Logging.Level)
}

func TestInitAndGet(t *testing.T) {
	// Test that Get panics when config is not initialized
	assert.Panics(t, func() {
		cfg = nil
		Get()
	})

	// Test successful initialization
	err := Init()
	require.NoError(t, err)

	config := Get()
	assert.NotNil(t, config)
	assert.Equal(t, defaultPort, config.Server.Port)
	assert.Equal(t, defaultTimeout, config.Server.ShutdownTimeout)
	assert.Equal(t, defaultIOTimeout, config.Server.ReadTimeout)
	assert.Equal(t, defaultIOTimeout, config.Server.WriteTimeout)
	assert.Equal(t, defaultRateLimit, config.Server.RateLimit)
	assert.Equal(t, "info", config.Logging.Level)
}

func TestWriteConfig(t *testing.T) {
	cfg := CreateDefaultConfig()

	// Create a temporary file for testing
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			t.Errorf("Failed to remove temporary file: %v", err)
		}
	}()
	defer func() {
		if err := tmpFile.Close(); err != nil {
			t.Errorf("Failed to close temporary file: %v", err)
		}
	}()

	// Test writing config
	err = WriteConfig(cfg, tmpFile)
	require.NoError(t, err)

	// Verify the file was written
	fileInfo, err := tmpFile.Stat()
	require.NoError(t, err)
	assert.Greater(t, fileInfo.Size(), int64(0))
}
