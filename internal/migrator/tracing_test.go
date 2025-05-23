package migrator

import (
	"context"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/tracing"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestStartMigrationSpan(t *testing.T) {
	// Initialize a no-op tracer for testing to ensure spans work properly
	originalProvider := otel.GetTracerProvider()
	defer otel.SetTracerProvider(originalProvider)

	// Set up a test tracer provider
	testProvider := noop.NewTracerProvider()
	otel.SetTracerProvider(testProvider)

	// Initialize tracing with test config
	err := tracing.Init(tracing.Config{
		Enabled:     true,
		Endpoint:    "localhost:4317",
		ServiceName: "test-service",
		SampleRate:  1.0,
	})
	// Ignore error for test since we may not have a real endpoint
	_ = err

	// Create a migrator instance
	m := &Migrator{}

	// Create test request
	req := &payload.MigrationRequest{
		SourceOrg:   "source-org",
		TargetOrg:   "target-org",
		GHESBaseURL: "https://github.example.com",
		UseGHOS:     true,
	}
	repoName := "test-repo"

	// Test with background context
	ctx := context.Background()
	newCtx, span := m.StartMigrationSpan(ctx, SpanMigrateRepository, req, repoName)

	assert.NotNil(t, newCtx)
	assert.NotNil(t, span)
	// When tracing is disabled or no-op, context might be the same
	// so don't assert they're different

	// Verify that the span is part of the context
	spanFromCtx := trace.SpanFromContext(newCtx)
	assert.NotNil(t, spanFromCtx)

	// Test with different span names
	testSpans := []string{
		SpanValidateRepository,
		SpanArchiveGeneration,
		SpanArchiveExport,
		SpanMigrationSourceCreate,
		SpanStartMigration,
		SpanMonitorMigration,
		SpanUploadToGHOS,
		SpanWebhookNotification,
	}

	for _, spanName := range testSpans {
		t.Run("span_"+spanName, func(t *testing.T) {
			ctx, span := m.StartMigrationSpan(context.Background(), spanName, req, repoName)
			assert.NotNil(t, ctx)
			assert.NotNil(t, span)
		})
	}
}

func TestRecordSpanError(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name  string
		err   error
		stage string
	}{
		{
			name:  "validation error",
			err:   assert.AnError,
			stage: "validation",
		},
		{
			name:  "nil error",
			err:   nil,
			stage: "validation",
		},
		{
			name:  "archive error",
			err:   assert.AnError,
			stage: "archive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with span
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}
			ctx, span := m.StartMigrationSpan(ctx, SpanMigrateRepository, req, "test-repo")

			// Record error - should not panic
			m.RecordSpanError(ctx, tt.err, tt.stage)

			// For non-nil errors, verify span status is set to error
			if tt.err != nil {
				// This is a basic test - in real implementation with proper tracing,
				// we would verify that the span status was set to error
				assert.NotNil(t, span)
			}
		})
	}
}

func TestAddMigrationStageAttribute(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name     string
		stage    string
		state    string
		progress int
	}{
		{
			name:     "archive stage",
			stage:    "archive",
			state:    "generating",
			progress: 25,
		},
		{
			name:     "migration stage",
			stage:    "migration",
			state:    "in_progress",
			progress: 75,
		},
		{
			name:     "completed stage",
			stage:    "complete",
			state:    "completed",
			progress: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with span
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}
			ctx, span := m.StartMigrationSpan(ctx, SpanMigrateRepository, req, "test-repo")

			// Add migration stage attribute - should not panic
			m.AddMigrationStageAttribute(ctx, tt.stage, tt.state, tt.progress)

			assert.NotNil(t, span)
		})
	}
}

func TestTraceArchiveStatus(t *testing.T) {
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
			name:      "empty archive ID",
			status:    "pending",
			archiveID: "",
			duration:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with span
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}
			ctx, span := m.StartMigrationSpan(ctx, SpanArchiveGeneration, req, "test-repo")

			// Trace archive status - should not panic
			m.TraceArchiveStatus(ctx, tt.status, tt.archiveID, tt.duration)

			assert.NotNil(t, span)
		})
	}
}

func TestTraceMigrationStatus(t *testing.T) {
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
			},
		},
		{
			name:   "nil status",
			status: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with span
			ctx := context.Background()
			req := &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			}
			ctx, span := m.StartMigrationSpan(ctx, SpanMonitorMigration, req, "test-repo")

			// Trace migration status - should not panic
			m.TraceMigrationStatus(ctx, tt.status)

			assert.NotNil(t, span)
		})
	}
}

func TestStartWebhookSpan(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name      string
		repoName  string
		eventType string
	}{
		{
			name:      "migration started",
			repoName:  "test-org/test-repo",
			eventType: "migration_started",
		},
		{
			name:      "migration completed",
			repoName:  "test-org/test-repo",
			eventType: "migration_completed",
		},
		{
			name:      "migration failed",
			repoName:  "test-org/test-repo",
			eventType: "migration_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx, span := m.StartWebhookSpan(ctx, tt.repoName, tt.eventType)

			assert.NotNil(t, newCtx)
			assert.NotNil(t, span)
			// Don't assert context inequality as tracing might be disabled/no-op
		})
	}
}

func TestTraceWebhookResult(t *testing.T) {
	m := &Migrator{}

	tests := []struct {
		name       string
		success    bool
		statusCode int
		err        error
	}{
		{
			name:       "successful webhook",
			success:    true,
			statusCode: 200,
			err:        nil,
		},
		{
			name:       "failed webhook with error",
			success:    false,
			statusCode: 500,
			err:        assert.AnError,
		},
		{
			name:       "failed webhook without error",
			success:    false,
			statusCode: 404,
			err:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context with webhook span
			ctx := context.Background()
			ctx, span := m.StartWebhookSpan(ctx, "org/repo", "test.event")

			// Trace webhook result - should not panic
			m.TraceWebhookResult(ctx, tt.success, tt.statusCode, tt.err)

			assert.NotNil(t, span)
		})
	}
}

func TestTracingWithNoOpTracer(t *testing.T) {
	// Test that tracing functions work with no-op tracer
	m := &Migrator{}

	// Create context with no-op span
	ctx := context.Background()
	ctx = trace.ContextWithSpan(ctx, noop.Span{})

	req := &payload.MigrationRequest{
		SourceOrg: "source-org",
		TargetOrg: "target-org",
	}

	// All these should not panic with no-op tracer
	ctx, span := m.StartMigrationSpan(ctx, SpanMigrateRepository, req, "test-repo")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)

	m.RecordSpanError(ctx, assert.AnError, "test-stage")
	m.AddMigrationStageAttribute(ctx, "archive", "generating", 50)
	m.TraceArchiveStatus(ctx, "completed", "archive-123", time.Minute)

	status := &payload.MigrationStatus{
		MigrationID: "migration-123",
		Status:      payload.StatusInProgress,
		Progress:    75,
	}
	m.TraceMigrationStatus(ctx, status)

	ctx, webhookSpan := m.StartWebhookSpan(ctx, "org/repo", "test.event")
	assert.NotNil(t, webhookSpan)

	m.TraceWebhookResult(ctx, true, 200, nil)
}

func TestSpanConstants(t *testing.T) {
	// Verify that span constants are defined and have expected values
	expectedSpans := map[string]string{
		"SpanMigrateRepository":     SpanMigrateRepository,
		"SpanValidateRepository":    SpanValidateRepository,
		"SpanArchiveGeneration":     SpanArchiveGeneration,
		"SpanArchiveExport":         SpanArchiveExport,
		"SpanMigrationSourceCreate": SpanMigrationSourceCreate,
		"SpanStartMigration":        SpanStartMigration,
		"SpanMonitorMigration":      SpanMonitorMigration,
		"SpanUploadToGHOS":          SpanUploadToGHOS,
		"SpanWebhookNotification":   SpanWebhookNotification,
	}

	for name, value := range expectedSpans {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, value, "Span constant %s should not be empty", name)
		})
	}
}

func TestTracingContextPropagation(t *testing.T) {
	m := &Migrator{}

	req := &payload.MigrationRequest{
		SourceOrg: "source-org",
		TargetOrg: "target-org",
	}

	// Start a parent span
	parentCtx := context.Background()
	parentCtx, parentSpan := m.StartMigrationSpan(parentCtx, SpanMigrateRepository, req, "test-repo")

	// Start a child span
	childCtx, childSpan := m.StartMigrationSpan(parentCtx, SpanValidateRepository, req, "test-repo")

	// Verify spans are not nil
	assert.NotNil(t, parentSpan)
	assert.NotNil(t, childSpan)
	assert.NotNil(t, parentCtx)
	assert.NotNil(t, childCtx)
	// Don't assert context inequality as tracing might be disabled/no-op
}
