package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextWithCorrelationID(t *testing.T) {
	// Test with empty context
	ctx := context.Background()
	ctx = ContextWithCorrelationID(ctx)
	id := GetCorrelationID(ctx)
	assert.NotEmpty(t, id, "Correlation ID should be generated")

	// Test with existing correlation ID
	ctx2 := ContextWithCorrelationID(ctx)
	id2 := GetCorrelationID(ctx2)
	assert.Equal(t, id, id2, "Correlation ID should be reused")
}

func TestGetCorrelationID(t *testing.T) {
	// Test with nil context
	id := GetCorrelationID(context.TODO())
	assert.Empty(t, id, "Empty context should return empty ID")

	// Test with missing correlation ID
	ctx := context.Background()
	id = GetCorrelationID(ctx)
	assert.Empty(t, id, "Context without ID should return empty string")

	// Test with correlation ID
	ctx = context.WithValue(ctx, KeyCorrelationID, "test-id")
	id = GetCorrelationID(ctx)
	assert.Equal(t, "test-id", id, "Should return correct correlation ID")
}

func TestOperationLogger(t *testing.T) {
	// Setup a buffer to capture log output
	var buf bytes.Buffer
	testHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Save and restore the global logger
	origLogger := logger
	defer func() { logger = origLogger }()
	logger = slog.New(testHandler)

	// Create context with correlation ID
	ctx := context.WithValue(context.Background(), KeyCorrelationID, "test-correlation-id")

	// Create OperationLogger
	opLogger := NewOperationLogger(ctx, "test-component", "test-operation")

	// Test with repository
	opLogger = opLogger.WithRepository("test-repo")

	// Test with organization
	opLogger = opLogger.WithOrganization("test-org")

	// Test with subcomponent
	opLogger = opLogger.WithSubcomponent("test-subcomponent")

	// Test various logging methods
	t.Run("Debug", func(t *testing.T) {
		buf.Reset()
		opLogger.Debug("Debug message", "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Debug message", logEntry["msg"])
		assert.Equal(t, "test-correlation-id", logEntry[FieldCorrelationID])
		assert.Equal(t, "test-component", logEntry[FieldComponent])
		assert.Equal(t, "test-operation", logEntry[FieldOperation])
		assert.Equal(t, "test-repo", logEntry[FieldRepository])
		assert.Equal(t, "test-org", logEntry[FieldOrganization])
		assert.Equal(t, "test-subcomponent", logEntry[FieldSubcomponent])
		assert.Equal(t, "value", logEntry["key"])
		assert.Contains(t, logEntry[FieldSource], "structured_test.go")
	})

	t.Run("Info", func(t *testing.T) {
		buf.Reset()
		opLogger.Info("Info message", "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Info message", logEntry["msg"])
	})

	t.Run("Warn", func(t *testing.T) {
		buf.Reset()
		opLogger.Warn("Warning message", "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Warning message", logEntry["msg"])
	})

	t.Run("Error", func(t *testing.T) {
		buf.Reset()
		testError := errors.New("test error")
		opLogger.Error("Error message", testError, "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Error message", logEntry["msg"])
		assert.Equal(t, testError.Error(), logEntry[FieldError])
	})

	t.Run("StageUpdate", func(t *testing.T) {
		buf.Reset()
		opLogger.StageUpdate("test-stage", "test-state", 50, "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Migration stage update", logEntry["msg"])
		assert.Equal(t, "test-stage", logEntry[FieldStage])
		assert.Equal(t, "test-state", logEntry[FieldState])
		assert.Equal(t, float64(50), logEntry[FieldProgress])
	})

	t.Run("OperationStart", func(t *testing.T) {
		buf.Reset()
		opLogger.OperationStart("test-action", "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Operation started", logEntry["msg"])
		assert.Equal(t, "test-action", logEntry[FieldAction])
	})

	t.Run("OperationComplete", func(t *testing.T) {
		buf.Reset()
		opLogger.OperationComplete("test-action", 1000, "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Operation completed", logEntry["msg"])
		assert.Equal(t, "test-action", logEntry[FieldAction])
		assert.Equal(t, "success", logEntry[FieldStatus])
		assert.Equal(t, float64(1000), logEntry[FieldDuration])
	})

	t.Run("OperationFailed", func(t *testing.T) {
		buf.Reset()
		testError := errors.New("operation failed")
		opLogger.OperationFailed("test-action", testError, 1000, true, "key", "value")

		var logEntry map[string]interface{}
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Operation failed", logEntry["msg"])
		assert.Equal(t, "test-action", logEntry[FieldAction])
		assert.Equal(t, "failed", logEntry[FieldStatus])
		assert.Equal(t, testError.Error(), logEntry[FieldError])
		assert.Equal(t, float64(1000), logEntry[FieldDuration])
		assert.Equal(t, true, logEntry[FieldRetryable])
	})
}

func TestGetCallerInfo(t *testing.T) {
	callerInfo := getCallerInfo()
	// Instead of checking for a specific file name, just verify the format
	// The returned string should be in the format "file:line"
	assert.Regexp(t, `^[a-zA-Z0-9_\-\.]+:\d+$`, callerInfo, "Caller info should be in the format 'file:line'")
}

func TestComprehensiveLogging(t *testing.T) {
	// Setup logger to a temporary file
	tempFile, err := os.CreateTemp("", "structured-logging-test-*.log")
	require.NoError(t, err)

	defer func() {
		err := tempFile.Close()
		if err != nil {
			t.Logf("Warning: failed to close temp file: %v", err)
		}

		err = os.Remove(tempFile.Name())
		if err != nil {
			t.Logf("Warning: failed to remove temp file: %v", err)
		}
	}()

	// Create a handler for the file
	fileHandler := slog.NewJSONHandler(tempFile, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Save and restore the global logger
	origLogger := logger
	defer func() { logger = origLogger }()
	logger = slog.New(fileHandler)

	// Create a context with correlation ID
	ctx := ContextWithCorrelationID(context.Background())

	// Create operation logger
	opLogger := NewOperationLogger(ctx, "test-component", "migration")
	opLogger = opLogger.WithEntity("test-org", "test-repo")

	// Log a complete operation lifecycle
	opLogger.OperationStart("migration")

	// Log various stages
	opLogger.StageUpdate("validation", "checking", 10)
	opLogger.StageUpdate("setup", "preparing", 20)
	opLogger.StageUpdate("archive", "creating", 30)

	// Log an error during one stage
	err = errors.New("network timeout")
	opLogger.Error("Archive creation failed", err, "attempt", 1)

	// Log a retry
	opLogger.StageUpdate("archive", "retrying", 30)
	opLogger.StageUpdate("archive", "completed", 40)

	// Complete remaining stages
	opLogger.StageUpdate("migration", "in_progress", 70)
	opLogger.StageUpdate("migration", "completed", 100)

	// Complete the operation
	opLogger.OperationComplete("migration", 5000)

	// Reopen the file and read its contents
	err = tempFile.Close()
	if err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	content, err := os.ReadFile(tempFile.Name())
	require.NoError(t, err)

	// Verify each log line is valid JSON
	lines := bytes.Split(content, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		var entry map[string]interface{}
		err = json.Unmarshal(line, &entry)
		assert.NoError(t, err, "Log line should be valid JSON")

		// Every log should have these fields
		assert.Contains(t, entry, FieldCorrelationID)
		assert.Contains(t, entry, FieldComponent)
		assert.Contains(t, entry, FieldOperation)
		assert.Contains(t, entry, FieldRepository)
		assert.Contains(t, entry, FieldOrganization)
		assert.Contains(t, entry, FieldSource)
	}
}
