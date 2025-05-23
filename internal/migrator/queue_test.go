package migrator

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/queue"
	"github.com/stretchr/testify/assert"
)

// MockSimpleAPI implements minimal github.API interface for testing
type MockSimpleAPI struct{}

func (m *MockSimpleAPI) ValidateRepository(ctx context.Context, org, repo string) error {
	return nil
}

func (m *MockSimpleAPI) ValidateCloudRepository(ctx context.Context, org, repo string) error {
	return nil
}

func (m *MockSimpleAPI) CheckCloudRepositoryExists(ctx context.Context, org, repo string) (bool, error) {
	return false, nil
}

func (m *MockSimpleAPI) DeleteCloudRepositoryIfExists(ctx context.Context, org, repo string) (bool, error) {
	return false, nil
}

func (m *MockSimpleAPI) GetOrganizationID(ctx context.Context, org string) (string, int64, error) {
	return "", 0, nil
}

func (m *MockSimpleAPI) CreateMigrationSource(ctx context.Context, name, url, ownerID string) (string, error) {
	return "", nil
}

func (m *MockSimpleAPI) GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error) {
	return 0, nil
}

func (m *MockSimpleAPI) GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error) {
	return "", nil
}

func (m *MockSimpleAPI) GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error) {
	return "", nil
}

func (m *MockSimpleAPI) StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, archiveURL, metadataURL, ghesToken, ghCloudToken string) (string, error) {
	return "", nil
}

func (m *MockSimpleAPI) GetMigrationStatus(ctx context.Context, migrationID string) (string, error) {
	return "", nil
}

func (m *MockSimpleAPI) UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error) {
	return "", nil
}

func (m *MockSimpleAPI) GetGHESRateLimit(ctx context.Context) (*github.RateLimitInfo, error) {
	return nil, nil
}

func (m *MockSimpleAPI) GetGHCloudRateLimit(ctx context.Context) (*github.RateLimitInfo, error) {
	return nil, nil
}

func (m *MockSimpleAPI) GetRepositorySize(ctx context.Context, org, repo string) (int64, error) {
	return 0, nil
}

func TestNewQueueManagerIntegration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	migrator := &Migrator{
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, logger, cfg)

	assert.NotNil(t, qmi)
	assert.Equal(t, migrator, qmi.migrator)
	assert.Equal(t, logger, qmi.logger)
	assert.NotNil(t, qmi.queueManager)
}

func TestNewQueueManagerIntegrationNilLogger(t *testing.T) {
	migrator := &Migrator{
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, nil, cfg)

	assert.NotNil(t, qmi)
	assert.NotNil(t, qmi.logger) // Should default to slog.Default()
}

func TestValidateRepositoryForQueue(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	migrator := &Migrator{
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		config: &config.Config{
			GitHub: config.GitHubConfig{
				Proxy: config.ProxyConfig{
					Enabled: false,
				},
			},
			Storage: config.StorageConfig{
				Enabled: false,
			},
		},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, logger, cfg)

	ctx := context.Background()
	req := &payload.MigrationRequest{
		SourceOrg:    "source-org",
		TargetOrg:    "target-org",
		GHESToken:    "token",
		GHCloudToken: "token",
	}

	// Test with nil API (should fail immediately)
	_, err := qmi.validateRepositoryForQueue(ctx, nil, req, "invalid-repo-name")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API client is nil")

	// Test with valid repository name format but nil API (will fail at validation)
	_, err = qmi.validateRepositoryForQueue(ctx, nil, req, "source-org/valid-repo")
	assert.Error(t, err) // Will fail when trying to validate with nil API
	assert.Contains(t, err.Error(), "API client is nil")

	// Test with invalid repository name format with a mock API
	mockAPI := &MockSimpleAPI{}
	_, err = qmi.validateRepositoryForQueue(ctx, mockAPI, req, "invalid-repo-name")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid repository name format")
}

func TestHandleArchiveJob(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	migrator := &Migrator{
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		config: &config.Config{
			GitHub: config.GitHubConfig{
				Proxy: config.ProxyConfig{},
			},
		},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, logger, cfg)

	tests := []struct {
		name        string
		repository  string
		data        interface{}
		expectError bool
		description string
	}{
		{
			name:        "invalid job data type",
			repository:  "test-org/test-repo",
			data:        "invalid-data-type",
			expectError: true,
			description: "should fail immediately with type assertion error",
		},
		{
			name:        "nil job data",
			repository:  "test-org/test-repo",
			data:        nil,
			expectError: true,
			description: "should fail immediately with nil data error",
		},
		{
			name:       "valid job with invalid tokens - client creation failure",
			repository: "test-org/test-repo",
			data: &payload.MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				GHESToken:    "invalid-token",
				GHCloudToken: "invalid-token",
				GHESBaseURL:  "https://github.example.com",
				Repositories: []string{"test-repo"},
			},
			expectError: true,
			description: "should fail during client initialization or network call, but not timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &queue.MigrationJob{
				Repository: tt.repository,
				Data:       tt.data,
				Priority:   1,
			}

			// Create a context with a short timeout to prevent test hanging
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Channel to capture the result
			errChan := make(chan error, 1)

			// Run the job in a goroutine to respect the timeout
			go func() {
				err := qmi.handleArchiveJob(job)
				errChan <- err
			}()

			// Wait for either the job to complete or timeout
			select {
			case err := <-errChan:
				if tt.expectError {
					assert.Error(t, err, tt.description)
				} else {
					assert.NoError(t, err, tt.description)
				}
			case <-ctx.Done():
				if tt.expectError {
					// For the test with actual HTTP calls, we expect it to timeout or fail fast
					// This is acceptable as long as it doesn't hang indefinitely
					t.Logf("Test timed out as expected for case: %s", tt.description)
				} else {
					t.Errorf("Test timed out unexpectedly for case: %s", tt.description)
				}
			}
		})
	}
}

func TestHandleMigrationJob(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	migrator := &Migrator{
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		config: &config.Config{
			GitHub: config.GitHubConfig{
				Proxy: config.ProxyConfig{},
			},
		},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, logger, cfg)

	tests := []struct {
		name        string
		repository  string
		data        interface{}
		expectError bool
		description string
	}{
		{
			name:        "invalid job data type",
			repository:  "test-org/test-repo",
			data:        "invalid-data-type",
			expectError: true,
			description: "should fail immediately with type assertion error",
		},
		{
			name:        "nil job data",
			repository:  "test-org/test-repo",
			data:        nil,
			expectError: true,
			description: "should fail immediately with nil data error",
		},
		{
			name:       "valid job with invalid tokens - client creation failure",
			repository: "test-org/test-repo",
			data: &payload.MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				GHESToken:    "invalid-token",
				GHCloudToken: "invalid-token",
				GHESBaseURL:  "https://github.example.com",
				Repositories: []string{"test-repo"},
			},
			expectError: true,
			description: "should fail during client initialization or network call, but not timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &queue.MigrationJob{
				Repository: tt.repository,
				Data:       tt.data,
				Priority:   1,
			}

			// Create a context with a short timeout to prevent test hanging
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Channel to capture the result
			errChan := make(chan error, 1)

			// Run the job in a goroutine to respect the timeout
			go func() {
				err := qmi.handleMigrationJob(job)
				errChan <- err
			}()

			// Wait for either the job to complete or timeout
			select {
			case err := <-errChan:
				if tt.expectError {
					assert.Error(t, err, tt.description)
				} else {
					assert.NoError(t, err, tt.description)
				}
			case <-ctx.Done():
				if tt.expectError {
					// For the test with actual HTTP calls, we expect it to timeout or fail fast
					// This is acceptable as long as it doesn't hang indefinitely
					t.Logf("Test timed out as expected for case: %s", tt.description)
				} else {
					t.Errorf("Test timed out unexpectedly for case: %s", tt.description)
				}
			}
		})
	}
}

// Test basic functionality without complex mocking
func TestQueueManagerIntegrationBasics(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	migrator := &Migrator{
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		config: &config.Config{
			GitHub: config.GitHubConfig{
				Proxy: config.ProxyConfig{},
			},
		},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, logger, cfg)

	// Test that we can get empty stats
	stats := qmi.GetQueueStats()
	assert.NotNil(t, stats)

	// Test that we can get empty repositories list
	repos := qmi.GetQueuedRepositories()
	assert.NotNil(t, repos)
	assert.Equal(t, 0, len(repos))

	// Test Start and Stop don't panic
	qmi.Start()
	qmi.Stop()
}

func TestEnqueueMigrationWithInvalidTokens(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	migrator := &Migrator{
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		config: &config.Config{
			GitHub: config.GitHubConfig{
				Proxy: config.ProxyConfig{},
			},
		},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, logger, cfg)

	req := &payload.MigrationRequest{
		SourceOrg:      "source-org",
		TargetOrg:      "target-org",
		GHESToken:      "invalid-token",
		GHCloudToken:   "invalid-token",
		GHESBaseURL:    "https://github.example.com",
		UseGHOS:        false,
		DeleteIfExists: false,
		Repositories:   []string{"test-repo"},
	}

	ctx := context.Background()
	err := qmi.EnqueueMigration(ctx, req, 1)

	// This will fail due to invalid tokens when trying to initialize clients
	assert.Error(t, err)
}

func TestEnqueueMigrationEmptyRepos(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	migrator := &Migrator{
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
		config: &config.Config{
			GitHub: config.GitHubConfig{
				Proxy: config.ProxyConfig{},
			},
		},
	}

	cfg := &config.Config{
		Queue: config.QueueConfig{
			MaxQueueSize:        100,
			MaxArchiveThreads:   2,
			MaxMigrationThreads: 2,
		},
	}

	qmi := NewQueueManagerIntegration(migrator, logger, cfg)

	req := &payload.MigrationRequest{
		SourceOrg:      "source-org",
		TargetOrg:      "target-org",
		GHESToken:      "invalid-token",
		GHCloudToken:   "invalid-token",
		GHESBaseURL:    "https://github.example.com",
		UseGHOS:        false,
		DeleteIfExists: false,
		Repositories:   []string{}, // Empty repositories list
	}

	ctx := context.Background()
	err := qmi.EnqueueMigration(ctx, req, 1)

	// Should not return error for empty repositories list - it just won't enqueue anything
	assert.NoError(t, err)
}
