// Package logging provides a structured logging system for the application.
// It supports multiple output destinations (console with colors and JSON file logs),
// log rotation, and runtime log level adjustment.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/lmittmann/tint"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	logger    *slog.Logger
	once      sync.Once
	logLevel  = slog.LevelInfo // Default log level
	levelLock sync.RWMutex
)

// Init initializes the logging system with file and terminal output.
// It creates a rotating file logger and a colorized terminal logger.
// This function ensures the logger is only initialized once.
func Init() error {
	var err error
	once.Do(func() {
		err = setupLogger()
	})
	return err
}

// Get returns the singleton logger instance.
// If the logger hasn't been initialized yet, it will initialize it.
// In case of initialization failure, it falls back to a console-only logger.
func Get() *slog.Logger {
	if logger == nil {
		if err := Init(); err != nil {
			// Fallback to stdout logger if initialization fails
			logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
				Level:      getLogLevel(),
				TimeFormat: "15:04:05",
			}))
		}
	}
	return logger
}

// SetLevel changes the logging level at runtime.
// It safely updates the global log level and recreates the logger
// with the new level if the logger already exists.
func SetLevel(level slog.Level) {
	levelLock.Lock()
	defer levelLock.Unlock()

	// Store the new log level
	logLevel = level

	// If logger already exists, create a new one with the updated level
	if logger != nil {
		// Create handlers with the new level
		fileLogger := setupFileLogger()

		fileHandler := slog.NewJSONHandler(fileLogger, &slog.HandlerOptions{
			Level: level,
		})

		terminalHandler := tint.NewHandler(os.Stdout, &tint.Options{
			Level:      level,
			TimeFormat: "15:04:05",
		})

		// Create multi-handler
		multiHandler := NewMultiHandler(terminalHandler, fileHandler)

		// Create logger
		logger = slog.New(multiHandler)
	}
}

// setupFileLogger creates and configures a rotating file logger.
// It creates the necessary directory structure and returns a lumberjack logger
// configured with size and age-based rotation.
func setupFileLogger() *lumberjack.Logger {
	// Create logs directory
	logDir := filepath.Join(os.TempDir(), "gh-ghes-2-ghec", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil
	}

	// Setup rotating file logger
	return &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "gh-ghes-2-ghec.log"),
		MaxSize:    10, // MB
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   true,
	}
}

// getLogLevel safely returns the current log level using a read lock
// to prevent race conditions with SetLevel.
func getLogLevel() slog.Level {
	levelLock.RLock()
	defer levelLock.RUnlock()
	return logLevel
}

// ParseLevel converts a string level name to the corresponding slog.Level value.
// Supported level names are: "debug", "info", "warn", and "error".
// If an unknown level name is provided, it defaults to info.
func ParseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// setupLogger creates and configures the main logger instance.
// It sets up both file and terminal handlers with the appropriate log level.
func setupLogger() error {
	// Setup file logger
	fileLogger := setupFileLogger()
	if fileLogger == nil {
		return fmt.Errorf("failed to create logs directory")
	}

	// Get current log level
	level := getLogLevel()

	// Create handlers
	fileHandler := slog.NewJSONHandler(fileLogger, &slog.HandlerOptions{
		Level: level,
	})

	terminalHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      level,
		TimeFormat: "15:04:05",
	})

	// Create multi-handler
	multiHandler := NewMultiHandler(terminalHandler, fileHandler)

	// Create logger
	logger = slog.New(multiHandler)

	return nil
}

// MultiHandler is a custom slog.Handler implementation that distributes
// log records to multiple underlying handlers.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler creates a new MultiHandler with the provided handlers.
// Each handler will receive all log records that pass their individual level checks.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{
		handlers: handlers,
	}
}

// Enabled implements slog.Handler.Enabled.
// It returns true if any of the contained handlers are enabled for the given level.
func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle implements slog.Handler.Handle.
// It dispatches the log record to all handlers and returns the first error encountered.
func (h *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, record); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// WithAttrs implements slog.Handler.WithAttrs.
// It creates a new MultiHandler with the attributes added to each underlying handler.
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return NewMultiHandler(handlers...)
}

// WithGroup implements slog.Handler.WithGroup.
// It creates a new MultiHandler with the group added to each underlying handler.
func (h *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return NewMultiHandler(handlers...)
}
