package utils

import (
	"context"
	"errors"
	"log/slog"
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
	expectedErr := errors.New("permanent error")

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
