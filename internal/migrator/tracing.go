// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
// This file contains tracing utilities for the migrator package.
package migrator

import (
	"context"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Trace span names
const (
	SpanMigrateRepository     = "migrate_repository"
	SpanValidateRepository    = "validate_repository"
	SpanArchiveGeneration     = "archive_generation"
	SpanArchiveExport         = "archive_export"
	SpanMigrationSourceCreate = "migration_source_create"
	SpanStartMigration        = "start_migration"
	SpanMonitorMigration      = "monitor_migration"
	SpanUploadToGHOS          = "upload_to_ghos"
	SpanWebhookNotification   = "webhook_notification"
)

// StartMigrationSpan starts a new span for a migration operation and returns the context with the span.
// It adds repository and organization information as span attributes.
func (m *Migrator) StartMigrationSpan(ctx context.Context, spanName string, req *payload.MigrationRequest, repoName string) (context.Context, trace.Span) {
	ctx, span := tracing.StartSpan(ctx, spanName)

	// Add common attributes to span
	span.SetAttributes(
		attribute.String("repository", repoName),
		attribute.String("source_org", req.SourceOrg),
		attribute.String("target_org", req.TargetOrg),
		attribute.String("ghes_base_url", req.GHESBaseURL),
		attribute.Bool("use_ghos", req.UseGHOS),
	)

	return ctx, span
}

// RecordSpanError records an error on the current span and adds contextual information.
func (m *Migrator) RecordSpanError(ctx context.Context, err error, stage string) {
	if err == nil {
		return
	}

	// Record the error on the span
	tracing.RecordError(ctx, err)

	// Add additional context about the error
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("error.stage", stage),
		attribute.String("error.message", err.Error()),
	)

	// Set span status to error
	span.SetStatus(codes.Error, err.Error())
}

// AddMigrationStageAttribute adds information about the current migration stage to the span.
func (m *Migrator) AddMigrationStageAttribute(ctx context.Context, stage string, state string, progress int) {
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("migration.stage", stage),
		attribute.String("migration.state", state),
		attribute.Int("migration.progress", progress),
	)
}

// TraceArchiveStatus adds archive status information to the current span.
func (m *Migrator) TraceArchiveStatus(ctx context.Context, status string, archiveID string, duration time.Duration) {
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("archive.id", archiveID),
		attribute.String("archive.status", status),
		attribute.Int64("archive.duration_ms", duration.Milliseconds()),
	)
}

// TraceMigrationStatus adds migration status information to the current span.
func (m *Migrator) TraceMigrationStatus(ctx context.Context, status *payload.MigrationStatus) {
	if status == nil {
		return
	}

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("migration.id", status.MigrationID),
		attribute.String("migration.status", status.Status),
		attribute.Int("migration.progress", status.Progress),
		attribute.String("migration.stage", status.Stage),
		attribute.String("migration.state", status.State),
	)

	if status.Error != "" {
		span.SetAttributes(attribute.String("migration.error", status.Error))
	}
}

// StartWebhookSpan starts a new span for a webhook notification.
func (m *Migrator) StartWebhookSpan(ctx context.Context, repoName string, eventType string) (context.Context, trace.Span) {
	ctx, span := tracing.StartSpan(ctx, SpanWebhookNotification)

	span.SetAttributes(
		attribute.String("repository", repoName),
		attribute.String("webhook.event_type", eventType),
	)

	return ctx, span
}

// TraceWebhookResult records the result of a webhook notification.
func (m *Migrator) TraceWebhookResult(ctx context.Context, success bool, statusCode int, err error) {
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.Bool("webhook.success", success),
		attribute.Int("webhook.status_code", statusCode),
	)

	if err != nil {
		tracing.RecordError(ctx, err)
		span.SetStatus(codes.Error, err.Error())
	}
}
