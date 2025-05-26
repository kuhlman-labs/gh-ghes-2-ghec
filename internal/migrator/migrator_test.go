// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
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
