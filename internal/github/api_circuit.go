package github

import (
	"context"
	"net/http"

	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
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
func (a *GitHubAPI) retryableHTTP(client *http.Client, operation string) func(req *http.Request) (*http.Response, error) {
	httpExecutor := utils.RetryMiddleware(client, a.retryConfig, operation)

	return func(req *http.Request) (*http.Response, error) {
		resp, err := httpExecutor(req)
		if err != nil {
			// Classify HTTP errors for better handling
			return resp, a.classifyHTTPError(err, req)
		}

		// Check if response status code indicates an error
		if resp != nil && resp.StatusCode >= 400 {
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
