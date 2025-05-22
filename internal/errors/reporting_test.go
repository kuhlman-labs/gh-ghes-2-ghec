package errors

import (
	"errors"
	"sync"
	"testing"
)

func TestReportError(t *testing.T) {
	// Clear error counts before test
	errorCountsLock.Lock()
	errorCounts = make(map[ErrorCategory]int)
	errorCountsLock.Unlock()

	tests := []struct {
		name             string
		err              error
		expectedCategory ErrorCategory
	}{
		{
			name:             "nil error",
			err:              nil,
			expectedCategory: "",
		},
		{
			name:             "unauthorized error",
			err:              errors.New("unauthorized access"),
			expectedCategory: CategoryAuthentication,
		},
		{
			name:             "rate limit error",
			err:              errors.New("rate limit exceeded"),
			expectedCategory: CategoryRateLimit,
		},
		{
			name:             "not found error",
			err:              errors.New("resource not found"),
			expectedCategory: CategoryResourceNotFound,
		},
		{
			name:             "validation error",
			err:              errors.New("invalid input"),
			expectedCategory: CategoryValidation,
		},
		{
			name:             "unknown error",
			err:              errors.New("some random error"),
			expectedCategory: CategoryUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialCounts := GetErrorCounts()
			initialCount := initialCounts[tt.expectedCategory]

			ReportError(tt.err)

			if tt.err == nil {
				// For nil error, counts should not change
				newCounts := GetErrorCounts()
				for category, count := range newCounts {
					if count != initialCounts[category] {
						t.Errorf("Expected no change in counts for nil error, but category %s changed from %d to %d",
							category, initialCounts[category], count)
					}
				}
				return
			}

			newCounts := GetErrorCounts()
			newCount := newCounts[tt.expectedCategory]

			if newCount != initialCount+1 {
				t.Errorf("Expected count for category %s to increase by 1 (from %d to %d), got %d",
					tt.expectedCategory, initialCount, initialCount+1, newCount)
			}
		})
	}
}

func TestGetErrorCounts(t *testing.T) {
	// Clear error counts before test
	errorCountsLock.Lock()
	errorCounts = make(map[ErrorCategory]int)
	errorCountsLock.Unlock()

	t.Run("empty counts", func(t *testing.T) {
		counts := GetErrorCounts()
		if len(counts) != 0 {
			t.Errorf("Expected empty error counts, got %v", counts)
		}
	})

	t.Run("with some errors", func(t *testing.T) {
		// Add some errors
		ReportError(errors.New("unauthorized"))
		ReportError(errors.New("rate limit exceeded"))
		ReportError(errors.New("unauthorized")) // Same category again

		counts := GetErrorCounts()

		expectedCounts := map[ErrorCategory]int{
			CategoryAuthentication: 2,
			CategoryRateLimit:      1,
		}

		if len(counts) != len(expectedCounts) {
			t.Errorf("Expected %d categories, got %d", len(expectedCounts), len(counts))
		}

		for category, expectedCount := range expectedCounts {
			if actualCount, exists := counts[category]; !exists || actualCount != expectedCount {
				t.Errorf("Expected count for category %s to be %d, got %d (exists: %v)",
					category, expectedCount, actualCount, exists)
			}
		}
	})

	t.Run("returns copy", func(t *testing.T) {
		// Clear error counts
		errorCountsLock.Lock()
		errorCounts = make(map[ErrorCategory]int)
		errorCountsLock.Unlock()

		// Add an error
		ReportError(errors.New("test error"))

		// Get counts
		counts1 := GetErrorCounts()
		counts2 := GetErrorCounts()

		// Modify one copy
		counts1[CategoryUnknown] = 999

		// Check that the other copy is not affected
		if counts2[CategoryUnknown] == 999 {
			t.Error("GetErrorCounts() should return a copy, not the original map")
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	// Clear error counts before test
	errorCountsLock.Lock()
	errorCounts = make(map[ErrorCategory]int)
	errorCountsLock.Unlock()

	const numGoroutines = 100
	const errorsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple goroutines that report errors concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < errorsPerGoroutine; j++ {
				ReportError(errors.New("concurrent test error"))
			}
		}()
	}

	// Start a goroutine that reads counts concurrently
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				GetErrorCounts()
			}
		}
	}()

	wg.Wait()
	close(done)

	// Check final count
	counts := GetErrorCounts()
	expectedTotal := numGoroutines * errorsPerGoroutine
	actualTotal := counts[CategoryUnknown]

	if actualTotal != expectedTotal {
		t.Errorf("Expected total count of %d, got %d", expectedTotal, actualTotal)
	}
}

func TestReportClassifiedError(t *testing.T) {
	// Clear error counts before test
	errorCountsLock.Lock()
	errorCounts = make(map[ErrorCategory]int)
	errorCountsLock.Unlock()

	// Create a classified error
	originalErr := errors.New("original error")
	classifiedErr := NewClassifiedError(originalErr, CategoryRateLimit)

	ReportError(classifiedErr)

	counts := GetErrorCounts()
	if counts[CategoryRateLimit] != 1 {
		t.Errorf("Expected 1 rate limit error, got %d", counts[CategoryRateLimit])
	}

	// Ensure it's not counted in other categories
	for category, count := range counts {
		if category != CategoryRateLimit && count > 0 {
			t.Errorf("Unexpected count for category %s: %d", category, count)
		}
	}
}
