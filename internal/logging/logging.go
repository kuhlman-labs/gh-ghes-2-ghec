package logging

import (
	"log/slog"
	"os"
)

var logger *slog.Logger

// Init initializes the logger
func Init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// Get returns the logger instance
func Get() *slog.Logger {
	if logger == nil {
		Init()
	}
	return logger
}
