// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"context"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// parseMessageToStageAndState extracts the stage and state information from a status message.
// It returns the appropriate stage and state strings based on the message content.
// It also considers the existing status to prevent progress regression.
func parseMessageToStageAndState(message string, overallStatus string, existing *payload.MigrationStatus) (stage, state string) {
	// First, determine the raw stage and state based on message content
	rawStage, rawState := parseRawMessageToStageAndState(message, overallStatus)

	// If no existing status, return the raw parsing result
	if existing == nil {
		return rawStage, rawState
	}

	// Context-aware adjustments to prevent progress regression
	return adjustStageAndStateForContext(rawStage, rawState, existing, message)
}

// parseRawMessageToStageAndState provides the basic message-to-stage-state mapping
func parseRawMessageToStageAndState(message string, overallStatus string) (stage, state string) {
	switch {
	case strings.Contains(message, "starting migration process"):
		stage = "init"
		state = "starting"
	case strings.Contains(message, "Migration process initiated"):
		stage = "migration"
		state = "starting"
	case strings.Contains(message, "pre-enqueue validation: repository exists in target organization"):
		stage = "validation"
		state = "target_exists"
	case strings.Contains(message, "pre-enqueue validation: successfully deleted existing repository"):
		stage = "validation"
		state = "target_cleaned"
	case strings.Contains(message, "repository exists in target organization, attempting to delete"):
		stage = "validation"
		state = "target_exists"
	case strings.Contains(message, "successfully deleted existing repository"):
		stage = "validation"
		state = "target_cleaned"
	case strings.Contains(message, "pre-enqueue validation"):
		stage = "queue"
		state = "pre_validation"
	case strings.Contains(message, "initializing archive job"):
		stage = "queue"
		state = "initializing_archive"
	case strings.Contains(message, "validating source repository"):
		stage = "validation"
		state = "checking_source"
	case strings.Contains(message, "estimating repository size"):
		stage = "validation"
		state = "estimating_size"
	case strings.Contains(message, "repository size:"):
		stage = "validation"
		state = "size_estimated"
	case strings.Contains(message, "checking if repository exists in target organization"):
		stage = "validation"
		state = "checking_target"
	case strings.Contains(message, "checking target repository"):
		stage = "validation"
		state = "checking_target"
	case strings.Contains(message, "getting target organization ID"):
		stage = "validation"
		state = "checking_target"
	case strings.Contains(message, "creating migration source"):
		stage = "setup"
		state = "creating_source"
	case strings.Contains(message, "creating migration source in GHEC"):
		stage = "setup"
		state = "creating_source"
	case strings.Contains(message, "starting archive generation"):
		stage = "archive"
		state = "preparing"
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
	case strings.Contains(message, "retrieving archive URL"):
		stage = "archive"
		state = "retrieving_url"
	case strings.Contains(message, "uploading archive to GitHub Owned Storage"):
		stage = "storage"
		state = "uploading"
	case strings.Contains(message, "uploading archive to GHOS"):
		stage = "storage"
		state = "uploading"
	case strings.Contains(message, "archive uploaded to GHOS:"):
		stage = "storage"
		state = "completed"
	case strings.Contains(message, "archive complete, waiting for migration worker"):
		stage = "queue"
		state = "waiting_migration_worker"
	case strings.Contains(message, "starting repository migration"):
		stage = "migration"
		state = "starting"
	case strings.Contains(message, "Starting migration import"):
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

// adjustStageAndStateForContext prevents progress regression by adjusting stage/state based on existing status
func adjustStageAndStateForContext(rawStage, rawState string, existing *payload.MigrationStatus, message string) (stage, state string) {
	// Define stage progression order for context checking
	stageOrder := map[string]int{
		"init":       0,
		"validation": 1,
		"setup":      2,
		"archive":    3,
		"storage":    4,
		"migration":  5,
		"queue":      6, // Queue is special - can appear at multiple points
		"error":      7,
		"unknown":    8,
	}

	currentStageOrder := stageOrder[existing.Stage]
	newStageOrder := stageOrder[rawStage]

	// Special handling for specific patterns that should not regress progress

	// 1. Validation checks during migration phase should be treated as migration activities
	if existing.Stage == "migration" || existing.Stage == "queue" && existing.State == "waiting_migration_worker" {
		if rawStage == "validation" {
			// These are validation checks happening during migration setup, not initial validation
			if strings.Contains(message, "repository exists in target organization") ||
				strings.Contains(message, "checking target repository") ||
				strings.Contains(message, "successfully deleted existing repository") {
				return "migration", "pre_migration_validation"
			}
		}

		// 2. GHOS uploads during migration should be treated as migration activities, not separate storage stage
		if rawStage == "storage" {
			if strings.Contains(message, "uploading archive to GHOS") {
				return "migration", "uploading_to_ghos"
			}
			if strings.Contains(message, "archive uploaded to GHOS") {
				return "migration", "ghos_upload_complete"
			}
		}
	}

	// 3. If we're past the archive stage and get archive-related messages, treat them as migration setup
	if currentStageOrder >= stageOrder["storage"] && rawStage == "archive" {
		// Archive operations happening late in the process are likely part of migration setup
		if strings.Contains(message, "retrieving archive URL") ||
			strings.Contains(message, "archive ready") {
			return "migration", "preparing_archive"
		}
	}

	// 4. Queue stage special handling - preserve progress based on state
	if rawStage == "queue" {
		// Queue states should preserve existing progress appropriately
		return rawStage, rawState
	}

	// 5. Prevent regression to earlier stages unless it's an error
	if newStageOrder < currentStageOrder && rawStage != "error" && rawStage != "queue" {
		// If the new stage would regress progress, keep the current stage but update state if meaningful
		if rawStage == "validation" && existing.Stage == "migration" {
			// Validation during migration becomes a migration sub-activity
			return "migration", "validating"
		}

		// For other regressions, preserve current stage but update state to reflect activity
		return existing.Stage, rawState
	}

	// 6. Allow progression to later stages
	return rawStage, rawState
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
	if m.config != nil && m.config.Storage.Enabled {
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
	m.updateStatusWithContext(repoFullName, newOverallStatus, message, timestamp, attemptStartTime, nil)
}

// updateStatusWithContext updates the in-memory status of a migration with additional context and triggers persistence.
// This version allows passing migration request context for proper initialization of new migration statuses.
// repoFullName is the unique identifier in "org/repo" format.
// newOverallStatus is one of payload.StatusInProgress, payload.StatusSucceeded, payload.StatusFailed.
// message often contains details about the current state or an error.
// timestamp is when this specific update event occurred.
// attemptStartTime is the time this particular migration attempt (fresh or retry) began.
// req is the migration request context (can be nil for existing migrations).
func (m *Migrator) updateStatusWithContext(repoFullName string, newOverallStatus string, message string, timestamp time.Time, attemptStartTime time.Time, req *payload.MigrationRequest) {
	m.mu.Lock() // Lock is released before potentially long-running operations

	var currentAttemptStatus *payload.MigrationStatus
	var isNewOrChanged bool
	var oldStatus *payload.MigrationStatus
	var migrationStatus *payload.MigrationStatus

	// Parse the message to determine stage and state for this update
	var existing *payload.MigrationStatus
	if existingStatus, ok := m.migrations[repoFullName]; ok {
		existing = existingStatus
	}
	stage, state := parseMessageToStageAndState(message, newOverallStatus, existing)

	if existing, ok := m.migrations[repoFullName]; !ok {
		// New status - Initialize with first stage
		isNewOrChanged = true

		// Determine progression data
		progressData := calculateProgressData(stage, state, nil, req)

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

		// Set UseGHOS from request context if available
		if req != nil {
			migrationStatus.UseGHOS = req.UseGHOS
			migrationStatus.TargetOrg = req.TargetOrg
			migrationStatus.GHESBaseURL = req.GHESBaseURL
			migrationStatus.DeleteIfExists = req.DeleteIfExists
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
		progressData := calculateProgressData(stage, state, existing, req)

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
	case "queue":
		return "Queue management"
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
		case "estimating_size":
			return "Estimating repository size"
		case "size_estimated":
			return "Repository size has been estimated"
		case "checking_target":
			return "Validating target organization"
		case "target_exists":
			return "Target repository exists, handling accordingly"
		case "target_cleaned":
			return "Existing target repository has been removed"
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
		case "preparing":
			return "Preparing archive generation"
		case "generating":
			return "Generating migration archive"
		case "waiting":
			return "Waiting for archive to be created"
		case "exported":
			return "Archive has been exported"
		case "retrieving_url":
			return "Retrieving archive download URL"
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
		case "pre_migration_validation":
			return "Performing pre-migration validation checks"
		case "uploading_to_ghos":
			return "Uploading archive to GitHub Owned Storage"
		case "ghos_upload_complete":
			return "Archive upload to GHOS completed"
		case "preparing_archive":
			return "Preparing migration archive"
		case "validating":
			return "Performing validation checks"
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
	case "queue":
		switch state {
		case "pre_validation":
			return "Performing pre-enqueue validation"
		case "initializing_archive":
			return "Initializing archive job"
		case "waiting_archive_worker":
			return "Waiting for available archive worker"
		case "waiting_migration_worker":
			return "Waiting for available migration worker"
		default:
			return "Waiting for worker"
		}
	default:
		return state
	}
}
