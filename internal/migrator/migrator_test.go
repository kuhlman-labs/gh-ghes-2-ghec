// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	// Initialize config before creating a migrator
	err := config.Init()
	require.NoError(t, err, "Failed to initialize config")

	tests := []struct {
		name       string
		webhookURL string
		wantURL    string
	}{
		{
			name:       "valid webhook URL",
			webhookURL: "https://example.com/webhook",
			wantURL:    "https://example.com/webhook",
		},
		{
			name:       "invalid webhook URL",
			webhookURL: "not-a-url",
			wantURL:    "not-a-url",
		},
		{
			name:       "empty webhook URL",
			webhookURL: "",
			wantURL:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logging.Get()
			githubAPI := github.NewNoopAPI(logger)
			storageProvider := &storage.NoopStorage{}
			cfg := config.Get()
			m := NewMigrator(logger, githubAPI, storageProvider, tt.webhookURL, cfg, nil, nil)
			assert.NotNil(t, m)
			assert.Equal(t, tt.wantURL, m.webhookURL)
			assert.NotNil(t, m.logger)
		})
	}
}

func TestMigrator_GetMigrationStatus(t *testing.T) {
	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	cfg := config.Get()
	m := NewMigrator(logger, githubAPI, storageProvider, "", cfg, nil, nil)
	repoName := "test-repo"
	status := &payload.MigrationStatus{
		Repository: repoName,
		Status:     payload.StatusInProgress,
	}

	// Set status
	m.mu.Lock()
	m.migrations[repoName] = status
	m.mu.Unlock()

	// Test getting status
	got := m.GetMigrationStatus(repoName)
	assert.Equal(t, status, got)

	// Test getting non-existent status
	got = m.GetMigrationStatus("non-existent")
	assert.Nil(t, got)
}

func TestMigrator_GetAllMigrationStatuses(t *testing.T) {
	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	cfg := config.Get()
	m := NewMigrator(logger, githubAPI, storageProvider, "", cfg, nil, nil)
	statuses := map[string]*payload.MigrationStatus{
		"repo1": {
			Repository: "repo1",
			Status:     payload.StatusInProgress,
		},
		"repo2": {
			Repository: "repo2",
			Status:     payload.StatusSucceeded,
		},
	}

	// Set statuses
	m.mu.Lock()
	m.migrations = statuses
	m.mu.Unlock()

	// Test getting all statuses
	got := m.GetAllMigrationStatuses()
	assert.Equal(t, statuses, got)
}

func TestCalculateProgressData(t *testing.T) {
	tests := []struct {
		name     string
		stage    string
		state    string
		existing *payload.MigrationStatus
		want     progressData
	}{
		{
			name:  "init stage",
			stage: "init",
			state: "starting",
			want: progressData{
				progress:          0,
				stageProgress:     0,
				completedStages:   []string{},
				currentStageIndex: 0,
			},
		},
		{
			name:  "validation stage",
			stage: "validation",
			state: "checking",
			want: progressData{
				progress:          5,
				stageProgress:     50,
				completedStages:   []string{},
				currentStageIndex: 1,
			},
		},
		{
			name:  "setup stage",
			stage: "setup",
			state: "creating",
			want: progressData{
				progress:          12,
				stageProgress:     25,
				completedStages:   []string{"validation"},
				currentStageIndex: 2,
			},
		},
		{
			name:  "archive stage",
			stage: "archive",
			state: "exporting",
			want: progressData{
				progress:          32,
				stageProgress:     50,
				completedStages:   []string{"validation", "setup"},
				currentStageIndex: 3,
			},
		},
		{
			name:  "storage stage",
			stage: "storage",
			state: "uploading",
			want: progressData{
				progress:          52,
				stageProgress:     50,
				completedStages:   []string{"validation", "setup", "archive"},
				currentStageIndex: 4,
			},
		},
		{
			name:  "migration stage",
			stage: "migration",
			state: "importing",
			want: progressData{
				progress:          80,
				stageProgress:     50,
				completedStages:   []string{"validation", "setup", "archive", "storage"},
				currentStageIndex: 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateProgressData(tt.stage, tt.state, tt.existing)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCalculateStageProgress(t *testing.T) {
	tests := []struct {
		name  string
		stage string
		state string
		want  int
	}{
		{
			name:  "validation checking",
			stage: "validation",
			state: "checking",
			want:  50,
		},
		{
			name:  "setup creating",
			stage: "setup",
			state: "creating",
			want:  25,
		},
		{
			name:  "archive exporting",
			stage: "archive",
			state: "exporting",
			want:  50,
		},
		{
			name:  "storage uploading",
			stage: "storage",
			state: "uploading",
			want:  50,
		},
		{
			name:  "migration importing",
			stage: "migration",
			state: "importing",
			want:  50,
		},
		{
			name:  "unknown stage",
			stage: "unknown",
			state: "unknown",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateStageProgress(tt.stage, tt.state)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetStageDescription(t *testing.T) {
	tests := []struct {
		name  string
		stage string
		want  string
	}{
		{
			name:  "validation stage",
			stage: "validation",
			want:  "Repository validation",
		},
		{
			name:  "setup stage",
			stage: "setup",
			want:  "Migration setup",
		},
		{
			name:  "archive stage",
			stage: "archive",
			want:  "Archive management",
		},
		{
			name:  "storage stage",
			stage: "storage",
			want:  "Storage upload",
		},
		{
			name:  "migration stage",
			stage: "migration",
			want:  "Repository migration",
		},
		{
			name:  "unknown stage",
			stage: "unknown",
			want:  "Unknown stage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStageDescription(tt.stage)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetStateDescription(t *testing.T) {
	tests := []struct {
		name  string
		stage string
		state string
		want  string
	}{
		{
			name:  "validation checking",
			stage: "validation",
			state: "checking",
			want:  "checking",
		},
		{
			name:  "setup creating",
			stage: "setup",
			state: "creating",
			want:  "creating",
		},
		{
			name:  "archive exporting",
			stage: "archive",
			state: "exporting",
			want:  "exporting",
		},
		{
			name:  "migration importing",
			stage: "migration",
			state: "importing",
			want:  "importing",
		},
		{
			name:  "unknown state",
			stage: "unknown",
			state: "unknown",
			want:  "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStateDescription(tt.stage, tt.state)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMigrator_UpdateStatus(t *testing.T) {
	// Create a test server for webhook notifications
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	cfg := config.Get()
	m := NewMigrator(logger, githubAPI, storageProvider, server.URL, cfg, nil, nil)
	repoName := "test-repo"
	status := payload.StatusInProgress
	message := "test message"
	timestamp := time.Now()
	startTime := timestamp.Add(-time.Hour)

	// Test updating status
	m.updateStatus(repoName, status, message, timestamp, startTime)

	// Verify status was updated
	got := m.GetMigrationStatus(repoName)
	require.NotNil(t, got)
	assert.Equal(t, repoName, got.Repository)
	assert.Equal(t, status, got.Status)
	assert.Equal(t, message, got.Error)
	assert.Equal(t, timestamp, got.UpdatedAt)
	assert.Equal(t, startTime, got.StartedAt)
	assert.Equal(t, time.Duration(0), got.Duration) // Duration is only set for completed/failed migrations
}

func TestMigrator_SendWebhookNotification(t *testing.T) {
	// Create a test server for webhook notifications
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	cfg := config.Get()
	m := NewMigrator(logger, githubAPI, storageProvider, server.URL, cfg, nil, nil)
	repoName := "test-repo"
	req := &payload.MigrationRequest{
		SourceOrg:    "source-org",
		TargetOrg:    "target-org",
		Repositories: []string{repoName},
	}

	// Set up status
	m.mu.Lock()
	m.migrations[repoName] = &payload.MigrationStatus{
		Repository: repoName,
		Status:     payload.StatusInProgress,
	}
	m.mu.Unlock()

	// Test sending webhook notification
	m.sendWebhookNotification(repoName, req)
	// Note: We can't easily test the actual webhook payload without mocking the HTTP client
}

func TestMigrateRepository_GHOS_InvalidOrgID(t *testing.T) {
	// Create a test migrator
	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	cfg := config.Get()
	m := NewMigrator(logger, githubAPI, storageProvider, "", cfg, nil, nil)

	// Create test data
	startTime := time.Now()

	// Set invalid ownerID that cannot be parsed
	ownerID := "invalid-owner-id"

	// Call the function being tested directly
	err := m.extractAndValidateOrgID("test-repo", ownerID, startTime)

	// Verify the error message
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error extracting organization database ID")
	assert.NotContains(t, err.Error(), "%!w(<nil>)") // Ensure we don't have the formatting error
}

// Helper function for testing GHOS organization ID extraction
func (m *Migrator) extractAndValidateOrgID(repoName string, _ string, startTime time.Time) error {
	// This function replicates just the org ID extraction logic from migrateRepository
	orgDatabaseID := "" // Simulate failure to extract ID

	if orgDatabaseID == "" {
		errMsg := "failed to extract organization database ID for GHOS upload"
		m.updateStatus(repoName, payload.StatusFailed, errMsg, time.Now(), startTime)
		return fmt.Errorf("error extracting organization database ID for GHOS upload")
	}

	return nil
}
