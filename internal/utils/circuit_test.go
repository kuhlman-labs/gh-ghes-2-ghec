package utils

import (
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultCircuitConfig(t *testing.T) {
	logger := slog.Default()
	config := DefaultCircuitConfig("test-circuit", logger)

	assert.Equal(t, "test-circuit", config.Name)
	assert.Equal(t, 5, config.FailureThreshold)
	assert.Equal(t, 1*time.Minute, config.ResetTimeout)
	assert.Equal(t, 2, config.HalfOpenSuccessThreshold)
	assert.Equal(t, 0, config.MaxConcurrentRequests)
	assert.Equal(t, 30*time.Second, config.RequestTimeout)
	assert.Equal(t, logger, config.Logger)
}

func TestCircuitConfig_WithMethods(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default())

	config.WithFailureThreshold(10)
	assert.Equal(t, 10, config.FailureThreshold)

	config.WithResetTimeout(2 * time.Minute)
	assert.Equal(t, 2*time.Minute, config.ResetTimeout)

	config.WithHalfOpenSuccessThreshold(3)
	assert.Equal(t, 3, config.HalfOpenSuccessThreshold)

	config.WithMaxConcurrentRequests(5)
	assert.Equal(t, 5, config.MaxConcurrentRequests)

	config.WithRequestTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, config.RequestTimeout)
}

func TestCircuitBreaker_InitialState(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default())
	cb := NewCircuitBreaker(config)

	assert.Equal(t, StateClosed, cb.GetState())
	metrics := cb.Metrics()
	assert.Equal(t, "test-circuit", metrics["name"])
	assert.Equal(t, "CLOSED", metrics["state"])
	assert.Equal(t, int64(0), metrics["total_calls"])
}

func TestCircuitBreaker_ExecuteSuccess(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default())
	cb := NewCircuitBreaker(config)

	err := cb.Execute(func() error {
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int64(1), cb.Metrics()["total_calls"])
	assert.Equal(t, int64(1), cb.Metrics()["successful_calls"])
	assert.Equal(t, int64(0), cb.Metrics()["failed_calls"])
}

func TestCircuitBreaker_ExecuteFailure(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default())
	cb := NewCircuitBreaker(config)

	expectedErr := errors.New("test error")
	err := cb.Execute(func() error {
		return expectedErr
	})

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, int64(1), cb.Metrics()["total_calls"])
	assert.Equal(t, int64(0), cb.Metrics()["successful_calls"])
	assert.Equal(t, int64(1), cb.Metrics()["failed_calls"])
	assert.Equal(t, 1, cb.Metrics()["current_failures"])
}

func TestCircuitBreaker_TripOnFailures(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(3)
	cb := NewCircuitBreaker(config)

	// State change notification
	stateChanges := make(chan CircuitState, 5)
	cb.OnStateChange(func(oldState, newState CircuitState) {
		stateChanges <- newState
	})

	// Execute with failures
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error {
			return errors.New("test error")
		})
		assert.Error(t, err)
	}

	// Third failure should trip the circuit
	assert.Equal(t, StateOpen, cb.GetState())
	assert.Equal(t, int64(3), cb.Metrics()["total_calls"])
	assert.Equal(t, int64(3), cb.Metrics()["failed_calls"])

	// Verify state change notification was received
	select {
	case state := <-stateChanges:
		assert.Equal(t, StateOpen, state)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No state change event received")
	}
}

func TestCircuitBreaker_RejectWhenOpen(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(1)
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	err := cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Attempt to execute when circuit is open
	err = cb.Execute(func() error {
		t.Fatal("Should not execute function when circuit is open")
		return nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker 'test-circuit' is open")
	assert.Equal(t, int64(1), cb.Metrics()["rejected_calls"])
}

func TestCircuitBreaker_TransitionToHalfOpen(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(1).
		WithResetTimeout(50 * time.Millisecond)
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	err := cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for reset timeout to transition to half-open
	time.Sleep(60 * time.Millisecond)

	// Verify the first call after reset timeout will be attempted (half-open state)
	executed := false
	err = cb.Execute(func() error {
		executed = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, executed)
	assert.Equal(t, StateHalfOpen, cb.GetState())
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(1).
		WithResetTimeout(50 * time.Millisecond)
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	err := cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for reset timeout to transition to half-open
	time.Sleep(60 * time.Millisecond)

	// First call fails, should go back to open
	err = cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(1).
		WithResetTimeout(50 * time.Millisecond).
		WithHalfOpenSuccessThreshold(2)
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	err := cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for reset timeout to transition to half-open
	time.Sleep(60 * time.Millisecond)

	// First successful call, should stay half-open
	err = cb.Execute(func() error {
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())
	assert.Equal(t, 1, cb.Metrics()["half_open_successes"])

	// Second successful call, should transition to closed
	err = cb.Execute(func() error {
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_RejectConcurrentHalfOpenCalls(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(1).
		WithResetTimeout(50 * time.Millisecond)
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	err := cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for reset timeout to transition to half-open
	time.Sleep(60 * time.Millisecond)

	// Start a slow request that keeps the half-open state busy
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := cb.Execute(func() error {
			time.Sleep(100 * time.Millisecond)
			return nil
		})
		assert.NoError(t, err)
	}()

	// Allow time for the first request to start
	time.Sleep(10 * time.Millisecond)

	// Try another request which should be rejected
	err = cb.Execute(func() error {
		t.Fatal("Should not execute second request in half-open state")
		return nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "half-open and already processing a request")

	// Wait for the first request to complete
	wg.Wait()
	assert.Equal(t, int64(1), cb.Metrics()["rejected_calls"])
}

func TestCircuitBreaker_MaxConcurrentRequests(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithMaxConcurrentRequests(1)
	cb := NewCircuitBreaker(config)

	// Start a slow request that occupies the circuit
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := cb.Execute(func() error {
			time.Sleep(100 * time.Millisecond)
			return nil
		})
		assert.NoError(t, err)
	}()

	// Allow time for the first request to start
	time.Sleep(10 * time.Millisecond)

	// Try another request which should be rejected due to max concurrent limit
	err := cb.Execute(func() error {
		t.Fatal("Should not execute due to max concurrent limit")
		return nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum concurrent requests exceeded")

	// Wait for the first request to complete
	wg.Wait()
}

func TestCircuitBreaker_RequestTimeout(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithRequestTimeout(50 * time.Millisecond)
	cb := NewCircuitBreaker(config)

	// Execute a request that takes too long
	err := cb.Execute(func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "request timed out")
	assert.Equal(t, int64(1), cb.Metrics()["timeout_calls"])
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(1)
	cb := NewCircuitBreaker(config)

	// Trip the circuit
	err := cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())

	// Reset the circuit
	cb.Reset()
	assert.Equal(t, StateClosed, cb.GetState())

	// Should allow new requests
	executed := false
	err = cb.Execute(func() error {
		executed = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, executed)
}

func TestCircuitBreaker_ConcurrentExecution(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(5)
	cb := NewCircuitBreaker(config)

	// Run multiple concurrent requests
	var wg sync.WaitGroup
	numRequests := 10
	wg.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer wg.Done()
			err := cb.Execute(func() error {
				time.Sleep(10 * time.Millisecond)
				if id%2 == 0 {
					return errors.New("test error")
				}
				return nil
			})
			if id%2 == 0 {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	metrics := cb.Metrics()
	assert.Equal(t, int64(numRequests), metrics["total_calls"])
	assert.Equal(t, int64(numRequests/2), metrics["successful_calls"])
	assert.Equal(t, int64(numRequests/2), metrics["failed_calls"])
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	config := DefaultCircuitConfig("test-circuit", slog.Default()).
		WithFailureThreshold(3)
	cb := NewCircuitBreaker(config)

	// Execute with failures, but not enough to trip
	for i := 0; i < 2; i++ {
		err := cb.Execute(func() error {
			return errors.New("test error")
		})
		assert.Error(t, err)
	}
	assert.Equal(t, 2, cb.Metrics()["current_failures"])

	// Execute a successful request, which should reset the failure count
	err := cb.Execute(func() error {
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, cb.Metrics()["current_failures"])

	// One more failure shouldn't trip the circuit since count was reset
	err = cb.Execute(func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, StateClosed, cb.GetState())
	assert.Equal(t, 1, cb.Metrics()["current_failures"])
}
