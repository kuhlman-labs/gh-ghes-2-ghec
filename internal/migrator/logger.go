// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
// This file contains logging utilities specific to the migrator package.
package migrator

import (
	"context"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// component name for logging
const (
	ComponentName = "migrator"
)

// migration operation names
const (
	OperationMigration    = "repository_migration"
	OperationValidation   = "repository_validation"
	OperationArchive      = "archive_creation"
	OperationStatusUpdate = "status_update"
	OperationWebhook      = "webhook_notification"
)

// migration stages for logging
const (
	StageInit      = "init"
	StageValidate  = "validation"
	StageSetup     = "setup"
	StageArchive   = "archive"
	StageStorage   = "storage"
	StageMigration = "migration"
	StageComplete  = "complete"
)

// MigrationLogger creates a structured logger for migration operations.
// It sets up the logger with standard fields like component, operation, repository, and organizations.
func (m *Migrator) MigrationLogger(ctx context.Context, req *payload.MigrationRequest, repoName string) *logging.OperationLogger {
	// Create a new operation logger with context
	ctx = logging.ContextWithCorrelationID(ctx)
	logger := logging.NewOperationLogger(ctx, ComponentName, OperationMigration)

	// Add repository and organization details
	logger = logger.WithEntity(req.SourceOrg, repoName)

	return logger
}

// LogMigrationStart logs the start of a migration operation with structured context.
func (m *Migrator) LogMigrationStart(logger *logging.OperationLogger, repoName string) {
	logger.OperationStart("migration_start")
}

// LogMigrationComplete logs the successful completion of a migration.
func (m *Migrator) LogMigrationComplete(logger *logging.OperationLogger, repoName string, startTime time.Time) {
	duration := time.Since(startTime)
	logger.OperationComplete("migration_complete", duration.Milliseconds(),
		"duration_formatted", formatDuration(duration))
}

// LogMigrationFailed logs a failed migration with error details.
func (m *Migrator) LogMigrationFailed(logger *logging.OperationLogger, repoName string, err error, startTime time.Time) {
	duration := time.Since(startTime)
	// Determine if the error is retryable (could be enhanced with actual error analysis)
	retryable := false

	logger.OperationFailed("migration_failed", err, duration.Milliseconds(), retryable,
		"duration_formatted", formatDuration(duration))
}

// LogStageUpdate logs a migration stage update with consistent fields.
func (m *Migrator) LogStageUpdate(logger *logging.OperationLogger, stage, state string, progress int) {
	logger.StageUpdate(stage, state, progress)
}

// LogArchiveStatus logs the status of an archive operation.
func (m *Migrator) LogArchiveStatus(logger *logging.OperationLogger, status string, archiveID string, duration time.Duration) {
	logger.Info("Archive status update",
		logging.FieldArchiveID, archiveID,
		logging.FieldState, status,
		logging.FieldDuration, duration.Milliseconds(),
		"duration_formatted", formatDuration(duration))
}

// LogMigrationStatus logs the status of a migration operation.
func (m *Migrator) LogMigrationStatus(logger *logging.OperationLogger, status *payload.MigrationStatus) {
	logger.Info("Migration status update",
		logging.FieldMigrationID, status.MigrationID,
		logging.FieldStatus, status.Status,
		logging.FieldProgress, status.Progress,
		logging.FieldStage, status.Stage,
		"state", status.State,
		"error", status.Error,
		"updated_at", status.UpdatedAt.Format(time.RFC3339))
}

// LogWebhookEvent logs information about webhook notifications.
func (m *Migrator) LogWebhookEvent(ctx context.Context, repoName string, eventType string, success bool, statusCode int, err error) {
	logger := logging.NewOperationLogger(ctx, ComponentName, OperationWebhook).
		WithRepository(repoName)

	if success {
		logger.Info("Webhook notification sent",
			"event_type", eventType,
			"status_code", statusCode)
	} else {
		logger.Error("Webhook notification failed", err,
			"event_type", eventType,
			"status_code", statusCode)
	}
}
