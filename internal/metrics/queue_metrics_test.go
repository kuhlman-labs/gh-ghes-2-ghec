package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestQueueMetrics(t *testing.T) {
	t.Run("QueueSize metric", func(t *testing.T) {
		// Test that we can set and get the queue size
		initialValue := getGaugeValue(QueueSize)

		QueueSize.Set(10)
		if value := getGaugeValue(QueueSize); value != 10 {
			t.Errorf("Expected QueueSize to be 10, got %f", value)
		}

		QueueSize.Set(0) // Reset for other tests

		// Test increment/decrement
		QueueSize.Inc()
		if value := getGaugeValue(QueueSize); value != 1 {
			t.Errorf("Expected QueueSize to be 1 after increment, got %f", value)
		}

		QueueSize.Dec()
		if value := getGaugeValue(QueueSize); value != 0 {
			t.Errorf("Expected QueueSize to be 0 after decrement, got %f", value)
		}

		// Restore initial value
		QueueSize.Set(initialValue)
	})

	t.Run("QueuedJobs counter", func(t *testing.T) {
		initialValue := getCounterValue(QueuedJobs)

		QueuedJobs.Inc()
		newValue := getCounterValue(QueuedJobs)
		if newValue <= initialValue {
			t.Errorf("Expected QueuedJobs to increase, initial: %f, new: %f", initialValue, newValue)
		}

		QueuedJobs.Add(5)
		finalValue := getCounterValue(QueuedJobs)
		if finalValue != newValue+5 {
			t.Errorf("Expected QueuedJobs to be %f, got %f", newValue+5, finalValue)
		}
	})

	t.Run("ActiveArchives gauge", func(t *testing.T) {
		initialValue := getGaugeValue(ActiveArchives)

		ActiveArchives.Set(3)
		if value := getGaugeValue(ActiveArchives); value != 3 {
			t.Errorf("Expected ActiveArchives to be 3, got %f", value)
		}

		ActiveArchives.Add(2)
		if value := getGaugeValue(ActiveArchives); value != 5 {
			t.Errorf("Expected ActiveArchives to be 5 after adding 2, got %f", value)
		}

		ActiveArchives.Sub(1)
		if value := getGaugeValue(ActiveArchives); value != 4 {
			t.Errorf("Expected ActiveArchives to be 4 after subtracting 1, got %f", value)
		}

		// Restore initial value
		ActiveArchives.Set(initialValue)
	})

	t.Run("ActiveMigrations gauge", func(t *testing.T) {
		initialValue := getGaugeValue(ActiveMigrations)

		ActiveMigrations.Set(2)
		if value := getGaugeValue(ActiveMigrations); value != 2 {
			t.Errorf("Expected ActiveMigrations to be 2, got %f", value)
		}

		// Restore initial value
		ActiveMigrations.Set(initialValue)
	})

	t.Run("CompletedArchives counter", func(t *testing.T) {
		initialValue := getCounterValue(CompletedArchives)

		CompletedArchives.Inc()
		newValue := getCounterValue(CompletedArchives)
		if newValue <= initialValue {
			t.Errorf("Expected CompletedArchives to increase, initial: %f, new: %f", initialValue, newValue)
		}
	})

	t.Run("CompletedMigrations counter", func(t *testing.T) {
		initialValue := getCounterValue(CompletedMigrations)

		CompletedMigrations.Inc()
		newValue := getCounterValue(CompletedMigrations)
		if newValue <= initialValue {
			t.Errorf("Expected CompletedMigrations to increase, initial: %f, new: %f", initialValue, newValue)
		}
	})
}

func TestMetricNames(t *testing.T) {
	tests := []struct {
		name     string
		metric   prometheus.Metric
		expected string
	}{
		{"QueueSize", QueueSize, "migration_queue_size"},
		{"QueuedJobs", QueuedJobs, "migration_queued_jobs_total"},
		{"ActiveArchives", ActiveArchives, "migration_active_archives"},
		{"ActiveMigrations", ActiveMigrations, "migration_active_migrations"},
		{"CompletedArchives", CompletedArchives, "migration_completed_archives_total"},
		{"CompletedMigrations", CompletedMigrations, "migration_completed_migrations_total"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric := &dto.Metric{}
			if err := tt.metric.Write(metric); err != nil {
				t.Fatalf("Failed to write metric: %v", err)
			}

			desc := tt.metric.Desc()
			// We check if the expected name is contained in the description
			// since the full description format may vary
			if !contains(desc.String(), tt.expected) {
				t.Errorf("Expected metric name %s to be in description %s", tt.expected, desc.String())
			}
		})
	}
}

// Helper function to get gauge value
func getGaugeValue(gauge prometheus.Gauge) float64 {
	metric := &dto.Metric{}
	gauge.Write(metric)
	return metric.GetGauge().GetValue()
}

// Helper function to get counter value
func getCounterValue(counter prometheus.Counter) float64 {
	metric := &dto.Metric{}
	counter.Write(metric)
	return metric.GetCounter().GetValue()
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && s[len(s)-len(substr):] == substr ||
		(len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
