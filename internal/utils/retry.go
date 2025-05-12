// Package utils provides utility functions and helpers for the migration tool.
// It includes reusable components like retry mechanisms, error handling, and
// other common operations used throughout the application.
package utils

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"time"
)

// RetryConfig holds the configuration for retry operations.
// It defines the retry behavior including intervals, backoff strategy,
// maximum attempts, and logging.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts after initial failure.
	MaxRetries int
	// InitialInterval is the delay before the first retry.
	InitialInterval time.Duration
	// MaxInterval is the upper limit for the backoff delay.
	MaxInterval time.Duration
	// Factor is the multiplier for the backoff interval after each retry.
	Factor float64
	// Logger is used for logging retry attempts and results.
	Logger *slog.Logger
}

// DefaultRetryConfig returns a default retry configuration with reasonable values.
// It configures exponential backoff with jitter for robustness in distributed systems.
//
// Parameters:
//   - logger: A structured logger for recording retry operations.
//
// Returns:
//   - *RetryConfig: A configured retry policy ready for use or customization.
func DefaultRetryConfig(logger *slog.Logger) *RetryConfig {
	return &RetryConfig{
		MaxRetries:      3,                // Retry 3 times by default
		InitialInterval: time.Second,      // Start with 1 second delay
		MaxInterval:     30 * time.Second, // Cap at 30 seconds
		Factor:          2.0,              // Double the wait time each retry
		Logger:          logger,
	}
}

// WithMaxRetries sets the maximum number of retries.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - maxRetries: The maximum number of retry attempts after the initial attempt.
//
// Returns:
//   - *RetryConfig: The updated retry configuration.
func (c *RetryConfig) WithMaxRetries(maxRetries int) *RetryConfig {
	c.MaxRetries = maxRetries
	return c
}

// WithInitialInterval sets the initial backoff interval.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - initialInterval: The delay before the first retry attempt.
//
// Returns:
//   - *RetryConfig: The updated retry configuration.
func (c *RetryConfig) WithInitialInterval(initialInterval time.Duration) *RetryConfig {
	c.InitialInterval = initialInterval
	return c
}

// WithMaxInterval sets the maximum backoff interval.
// This caps the exponential growth of the backoff to avoid extremely long delays.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - maxInterval: The maximum delay between retry attempts.
//
// Returns:
//   - *RetryConfig: The updated retry configuration.
func (c *RetryConfig) WithMaxInterval(maxInterval time.Duration) *RetryConfig {
	c.MaxInterval = maxInterval
	return c
}

// WithFactor sets the backoff multiplier.
// This determines how quickly the backoff time increases between retries.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - factor: The multiplier applied to the backoff interval after each retry.
//
// Returns:
//   - *RetryConfig: The updated retry configuration.
func (c *RetryConfig) WithFactor(factor float64) *RetryConfig {
	c.Factor = factor
	return c
}

// Retry executes the provided function with retries according to the config.
// It implements exponential backoff with jitter, and respects context cancellation.
// Operation failures and retries are logged based on the config's logger.
//
// Parameters:
//   - ctx: Context for cancellation control.
//   - config: The retry configuration to use.
//   - operation: A name for the operation being retried (for logging).
//   - fn: The function to execute with retries.
//
// Returns:
//   - error: The last error returned by the function, or nil if successful.
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
		randomValue, err := rand.Int(rand.Reader, big.NewInt(1000))
		if err != nil {
			// Fall back to a simpler approach if crypto/rand fails
			backoff = backoff + jitter/2
		} else {
			// Convert to float64 between 0 and 1
			randFloat := float64(randomValue.Int64()) / 1000.0
			backoff = backoff - jitter/2 + jitter*randFloat
		}

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
