// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"fmt"
	"time"
)

// formatDuration returns a human-readable duration string
func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
}
