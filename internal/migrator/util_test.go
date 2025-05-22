package migrator

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			duration: 0,
			expected: "0h0m0s",
		},
		{
			name:     "1 second",
			duration: time.Second,
			expected: "0h0m1s",
		},
		{
			name:     "1 minute",
			duration: time.Minute,
			expected: "0h1m0s",
		},
		{
			name:     "1 hour",
			duration: time.Hour,
			expected: "1h0m0s",
		},
		{
			name:     "complex duration",
			duration: 2*time.Hour + 30*time.Minute + 45*time.Second,
			expected: "2h30m45s",
		},
		{
			name:     "1 day",
			duration: 24 * time.Hour,
			expected: "24h0m0s",
		},
		{
			name:     "multiple days",
			duration: 48*time.Hour + 15*time.Minute + 30*time.Second,
			expected: "48h15m30s",
		},
		{
			name:     "milliseconds truncated",
			duration: 1*time.Hour + 30*time.Minute + 45*time.Second + 123*time.Millisecond,
			expected: "1h30m45s",
		},
		{
			name:     "only minutes and seconds",
			duration: 15*time.Minute + 30*time.Second,
			expected: "0h15m30s",
		},
		{
			name:     "59 minutes 59 seconds",
			duration: 59*time.Minute + 59*time.Second,
			expected: "0h59m59s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %v, want %v", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatDurationEdgeCases(t *testing.T) {
	t.Run("negative duration", func(t *testing.T) {
		// Test with negative duration to ensure it handles edge cases gracefully
		duration := -1 * time.Hour
		result := formatDuration(duration)
		// The function should handle negative durations (though behavior may vary)
		// We just ensure it doesn't panic and returns a string
		if len(result) == 0 {
			t.Error("formatDuration should return a non-empty string even for negative durations")
		}
	})

	t.Run("very large duration", func(t *testing.T) {
		// Test with a very large duration
		duration := 999*time.Hour + 59*time.Minute + 59*time.Second
		result := formatDuration(duration)
		expected := "999h59m59s"
		if result != expected {
			t.Errorf("formatDuration(%v) = %v, want %v", duration, result, expected)
		}
	})
}
