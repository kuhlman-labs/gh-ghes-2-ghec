package errors

import (
	"sync"
)

var (
	// In-memory error counts used for the dashboard
	errorCounts     = make(map[ErrorCategory]int)
	errorCountsLock sync.RWMutex
)

// ReportError records an error and its category in memory for the dashboard.
func ReportError(err error) {
	if err == nil {
		return
	}

	// Classify the error
	category := Classify(err)

	// Record in memory for the dashboard
	errorCountsLock.Lock()
	errorCounts[category]++
	errorCountsLock.Unlock()
}

// GetErrorCounts returns the in-memory counts of errors by category.
func GetErrorCounts() map[ErrorCategory]int {
	errorCountsLock.RLock()
	defer errorCountsLock.RUnlock()

	// Make a copy to avoid races
	result := make(map[ErrorCategory]int, len(errorCounts))
	for category, count := range errorCounts {
		result[category] = count
	}

	return result
}
