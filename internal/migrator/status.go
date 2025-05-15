// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"context"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// parseMessageToStageAndState extracts the stage and state information from a status message.
// It returns the appropriate stage and state strings based on the message content.
func parseMessageToStageAndState(message string, overallStatus string) (stage, state string) {
	switch {
	case strings.Contains(message, "starting migration process"):
		stage = "init"
		state = "starting"
	case strings.Contains(message, "validating source repository"):
		stage = "validation"
		state = "checking_source"
	case strings.Contains(message, "getting target organization ID"):
		stage = "validation"
		state = "checking_target"
	case strings.Contains(message, "creating migration source"):
		stage = "setup"
		state = "creating_source"
	case strings.Contains(message, "generating migration archive"):
		stage = "archive"
		state = "generating"
	case strings.Contains(message, "waiting for archive export (status: "):
		// Extract the actual status from the message for more accurate state reporting
		stage = "archive"
		startIdx := strings.Index(message, "status: ") + len("status: ")
		endIdx := strings.Index(message[startIdx:], ",")
		if endIdx > 0 {
			state = message[startIdx : startIdx+endIdx]
		} else {
			state = "waiting" // Fallback if we can't parse the status
		}
	case strings.Contains(message, "waiting for archive export"):
		stage = "archive"
		state = "waiting"
	case strings.Contains(message, "archive export state:"):
		stage = "archive"
		state = strings.TrimSpace(strings.TrimPrefix(message, "archive export state:"))
	case strings.Contains(message, "archive ready for migration"):
		stage = "archive"
		state = "ready"
	case strings.Contains(message, "uploading archive to GitHub Owned Storage"):
		stage = "storage"
		state = "uploading"
	case strings.Contains(message, "archive uploaded to GHOS:"):
		stage = "storage"
		state = "completed"
	case strings.Contains(message, "starting repository migration"):
		stage = "migration"
		state = "starting"
	case strings.Contains(message, "starting repository migration with GHOS archive"):
		stage = "migration"
		state = "starting"
	case strings.Contains(message, "migration created with ID:"):
		stage = "migration"
		state = "created"
	case strings.Contains(message, "waiting for migration to complete"):
		stage = "migration"
		state = "waiting"
	case strings.Contains(message, "migration in progress (state: "):
		// Extract the actual status from the message for more accurate state reporting
		stage = "migration"
		startIdx := strings.Index(message, "state: ") + len("state: ")
		endIdx := strings.Index(message[startIdx:], ",")
		if endIdx > 0 {
			state = message[startIdx : startIdx+endIdx]
		} else {
			state = "in_progress" // Fallback if we can't parse the status
		}
	case strings.Contains(message, "migration state:"):
		stage = "migration"
		state = strings.TrimSpace(strings.TrimPrefix(message, "migration state:"))
	case strings.Contains(message, "migration completed successfully"):
		stage = "migration"
		state = "completed"
	case overallStatus == payload.StatusFailed:
		stage = "error"
		state = "failed"
	default:
		// Default values if no matching pattern is found
		stage = "unknown"
		state = "unknown"
	}

	return stage, state
}

// persistAndNotifyStatusUpdate handles persisting the status to storage and sending webhook notifications.
// This is run asynchronously to avoid blocking the main status update flow.
func (m *Migrator) persistAndNotifyStatusUpdate(status *payload.MigrationStatus, isNewOrChanged bool) {
	if status == nil {
		m.logger.Error("Status is nil, cannot persist or send webhook", "repository_full_name", "unknown")
		return
	}

	repoFullName := status.Repository

	// Persist to storage (in a background goroutine to avoid blocking)
	if config.Get().Storage.Enabled {
		go func(status payload.MigrationStatus) {
			// Use a dedicated timeout for saving migration status,
			// as the server write timeout might be too short for this operation.
			// Defaulting to 60 seconds, similar to default DB operation timeouts.
			saveTimeout := 60 * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
			defer cancel()

			if err := m.storage.SaveMigrationStatus(ctx, &status); err != nil {
				m.logger.Error("Failed to persist migration status",
					"repository", status.Repository,
					"error", err,
				)
			} else {
				m.logger.Debug("Migration status persisted to storage",
					"repository", status.Repository,
				)
			}
		}(*status)
	}

	// Send webhook notification if the status changed
	if isNewOrChanged && m.webhookURL != "" {
		// Passing nil for the second argument, assuming sendWebhookNotification will fetch the status using repoFullName.
		go m.sendWebhookNotification(repoFullName, nil)
	}
}

// updateStatus updates the in-memory status of a migration and triggers persistence.
// repoFullName is the unique identifier in "org/repo" format.
// newOverallStatus is one of payload.StatusInProgress, payload.StatusSucceeded, payload.StatusFailed.
// message often contains details about the current state or an error.
// timestamp is when this specific update event occurred.
// attemptStartTime is the time this particular migration attempt (fresh or retry) began.
func (m *Migrator) updateStatus(repoFullName string, newOverallStatus string, message string, timestamp time.Time, attemptStartTime time.Time) {
	m.mu.Lock() // Lock is released before potentially long-running operations

	var currentAttemptStatus *payload.MigrationStatus
	var isNewOrChanged bool
	var oldStatus *payload.MigrationStatus
	var migrationStatus *payload.MigrationStatus

	// Parse the message to determine stage and state for this update
	stage, state := parseMessageToStageAndState(message, newOverallStatus)

	if existing, ok := m.migrations[repoFullName]; !ok {
		// New status - Initialize with first stage
		isNewOrChanged = true

		// Determine progression data
		progressData := calculateProgressData(stage, state, nil)

		migrationStatus = &payload.MigrationStatus{
			Repository:        repoFullName,
			Status:            newOverallStatus,
			Error:             message,
			UpdatedAt:         timestamp,
			Stage:             stage,
			State:             state,
			StartedAt:         attemptStartTime,
			Progress:          progressData.progress,
			StageProgress:     progressData.stageProgress,
			CompletedStages:   progressData.completedStages,
			TotalStages:       len(payload.MigrationStages),
			CurrentStageIndex: progressData.currentStageIndex,
		}
		m.migrations[repoFullName] = migrationStatus
		currentAttemptStatus = migrationStatus
		m.logger.Debug("Created new in-memory status for first update", "repository_full_name", repoFullName, "attempt_start_time", attemptStartTime)
	} else {
		// Save old status for comparison
		oldStatus = &payload.MigrationStatus{
			Status:      existing.Status,
			Stage:       existing.Stage,
			State:       existing.State,
			MigrationID: existing.MigrationID,
		}

		// Calculate progress data based on current stage/state and previous state
		progressData := calculateProgressData(stage, state, existing)

		// Update existing status
		existing.Repository = repoFullName
		existing.Status = newOverallStatus
		existing.UpdatedAt = timestamp
		existing.Stage = stage
		existing.State = state
		existing.Progress = progressData.progress
		existing.StageProgress = progressData.stageProgress
		existing.CompletedStages = progressData.completedStages
		existing.CurrentStageIndex = progressData.currentStageIndex
		existing.TotalStages = len(payload.MigrationStages)

		// Only update error message if there's an error
		if newOverallStatus == payload.StatusFailed {
			existing.Error = message
		} else if existing.Status != payload.StatusFailed && newOverallStatus != payload.StatusFailed {
			// If not failing now, and wasn't already failed, clear any old error from a previous stage if message is not an error.
			existing.Error = ""
		}

		// Keep the original start time
		if existing.StartedAt.IsZero() {
			existing.StartedAt = attemptStartTime
			m.logger.Warn("Existing status had zero StartedAt, setting from attemptStartTime", "repository_full_name", repoFullName, "attempt_start_time", attemptStartTime)
		}

		// Check if stage or state changed
		isNewOrChanged = (oldStatus.Status != newOverallStatus ||
			oldStatus.Stage != stage ||
			oldStatus.State != state)

		// Only need to set currentAttemptStatus
		currentAttemptStatus = existing
	}

	// Calculate duration for terminal states (Succeeded or Failed)
	if newOverallStatus == payload.StatusSucceeded || newOverallStatus == payload.StatusFailed {
		if currentAttemptStatus != nil && !currentAttemptStatus.StartedAt.IsZero() {
			currentAttemptStatus.Duration = timestamp.Sub(currentAttemptStatus.StartedAt)
			if currentAttemptStatus.Status == payload.StatusSucceeded || currentAttemptStatus.Status == payload.StatusFailed {
				if currentAttemptStatus.Progress < 100 {
					if currentAttemptStatus.Status == payload.StatusSucceeded {
						currentAttemptStatus.Progress = 100
					}
				}
			}
		}
	}

	// Create a deep copy for persistence and webhook to avoid race conditions if the in-memory object is further modified.
	statusForAsyncTasks := deepCopyMigrationStatus(currentAttemptStatus)

	m.mu.Unlock() // Unlock before long-running I/O

	// Log status update (only for significant changes to reduce noise)
	if isNewOrChanged {
		if newOverallStatus == payload.StatusSucceeded {
			duration := timestamp.Sub(attemptStartTime)
			m.logger.Info("Status updated",
				"repository_full_name", repoFullName,
				"status", newOverallStatus,
				"stage", stage,
				"state", state,
				"total_duration", formatDuration(duration),
				"progress", statusForAsyncTasks.Progress,
			)
		} else {
			m.logger.Info("Status updated",
				"repository_full_name", repoFullName,
				"status", newOverallStatus,
				"stage", stage,
				"state", state,
				"progress", statusForAsyncTasks.Progress,
			)
		}
	} else {
		m.logger.Info("Current status (no change detected for logging/webhook)",
			"repository_full_name", repoFullName,
			"status", newOverallStatus,
			"stage", stage,
			"state", state,
		)
	}

	// Handle persistence and notifications asynchronously
	m.persistAndNotifyStatusUpdate(statusForAsyncTasks, isNewOrChanged)
}

// getStageDescription returns a human-readable description of a migration stage
func getStageDescription(stage string) string {
	switch stage {
	case "init":
		return "Migration initialization"
	case "validation":
		return "Repository validation"
	case "setup":
		return "Migration setup"
	case "archive":
		return "Archive management"
	case "storage":
		return "Storage upload"
	case "migration":
		return "Repository migration"
	case "error":
		return "Error occurred"
	default:
		return "Unknown stage"
	}
}

// getStateDescription returns a human-readable description of a migration state
func getStateDescription(stage, state string) string {
	// First handle common states across stages
	if state == "failed" {
		return "The operation has failed"
	}

	// Then handle stage-specific states
	switch stage {
	case "validation":
		switch state {
		case "checking_source":
			return "Validating source repository exists"
		case "checking_target":
			return "Validating target organization"
		default:
			return state
		}
	case "setup":
		switch state {
		case "creating_source":
			return "Creating migration source"
		default:
			return state
		}
	case "archive":
		switch state {
		case "generating":
			return "Generating migration archive"
		case "waiting":
			return "Waiting for archive to be created"
		case "exported":
			return "Archive has been exported"
		case "ready":
			return "Archive is ready for migration"
		default:
			return state
		}
	case "storage":
		switch state {
		case "uploading":
			return "Uploading archive to GitHub Owned Storage"
		case "completed":
			return "Archive upload completed successfully"
		default:
			return state
		}
	case "migration":
		switch state {
		case "starting":
			return "Starting repository migration"
		case "created":
			return "Migration has been created"
		case "waiting":
			return "Waiting for migration to complete"
		case "IN_PROGRESS":
			return "Migration is in progress"
		case "PENDING":
			return "Migration is pending"
		case "QUEUED":
			return "Migration is queued"
		case "SUCCEEDED":
			return "Migration has succeeded"
		case "FAILED":
			return "Migration has failed"
		case "completed":
			return "Migration completed successfully"
		default:
			return state
		}
	default:
		return state
	}
}
