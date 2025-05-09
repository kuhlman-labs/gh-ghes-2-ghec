package logging

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/lmittmann/tint"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	logger *slog.Logger
	once   sync.Once
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
				Level:      slog.LevelInfo,
				TimeFormat: "15:04:05",
			}))
		}
	}
	return logger
}

func setupLogger() error {
	// Create logs directory
	logDir := filepath.Join(os.TempDir(), "gh-ghes-2-ghec", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	// Setup rotating file logger
	fileLogger := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "gh-ghes-2-ghec.log"),
		MaxSize:    10, // MB
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   true,
	}

	// Create handlers
	fileHandler := slog.NewJSONHandler(fileLogger, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	terminalHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelInfo,
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
