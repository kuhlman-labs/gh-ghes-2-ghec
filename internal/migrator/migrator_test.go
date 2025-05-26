// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"context"
	"log/slog"
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

// MockStorage is a simple mock implementation of MigrationStorage for testing
type MockStorage struct {
	statuses map[string]*payload.MigrationStatus
	archived map[string][]*payload.MigrationStatus
}

func (m *MockStorage) Initialize(ctx context.Context) error {
	return nil
}

func (m *MockStorage) Close() error {
	return nil
}

func (m *MockStorage) SaveMigrationStatus(ctx context.Context, status *payload.MigrationStatus) error {
	m.statuses[status.Repository] = status
	return nil
}

func (m *MockStorage) GetMigrationStatus(ctx context.Context, repoFullName string) (*payload.MigrationStatus, error) {
	status, exists := m.statuses[repoFullName]
	if !exists {
		return nil, nil
	}
	return status, nil
}

func (m *MockStorage) GetAllMigrationStatuses(ctx context.Context) (map[string]*payload.MigrationStatus, error) {
	return m.statuses, nil
}

func (m *MockStorage) DeleteMigrationStatus(ctx context.Context, repoFullName string) error {
	delete(m.statuses, repoFullName)
	return nil
}

func (m *MockStorage) CheckAndRepairDatabase(ctx context.Context) (string, error) {
	return "Mock database is healthy", nil
}

func (m *MockStorage) ArchiveMigrationAttempt(ctx context.Context, attempt *payload.MigrationStatus) error {
	if m.archived == nil {
		m.archived = make(map[string][]*payload.MigrationStatus)
	}
	m.archived[attempt.Repository] = append(m.archived[attempt.Repository], attempt)
	return nil
}

func (m *MockStorage) GetArchivedMigrationAttempts(ctx context.Context, repoFullName string) ([]*payload.MigrationStatus, error) {
	attempts, exists := m.archived[repoFullName]
	if !exists {
		return nil, nil
	}
	return attempts, nil
}

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

// Note: Removed TestMigrateRepository_GHOS_InvalidOrgID test as it was testing non-existent functionality

func TestStaleInProgressMigrationDetection(t *testing.T) {
	// Create a test config with stale detection enabled
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Enabled: true,
			StaleDetection: struct {
				Enabled         bool `mapstructure:"enabled"`
				MaxUpdateAge    int  `mapstructure:"max_update_age"`
				MaxMigrationAge int  `mapstructure:"max_migration_age"`
			}{
				Enabled:         true,
				MaxUpdateAge:    2, // 2 hours
				MaxMigrationAge: 6, // 6 hours
			},
		},
	}

	// Create mock storage
	mockStorage := &MockStorage{
		statuses: make(map[string]*payload.MigrationStatus),
		archived: make(map[string][]*payload.MigrationStatus),
	}

	// Create migrator
	migrator := NewMigrator(
		slog.Default(),
		nil, // GitHub API
		mockStorage,
		"", // webhook URL
		cfg,
		nil, // HTTP client
		nil, // tracer
	)

	now := time.Now()

	tests := []struct {
		name          string
		status        *payload.MigrationStatus
		expectedStale bool
		description   string
	}{
		{
			name: "recent_migration_not_stale",
			status: &payload.MigrationStatus{
				Repository: "test/repo1",
				Status:     payload.StatusInProgress,
				StartedAt:  now.Add(-30 * time.Minute),
				UpdatedAt:  now.Add(-10 * time.Minute),
			},
			expectedStale: false,
			description:   "Recent migration should not be considered stale",
		},
		{
			name: "old_update_stale",
			status: &payload.MigrationStatus{
				Repository: "test/repo2",
				Status:     payload.StatusInProgress,
				StartedAt:  now.Add(-1 * time.Hour),
				UpdatedAt:  now.Add(-3 * time.Hour), // 3 hours since last update
			},
			expectedStale: true,
			description:   "Migration with old update should be considered stale",
		},
		{
			name: "old_migration_stale",
			status: &payload.MigrationStatus{
				Repository: "test/repo3",
				Status:     payload.StatusInProgress,
				StartedAt:  now.Add(-7 * time.Hour), // 7 hours since start
				UpdatedAt:  now.Add(-1 * time.Hour),
			},
			expectedStale: true,
			description:   "Old migration should be considered stale",
		},
		{
			name: "completed_migration_not_stale",
			status: &payload.MigrationStatus{
				Repository: "test/repo4",
				Status:     payload.StatusSucceeded,
				StartedAt:  now.Add(-10 * time.Hour),
				UpdatedAt:  now.Add(-10 * time.Hour),
			},
			expectedStale: false,
			description:   "Completed migration should not be considered stale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isStale := migrator.isStaleInProgressMigration(tt.status)
			assert.Equal(t, tt.expectedStale, isStale, tt.description)
		})
	}
}

func TestStaleInProgressMigrationRecovery(t *testing.T) {
	// Create a test config with stale detection enabled
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Enabled: true,
			StaleDetection: struct {
				Enabled         bool `mapstructure:"enabled"`
				MaxUpdateAge    int  `mapstructure:"max_update_age"`
				MaxMigrationAge int  `mapstructure:"max_migration_age"`
			}{
				Enabled:         true,
				MaxUpdateAge:    2, // 2 hours
				MaxMigrationAge: 6, // 6 hours
			},
		},
	}

	// Create mock storage with a stale migration
	mockStorage := &MockStorage{
		statuses: make(map[string]*payload.MigrationStatus),
		archived: make(map[string][]*payload.MigrationStatus),
	}

	now := time.Now()
	staleStatus := &payload.MigrationStatus{
		Repository:  "test/stale-repo",
		Status:      payload.StatusInProgress,
		Stage:       "migration",
		State:       "in_progress",
		StartedAt:   now.Add(-7 * time.Hour), // 7 hours ago
		UpdatedAt:   now.Add(-3 * time.Hour), // 3 hours ago
		MigrationID: "12345",
		Progress:    75,
	}

	// Add the stale migration to storage
	mockStorage.statuses["test/stale-repo"] = staleStatus

	// Create migrator
	migrator := NewMigrator(
		slog.Default(),
		nil, // GitHub API
		mockStorage,
		"", // webhook URL
		cfg,
		nil, // HTTP client
		nil, // tracer
	)

	// Load migrations from storage (this should trigger stale detection)
	err := migrator.loadMigrationsFromStorage()
	assert.NoError(t, err)

	// Verify the stale migration was handled
	updatedStatus := mockStorage.statuses["test/stale-repo"]
	assert.Equal(t, payload.StatusFailed, updatedStatus.Status)
	assert.Contains(t, updatedStatus.Error, "interrupted by server shutdown")

	// Verify the stale migration was archived
	archivedAttempts := mockStorage.archived["test/stale-repo"]
	assert.Len(t, archivedAttempts, 1)
	assert.Equal(t, payload.StatusFailed, archivedAttempts[0].Status)
	assert.Contains(t, archivedAttempts[0].Error, "interrupted by server shutdown")
}

func TestStaleDetectionDisabled(t *testing.T) {
	// Create a test config with stale detection disabled
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Enabled: true,
			StaleDetection: struct {
				Enabled         bool `mapstructure:"enabled"`
				MaxUpdateAge    int  `mapstructure:"max_update_age"`
				MaxMigrationAge int  `mapstructure:"max_migration_age"`
			}{
				Enabled:         false, // Disabled
				MaxUpdateAge:    2,
				MaxMigrationAge: 6,
			},
		},
	}

	// Create mock storage with a stale migration
	mockStorage := &MockStorage{
		statuses: make(map[string]*payload.MigrationStatus),
		archived: make(map[string][]*payload.MigrationStatus),
	}

	now := time.Now()
	staleStatus := &payload.MigrationStatus{
		Repository: "test/stale-repo",
		Status:     payload.StatusInProgress,
		StartedAt:  now.Add(-7 * time.Hour), // 7 hours ago
		UpdatedAt:  now.Add(-3 * time.Hour), // 3 hours ago
	}

	// Add the stale migration to storage
	mockStorage.statuses["test/stale-repo"] = staleStatus

	// Create migrator
	migrator := NewMigrator(
		slog.Default(),
		nil, // GitHub API
		mockStorage,
		"", // webhook URL
		cfg,
		nil, // HTTP client
		nil, // tracer
	)

	// Load migrations from storage (stale detection should be skipped)
	err := migrator.loadMigrationsFromStorage()
	assert.NoError(t, err)

	// Verify the migration status was not changed
	updatedStatus := mockStorage.statuses["test/stale-repo"]
	assert.Equal(t, payload.StatusInProgress, updatedStatus.Status)
	assert.Empty(t, updatedStatus.Error)

	// Verify nothing was archived
	archivedAttempts := mockStorage.archived["test/stale-repo"]
	assert.Len(t, archivedAttempts, 0)
}
