package migrator

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/stretchr/testify/assert"
)

func TestParseMessageToStageAndState(t *testing.T) {
	tests := []struct {
		name          string
		message       string
		overallStatus string
		expectedStage string
		expectedState string
	}{
		{
			name:          "starting migration process",
			message:       "starting migration process",
			overallStatus: payload.StatusInProgress,
			expectedStage: "init",
			expectedState: "starting",
		},
		{
			name:          "validating source repository",
			message:       "validating source repository",
			overallStatus: payload.StatusInProgress,
			expectedStage: "validation",
			expectedState: "checking_source",
		},
		{
			name:          "getting target organization ID",
			message:       "getting target organization ID",
			overallStatus: payload.StatusInProgress,
			expectedStage: "validation",
			expectedState: "checking_target",
		},
		{
			name:          "creating migration source",
			message:       "creating migration source",
			overallStatus: payload.StatusInProgress,
			expectedStage: "setup",
			expectedState: "creating_source",
		},
		{
			name:          "generating migration archive",
			message:       "generating migration archive",
			overallStatus: payload.StatusInProgress,
			expectedStage: "archive",
			expectedState: "generating",
		},
		{
			name:          "waiting for archive export",
			message:       "waiting for archive export",
			overallStatus: payload.StatusInProgress,
			expectedStage: "archive",
			expectedState: "waiting",
		},
		{
			name:          "waiting for archive export with status",
			message:       "waiting for archive export (status: exporting, elapsed: 30s)",
			overallStatus: payload.StatusInProgress,
			expectedStage: "archive",
			expectedState: "exporting",
		},
		{
			name:          "archive export state",
			message:       "archive export state: exported",
			overallStatus: payload.StatusInProgress,
			expectedStage: "archive",
			expectedState: "exported",
		},
		{
			name:          "archive ready for migration",
			message:       "archive ready for migration",
			overallStatus: payload.StatusInProgress,
			expectedStage: "archive",
			expectedState: "ready",
		},
		{
			name:          "uploading archive to GitHub Owned Storage",
			message:       "uploading archive to GitHub Owned Storage",
			overallStatus: payload.StatusInProgress,
			expectedStage: "storage",
			expectedState: "uploading",
		},
		{
			name:          "archive uploaded to GHOS",
			message:       "archive uploaded to GHOS: https://example.com/archive",
			overallStatus: payload.StatusInProgress,
			expectedStage: "storage",
			expectedState: "completed",
		},
		{
			name:          "starting repository migration",
			message:       "starting repository migration",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "starting",
		},
		{
			name:          "starting repository migration with GHOS archive",
			message:       "starting repository migration with GHOS archive",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "starting",
		},
		{
			name:          "migration created with ID",
			message:       "migration created with ID: 12345",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "created",
		},
		{
			name:          "waiting for migration to complete",
			message:       "waiting for migration to complete",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "waiting",
		},
		{
			name:          "migration in progress with state",
			message:       "migration in progress (state: importing, progress: 75%)",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "importing",
		},
		{
			name:          "migration state",
			message:       "migration state: importing",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "importing",
		},
		{
			name:          "migration completed successfully",
			message:       "migration completed successfully",
			overallStatus: payload.StatusSucceeded,
			expectedStage: "migration",
			expectedState: "completed",
		},
		{
			name:          "failed migration",
			message:       "API rate limit exceeded",
			overallStatus: payload.StatusFailed,
			expectedStage: "error",
			expectedState: "failed",
		},
		{
			name:          "unknown message",
			message:       "some unknown status message",
			overallStatus: payload.StatusInProgress,
			expectedStage: "unknown",
			expectedState: "unknown",
		},
		{
			name:          "empty message",
			message:       "",
			overallStatus: payload.StatusInProgress,
			expectedStage: "unknown",
			expectedState: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, state := parseMessageToStageAndState(tt.message, tt.overallStatus)
			assert.Equal(t, tt.expectedStage, stage)
			assert.Equal(t, tt.expectedState, state)
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	tests := []struct {
		name              string
		repoFullName      string
		newOverallStatus  string
		message           string
		existingStatus    *payload.MigrationStatus
		expectedNewStatus bool
	}{
		{
			name:              "create new status",
			repoFullName:      "org/new-repo",
			newOverallStatus:  payload.StatusInProgress,
			message:           "starting migration process",
			existingStatus:    nil,
			expectedNewStatus: true,
		},
		{
			name:             "update existing status",
			repoFullName:     "org/existing-repo",
			newOverallStatus: payload.StatusInProgress,
			message:          "generating migration archive",
			existingStatus: &payload.MigrationStatus{
				Repository: "org/existing-repo",
				Status:     payload.StatusInProgress,
				Stage:      "init",
				State:      "starting",
				Progress:   0,
			},
			expectedNewStatus: false,
		},
		{
			name:             "update to success",
			repoFullName:     "org/success-repo",
			newOverallStatus: payload.StatusSucceeded,
			message:          "migration completed successfully",
			existingStatus: &payload.MigrationStatus{
				Repository: "org/success-repo",
				Status:     payload.StatusInProgress,
				Stage:      "migration",
				State:      "in_progress",
				Progress:   90,
			},
			expectedNewStatus: false,
		},
		{
			name:             "update to failure",
			repoFullName:     "org/failed-repo",
			newOverallStatus: payload.StatusFailed,
			message:          "API rate limit exceeded",
			existingStatus: &payload.MigrationStatus{
				Repository: "org/failed-repo",
				Status:     payload.StatusInProgress,
				Stage:      "archive",
				State:      "generating",
				Progress:   25,
			},
			expectedNewStatus: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create migrator with minimal required fields
			m := &Migrator{
				logger:     slog.Default(),
				migrations: make(map[string]*payload.MigrationStatus),
				mu:         sync.RWMutex{},
				webhookURL: "", // No webhook for test
			}

			// Set up existing status if provided
			if tt.existingStatus != nil {
				m.migrations[tt.repoFullName] = tt.existingStatus
			}

			// Call updateStatus
			timestamp := time.Now()
			attemptStartTime := time.Now().Add(-1 * time.Minute)
			m.updateStatus(tt.repoFullName, tt.newOverallStatus, tt.message, timestamp, attemptStartTime)

			// Verify the status was created or updated
			m.mu.RLock()
			status, exists := m.migrations[tt.repoFullName]
			m.mu.RUnlock()

			assert.True(t, exists, "Status should exist after update")
			assert.NotNil(t, status, "Status should not be nil")
			assert.Equal(t, tt.newOverallStatus, status.Status)
			assert.Equal(t, tt.repoFullName, status.Repository)

			// Verify timestamp was updated
			assert.True(t, status.UpdatedAt.Equal(timestamp) || status.UpdatedAt.After(timestamp.Add(-1*time.Second)))

			// Verify stage and state were parsed correctly
			expectedStage, expectedState := parseMessageToStageAndState(tt.message, tt.newOverallStatus)
			assert.Equal(t, expectedStage, status.Stage)
			assert.Equal(t, expectedState, status.State)
		})
	}
}

func TestUpdateStatusConcurrency(t *testing.T) {
	// Test concurrent updates to the same repository
	m := &Migrator{
		logger:     slog.Default(),
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		webhookURL: "", // No webhook for test
	}

	repoName := "org/concurrent-repo"
	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	// Start multiple goroutines updating the same repository
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			message := fmt.Sprintf("update %d", id)
			timestamp := time.Now()
			attemptStartTime := time.Now().Add(-1 * time.Minute)

			m.updateStatus(repoName, payload.StatusInProgress, message, timestamp, attemptStartTime)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify the repository has a status (no panics occurred)
	m.mu.RLock()
	status, exists := m.migrations[repoName]
	m.mu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, status)
	assert.Equal(t, repoName, status.Repository)
}

func TestUpdateStatusMultipleRepositories(t *testing.T) {
	// Test updates to multiple different repositories
	m := &Migrator{
		logger:     slog.Default(),
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		webhookURL: "", // No webhook for test
	}

	numRepos := 5
	done := make(chan bool, numRepos)

	// Update multiple repositories concurrently
	for i := 0; i < numRepos; i++ {
		go func(id int) {
			repoName := fmt.Sprintf("org/repo-%d", id)
			message := "starting migration process"
			timestamp := time.Now()
			attemptStartTime := time.Now().Add(-1 * time.Minute)

			m.updateStatus(repoName, payload.StatusInProgress, message, timestamp, attemptStartTime)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numRepos; i++ {
		<-done
	}

	// Verify all repositories have statuses
	m.mu.RLock()
	assert.Equal(t, numRepos, len(m.migrations))
	for i := 0; i < numRepos; i++ {
		repoName := fmt.Sprintf("org/repo-%d", i)
		status, exists := m.migrations[repoName]
		assert.True(t, exists, "Repository %s should have status", repoName)
		assert.NotNil(t, status)
		assert.Equal(t, repoName, status.Repository)
	}
	m.mu.RUnlock()
}

func TestGetStageDescription(t *testing.T) {
	tests := []struct {
		stage       string
		expectedLen int // Expected minimum length of description
	}{
		{"init", 5},
		{"validation", 5},
		{"setup", 5},
		{"archive", 5},
		{"storage", 5},
		{"migration", 5},
		{"complete", 5},
		{"error", 5},
		{"unknown", 5},
		{"", 0}, // Empty stage might return empty or default description
	}

	for _, tt := range tests {
		t.Run(tt.stage, func(t *testing.T) {
			desc := getStageDescription(tt.stage)
			if tt.expectedLen > 0 {
				assert.True(t, len(desc) >= tt.expectedLen, "Description should be at least %d characters for stage %s", tt.expectedLen, tt.stage)
			}
			// The function should not panic for any input
		})
	}
}

func TestGetStateDescription(t *testing.T) {
	tests := []struct {
		stage       string
		state       string
		expectedLen int // Expected minimum length of description
	}{
		{"init", "starting", 5},
		{"validation", "checking_source", 5},
		{"validation", "checking_target", 5},
		{"setup", "creating_source", 5},
		{"archive", "generating", 5},
		{"archive", "waiting", 5},
		{"archive", "exported", 5},
		{"storage", "uploading", 5},
		{"migration", "starting", 5},
		{"migration", "in_progress", 5},
		{"migration", "completed", 5},
		{"error", "failed", 5},
		{"unknown", "unknown", 0},
		{"", "", 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.stage, tt.state), func(t *testing.T) {
			desc := getStateDescription(tt.stage, tt.state)
			if tt.expectedLen > 0 {
				assert.True(t, len(desc) >= tt.expectedLen, "Description should be at least %d characters for stage %s, state %s", tt.expectedLen, tt.stage, tt.state)
			}
			// The function should not panic for any input
		})
	}
}

func TestPersistAndNotifyStatusUpdate(t *testing.T) {
	// Test that persistAndNotifyStatusUpdate doesn't panic
	m := &Migrator{
		logger:     slog.Default(),
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		webhookURL: "",  // No webhook for test
		storage:    nil, // No storage for test
	}

	tests := []struct {
		name           string
		status         *payload.MigrationStatus
		isNewOrChanged bool
	}{
		{
			name: "valid status",
			status: &payload.MigrationStatus{
				Repository: "org/test-repo",
				Status:     payload.StatusInProgress,
				Stage:      "archive",
				State:      "generating",
				UpdatedAt:  time.Now(),
			},
			isNewOrChanged: true,
		},
		{
			name:           "nil status",
			status:         nil,
			isNewOrChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This should not panic even with nil storage and webhook
			m.persistAndNotifyStatusUpdate(tt.status, tt.isNewOrChanged)
		})
	}
}

func TestUpdateStatusProgressCalculation(t *testing.T) {
	// Test that progress calculation works correctly through status updates
	m := &Migrator{
		logger:     slog.Default(),
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		webhookURL: "", // No webhook for test
	}

	repoName := "org/progress-repo"
	timestamp := time.Now()
	attemptStartTime := time.Now().Add(-1 * time.Minute)

	// Test progression through stages
	stages := []struct {
		message     string
		status      string
		minProgress int
	}{
		{"starting migration process", payload.StatusInProgress, 0},
		{"validating source repository", payload.StatusInProgress, 2},   // validation(10) * checking_source(25%) = 2.5 → 2
		{"generating migration archive", payload.StatusInProgress, 12},  // validation(10) + archive(25) * generating(10%) = 12.5 → 12
		{"waiting for archive export", payload.StatusInProgress, 17},    // validation(10) + archive(25) * waiting(30%) = 17.5 → 17
		{"starting repository migration", payload.StatusInProgress, 45}, // validation(10) + setup(10) + archive(25) + migration(40) * starting(10%) = 49
		{"migration completed successfully", payload.StatusSucceeded, 100},
	}

	lastProgress := -1
	for _, stage := range stages {
		m.updateStatus(repoName, stage.status, stage.message, timestamp, attemptStartTime)

		m.mu.RLock()
		status, exists := m.migrations[repoName]
		m.mu.RUnlock()

		assert.True(t, exists)
		assert.NotNil(t, status)
		assert.True(t, status.Progress >= stage.minProgress, "Progress should be at least %d for message '%s', got %d", stage.minProgress, stage.message, status.Progress)
		assert.True(t, status.Progress >= lastProgress, "Progress should not go backwards")
		lastProgress = status.Progress
	}
}
