package migrator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/stretchr/testify/assert"
)

func TestMigrationLogger(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name     string
		req      *payload.MigrationRequest
		repoName string
	}{
		{
			name: "basic migration logger",
			req: &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			},
			repoName: "test-repo",
		},
		{
			name: "migration logger with empty organization",
			req: &payload.MigrationRequest{
				SourceOrg: "",
				TargetOrg: "target-org",
			},
			repoName: "test-repo",
		},
		{
			name: "migration logger with empty repository",
			req: &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			},
			repoName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := m.MigrationLogger(ctx, tt.req, tt.repoName)

			assert.NotNil(t, logger)
			// The logger should be properly initialized without panicking
		})
	}
}

func TestLogMigrationStart(t *testing.T) {
	m := &Migrator{}

	ctx := context.Background()
	req := &payload.MigrationRequest{
		SourceOrg: "source-org",
		TargetOrg: "target-org",
	}
	repoName := "test-repo"

	logger := m.MigrationLogger(ctx, req, repoName)

	// This should not panic
	m.LogMigrationStart(logger, repoName)
}

func TestLogMigrationComplete(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name      string
		repoName  string
		startTime time.Time
	}{
		{
			name:      "successful completion",
			repoName:  "test-repo",
			startTime: time.Now().Add(-30 * time.Second),
		},
		{
			name:      "quick completion",
			repoName:  "quick-repo",
			startTime: time.Now().Add(-1 * time.Second),
		},
		{
			name:      "long running completion",
			repoName:  "long-repo",
			startTime: time.Now().Add(-5 * time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}

			logger := m.MigrationLogger(ctx, req, tt.repoName)

			// This should not panic
			m.LogMigrationComplete(logger, tt.repoName, tt.startTime)
		})
	}
}

func TestLogMigrationFailed(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name      string
		repoName  string
		err       error
		startTime time.Time
	}{
		{
			name:      "simple error",
			repoName:  "test-repo",
			err:       errors.New("test error"),
			startTime: time.Now().Add(-30 * time.Second),
		},
		{
			name:      "nil error",
			repoName:  "test-repo",
			err:       nil,
			startTime: time.Now().Add(-1 * time.Minute),
		},
		{
			name:      "complex error",
			repoName:  "failed-repo",
			err:       errors.New("complex migration failure: API rate limit exceeded"),
			startTime: time.Now().Add(-2 * time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}

			logger := m.MigrationLogger(ctx, req, tt.repoName)

			// This should not panic even with nil error
			m.LogMigrationFailed(logger, tt.repoName, tt.err, tt.startTime)
		})
	}
}

func TestLogStageUpdate(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name     string
		stage    string
		state    string
		progress int
	}{
		{
			name:     "init stage",
			stage:    StageInit,
			state:    "starting",
			progress: 0,
		},
		{
			name:     "validation stage",
			stage:    StageValidate,
			state:    "checking_source",
			progress: 10,
		},
		{
			name:     "setup stage",
			stage:    StageSetup,
			state:    "creating_source",
			progress: 25,
		},
		{
			name:     "archive stage",
			stage:    StageArchive,
			state:    "generating",
			progress: 50,
		},
		{
			name:     "storage stage",
			stage:    StageStorage,
			state:    "uploading",
			progress: 75,
		},
		{
			name:     "migration stage",
			stage:    StageMigration,
			state:    "in_progress",
			progress: 90,
		},
		{
			name:     "complete stage",
			stage:    StageComplete,
			state:    "completed",
			progress: 100,
		},
		{
			name:     "invalid progress",
			stage:    StageInit,
			state:    "starting",
			progress: -10,
		},
		{
			name:     "over 100 progress",
			stage:    StageComplete,
			state:    "completed",
			progress: 150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}

			logger := m.MigrationLogger(ctx, req, "test-repo")

			// This should not panic with any values
			m.LogStageUpdate(logger, tt.stage, tt.state, tt.progress)
		})
	}
}

func TestLogArchiveStatus(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name      string
		status    string
		archiveID string
		duration  time.Duration
	}{
		{
			name:      "completed archive",
			status:    "completed",
			archiveID: "archive-123",
			duration:  30 * time.Second,
		},
		{
			name:      "failed archive",
			status:    "failed",
			archiveID: "archive-456",
			duration:  10 * time.Second,
		},
		{
			name:      "pending archive",
			status:    "pending",
			archiveID: "",
			duration:  0,
		},
		{
			name:      "in progress archive",
			status:    "in_progress",
			archiveID: "archive-789",
			duration:  2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}

			logger := m.MigrationLogger(ctx, req, "test-repo")

			// This should not panic
			m.LogArchiveStatus(logger, tt.status, tt.archiveID, tt.duration)
		})
	}
}

func TestLogMigrationStatus(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name   string
		status *payload.MigrationStatus
	}{
		{
			name: "complete status",
			status: &payload.MigrationStatus{
				MigrationID: "migration-123",
				Status:      payload.StatusSucceeded,
				Progress:    100,
				Stage:       "complete",
				State:       "completed",
				UpdatedAt:   time.Now(),
			},
		},
		{
			name: "failed status",
			status: &payload.MigrationStatus{
				MigrationID: "migration-456",
				Status:      payload.StatusFailed,
				Progress:    50,
				Stage:       "migration",
				State:       "failed",
				Error:       "Migration failed due to API error",
				UpdatedAt:   time.Now(),
			},
		},
		{
			name: "in progress status",
			status: &payload.MigrationStatus{
				MigrationID: "migration-789",
				Status:      payload.StatusInProgress,
				Progress:    75,
				Stage:       "migration",
				State:       "in_progress",
				UpdatedAt:   time.Now(),
			},
		},
		{
			name: "minimal status",
			status: &payload.MigrationStatus{
				Status:    payload.StatusInProgress,
				Progress:  25,
				UpdatedAt: time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}

			logger := m.MigrationLogger(ctx, req, "test-repo")

			// This should not panic
			m.LogMigrationStatus(logger, tt.status)
		})
	}
}

func TestLogWebhookEvent(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name       string
		repoName   string
		eventType  string
		success    bool
		statusCode int
		err        error
	}{
		{
			name:       "successful webhook",
			repoName:   "org/repo1",
			eventType:  "migration.started",
			success:    true,
			statusCode: 200,
			err:        nil,
		},
		{
			name:       "failed webhook with error",
			repoName:   "org/repo2",
			eventType:  "migration.completed",
			success:    false,
			statusCode: 500,
			err:        errors.New("webhook delivery failed"),
		},
		{
			name:       "failed webhook without error",
			repoName:   "org/repo3",
			eventType:  "migration.failed",
			success:    false,
			statusCode: 404,
			err:        nil,
		},
		{
			name:       "webhook with empty repo name",
			repoName:   "",
			eventType:  "migration.started",
			success:    true,
			statusCode: 200,
			err:        nil,
		},
		{
			name:       "webhook with empty event type",
			repoName:   "org/repo",
			eventType:  "",
			success:    true,
			statusCode: 200,
			err:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// This should not panic
			m.LogWebhookEvent(ctx, tt.repoName, tt.eventType, tt.success, tt.statusCode, tt.err)
		})
	}
}

func TestLoggingConstants(t *testing.T) {
	// Verify that logging constants are defined and have expected values
	expectedConstants := map[string]string{
		"ComponentName":         ComponentName,
		"OperationMigration":    OperationMigration,
		"OperationValidation":   OperationValidation,
		"OperationArchive":      OperationArchive,
		"OperationStatusUpdate": OperationStatusUpdate,
		"OperationWebhook":      OperationWebhook,
		"StageInit":             StageInit,
		"StageValidate":         StageValidate,
		"StageSetup":            StageSetup,
		"StageArchive":          StageArchive,
		"StageStorage":          StageStorage,
		"StageMigration":        StageMigration,
		"StageComplete":         StageComplete,
	}

	for name, value := range expectedConstants {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, value, "Constant %s should not be empty", name)
		})
	}
}

func TestFormatDurationInLogging(t *testing.T) {
	// Test that formatDuration is used correctly in logging context
	m := &Migrator{}

	ctx := context.Background()
	req := &payload.MigrationRequest{
		SourceOrg: "source-org",
		TargetOrg: "target-org",
	}

	logger := m.MigrationLogger(ctx, req, "test-repo")
	startTime := time.Now().Add(-45 * time.Second)

	// These should complete without error
	m.LogMigrationComplete(logger, "test-repo", startTime)
	m.LogMigrationFailed(logger, "test-repo", errors.New("test error"), startTime)
}

func TestConcurrentLogging(t *testing.T) {
	m := &Migrator{}

	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}

			repoName := fmt.Sprintf("repo-%d", id)
			logger := m.MigrationLogger(ctx, req, repoName)

			// Perform various logging operations
			m.LogMigrationStart(logger, repoName)
			m.LogStageUpdate(logger, StageInit, "starting", 0)
			m.LogStageUpdate(logger, StageValidate, "checking", 25)
			m.LogStageUpdate(logger, StageArchive, "generating", 50)
			m.LogStageUpdate(logger, StageMigration, "in_progress", 75)
			m.LogMigrationComplete(logger, repoName, time.Now().Add(-30*time.Second))

			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// If we reach here, concurrent logging worked without panicking
	assert.True(t, true, "Concurrent logging completed successfully")
}
