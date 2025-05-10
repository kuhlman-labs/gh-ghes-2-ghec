package utils

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// RetryConfig holds the configuration for retry operations
type RetryConfig struct {
	MaxRetries      int           // Maximum number of retry attempts
	InitialInterval time.Duration // Initial backoff interval
	MaxInterval     time.Duration // Maximum backoff interval
	Factor          float64       // Multiplier for backoff interval after each retry
	Logger          *slog.Logger  // Logger for retry operations
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig(logger *slog.Logger) *RetryConfig {
	return &RetryConfig{
		MaxRetries:      3,                // Retry 3 times by default
		InitialInterval: time.Second,      // Start with 1 second delay
		MaxInterval:     30 * time.Second, // Cap at 30 seconds
		Factor:          2.0,              // Double the wait time each retry
		Logger:          logger,
	}
}

// WithMaxRetries sets the maximum number of retries
func (c *RetryConfig) WithMaxRetries(maxRetries int) *RetryConfig {
	c.MaxRetries = maxRetries
	return c
}

// WithInitialInterval sets the initial backoff interval
func (c *RetryConfig) WithInitialInterval(initialInterval time.Duration) *RetryConfig {
	c.InitialInterval = initialInterval
	return c
}

// WithMaxInterval sets the maximum backoff interval
func (c *RetryConfig) WithMaxInterval(maxInterval time.Duration) *RetryConfig {
	c.MaxInterval = maxInterval
	return c
}

// WithFactor sets the backoff multiplier
func (c *RetryConfig) WithFactor(factor float64) *RetryConfig {
	c.Factor = factor
	return c
}

// Retry executes the provided function with retries according to the config
func Retry(ctx context.Context, config *RetryConfig, operation string, fn func() error) error {
	var err error

	// Calculate backoff for each attempt
	nextBackoff := func(attempt int) time.Duration {
		if attempt == 0 {
			return 0
		}

		backoff := float64(config.InitialInterval) * math.Pow(config.Factor, float64(attempt-1))
		if backoff > float64(config.MaxInterval) {
			backoff = float64(config.MaxInterval)
		}

		// Add some jitter (±10%)
		jitter := 0.1 * backoff
		backoff = backoff - jitter/2 + jitter*rand.Float64()

		return time.Duration(backoff)
	}

	// Attempt the operation with retries
	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// If this is a retry, wait before the next attempt
		if attempt > 0 {
			backoff := nextBackoff(attempt)
			config.Logger.Debug("Retrying operation",
				"operation", operation,
				"attempt", attempt,
				"max_attempts", config.MaxRetries,
				"backoff_ms", backoff.Milliseconds(),
				"error", err.Error(),
			)

			// Wait for backoff period or until context is canceled
			select {
			case <-time.After(backoff):
				// Continue with retry
			case <-ctx.Done():
				return fmt.Errorf("operation %s canceled during retry: %w", operation, ctx.Err())
			}
		}

		// Attempt the operation
		err = fn()
		if err == nil {
			// Success
			if attempt > 0 {
				config.Logger.Info("Operation succeeded after retries",
					"operation", operation,
					"attempts", attempt+1,
				)
			}
			return nil
		}
	}

	// All retries failed
	config.Logger.Error("Operation failed after all retry attempts",
		"operation", operation,
		"max_attempts", config.MaxRetries+1,
		"error", err.Error(),
	)

	return fmt.Errorf("operation %s failed after %d attempts: %w", operation, config.MaxRetries+1, err)
}
