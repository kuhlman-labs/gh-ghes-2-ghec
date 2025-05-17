package utils

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultRetryConfig(t *testing.T) {
	logger := slog.Default()
	config := DefaultRetryConfig(logger)

	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, time.Second, config.InitialInterval)
	assert.Equal(t, 30*time.Second, config.MaxInterval)
	assert.Equal(t, 2.0, config.Factor)
	assert.Equal(t, logger, config.Logger)
}

func TestRetryConfig_WithMaxRetries(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	newConfig := config.WithMaxRetries(5)

	assert.Equal(t, 5, newConfig.MaxRetries)
	assert.Equal(t, config, newConfig) // Should return same instance
}

func TestRetryConfig_WithInitialInterval(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	newInterval := 2 * time.Second
	newConfig := config.WithInitialInterval(newInterval)

	assert.Equal(t, newInterval, newConfig.InitialInterval)
	assert.Equal(t, config, newConfig) // Should return same instance
}

func TestRetryConfig_WithMaxInterval(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	newInterval := 60 * time.Second
	newConfig := config.WithMaxInterval(newInterval)

	assert.Equal(t, newInterval, newConfig.MaxInterval)
	assert.Equal(t, config, newConfig) // Should return same instance
}

func TestRetryConfig_WithFactor(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	newFactor := 1.5
	newConfig := config.WithFactor(newFactor)

	assert.Equal(t, newFactor, newConfig.Factor)
	assert.Equal(t, config, newConfig) // Should return same instance
}

func TestRetry_Success(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	ctx := context.Background()
	attempts := 0

	err := Retry(ctx, config, "test", func() error {
		attempts++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, attempts)
}

func TestRetry_SuccessAfterRetries(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	ctx := context.Background()
	attempts := 0
	maxAttempts := 2

	err := Retry(ctx, config, "test", func() error {
		attempts++
		if attempts < maxAttempts {
			return errors.New("temporary error")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, maxAttempts, attempts)
}

func TestRetry_MaxRetriesExceeded(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	ctx := context.Background()
	attempts := 0
	expectedErr := errors.New("transient network error")

	err := Retry(ctx, config, "test", func() error {
		attempts++
		return expectedErr
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), expectedErr.Error())
	assert.Equal(t, config.MaxRetries+1, attempts)
}

func TestRetry_ContextCancellation(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	// Cancel context after first attempt
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, config, "test", func() error {
		attempts++
		return errors.New("temporary error")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
	assert.True(t, attempts <= 2) // Should not exceed 2 attempts due to cancellation
}

func TestRetry_BackoffCalculation(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	config.InitialInterval = 100 * time.Millisecond
	config.MaxInterval = 1 * time.Second
	config.Factor = 2.0

	ctx := context.Background()
	attempts := 0
	startTime := time.Now()

	err := Retry(ctx, config, "test", func() error {
		attempts++
		if attempts == 1 {
			return errors.New("first attempt")
		}
		return nil
	})

	elapsed := time.Since(startTime)
	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.True(t, elapsed >= config.InitialInterval, "Should wait at least initial interval")
	assert.True(t, elapsed <= config.MaxInterval, "Should not exceed max interval")
}

func TestRetry_ZeroMaxRetries(t *testing.T) {
	config := DefaultRetryConfig(slog.Default())
	config.MaxRetries = 0
	ctx := context.Background()
	attempts := 0

	err := Retry(ctx, config, "test", func() error {
		attempts++
		return errors.New("error")
	})

	assert.Error(t, err)
	assert.Equal(t, 1, attempts) // Should only try once with zero max retries
}

func TestRetry_CustomConfig(t *testing.T) {
	logger := slog.Default()
	config := &RetryConfig{
		MaxRetries:      2,
		InitialInterval: 50 * time.Millisecond,
		MaxInterval:     200 * time.Millisecond,
		Factor:          1.5,
		Logger:          logger,
	}

	ctx := context.Background()
	attempts := 0
	startTime := time.Now()

	err := Retry(ctx, config, "test", func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	elapsed := time.Since(startTime)
	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
	assert.True(t, elapsed >= config.InitialInterval, "Should wait at least initial interval")
	assert.True(t, elapsed <= config.MaxInterval*2, "Should not exceed max interval too much")
}

// TestRetryMiddleware_Success tests that the RetryMiddleware successfully handles a request without retries
func TestRetryMiddleware_Success(t *testing.T) {
	// Create a test server that always succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	// Create the retry config
	config := DefaultRetryConfig(slog.Default())

	// Create a client with the retry middleware
	client := &http.Client{}
	executeRequest := RetryMiddleware(client, config, "test_request")

	// Create a request
	req, err := http.NewRequest("GET", server.URL, nil)
	assert.NoError(t, err)

	// Execute the request
	resp, err := executeRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read the body
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "success", string(body))
}

// TestRetryMiddleware_ServerError tests that the RetryMiddleware retries on server errors
func TestRetryMiddleware_ServerError(t *testing.T) {
	attempts := 0
	maxAttempts := 3

	// Create a test server that fails with 500 a few times, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < maxAttempts {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success after retry"))
	}))
	defer server.Close()

	// Create the retry config with a short interval to speed up the test
	config := DefaultRetryConfig(slog.Default()).
		WithInitialInterval(50 * time.Millisecond).
		WithMaxInterval(200 * time.Millisecond)

	// Create a client with the retry middleware
	client := &http.Client{}
	executeRequest := RetryMiddleware(client, config, "test_retry_server_error")

	// Create a request
	req, err := http.NewRequest("GET", server.URL, nil)
	assert.NoError(t, err)

	// Execute the request
	resp, err := executeRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read the body
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "success after retry", string(body))
	assert.Equal(t, maxAttempts, attempts)
}

// TestRetryMiddleware_RateLimited tests that the RetryMiddleware retries on rate limit errors
func TestRetryMiddleware_RateLimited(t *testing.T) {
	attempts := 0
	maxAttempts := 2

	// Create a test server that fails with 429 a few times, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < maxAttempts {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success after rate limit"))
	}))
	defer server.Close()

	// Create the retry config with a short interval to speed up the test
	config := DefaultRetryConfig(slog.Default()).
		WithInitialInterval(50 * time.Millisecond).
		WithMaxInterval(200 * time.Millisecond)

	// Create a client with the retry middleware
	client := &http.Client{}
	executeRequest := RetryMiddleware(client, config, "test_retry_rate_limit")

	// Create a request
	req, err := http.NewRequest("GET", server.URL, nil)
	assert.NoError(t, err)

	// Execute the request
	resp, err := executeRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read the body
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "success after rate limit", string(body))
	assert.Equal(t, maxAttempts, attempts)
}

// TestRetryMiddleware_MaxRetries tests that the RetryMiddleware gives up after max retries
func TestRetryMiddleware_MaxRetries(t *testing.T) {
	attempts := 0

	// Create a test server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Create the retry config with a short interval to speed up the test
	config := DefaultRetryConfig(slog.Default()).
		WithMaxRetries(2). // 3 total attempts (1 initial + 2 retries)
		WithInitialInterval(50 * time.Millisecond).
		WithMaxInterval(200 * time.Millisecond)

	// Create a client with the retry middleware
	client := &http.Client{}
	executeRequest := RetryMiddleware(client, config, "test_max_retries")

	// Create a request
	req, err := http.NewRequest("GET", server.URL, nil)
	assert.NoError(t, err)

	// Execute the request
	resp, err := executeRequest(req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, config.MaxRetries+1, attempts) // Initial attempt + retries
}

// TestRetryMiddleware_BodyPreservation tests that the request body is preserved across retries
func TestRetryMiddleware_BodyPreservation(t *testing.T) {
	requestBodies := []string{}
	attempts := 0
	maxAttempts := 2

	// Create a test server that records request bodies and fails initially
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++

		// Read and record the request body
		body, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(body))

		if attempts < maxAttempts {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create the retry config with a short interval to speed up the test
	config := DefaultRetryConfig(slog.Default()).
		WithInitialInterval(50 * time.Millisecond).
		WithMaxInterval(200 * time.Millisecond)

	// Create a client with the retry middleware
	client := &http.Client{}
	executeRequest := RetryMiddleware(client, config, "test_body_preservation")

	// Create a string reader that can be cloned
	bodyContent := "test request body"
	bodyReader := strings.NewReader(bodyContent)

	// Create a request with a body
	req, err := http.NewRequest("POST", server.URL, bodyReader)
	assert.NoError(t, err)

	// Set GetBody function to allow body reuse
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(bodyContent)), nil
	}

	// Execute the request
	resp, err := executeRequest(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Verify each attempt had the same body
	assert.Equal(t, maxAttempts, len(requestBodies))
	for _, body := range requestBodies {
		assert.Equal(t, bodyContent, body)
	}
}

func TestIsPermanentError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		isPermanent bool
	}{
		{
			name:        "Repository already exists error",
			err:         errors.New("Failed to create repository: A repository called org/repo already exists"),
			isPermanent: true,
		},
		{
			name:        "Repository conflict error",
			err:         errors.New("Repository conflict detected"),
			isPermanent: true,
		},
		{
			name:        "Authentication error",
			err:         errors.New("Unauthorized access: invalid token"),
			isPermanent: true,
		},
		{
			name:        "Permission error",
			err:         errors.New("Permission denied to access resource"),
			isPermanent: true,
		},
		{
			name:        "Resource not found",
			err:         errors.New("Repository not found"),
			isPermanent: true,
		},
		{
			name:        "Bad request error",
			err:         errors.New("Bad request: invalid parameter"),
			isPermanent: true,
		},
		{
			name:        "Status code 404",
			err:         errors.New("HTTP request failed with status code: 404"),
			isPermanent: true,
		},
		{
			name:        "Status code 401",
			err:         errors.New("HTTP request failed with status code: 401"),
			isPermanent: true,
		},
		{
			name:        "Transient network error",
			err:         errors.New("Failed to connect: timeout"),
			isPermanent: false,
		},
		{
			name:        "Rate limit error",
			err:         errors.New("Rate limit exceeded, retry after 60 seconds"),
			isPermanent: false,
		},
		{
			name:        "Internal server error",
			err:         errors.New("Internal server error"),
			isPermanent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPermanentError(tt.err)
			if result != tt.isPermanent {
				t.Errorf("isPermanentError(%v) = %v, want %v", tt.err, result, tt.isPermanent)
			}
		})
	}
}
