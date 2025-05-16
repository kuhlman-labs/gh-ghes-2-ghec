package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
)

func TestCircuitProtectedGhesOperation(t *testing.T) {
	logger := slog.Default()

	// Create mock clients
	clients := &config.Clients{}

	// Create API client with circuit breaker config for testing
	ghAPI := &GitHubAPI{
		clients:     clients,
		logger:      logger,
		retryConfig: utils.DefaultRetryConfig(logger),
		ghesCircuitBreaker: utils.NewCircuitBreaker(
			utils.DefaultCircuitConfig("test-ghes", logger).
				WithFailureThreshold(2). // Trip after just 2 failures for faster test
				WithResetTimeout(100 * time.Millisecond),
		),
		ghCloudCircuitBreaker: utils.NewCircuitBreaker(
			utils.DefaultCircuitConfig("test-ghcloud", logger).
				WithFailureThreshold(2). // Trip after just 2 failures for faster test
				WithResetTimeout(100 * time.Millisecond),
		),
	}

	ctx := context.Background()

	// Test 1: Successful operation
	err := ghAPI.circuitProtectedGhesOperation(ctx, "test_op", func() error {
		return nil
	})
	assert.NoError(t, err)

	// Test 2: Trigger circuit breaker by exceeding failure threshold
	for i := 0; i < 3; i++ {
		err := ghAPI.circuitProtectedGhesOperation(ctx, "test_op", func() error {
			return errors.New("test failure")
		})
		assert.Error(t, err)
	}

	// Verify circuit is open (should get circuit error, not our test error)
	err = ghAPI.circuitProtectedGhesOperation(ctx, "test_op", func() error {
		// This should not execute
		t.Fail()
		return nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker 'test-ghes' is open")

	// Test 3: Wait for circuit to transition to half-open state
	time.Sleep(150 * time.Millisecond)

	// First call should be allowed to proceed
	err = ghAPI.circuitProtectedGhesOperation(ctx, "test_op", func() error {
		return nil
	})
	assert.NoError(t, err)

	// Circuit should be in half-open state, verify second successful call closes circuit
	err = ghAPI.circuitProtectedGhesOperation(ctx, "test_op", func() error {
		return nil
	})
	assert.NoError(t, err)

	// After success, circuit should close
	assert.Equal(t, utils.StateClosed, ghAPI.ghesCircuitBreaker.GetState())
}

func TestCircuitProtectedHTTP(t *testing.T) {
	logger := slog.Default()

	// Create API client with circuit breaker config for testing
	ghAPI := &GitHubAPI{
		logger:      logger,
		retryConfig: utils.DefaultRetryConfig(logger).WithMaxRetries(1), // Fast test with 1 retry
		ghCloudCircuitBreaker: utils.NewCircuitBreaker(
			utils.DefaultCircuitConfig("test-ghcloud-http", logger).
				WithFailureThreshold(2). // Trip after just 2 failures
				WithResetTimeout(100 * time.Millisecond).
				WithHalfOpenSuccessThreshold(1), // Only need 1 success to close circuit (for faster test)
		),
	}

	ctx := context.Background()

	// Create a test HTTP server that counts requests
	requestCount := 0
	responseBody := "Success"
	responseStatus := http.StatusInternalServerError

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Return the current configured status and body
		w.WriteHeader(responseStatus)
		_, _ = fmt.Fprintln(w, responseBody)
	}))
	defer server.Close()

	// Create HTTP client
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Wrap with circuit breaker
	executeRequest := ghAPI.circuitProtectedGhCloudHTTP(client, "test_http")

	// Test 1: First request fails with server error
	responseStatus = http.StatusInternalServerError
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := executeRequest(req)
	assert.Error(t, err) // Should fail with 500 error
	assert.Nil(t, resp)  // Response should be nil when there's an error

	// Test 2: Second request also fails, should trip the circuit after this
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err = executeRequest(req)
	assert.Error(t, err) // Should fail with 500 error
	assert.Nil(t, resp)  // Response should be nil when there's an error

	// Test 3: Circuit should be open now, verify circuit rejects
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err = executeRequest(req)
	assert.Error(t, err) // Should fail with circuit open error
	assert.Nil(t, resp)  // Response should be nil when there's an error
	assert.Contains(t, err.Error(), "circuit breaker 'test-ghcloud-http' is open")

	// Test 4: Wait for circuit to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Set server to return success
	responseStatus = http.StatusOK

	// First call should be allowed and should succeed
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err = executeRequest(req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	if resp != nil {
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		if resp.Body != nil {
			_ = resp.Body.Close() // Close the body to avoid resource leaks
		}
	}

	// Verify circuit has returned to closed state (since we set HalfOpenSuccessThreshold=1)
	assert.Equal(t, utils.StateClosed, ghAPI.ghCloudCircuitBreaker.GetState())
}
