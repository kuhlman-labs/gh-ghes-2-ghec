// Package utils provides utility functions and helpers for the migration tool.
// It includes reusable components like retry mechanisms, circuit breakers, error handling,
// and other common operations used throughout the application.
package utils

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker
type CircuitState string

const (
	// StateClosed represents a circuit that is allowing requests to pass through
	StateClosed CircuitState = "CLOSED"
	// StateOpen represents a circuit that is not allowing requests (it has tripped)
	StateOpen CircuitState = "OPEN"
	// StateHalfOpen represents a circuit in recovery mode, allowing a test request
	StateHalfOpen CircuitState = "HALF_OPEN"
)

// CircuitConfig holds the configuration for a circuit breaker.
// It defines parameters that control when the circuit trips and how it recovers.
type CircuitConfig struct {
	// Name is a unique identifier for this circuit breaker
	Name string
	// FailureThreshold is the number of failures that will trip the circuit
	FailureThreshold int
	// ResetTimeout is the time the circuit stays open before trying to recover
	ResetTimeout time.Duration
	// HalfOpenSuccessThreshold is successes needed in half-open state to close circuit
	HalfOpenSuccessThreshold int
	// MaxConcurrentRequests limits the number of concurrent requests (0 = unlimited)
	MaxConcurrentRequests int
	// RequestTimeout is timeout for circuit-wrapped requests (0 = no timeout)
	RequestTimeout time.Duration
	// Logger for circuit breaker events
	Logger *slog.Logger
}

// DefaultCircuitConfig returns a sensible default configuration for a circuit breaker.
// It provides reasonable values that can be customized as needed.
//
// Parameters:
//   - name: A unique name for the circuit breaker
//   - logger: A structured logger for recording circuit breaker activities
//
// Returns:
//   - *CircuitConfig: A default circuit breaker configuration
func DefaultCircuitConfig(name string, logger *slog.Logger) *CircuitConfig {
	return &CircuitConfig{
		Name:                     name,
		FailureThreshold:         5,                // Circuit trips after 5 consecutive failures
		ResetTimeout:             1 * time.Minute,  // Circuit stays open for 1 minute before recovery
		HalfOpenSuccessThreshold: 2,                // 2 successful requests needed to close circuit
		MaxConcurrentRequests:    0,                // No limit on concurrent requests
		RequestTimeout:           30 * time.Second, // 30 second timeout for requests
		Logger:                   logger,
	}
}

// WithFailureThreshold sets the number of consecutive failures required to trip the circuit.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - threshold: Number of failures that will cause the circuit to open
//
// Returns:
//   - *CircuitConfig: The updated circuit configuration
func (c *CircuitConfig) WithFailureThreshold(threshold int) *CircuitConfig {
	c.FailureThreshold = threshold
	return c
}

// WithResetTimeout sets the time the circuit stays open before moving to half-open state.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - timeout: Duration the circuit will remain open before attempting recovery
//
// Returns:
//   - *CircuitConfig: The updated circuit configuration
func (c *CircuitConfig) WithResetTimeout(timeout time.Duration) *CircuitConfig {
	c.ResetTimeout = timeout
	return c
}

// WithHalfOpenSuccessThreshold sets the number of consecutive successes in half-open
// state required to close the circuit.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - threshold: Number of successful calls needed to transition from half-open to closed
//
// Returns:
//   - *CircuitConfig: The updated circuit configuration
func (c *CircuitConfig) WithHalfOpenSuccessThreshold(threshold int) *CircuitConfig {
	c.HalfOpenSuccessThreshold = threshold
	return c
}

// WithMaxConcurrentRequests sets the maximum number of concurrent requests allowed.
// A value of 0 means no limit.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - max: Maximum number of concurrent requests (0 = unlimited)
//
// Returns:
//   - *CircuitConfig: The updated circuit configuration
func (c *CircuitConfig) WithMaxConcurrentRequests(max int) *CircuitConfig {
	c.MaxConcurrentRequests = max
	return c
}

// WithRequestTimeout sets the timeout for each request.
// A value of 0 means no timeout.
// It returns the modified config to allow for method chaining.
//
// Parameters:
//   - timeout: Maximum duration allowed for each request (0 = no timeout)
//
// Returns:
//   - *CircuitConfig: The updated circuit configuration
func (c *CircuitConfig) WithRequestTimeout(timeout time.Duration) *CircuitConfig {
	c.RequestTimeout = timeout
	return c
}

// CircuitBreaker implements the circuit breaker pattern to prevent cascading failures.
// It tracks successes and failures to intelligently "trip" when too many failures occur,
// preventing further requests until a cooling-off period has elapsed.
type CircuitBreaker struct {
	config             *CircuitConfig
	state              CircuitState
	failureCount       int
	halfOpenSuccesses  int
	lastStateChange    time.Time
	mutex              sync.RWMutex
	activeCalls        int
	totalCalls         int64
	successfulCalls    int64
	failedCalls        int64
	rejectedCalls      int64
	timeoutCalls       int64
	stateChangeHandler func(oldState, newState CircuitState)
}

// NewCircuitBreaker creates a new circuit breaker with the provided configuration.
// The circuit starts in the closed state (allowing requests).
//
// Parameters:
//   - config: Configuration for the circuit breaker
//
// Returns:
//   - *CircuitBreaker: A new circuit breaker instance
func NewCircuitBreaker(config *CircuitConfig) *CircuitBreaker {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	cb := &CircuitBreaker{
		config:          config,
		state:           StateClosed,
		lastStateChange: time.Now(),
	}

	config.Logger.Info("Circuit breaker initialized",
		"name", config.Name,
		"state", string(cb.state),
	)

	return cb
}

// OnStateChange sets a handler function that will be called when the circuit changes state.
// This can be used for notifications, metrics, or other tracking of circuit state.
//
// Parameters:
//   - handler: Function to be called with old and new states when a state change occurs
func (cb *CircuitBreaker) OnStateChange(handler func(oldState, newState CircuitState)) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.stateChangeHandler = handler
}

// GetState returns the current state of the circuit breaker.
//
// Returns:
//   - CircuitState: The current state (CLOSED, OPEN, or HALF_OPEN)
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// Execute runs the provided function with circuit breaker protection.
// If the circuit is open, it will prevent execution and return an error.
//
// Parameters:
//   - fn: The function to execute with circuit breaker protection
//
// Returns:
//   - error: Error from the function or circuit breaker if circuit is open
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mutex.Lock()
	// Check circuit state and determine if the request should proceed
	switch cb.state {
	case StateOpen:
		// Check if reset timeout has elapsed to transition to half-open
		if time.Since(cb.lastStateChange) > cb.config.ResetTimeout {
			cb.toState(StateHalfOpen)
		} else {
			cb.mutex.Unlock()
			cb.rejectedCalls++
			cb.config.Logger.Debug("Circuit open, request rejected",
				"name", cb.config.Name,
				"elapsed_since_trip", time.Since(cb.lastStateChange).String(),
				"reset_timeout", cb.config.ResetTimeout.String(),
			)
			return fmt.Errorf("circuit breaker '%s' is open", cb.config.Name)
		}
	case StateClosed:
		// In closed state, all requests proceed normally
		break
	case StateHalfOpen:
		// In half-open state, only allow one request at a time for testing
		if cb.activeCalls > 0 {
			cb.mutex.Unlock()
			cb.rejectedCalls++
			cb.config.Logger.Debug("Circuit half-open, concurrent request rejected",
				"name", cb.config.Name,
				"active_calls", cb.activeCalls,
			)
			return fmt.Errorf("circuit breaker '%s' is half-open and already processing a request", cb.config.Name)
		}
	}

	// Check concurrent requests limit if set
	if cb.config.MaxConcurrentRequests > 0 && cb.activeCalls >= cb.config.MaxConcurrentRequests {
		cb.mutex.Unlock()
		cb.rejectedCalls++
		cb.config.Logger.Debug("Maximum concurrent requests exceeded",
			"name", cb.config.Name,
			"active_calls", cb.activeCalls,
			"max_allowed", cb.config.MaxConcurrentRequests,
		)
		return fmt.Errorf("circuit breaker '%s' maximum concurrent requests exceeded", cb.config.Name)
	}

	// Track the call
	cb.activeCalls++
	cb.totalCalls++
	cb.mutex.Unlock()

	// Define cleanup function to update circuit state after execution
	defer func() {
		cb.mutex.Lock()
		cb.activeCalls--
		cb.mutex.Unlock()
	}()

	// Set up timeout if specified
	var err error
	done := make(chan struct{})

	go func() {
		defer close(done)
		err = fn()
	}()

	// Wait for completion or timeout
	if cb.config.RequestTimeout > 0 {
		select {
		case <-done:
			// Function completed normally
		case <-time.After(cb.config.RequestTimeout):
			cb.mutex.Lock()
			cb.timeoutCalls++
			cb.mutex.Unlock()
			return fmt.Errorf("circuit breaker '%s' request timed out after %s", cb.config.Name, cb.config.RequestTimeout)
		}
	} else {
		<-done // Wait for function to complete without timeout
	}

	// Update circuit state based on result
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if err != nil {
		// Request failed
		cb.failedCalls++
		cb.config.Logger.Debug("Circuit-protected request failed",
			"name", cb.config.Name,
			"state", string(cb.state),
			"error", err,
		)

		switch cb.state {
		case StateClosed:
			cb.failureCount++
			if cb.failureCount >= cb.config.FailureThreshold {
				cb.toState(StateOpen)
			}
		case StateHalfOpen:
			cb.toState(StateOpen)
		}
		return err
	}

	// Request succeeded
	cb.successfulCalls++
	cb.config.Logger.Debug("Circuit-protected request succeeded",
		"name", cb.config.Name,
		"state", string(cb.state),
	)

	switch cb.state {
	case StateClosed:
		// Reset failure count after a success
		cb.failureCount = 0
	case StateHalfOpen:
		cb.halfOpenSuccesses++
		if cb.halfOpenSuccesses >= cb.config.HalfOpenSuccessThreshold {
			cb.toState(StateClosed)
		}
	}

	return nil
}

// toState changes the circuit breaker's state and logs the transition.
// This should only be called when the mutex is already locked.
func (cb *CircuitBreaker) toState(newState CircuitState) {
	oldState := cb.state
	cb.state = newState
	cb.lastStateChange = time.Now()

	// Reset counters on state change
	switch newState {
	case StateOpen:
		cb.failureCount = 0
	case StateHalfOpen:
		cb.halfOpenSuccesses = 0
	case StateClosed:
		cb.failureCount = 0
		cb.halfOpenSuccesses = 0
	}

	cb.config.Logger.Info("Circuit state changed",
		"name", cb.config.Name,
		"old_state", string(oldState),
		"new_state", string(newState),
	)

	// Call state change handler if set
	if cb.stateChangeHandler != nil {
		go cb.stateChangeHandler(oldState, newState)
	}
}

// Metrics returns the current operational metrics for the circuit breaker.
// This provides insight into circuit behavior and performance.
//
// Returns:
//   - map[string]interface{}: A map containing metric values
func (cb *CircuitBreaker) Metrics() map[string]interface{} {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	metrics := map[string]interface{}{
		"name":                 cb.config.Name,
		"state":                string(cb.state),
		"failure_threshold":    cb.config.FailureThreshold,
		"reset_timeout_ms":     cb.config.ResetTimeout.Milliseconds(),
		"current_failures":     cb.failureCount,
		"active_calls":         cb.activeCalls,
		"total_calls":          cb.totalCalls,
		"successful_calls":     cb.successfulCalls,
		"failed_calls":         cb.failedCalls,
		"rejected_calls":       cb.rejectedCalls,
		"timeout_calls":        cb.timeoutCalls,
		"time_since_change_ms": time.Since(cb.lastStateChange).Milliseconds(),
	}

	if cb.state == StateHalfOpen {
		metrics["half_open_successes"] = cb.halfOpenSuccesses
		metrics["success_threshold"] = cb.config.HalfOpenSuccessThreshold
	}

	return metrics
}

// Reset forces the circuit breaker back to closed state and resets all counters.
// This should generally only be used for testing or manual intervention.
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	oldState := cb.state
	cb.state = StateClosed
	cb.failureCount = 0
	cb.halfOpenSuccesses = 0
	cb.lastStateChange = time.Now()

	cb.config.Logger.Info("Circuit manually reset",
		"name", cb.config.Name,
		"old_state", string(oldState),
	)

	if cb.stateChangeHandler != nil {
		go cb.stateChangeHandler(oldState, StateClosed)
	}
}
