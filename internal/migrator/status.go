// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

func (m *Migrator) updateStatus(repoName, status, message string, timestamp time.Time, startTime time.Time) {
	m.mu.Lock()

	// Parse the message to determine stage and state
	var stage, state string

	// Extract stage information from message (with improved mapping)
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
	case strings.Contains(message, "migration state:"):
		stage = "migration"
		state = strings.TrimSpace(strings.TrimPrefix(message, "migration state:"))
	case strings.Contains(message, "migration completed successfully"):
		stage = "migration"
		state = "completed"
	case status == payload.StatusFailed:
		stage = "error"
		state = "failed"
	default:
		// Default values if no matching pattern is found
		stage = "unknown"
		state = "unknown"
	}

	// Check if this is a new status or a change in stage/state
	var isNewOrChanged bool
	var oldStatus *payload.MigrationStatus

	if existing, ok := m.migrations[repoName]; !ok {
		// New status - Initialize with first stage
		isNewOrChanged = true

		// Determine progression data
		progressData := calculateProgressData(stage, state, nil)

		m.migrations[repoName] = &payload.MigrationStatus{
			Repository:        repoName,
			Status:            status,
			Error:             message,
			UpdatedAt:         timestamp,
			Stage:             stage,
			State:             state,
			StartedAt:         startTime,
			Progress:          progressData.progress,
			StageProgress:     progressData.stageProgress,
			CompletedStages:   progressData.completedStages,
			TotalStages:       len(payload.MigrationStages),
			CurrentStageIndex: progressData.currentStageIndex,
		}
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
		existing.Status = status
		existing.UpdatedAt = timestamp
		existing.Stage = stage
		existing.State = state
		existing.Progress = progressData.progress
		existing.StageProgress = progressData.stageProgress
		existing.CompletedStages = progressData.completedStages
		existing.CurrentStageIndex = progressData.currentStageIndex
		existing.TotalStages = len(payload.MigrationStages)

		// Only update error message if there's an error
		if status == payload.StatusFailed {
			existing.Error = message
		}

		// Keep the original start time
		if existing.StartedAt.IsZero() {
			existing.StartedAt = startTime
		}

		// Check if stage or state changed
		isNewOrChanged = (oldStatus.Status != status ||
			oldStatus.Stage != stage ||
			oldStatus.State != state)
	}

	// Calculate duration for completed or failed migrations
	if status == payload.StatusSucceeded || status == payload.StatusFailed {
		if migStatus, ok := m.migrations[repoName]; ok && !migStatus.StartedAt.IsZero() {
			migStatus.Duration = timestamp.Sub(migStatus.StartedAt)
			migStatus.Progress = 100 // Set to 100% when completed
		}
	}

	// Release lock before logging and sending webhook
	m.mu.Unlock()

	// Log status update (only for significant changes to reduce noise)
	if isNewOrChanged {
		if status == payload.StatusSucceeded {
			duration := timestamp.Sub(startTime)
			m.logger.Info("Status updated",
				"repository", repoName,
				"status", status,
				"stage", stage,
				"state", state,
				"total_duration", formatDuration(duration),
				"progress", m.migrations[repoName].Progress,
			)
		} else {
			m.logger.Info("Status updated",
				"repository", repoName,
				"status", status,
				"stage", stage,
				"state", state,
				"progress", m.migrations[repoName].Progress,
			)
		}
	} else {
		m.logger.Info("Current status",
			"repository", repoName,
			"status", status,
			"stage", stage,
			"state", state,
			"progress", m.migrations[repoName].Progress,
		)
	}

	// Send webhook notification if the status changed
	if isNewOrChanged && m.webhookURL != "" {
		go m.sendWebhookNotification(repoName, nil)
	}
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
