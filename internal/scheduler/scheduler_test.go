package scheduler

import (
	"log/slog"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

func TestIsWithinTimeWindow(t *testing.T) {
	// Create a minimal scheduler for testing
	s := &Scheduler{
		logger: slog.Default(),
	}

	// Test cases with fixed current time for consistency
	now, _ := time.Parse(time.RFC3339, "2023-05-15T14:30:00Z") // 2:30 PM

	tests := []struct {
		name      string
		timeStart string
		timeEnd   string
		expected  bool
	}{
		{
			name:      "No time window specified",
			timeStart: "",
			timeEnd:   "",
			expected:  true,
		},
		{
			name:      "Current time within window",
			timeStart: "10:00",
			timeEnd:   "18:00",
			expected:  true,
		},
		{
			name:      "Current time before window",
			timeStart: "16:00",
			timeEnd:   "20:00",
			expected:  false,
		},
		{
			name:      "Current time after window",
			timeStart: "08:00",
			timeEnd:   "12:00",
			expected:  false,
		},
		{
			name:      "Overnight window - current time in evening part",
			timeStart: "22:00",
			timeEnd:   "06:00",
			expected:  false, // Not in evening yet (2:30 PM)
		},
		{
			name:      "Overnight window - current time in morning part",
			timeStart: "22:00",
			timeEnd:   "16:00", // End time > current time (14:30)
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &payload.MigrationRequest{
				ScheduledTimeStart: tt.timeStart,
				ScheduledTimeEnd:   tt.timeEnd,
			}
			result := s.isWithinTimeWindow(now, req)
			if result != tt.expected {
				t.Errorf("isWithinTimeWindow() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsAllowedDayOfWeek(t *testing.T) {
	// Create a minimal scheduler for testing
	s := &Scheduler{
		logger: slog.Default(),
	}

	// Test cases with fixed day
	monday, _ := time.Parse("2006-01-02", "2023-05-15") // A Monday

	tests := []struct {
		name        string
		allowedDays []string
		expected    bool
	}{
		{
			name:        "No day restrictions",
			allowedDays: []string{},
			expected:    true,
		},
		{
			name:        "Current day allowed",
			allowedDays: []string{"Monday", "Wednesday", "Friday"},
			expected:    true,
		},
		{
			name:        "Current day not allowed",
			allowedDays: []string{"Tuesday", "Thursday", "Saturday"},
			expected:    false,
		},
		{
			name:        "Only current day allowed",
			allowedDays: []string{"Monday"},
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &payload.MigrationRequest{
				ScheduledDaysOnly: tt.allowedDays,
			}
			result := s.isAllowedDayOfWeek(monday, req)
			if result != tt.expected {
				t.Errorf("isAllowedDayOfWeek() = %v, want %v", result, tt.expected)
			}
		})
	}
}
