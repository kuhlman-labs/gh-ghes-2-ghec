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
		{
			name:          "Migration process initiated",
			message:       "Migration process initiated",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "starting",
		},
		{
			name:          "Starting migration import",
			message:       "Starting migration import",
			overallStatus: payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "starting",
		},
		{
			name:          "estimating repository size",
			message:       "estimating repository size",
			overallStatus: payload.StatusInProgress,
			expectedStage: "validation",
			expectedState: "estimating_size",
		},
		{
			name:          "checking if repository exists in target organization",
			message:       "checking if repository exists in target organization",
			overallStatus: payload.StatusInProgress,
			expectedStage: "validation",
			expectedState: "checking_target",
		},
		{
			name:          "checking target repository",
			message:       "checking target repository",
			overallStatus: payload.StatusInProgress,
			expectedStage: "validation",
			expectedState: "checking_target",
		},
		{
			name:          "validating source repository",
			message:       "validating source repository",
			overallStatus: payload.StatusInProgress,
			expectedStage: "validation",
			expectedState: "checking_source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, state := parseMessageToStageAndState(tt.message, tt.overallStatus, nil)
			assert.Equal(t, tt.expectedStage, stage)
			assert.Equal(t, tt.expectedState, state)
		})
	}
}

func TestParseMessageToStageAndState_NewPatterns(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		status    string
		wantStage string
		wantState string
	}{
		{
			name:      "pre-enqueue validation",
			message:   "pre-enqueue validation",
			status:    payload.StatusInProgress,
			wantStage: "queue",
			wantState: "pre_validation",
		},
		{
			name:      "initializing archive job",
			message:   "initializing archive job",
			status:    payload.StatusInProgress,
			wantStage: "queue",
			wantState: "initializing_archive",
		},
		{
			name:      "repository size message",
			message:   "repository size: Small (1.00 MB)",
			status:    payload.StatusInProgress,
			wantStage: "validation",
			wantState: "size_estimated",
		},
		{
			name:      "pre-enqueue validation repository exists",
			message:   "pre-enqueue validation: repository exists in target organization, attempting to delete",
			status:    payload.StatusInProgress,
			wantStage: "validation",
			wantState: "target_exists",
		},
		{
			name:      "pre-enqueue validation successfully deleted",
			message:   "pre-enqueue validation: successfully deleted existing repository: target-org/repo",
			status:    payload.StatusInProgress,
			wantStage: "validation",
			wantState: "target_cleaned",
		},
		{
			name:      "creating migration source in GHEC",
			message:   "creating migration source in GHEC",
			status:    payload.StatusInProgress,
			wantStage: "setup",
			wantState: "creating_source",
		},
		{
			name:      "starting archive generation",
			message:   "starting archive generation",
			status:    payload.StatusInProgress,
			wantStage: "archive",
			wantState: "preparing",
		},
		{
			name:      "retrieving archive URL",
			message:   "retrieving archive URL",
			status:    payload.StatusInProgress,
			wantStage: "archive",
			wantState: "retrieving_url",
		},
		{
			name:      "uploading archive to GHOS",
			message:   "uploading archive to GHOS",
			status:    payload.StatusInProgress,
			wantStage: "storage",
			wantState: "uploading",
		},
		{
			name:      "archive complete waiting for migration worker",
			message:   "archive complete, waiting for migration worker",
			status:    payload.StatusInProgress,
			wantStage: "queue",
			wantState: "waiting_migration_worker",
		},
		{
			name:      "repository exists attempting to delete",
			message:   "repository exists in target organization, attempting to delete: target-org/repo",
			status:    payload.StatusInProgress,
			wantStage: "validation",
			wantState: "target_exists",
		},
		{
			name:      "successfully deleted existing repository",
			message:   "successfully deleted existing repository: target-org/repo",
			status:    payload.StatusInProgress,
			wantStage: "validation",
			wantState: "target_cleaned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, state := parseMessageToStageAndState(tt.message, tt.status, nil)
			if stage != tt.wantStage {
				t.Errorf("parseMessageToStageAndState() stage = %v, want %v", stage, tt.wantStage)
			}
			if state != tt.wantState {
				t.Errorf("parseMessageToStageAndState() state = %v, want %v", state, tt.wantState)
			}
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
			expectedStage, expectedState := parseMessageToStageAndState(tt.message, tt.newOverallStatus, nil)
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

func TestParseMessageToStageAndState_ContextAware(t *testing.T) {
	tests := []struct {
		name          string
		message       string
		overallStatus string
		existing      *payload.MigrationStatus
		expectedStage string
		expectedState string
		description   string
	}{
		{
			name:          "validation during migration should not regress",
			message:       "repository exists in target organization, attempting to delete",
			overallStatus: payload.StatusInProgress,
			existing: &payload.MigrationStatus{
				Stage: "migration",
				State: "starting",
			},
			expectedStage: "migration",
			expectedState: "pre_migration_validation",
			description:   "Validation during migration phase should be treated as migration activity",
		},
		{
			name:          "GHOS upload during migration should not regress",
			message:       "uploading archive to GHOS",
			overallStatus: payload.StatusInProgress,
			existing: &payload.MigrationStatus{
				Stage: "queue",
				State: "waiting_migration_worker",
			},
			expectedStage: "migration",
			expectedState: "uploading_to_ghos",
			description:   "GHOS uploads during migration should be migration activities",
		},
		{
			name:          "GHOS upload completion during migration",
			message:       "archive uploaded to GHOS: https://example.com",
			overallStatus: payload.StatusInProgress,
			existing: &payload.MigrationStatus{
				Stage: "migration",
				State: "uploading_to_ghos",
			},
			expectedStage: "migration",
			expectedState: "ghos_upload_complete",
			description:   "GHOS upload completion should be migration activity",
		},
		{
			name:          "archive operations during storage stage treated as migration setup",
			message:       "retrieving archive URL",
			overallStatus: payload.StatusInProgress,
			existing: &payload.MigrationStatus{
				Stage: "storage",
				State: "uploading",
			},
			expectedStage: "migration",
			expectedState: "preparing_archive",
			description:   "Archive operations after storage stage should be migration setup",
		},
		{
			name:          "normal progression should work",
			message:       "generating migration archive",
			overallStatus: payload.StatusInProgress,
			existing: &payload.MigrationStatus{
				Stage: "validation",
				State: "checking_target",
			},
			expectedStage: "archive",
			expectedState: "generating",
			description:   "Normal stage progression should continue to work",
		},
		{
			name:          "queue state should preserve progress",
			message:       "archive complete, waiting for migration worker",
			overallStatus: payload.StatusInProgress,
			existing: &payload.MigrationStatus{
				Stage: "archive",
				State: "exported",
			},
			expectedStage: "queue",
			expectedState: "waiting_migration_worker",
			description:   "Queue states should work normally",
		},
		{
			name:          "validation during migration worker waiting",
			message:       "checking target repository",
			overallStatus: payload.StatusInProgress,
			existing: &payload.MigrationStatus{
				Stage: "queue",
				State: "waiting_migration_worker",
			},
			expectedStage: "migration",
			expectedState: "pre_migration_validation",
			description:   "Validation when waiting for migration worker should be migration activity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, state := parseMessageToStageAndState(tt.message, tt.overallStatus, tt.existing)
			assert.Equal(t, tt.expectedStage, stage, "Stage mismatch: %s", tt.description)
			assert.Equal(t, tt.expectedState, state, "State mismatch: %s", tt.description)
		})
	}
}

func TestProgressRegressionPrevention(t *testing.T) {
	// This test demonstrates the fix for the progress regression issue
	// where validation and GHOS uploads during migration would drop progress

	m := &Migrator{
		logger:     slog.Default(),
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		webhookURL: "",
	}

	repoName := "org/test-repo"
	timestamp := time.Now()
	attemptStartTime := time.Now().Add(-1 * time.Minute)

	// Set up initial state - simulate having completed archive stage
	m.migrations[repoName] = &payload.MigrationStatus{
		Repository:        repoName,
		Status:            payload.StatusInProgress,
		Stage:             "archive",
		State:             "exported",
		Progress:          40, // Archive stage completed
		StageProgress:     80,
		CompletedStages:   []string{"validation", "setup"},
		CurrentStageIndex: 3,
		StartedAt:         attemptStartTime,
		UpdatedAt:         timestamp,
	}

	// Simulate the progression that was causing issues
	testCases := []struct {
		message       string
		status        string
		expectedStage string
		expectedState string
		minProgress   int
		description   string
	}{
		{
			message:       "archive complete, waiting for migration worker",
			status:        payload.StatusInProgress,
			expectedStage: "queue",
			expectedState: "waiting_migration_worker",
			minProgress:   40, // Should maintain progress from archive completion
			description:   "After archive completion, waiting for migration worker",
		},
		{
			message:       "repository exists in target organization, attempting to delete",
			status:        payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "pre_migration_validation",
			minProgress:   40, // Should NOT regress from previous progress
			description:   "Validation during migration should not regress progress",
		},
		{
			message:       "uploading archive to GHOS",
			status:        payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "uploading_to_ghos",
			minProgress:   40, // Should NOT regress from previous progress
			description:   "GHOS upload during migration should not regress progress",
		},
		{
			message:       "archive uploaded to GHOS: https://example.com",
			status:        payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "ghos_upload_complete",
			minProgress:   40, // Should advance from previous state
			description:   "GHOS upload completion should advance progress",
		},
		{
			message:       "migration created with ID: 12345",
			status:        payload.StatusInProgress,
			expectedStage: "migration",
			expectedState: "created",
			minProgress:   70, // Should advance significantly
			description:   "Migration creation should advance progress",
		},
	}

	lastProgress := 40 // Start with the initial progress
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("step_%d_%s", i+1, tc.expectedState), func(t *testing.T) {
			m.updateStatus(repoName, tc.status, tc.message, timestamp, attemptStartTime)

			m.mu.RLock()
			status, exists := m.migrations[repoName]
			m.mu.RUnlock()

			assert.True(t, exists, "Status should exist")
			assert.NotNil(t, status, "Status should not be nil")
			assert.Equal(t, tc.expectedStage, status.Stage, "Stage should match expected: %s", tc.description)
			assert.Equal(t, tc.expectedState, status.State, "State should match expected: %s", tc.description)
			assert.True(t, status.Progress >= tc.minProgress,
				"Progress should be at least %d%% for %s, got %d%%",
				tc.minProgress, tc.description, status.Progress)
			assert.True(t, status.Progress >= lastProgress,
				"Progress should not regress: was %d%%, now %d%% for %s",
				lastProgress, status.Progress, tc.description)

			lastProgress = status.Progress
			t.Logf("Step %d: %s -> Stage: %s, State: %s, Progress: %d%%",
				i+1, tc.description, status.Stage, status.State, status.Progress)
		})
	}
}
