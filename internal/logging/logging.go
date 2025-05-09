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

// Init initializes the logging system
func Init() error {
	var err error
	once.Do(func() {
		err = setupLogger()
	})
	return err
}

// Get returns the logger instance
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

// SetLevel sets the logging level
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

// setupFileLogger creates and returns the file logger
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

// getLogLevel safely returns the current log level
func getLogLevel() slog.Level {
	levelLock.RLock()
	defer levelLock.RUnlock()
	return logLevel
}

// ParseLevel converts a string level to slog.Level
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

// MultiHandler combines multiple slog.Handler interfaces
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler creates a new MultiHandler
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{
		handlers: handlers,
	}
}

// Enabled implements slog.Handler
func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle implements slog.Handler
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

// WithAttrs implements slog.Handler
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return NewMultiHandler(handlers...)
}

// WithGroup implements slog.Handler
func (h *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return NewMultiHandler(handlers...)
}
