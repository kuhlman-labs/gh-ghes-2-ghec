package metrics

import (
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
)

func TestRegisterCircuitBreaker(t *testing.T) {
	// Create a test circuit breaker
	config := &utils.CircuitConfig{
		Name:                     "test-circuit",
		FailureThreshold:         3,
		ResetTimeout:             30 * time.Second,
		HalfOpenSuccessThreshold: 2,
		RequestTimeout:           5 * time.Second,
	}

	cb := utils.NewCircuitBreaker(config)

	// Register the circuit breaker
	stateChangeHandler := RegisterCircuitBreaker(cb)

	if stateChangeHandler == nil {
		t.Fatal("RegisterCircuitBreaker should return a non-nil state change handler")
	}

	// Check that initial metrics are set
	metric := &dto.Metric{}
	if err := CircuitBreakerState.WithLabelValues("test-circuit").Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	if metric.GetGauge().GetValue() != 0 { // Should be closed initially
		t.Errorf("Expected initial state to be 0 (closed), got %f", metric.GetGauge().GetValue())
	}

	// Check consecutive failures metric
	metric = &dto.Metric{}
	if err := CircuitBreakerConsecutiveFailures.WithLabelValues("test-circuit").Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	if metric.GetGauge().GetValue() != 0 {
		t.Errorf("Expected initial consecutive failures to be 0, got %f", metric.GetGauge().GetValue())
	}

	// Check that last state change time is set (should be recent)
	metric = &dto.Metric{}
	if err := CircuitBreakerLastStateChangeTime.WithLabelValues("test-circuit").Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	timestamp := metric.GetGauge().GetValue()
	if timestamp == 0 {
		t.Error("Expected last state change time to be set")
	}

	// Verify the timestamp is recent (within last 5 seconds)
	now := float64(time.Now().Unix())
	if now-timestamp > 5 {
		t.Errorf("Expected timestamp to be recent, got %f (now: %f)", timestamp, now)
	}
}

func TestCircuitBreakerStateChangeHandler(t *testing.T) {
	// Create a test circuit breaker
	config := &utils.CircuitConfig{
		Name:                     "test-state-change",
		FailureThreshold:         3,
		ResetTimeout:             30 * time.Second,
		HalfOpenSuccessThreshold: 2,
		RequestTimeout:           5 * time.Second,
	}

	cb := utils.NewCircuitBreaker(config)
	stateChangeHandler := RegisterCircuitBreaker(cb)

	// Test state changes
	testCases := []struct {
		name     string
		oldState utils.CircuitState
		newState utils.CircuitState
		expected float64
	}{
		{"closed_to_open", utils.StateClosed, utils.StateOpen, 2},
		{"open_to_half_open", utils.StateOpen, utils.StateHalfOpen, 1},
		{"half_open_to_closed", utils.StateHalfOpen, utils.StateClosed, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Call the state change handler
			stateChangeHandler(tc.oldState, tc.newState)

			// Check that the state metric is updated
			metric := &dto.Metric{}
			if err := CircuitBreakerState.WithLabelValues("test-state-change").Write(metric); err != nil {
				t.Fatalf("Failed to write metric: %v", err)
			}
			if metric.GetGauge().GetValue() != tc.expected {
				t.Errorf("Expected state metric to be %f, got %f", tc.expected, metric.GetGauge().GetValue())
			}

			// Check that last state change time is updated
			metric = &dto.Metric{}
			if err := CircuitBreakerLastStateChangeTime.WithLabelValues("test-state-change").Write(metric); err != nil {
				t.Fatalf("Failed to write metric: %v", err)
			}
			timestamp := metric.GetGauge().GetValue()
			if timestamp == 0 {
				t.Error("Expected last state change time to be updated")
			}

			// Verify the timestamp is recent (within last 5 seconds)
			now := float64(time.Now().Unix())
			if now-timestamp > 5 {
				t.Errorf("Expected timestamp to be recent, got %f (now: %f)", timestamp, now)
			}
		})
	}
}

func TestUpdateCircuitBreakerMetrics(t *testing.T) {
	// Create a test circuit breaker
	config := &utils.CircuitConfig{
		Name:                     "test-update-metrics",
		FailureThreshold:         3,
		ResetTimeout:             30 * time.Second,
		HalfOpenSuccessThreshold: 2,
		RequestTimeout:           5 * time.Second,
	}

	cb := utils.NewCircuitBreaker(config)
	RegisterCircuitBreaker(cb)

	// Get initial counter values
	getCounterValue := func(labelValues ...string) float64 {
		metric := &dto.Metric{}
		if err := CircuitBreakerTotalCalls.WithLabelValues(labelValues...).Write(metric); err != nil {
			t.Fatalf("Failed to write metric: %v", err)
		}
		return metric.GetCounter().GetValue()
	}

	initialSuccess := getCounterValue("test-update-metrics", "success")
	initialFailure := getCounterValue("test-update-metrics", "failure")
	initialRejected := getCounterValue("test-update-metrics", "rejected")
	initialTimeout := getCounterValue("test-update-metrics", "timeout")

	// Update metrics
	UpdateCircuitBreakerMetrics(cb)

	// Check that counters are updated (they should at least maintain their values)
	newSuccess := getCounterValue("test-update-metrics", "success")
	newFailure := getCounterValue("test-update-metrics", "failure")
	newRejected := getCounterValue("test-update-metrics", "rejected")
	newTimeout := getCounterValue("test-update-metrics", "timeout")

	if newSuccess < initialSuccess {
		t.Errorf("Success counter should not decrease: initial=%f, new=%f", initialSuccess, newSuccess)
	}
	if newFailure < initialFailure {
		t.Errorf("Failure counter should not decrease: initial=%f, new=%f", initialFailure, newFailure)
	}
	if newRejected < initialRejected {
		t.Errorf("Rejected counter should not decrease: initial=%f, new=%f", initialRejected, newRejected)
	}
	if newTimeout < initialTimeout {
		t.Errorf("Timeout counter should not decrease: initial=%f, new=%f", initialTimeout, newTimeout)
	}

	// Check consecutive failures gauge
	metric := &dto.Metric{}
	if err := CircuitBreakerConsecutiveFailures.WithLabelValues("test-update-metrics").Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	consecutiveFailures := metric.GetGauge().GetValue()
	if consecutiveFailures < 0 {
		t.Errorf("Consecutive failures should not be negative, got %f", consecutiveFailures)
	}
}

func TestCircuitBreakerStateValues(t *testing.T) {
	// Test that state enum values map correctly to metric values
	testCases := []struct {
		state    utils.CircuitState
		expected float64
	}{
		{utils.StateClosed, 0},
		{utils.StateHalfOpen, 1},
		{utils.StateOpen, 2},
	}

	config := &utils.CircuitConfig{
		Name:                     "test-state-values",
		FailureThreshold:         3,
		ResetTimeout:             30 * time.Second,
		HalfOpenSuccessThreshold: 2,
		RequestTimeout:           5 * time.Second,
	}

	cb := utils.NewCircuitBreaker(config)
	stateChangeHandler := RegisterCircuitBreaker(cb)

	for _, tc := range testCases {
		t.Run(string(tc.state), func(t *testing.T) {
			// Simulate state change
			stateChangeHandler(utils.StateClosed, tc.state)

			// Check the metric value
			metric := &dto.Metric{}
			if err := CircuitBreakerState.WithLabelValues("test-state-values").Write(metric); err != nil {
				t.Fatalf("Failed to write metric: %v", err)
			}
			if metric.GetGauge().GetValue() != tc.expected {
				t.Errorf("Expected state value %f for state %s, got %f",
					tc.expected, string(tc.state), metric.GetGauge().GetValue())
			}
		})
	}
}

func TestCircuitBreakerMetricLabels(t *testing.T) {
	// Test that metrics use correct labels
	config := &utils.CircuitConfig{
		Name:                     "test-labels",
		FailureThreshold:         3,
		ResetTimeout:             30 * time.Second,
		HalfOpenSuccessThreshold: 2,
		RequestTimeout:           5 * time.Second,
	}

	cb := utils.NewCircuitBreaker(config)
	RegisterCircuitBreaker(cb)

	// Test that all metrics can be accessed with the correct label
	labelValues := []string{"test-labels"}

	// Test state metric
	metric := &dto.Metric{}
	if err := CircuitBreakerState.WithLabelValues(labelValues...).Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	if len(metric.GetLabel()) == 0 {
		t.Error("State metric should have labels")
	}

	// Test consecutive failures metric
	metric = &dto.Metric{}
	if err := CircuitBreakerConsecutiveFailures.WithLabelValues(labelValues...).Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	if len(metric.GetLabel()) == 0 {
		t.Error("Consecutive failures metric should have labels")
	}

	// Test last state change metric
	metric = &dto.Metric{}
	if err := CircuitBreakerLastStateChangeTime.WithLabelValues(labelValues...).Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	if len(metric.GetLabel()) == 0 {
		t.Error("Last state change metric should have labels")
	}

	// Test total calls metric with result labels
	callLabels := []string{"test-labels", "success"}
	metric = &dto.Metric{}
	if err := CircuitBreakerTotalCalls.WithLabelValues(callLabels...).Write(metric); err != nil {
		t.Fatalf("Failed to write metric: %v", err)
	}
	if len(metric.GetLabel()) < 2 {
		t.Error("Total calls metric should have name and result labels")
	}
}
