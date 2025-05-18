package github

import (
	"context"
	"net/http"
	"strings"
	"time"

	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/metrics"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
)

// retryableOperation executes a function with retries based on the API's retry configuration.
// It logs attempts and results, and backs off exponentially between retry attempts.
// The operation name is used for logging and observability.
func (a *GitHubAPI) retryableOperation(ctx context.Context, operation string, fn func() error) error {
	return utils.Retry(ctx, a.retryConfig, operation, fn)
}

// circuitProtectedGhesOperation executes a function with circuit breaker protection for GHES API calls.
// It wraps the function with the GHES circuit breaker to prevent cascading failures.
func (a *GitHubAPI) circuitProtectedGhesOperation(ctx context.Context, operation string, fn func() error) error {
	return a.ghesCircuitBreaker.Execute(func() error {
		// Use a retry operation within the circuit breaker
		err := a.retryableOperation(ctx, operation, fn)
		if err != nil {
			// Classify the error for better handling
			return a.classifyGitHubError(err)
		}
		return nil
	})
}

// circuitProtectedGhCloudOperation executes a function with circuit breaker protection for GitHub Cloud API calls.
// It wraps the function with the GitHub Cloud circuit breaker to prevent cascading failures.
func (a *GitHubAPI) circuitProtectedGhCloudOperation(ctx context.Context, operation string, fn func() error) error {
	return a.ghCloudCircuitBreaker.Execute(func() error {
		// Use a retry operation within the circuit breaker
		err := a.retryableOperation(ctx, operation, fn)
		if err != nil {
			// Classify the error for better handling
			return a.classifyGitHubError(err)
		}
		return nil
	})
}

// retryableHTTP returns a function that executes HTTP requests with retry logic.
// It uses the RetryMiddleware from utils package to handle retries for HTTP requests.
// It also handles rate limit detection and adaptive retries.
func (a *GitHubAPI) retryableHTTP(client *http.Client, operation string) func(req *http.Request) (*http.Response, error) {
	httpExecutor := utils.RetryMiddleware(client, a.retryConfig, operation)

	return func(req *http.Request) (*http.Response, error) {
		resp, err := httpExecutor(req)

		// Process successful response to extract and log rate limit information
		if err == nil && resp != nil && resp.StatusCode < 400 {
			// Determine API type based on the host
			apiType := "UNKNOWN"
			if req.URL != nil {
				host := req.URL.Host
				if host != "" {
					if strings.Contains(host, "api.github.com") {
						apiType = "GHEC"
					} else {
						apiType = "GHES"
					}
				}
			}

			// Extract rate limit info from headers
			rateLimitInfo := getRateLimitInfoFromResponse(resp)
			if rateLimitInfo != nil && rateLimitInfo.Limit > 0 {
				// Check if rate limits are getting low and log appropriate messages
				a.checkAndLogRateLimit(rateLimitInfo, apiType, 10.0)

				// Record metric for observability
				metrics.SetGitHubRateLimit(apiType, rateLimitInfo.Remaining)
			}

			return resp, nil
		}

		// Handle error cases
		if err != nil {
			// Check for rate limit errors specifically
			isRateLimit, retryAfter, rateLimitErr := a.ShouldRetryRateLimitError(err, resp)

			if isRateLimit {
				// If this is a rate limit error with a future retry time,
				// set a context value with the retry-after duration
				if req.Context() != nil {
					ctx := req.Context()
					// Non-nil context with retry info
					a.logger.Info("Rate limit detected, applied adaptive backoff",
						"operation", operation,
						"retry_after", retryAfter.String(),
					)

					// Sleep for the retry period (normally the retry system handles this,
					// but for rate limits we want to handle it specially)
					select {
					case <-ctx.Done():
						// Context was canceled during wait
						return nil, ctx.Err()
					case <-time.After(retryAfter):
						// Wait completed, retry will be handled by the retry middleware
					}
				}

				return resp, rateLimitErr
			}

			// For non-rate-limit errors, classify and return
			return resp, a.classifyHTTPError(err, req)
		}

		// Check if response status code indicates an error
		if resp != nil && resp.StatusCode >= 400 {
			// Check specifically for rate limit status code (429)
			if resp.StatusCode == http.StatusTooManyRequests {
				// Create a new error to represent the rate limit
				err = apierrors.NewHTTPStatusError(resp.StatusCode, req.URL.String(), req.Method)

				// Process as a rate limit error
				isRateLimit, retryAfter, rateLimitErr := a.ShouldRetryRateLimitError(err, resp)

				if isRateLimit {
					a.logger.Warn("Rate limit status code detected",
						"operation", operation,
						"status_code", resp.StatusCode,
						"retry_after", retryAfter.String(),
					)

					// Sleep for the retry period
					if req.Context() != nil {
						select {
						case <-req.Context().Done():
							// Context was canceled during wait
							return nil, req.Context().Err()
						case <-time.After(retryAfter):
							// Wait completed, retry will be handled by the retry middleware
						}
					}

					return resp, rateLimitErr
				}
			}

			// For other error status codes
			err = apierrors.NewHTTPStatusError(resp.StatusCode, req.URL.String(), req.Method)
			return resp, a.classifyHTTPError(err, req)
		}

		return resp, nil
	}
}

// circuitProtectedGhesHTTP returns a function that executes HTTP requests with circuit breaker
// and retry protection for GHES API calls that use direct HTTP operations.
func (a *GitHubAPI) circuitProtectedGhesHTTP(client *http.Client, operation string) func(req *http.Request) (*http.Response, error) {
	// Get the standard retryable HTTP executor
	retryableExecutor := a.retryableHTTP(client, operation)

	// Return a function that first checks the circuit breaker state
	return func(req *http.Request) (*http.Response, error) {
		var resp *http.Response
		var err error

		// Execute within circuit breaker protection
		cbErr := a.ghesCircuitBreaker.Execute(func() error {
			resp, err = retryableExecutor(req)
			return err
		})

		if cbErr != nil {
			return nil, cbErr
		}

		return resp, nil
	}
}

// circuitProtectedGhCloudHTTP returns a function that executes HTTP requests with circuit breaker
// and retry protection for GitHub Cloud API calls that use direct HTTP operations.
func (a *GitHubAPI) circuitProtectedGhCloudHTTP(client *http.Client, operation string) func(req *http.Request) (*http.Response, error) {
	// Get the standard retryable HTTP executor
	retryableExecutor := a.retryableHTTP(client, operation)

	// Return a function that first checks the circuit breaker state
	return func(req *http.Request) (*http.Response, error) {
		var resp *http.Response
		var err error

		// Execute within circuit breaker protection
		cbErr := a.ghCloudCircuitBreaker.Execute(func() error {
			resp, err = retryableExecutor(req)
			return err
		})

		if cbErr != nil {
			return nil, cbErr
		}

		return resp, nil
	}
}
