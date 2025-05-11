package logging

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  slog.Level
	}{
		{
			name:  "debug level",
			level: "debug",
			want:  slog.LevelDebug,
		},
		{
			name:  "info level",
			level: "info",
			want:  slog.LevelInfo,
		},
		{
			name:  "warn level",
			level: "warn",
			want:  slog.LevelWarn,
		},
		{
			name:  "error level",
			level: "error",
			want:  slog.LevelError,
		},
		{
			name:  "unknown level",
			level: "unknown",
			want:  slog.LevelInfo, // Default level
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLevel(tt.level)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInitAndGet(t *testing.T) {
	// Test initialization
	err := Init()
	require.NoError(t, err)

	// Test getting logger
	logger := Get()
	assert.NotNil(t, logger)

	// Test that Get returns the same instance
	logger2 := Get()
	assert.Equal(t, logger, logger2)
}

func TestSetLevel(t *testing.T) {
	// Initialize logger first
	err := Init()
	require.NoError(t, err)

	// Test setting different levels
	levels := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
	}

	for _, level := range levels {
		t.Run(level.String(), func(t *testing.T) {
			SetLevel(level)
			// Verify the level was set by checking if debug messages are enabled
			logger := Get()
			ctx := context.Background()
			assert.Equal(t, level <= slog.LevelDebug, logger.Enabled(ctx, slog.LevelDebug))
		})
	}
}

func TestMultiHandler(t *testing.T) {
	// Create test handlers
	handler1 := slog.NewTextHandler(os.Stdout, nil)
	handler2 := slog.NewTextHandler(os.Stdout, nil)

	// Create multi-handler
	multiHandler := NewMultiHandler(handler1, handler2)
	assert.NotNil(t, multiHandler)

	// Test Enabled
	ctx := context.Background()
	assert.True(t, multiHandler.Enabled(ctx, slog.LevelInfo))

	// Test WithAttrs
	attrs := []slog.Attr{slog.String("key", "value")}
	newHandler := multiHandler.WithAttrs(attrs)
	assert.NotNil(t, newHandler)
	assert.NotEqual(t, multiHandler, newHandler)

	// Test WithGroup
	groupHandler := multiHandler.WithGroup("test")
	assert.NotNil(t, groupHandler)
	assert.NotEqual(t, multiHandler, groupHandler)
}

func TestSetupFileLogger(t *testing.T) {
	// Test file logger setup
	fileLogger := setupFileLogger()
	assert.NotNil(t, fileLogger)

	// Verify log directory exists
	logDir := filepath.Join(os.TempDir(), "gh-ghes-2-ghec", "logs")
	assert.DirExists(t, logDir)

	// Verify log file exists
	logFile := filepath.Join(logDir, "gh-ghes-2-ghec.log")
	assert.FileExists(t, logFile)

	// Clean up
	os.RemoveAll(logDir)
}

func TestGetLogLevel(t *testing.T) {
	// Reset logLevel to default for test isolation
	levelLock.Lock()
	logLevel = slog.LevelInfo
	levelLock.Unlock()

	// Test default level
	level := getLogLevel()
	assert.Equal(t, slog.LevelInfo, level)

	// Test after setting level
	SetLevel(slog.LevelDebug)
	level = getLogLevel()
	assert.Equal(t, slog.LevelDebug, level)
}

func TestLoggerConcurrency(t *testing.T) {
	// Initialize logger
	err := Init()
	require.NoError(t, err)

	// Test concurrent access to logger
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			logger := Get()
			assert.NotNil(t, logger)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}
