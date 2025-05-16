package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
)

var (
	// CircuitBreakerState tracks the current state of circuit breakers (0=closed, 1=half-open, 2=open)
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "Current state of circuit breakers (0=closed, 1=half-open, 2=open)",
		},
		[]string{"name"},
	)

	// CircuitBreakerTotalCalls tracks the total number of calls handled by circuit breakers
	CircuitBreakerTotalCalls = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "circuit_breaker_calls_total",
			Help: "Total number of calls handled by circuit breakers",
		},
		[]string{"name", "result"},
	)

	// CircuitBreakerConsecutiveFailures tracks current consecutive failures
	CircuitBreakerConsecutiveFailures = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_consecutive_failures",
			Help: "Current number of consecutive failures for circuit breakers",
		},
		[]string{"name"},
	)

	// CircuitBreakerLastStateChangeTime tracks when the circuit breaker last changed state
	CircuitBreakerLastStateChangeTime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_last_state_change_seconds",
			Help: "Time when the circuit breaker last changed state (unix timestamp)",
		},
		[]string{"name"},
	)
)

// RegisterCircuitBreaker registers a circuit breaker for metrics collection
// and returns a state change handler that updates circuit metrics.
//
// Parameters:
//   - circuitBreaker: The circuit breaker to register for metrics
//
// Returns:
//   - A state change handler function for the circuit breaker
func RegisterCircuitBreaker(circuitBreaker *utils.CircuitBreaker) func(oldState, newState utils.CircuitState) {
	// Get initial metrics from the circuit breaker
	metrics := circuitBreaker.Metrics()
	name := metrics["name"].(string)

	// Define a state change handler
	stateChangeHandler := func(oldState, newState utils.CircuitState) {
		// Update state metric
		var stateValue float64
		switch newState {
		case utils.StateClosed:
			stateValue = 0
		case utils.StateHalfOpen:
			stateValue = 1
		case utils.StateOpen:
			stateValue = 2
		}
		CircuitBreakerState.WithLabelValues(name).Set(stateValue)

		// Update last state change time
		CircuitBreakerLastStateChangeTime.WithLabelValues(name).Set(float64(time.Now().Unix()))

		// Get updated metrics
		updatedMetrics := circuitBreaker.Metrics()
		// Update consecutive failures gauge
		CircuitBreakerConsecutiveFailures.WithLabelValues(name).Set(float64(updatedMetrics["current_failures"].(int)))
	}

	// Set up initial metrics
	// Initialize state metric
	var initialState float64
	switch circuitBreaker.GetState() {
	case utils.StateClosed:
		initialState = 0
	case utils.StateHalfOpen:
		initialState = 1
	case utils.StateOpen:
		initialState = 2
	}
	CircuitBreakerState.WithLabelValues(name).Set(initialState)

	// Initialize last state change time
	CircuitBreakerLastStateChangeTime.WithLabelValues(name).Set(float64(time.Now().Unix()))

	// Initialize consecutive failures
	CircuitBreakerConsecutiveFailures.WithLabelValues(name).Set(0)

	// Set the state change handler on the circuit breaker
	circuitBreaker.OnStateChange(stateChangeHandler)

	return stateChangeHandler
}

// UpdateCircuitBreakerMetrics regularly updates metrics for a circuit breaker.
// This should be called periodically (e.g., from a background goroutine)
// to update metrics that don't change with state changes.
//
// Parameters:
//   - circuitBreaker: The circuit breaker to update metrics for
func UpdateCircuitBreakerMetrics(circuitBreaker *utils.CircuitBreaker) {
	// Get current metrics
	metrics := circuitBreaker.Metrics()
	name := metrics["name"].(string)

	// Update metrics that change over time
	CircuitBreakerTotalCalls.WithLabelValues(name, "success").Add(float64(metrics["successful_calls"].(int64)))
	CircuitBreakerTotalCalls.WithLabelValues(name, "failure").Add(float64(metrics["failed_calls"].(int64)))
	CircuitBreakerTotalCalls.WithLabelValues(name, "rejected").Add(float64(metrics["rejected_calls"].(int64)))
	CircuitBreakerTotalCalls.WithLabelValues(name, "timeout").Add(float64(metrics["timeout_calls"].(int64)))

	// Update consecutive failures gauge
	CircuitBreakerConsecutiveFailures.WithLabelValues(name).Set(float64(metrics["current_failures"].(int)))
}
