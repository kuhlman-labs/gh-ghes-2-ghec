// Package logging provides structured logging utilities for the application.
// This file contains enhancements for consistent structured logging patterns.
package logging

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/google/uuid"
	myerrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
)

// contextKey is a custom type for keys in the context to avoid collisions
type contextKey string

// Common field names for structured logging
const (
	// Core fields
	FieldCorrelationID = "correlation_id" // For tracking requests through the system
	FieldOperation     = "operation"      // High-level operation name
	FieldComponent     = "component"      // System component (e.g., "server", "migrator")
	FieldSubcomponent  = "subcomponent"   // Within a component (e.g., "webhook", "api")

	// Entity-related fields
	FieldRepository   = "repository"   // Repository name
	FieldOrganization = "organization" // Organization name
	FieldMigrationID  = "migration_id" // Migration ID
	FieldArchiveID    = "archive_id"   // Archive ID

	// Operation metadata fields
	FieldDuration = "duration_ms" // Duration in milliseconds
	FieldStatus   = "status"      // Operation status (success, failed)
	FieldStage    = "stage"       // Migration stage
	FieldState    = "state"       // Detailed state within a stage
	FieldProgress = "progress"    // Progress percentage

	// Error-related fields
	FieldError         = "error"          // Error message
	FieldErrorCode     = "error_code"     // Error code for categorization
	FieldErrorCategory = "error_category" // Error classification category
	FieldRetryable     = "is_retryable"   // Whether the error is retryable

	// Context fields
	FieldSource = "source" // Code location (file:line)
	FieldAction = "action" // Specific action being taken
)

// Context keys
const (
	// KeyCorrelationID is the context key for the correlation ID
	KeyCorrelationID contextKey = "correlation_id"
)

// ContextWithCorrelationID adds a correlation ID to the context.
// If the context already has a correlation ID, it is reused.
// Returns the updated context.
func ContextWithCorrelationID(ctx context.Context) context.Context {
	if GetCorrelationID(ctx) != "" {
		return ctx
	}
	return context.WithValue(ctx, KeyCorrelationID, uuid.New().String())
}

// GetCorrelationID retrieves the correlation ID from a context.
// Returns an empty string if no correlation ID is present.
func GetCorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(KeyCorrelationID).(string); ok {
		return id
	}
	return ""
}

// OperationLogger wraps a logger with standard operation context.
// It provides consistent structured logging for a specific operation.
type OperationLogger struct {
	logger        *slog.Logger
	correlationID string
	component     string
	subcomponent  string
	operation     string
	repository    string
	organization  string
}

// NewOperationLogger creates a new operation logger with the given context and operation details.
func NewOperationLogger(ctx context.Context, component, operation string) *OperationLogger {
	// Ensure we have a correlation ID
	ctx = ContextWithCorrelationID(ctx)
	correlationID := GetCorrelationID(ctx)

	return &OperationLogger{
		logger: Get().With(
			FieldCorrelationID, correlationID,
			FieldComponent, component,
			FieldOperation, operation,
		),
		correlationID: correlationID,
		component:     component,
		operation:     operation,
	}
}

// WithSubcomponent adds a subcomponent to the logger.
func (o *OperationLogger) WithSubcomponent(subcomponent string) *OperationLogger {
	o.subcomponent = subcomponent
	o.logger = o.logger.With(FieldSubcomponent, subcomponent)
	return o
}

// WithRepository adds a repository to the logger.
func (o *OperationLogger) WithRepository(repository string) *OperationLogger {
	o.repository = repository
	o.logger = o.logger.With(FieldRepository, repository)
	return o
}

// WithOrganization adds an organization to the logger.
func (o *OperationLogger) WithOrganization(organization string) *OperationLogger {
	o.organization = organization
	o.logger = o.logger.With(FieldOrganization, organization)
	return o
}

// WithEntity adds entity information (repository and org) to the logger.
func (o *OperationLogger) WithEntity(organization, repository string) *OperationLogger {
	return o.WithOrganization(organization).WithRepository(repository)
}

// Debug logs a debug message with structured context.
func (o *OperationLogger) Debug(msg string, args ...any) {
	args = append(args, FieldSource, getCallerInfo())
	o.logger.Debug(msg, args...)
}

// Info logs an info message with structured context.
func (o *OperationLogger) Info(msg string, args ...any) {
	args = append(args, FieldSource, getCallerInfo())
	o.logger.Info(msg, args...)
}

// Warn logs a warning message with structured context.
func (o *OperationLogger) Warn(msg string, args ...any) {
	args = append(args, FieldSource, getCallerInfo())
	o.logger.Warn(msg, args...)
}

// Error logs an error message with structured context.
func (o *OperationLogger) Error(msg string, err error, args ...any) {
	newArgs := append([]any{FieldError, err, FieldSource, getCallerInfo()}, args...)
	o.logger.Error(msg, newArgs...)
}

// ErrorWithCategory logs an error with its classification category
func (o *OperationLogger) ErrorWithCategory(msg string, err error, args ...any) {
	if err == nil {
		return
	}

	// Classify the error and report it
	category := myerrors.Classify(err)
	myerrors.ReportError(err)

	// Add error information and category to the attributes
	newArgs := append([]any{
		FieldError, err,
		FieldErrorCategory, string(category),
		FieldSource, getCallerInfo(),
	}, args...)

	// Log the error with its category
	o.logger.Error(msg, newArgs...)
}

// StageUpdate logs a migration stage update with consistent fields.
func (o *OperationLogger) StageUpdate(stage, state string, progress int, args ...any) {
	newArgs := append([]any{
		FieldStage, stage,
		FieldState, state,
		FieldProgress, progress,
		FieldSource, getCallerInfo(),
	}, args...)
	o.logger.Info("Migration stage update", newArgs...)
}

// OperationStart logs the start of an operation.
func (o *OperationLogger) OperationStart(action string, args ...any) {
	newArgs := append([]any{
		FieldAction, action,
		FieldSource, getCallerInfo(),
	}, args...)
	o.logger.Info("Operation started", newArgs...)
}

// OperationComplete logs the successful completion of an operation.
func (o *OperationLogger) OperationComplete(action string, durationMs int64, args ...any) {
	newArgs := append([]any{
		FieldAction, action,
		FieldStatus, "success",
		FieldDuration, durationMs,
		FieldSource, getCallerInfo(),
	}, args...)
	o.logger.Info("Operation completed", newArgs...)
}

// OperationFailed logs a failed operation.
func (o *OperationLogger) OperationFailed(action string, err error, durationMs int64, retryable bool, args ...any) {
	newArgs := append([]any{
		FieldAction, action,
		FieldStatus, "failed",
		FieldError, err,
		FieldDuration, durationMs,
		FieldRetryable, retryable,
		FieldSource, getCallerInfo(),
	}, args...)
	o.logger.Error("Operation failed", newArgs...)
}

// OperationFailedWithCategory logs a failed operation with error classification.
func (o *OperationLogger) OperationFailedWithCategory(action string, err error, durationMs int64, args ...any) {
	if err == nil {
		return
	}

	// Classify the error and report it
	category := myerrors.Classify(err)
	myerrors.ReportError(err)

	// Determine if the error is retryable
	retryable := myerrors.IsTransient(err)

	newArgs := append([]any{
		FieldAction, action,
		FieldStatus, "failed",
		FieldError, err,
		FieldErrorCategory, string(category),
		FieldDuration, durationMs,
		FieldRetryable, retryable,
		FieldSource, getCallerInfo(),
	}, args...)

	o.logger.Error("Operation failed", newArgs...)
}

// getCallerInfo returns the calling function's file and line number.
func getCallerInfo() string {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		return "unknown:0"
	}
	// Extract just the filename without the full path
	short := file
	for i := len(file) - 1; i > 0; i-- {
		if file[i] == '/' {
			short = file[i+1:]
			break
		}
	}
	return short + ":" + itoa(line)
}

// itoa converts an integer to a string
func itoa(i int) string {
	if i < 0 {
		return "-" + uitoa(uint(-i))
	}
	return uitoa(uint(i))
}

// uitoa converts a uint to a string
func uitoa(u uint) string {
	var buf [20]byte // big enough for 64bit value base 10
	i := len(buf) - 1
	for u >= 10 {
		buf[i] = byte(u%10 + '0')
		i--
		u /= 10
	}
	buf[i] = byte(u + '0')
	return string(buf[i:])
}
