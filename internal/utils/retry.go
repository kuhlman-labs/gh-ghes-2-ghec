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
	"net/http"
	"strings"
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

// calculateBackoffWithJitter calculates backoff duration with jitter for retries.
// It uses exponential backoff with a random jitter component to prevent thundering herd problems.
//
// Parameters:
//   - config: The retry configuration
//   - attempt: Current retry attempt number (1-based)
//
// Returns:
//   - time.Duration: The backoff duration to wait before the next retry
func calculateBackoffWithJitter(config *RetryConfig, attempt int) time.Duration {
	// Calculate base backoff using exponential formula
	backoff := float64(config.InitialInterval) * math.Pow(config.Factor, float64(attempt-1))

	// Cap at max interval
	if backoff > float64(config.MaxInterval) {
		backoff = float64(config.MaxInterval)
	}

	// Add some jitter (0-10% extra) - ensure we never go below the calculated backoff
	jitter := 0.1 * backoff

	// Generate a random number between 0 and 999 using crypto/rand
	randomBig, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		// Fall back to a simpler approach if crypto/rand fails
		backoff = backoff + jitter/2
	} else {
		// Convert to float64 between 0 and 1, and only add jitter (don't subtract)
		randFloat := float64(randomBig.Int64()) / 1000.0
		backoff = backoff + jitter*randFloat
	}

	return time.Duration(backoff)
}

// Retry executes the provided function with retry logic. It will retry the function
// up to the configured maximum number of retries, with exponential backoff.
//
// Parameters:
//   - ctx: Context that can be used to cancel retries
//   - config: Retry configuration (max retries, backoff, etc.)
//   - operation: A name for the operation being retried (for logging)
//   - fn: The function to retry
//
// Returns:
//   - error: The last error from the function or nil if successful
func Retry(ctx context.Context, config *RetryConfig, operation string, fn func() error) error {
	if config == nil {
		// If no config, just execute without retry
		return fn()
	}

	var err error
	var attempt int

	for attempt = 0; attempt <= config.MaxRetries; attempt++ {
		// For the first attempt, just execute
		if attempt == 0 {
			err = fn()
			if err == nil {
				return nil
			}

			// Check if this is a permanent error that should not be retried
			if isPermanentError(err) {
				config.Logger.Debug("Not retrying permanent error",
					"operation", operation,
					"error", err.Error(),
				)
				return err
			}

			continue
		}

		// Calculate backoff with jitter for subsequent attempts
		backoff := calculateBackoffWithJitter(config, attempt)

		// Log retry
		config.Logger.Debug("Retrying operation",
			"operation", operation,
			"attempt", attempt,
			"max_attempts", config.MaxRetries,
			"backoff_ms", backoff.Milliseconds(),
			"error", err.Error(),
		)

		// Create a timer for the backoff
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("operation %s canceled during retry: %w", operation, ctx.Err())
		case <-timer.C:
			// Timer expired, proceed with retry
		}

		// Execute the function
		err = fn()
		if err == nil {
			return nil
		}

		// Check if this is a permanent error that should not be retried further
		if isPermanentError(err) {
			config.Logger.Debug("Not retrying permanent error",
				"operation", operation,
				"attempt", attempt,
				"error", err.Error(),
			)
			return err
		}
	}

	// If we get here, we've exhausted all retries
	if err != nil {
		return fmt.Errorf("operation %s failed after %d attempts: %w", operation, attempt, err)
	}

	return nil
}

// isPermanentError determines if an error should be considered permanent and not retried.
// Examples of permanent errors include authentication failures, permission issues,
// resource conflicts, and validation errors.
func isPermanentError(err error) bool {
	// Check the error message for common permanent error indications
	errMsg := strings.ToLower(err.Error())

	// Repository conflicts (already exists)
	if strings.Contains(errMsg, "already exists") ||
		strings.Contains(errMsg, "repository conflict") ||
		strings.Contains(errMsg, "conflict") {
		return true
	}

	// Authentication errors
	if strings.Contains(errMsg, "unauthorized") ||
		strings.Contains(errMsg, "authentication") ||
		strings.Contains(errMsg, "unauthenticated") ||
		strings.Contains(errMsg, "auth") {
		return true
	}

	// Permission errors
	if strings.Contains(errMsg, "permission denied") ||
		strings.Contains(errMsg, "forbidden") ||
		strings.Contains(errMsg, "access denied") {
		return true
	}

	// Resource not found errors
	if strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "404") {
		return true
	}

	// Bad request / validation errors
	if strings.Contains(errMsg, "bad request") ||
		strings.Contains(errMsg, "validation") ||
		strings.Contains(errMsg, "invalid") {
		return true
	}

	// Check for errors that explicitly indicate they are permanent
	if strings.Contains(errMsg, "permanent error") ||
		strings.Contains(errMsg, "non-retryable") {
		return true
	}

	// If we have a GitHub API error, check the status code
	if strings.Contains(errMsg, "status code") {
		// Most 4xx errors are permanent (except 429 Too Many Requests)
		if strings.Contains(errMsg, "status code: 4") && !strings.Contains(errMsg, "status code: 429") {
			return true
		}
	}

	return false
}

// RetryMiddleware creates an HTTP client middleware that adds retry capability to HTTP requests.
// It handles common transient errors and retries requests based on the provided retry configuration.
//
// Parameters:
//   - client: The base HTTP client to wrap with retry logic
//   - config: The retry configuration to use
//   - operation: A name for the operation being retried (for logging)
//
// Returns:
//   - A function that will execute an HTTP request with retries
func RetryMiddleware(client *http.Client, config *RetryConfig, operation string) func(req *http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		var resp *http.Response
		err := Retry(req.Context(), config, operation, func() error {
			// Clone the request body for each retry attempt if it's not nil
			// This is necessary because the body may be consumed by the previous attempt
			if req.Body != nil && req.GetBody != nil {
				// Use GetBody to get a fresh reader for the body
				body, err := req.GetBody()
				if err != nil {
					return fmt.Errorf("failed to get fresh request body: %w", err)
				}
				req.Body = body
			}

			var err error
			resp, err = client.Do(req)
			if err != nil {
				return err
			}

			// Treat certain status codes as retryable errors
			if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
				// For server errors and rate limiting, we want to retry
				err = fmt.Errorf("server returned status code %d", resp.StatusCode)

				// We need to drain and close the body to avoid resource leaks
				if resp.Body != nil {
					_ = resp.Body.Close()
				}

				return err
			}

			return nil
		})

		if err != nil {
			return nil, err
		}

		return resp, nil
	}
}
