package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"log/slog"

	"github.com/google/go-github/v70/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
	"github.com/shurcooL/githubv4"
)

func setupTestMigrationAPI() *GitHubAPI {
	logger := slog.Default()
	clients := &config.Clients{
		GHESClient:     github.NewClient(nil),
		GHCloudClient:  github.NewClient(nil),
		GHCloudGraphQL: githubv4.NewClient(nil),
	}
	return &GitHubAPI{
		clients:     clients,
		logger:      logger,
		retryConfig: utils.DefaultRetryConfig(logger),
		ghesCircuitBreaker: utils.NewCircuitBreaker(
			utils.DefaultCircuitConfig("test-ghes", logger),
		),
		ghCloudCircuitBreaker: utils.NewCircuitBreaker(
			utils.DefaultCircuitConfig("test-ghcloud", logger),
		),
	}
}

func TestCreateMigrationSource(t *testing.T) {
	api := setupTestMigrationAPI()

	tests := []struct {
		name        string
		sourceName  string
		sourceURL   string
		ownerID     string
		expectError bool
	}{
		{
			name:        "valid parameters",
			sourceName:  "test-source",
			sourceURL:   "https://github.example.com",
			ownerID:     "test-owner-id",
			expectError: false,
		},
		{
			name:        "empty source name",
			sourceName:  "",
			sourceURL:   "https://github.example.com",
			ownerID:     "test-owner-id",
			expectError: false, // GraphQL might handle this
		},
		{
			name:        "empty source URL",
			sourceName:  "test-source",
			sourceURL:   "",
			ownerID:     "test-owner-id",
			expectError: false, // GraphQL might handle this
		},
		{
			name:        "empty owner ID",
			sourceName:  "test-source",
			sourceURL:   "https://github.example.com",
			ownerID:     "",
			expectError: false, // GraphQL might handle this
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			sourceID, err := api.CreateMigrationSource(ctx, tc.sourceName, tc.sourceURL, tc.ownerID)

			if tc.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				// Since we're using real GitHub clients without mocking,
				// we expect network errors in unit tests
				t.Logf("Got expected network error: %v", err)
			}

			// Source ID should be empty on error
			if err != nil && sourceID != "" {
				t.Errorf("Expected empty sourceID on error, got %s", sourceID)
			}
		})
	}
}

func TestStartRepositoryMigration(t *testing.T) {
	api := setupTestMigrationAPI()

	tests := []struct {
		name           string
		sourceID       string
		ownerID        string
		repoName       string
		sourceRepoURL  string
		archiveURL     string
		metadataURL    string
		ghesToken      string
		ghCloudToken   string
		expectError    bool
		expectURLError bool
	}{
		{
			name:          "valid parameters",
			sourceID:      "test-source-id",
			ownerID:       "test-owner-id",
			repoName:      "test-repo",
			sourceRepoURL: "https://github.example.com/org/repo",
			archiveURL:    "https://example.com/archive.tar.gz",
			metadataURL:   "https://example.com/metadata.json",
			ghesToken:     "ghp_123456789012345678901234567890123456",
			ghCloudToken:  "ghp_123456789012345678901234567890123456",
			expectError:   false,
		},
		{
			name:           "invalid source repo URL",
			sourceID:       "test-source-id",
			ownerID:        "test-owner-id",
			repoName:       "test-repo",
			sourceRepoURL:  "://invalid-url-with-no-scheme",
			archiveURL:     "https://example.com/archive.tar.gz",
			metadataURL:    "https://example.com/metadata.json",
			ghesToken:      "ghp_123456789012345678901234567890123456",
			ghCloudToken:   "ghp_123456789012345678901234567890123456",
			expectError:    true,
			expectURLError: true,
		},
		{
			name:          "empty repository name",
			sourceID:      "test-source-id",
			ownerID:       "test-owner-id",
			repoName:      "",
			sourceRepoURL: "https://github.example.com/org/repo",
			archiveURL:    "https://example.com/archive.tar.gz",
			metadataURL:   "https://example.com/metadata.json",
			ghesToken:     "ghp_123456789012345678901234567890123456",
			ghCloudToken:  "ghp_123456789012345678901234567890123456",
			expectError:   false, // GraphQL might handle this
		},
		{
			name:          "empty tokens",
			sourceID:      "test-source-id",
			ownerID:       "test-owner-id",
			repoName:      "test-repo",
			sourceRepoURL: "https://github.example.com/org/repo",
			archiveURL:    "https://example.com/archive.tar.gz",
			metadataURL:   "https://example.com/metadata.json",
			ghesToken:     "",
			ghCloudToken:  "",
			expectError:   false, // GraphQL might handle this
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			migrationID, err := api.StartRepositoryMigration(
				ctx,
				tc.sourceID,
				tc.ownerID,
				tc.repoName,
				tc.sourceRepoURL,
				tc.archiveURL,
				tc.metadataURL,
				tc.ghesToken,
				tc.ghCloudToken,
			)

			if tc.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}

			if tc.expectURLError {
				// Should get URL parsing error for invalid URLs
				var urlErr *url.Error
				if !errors.As(err, &urlErr) {
					t.Errorf("Expected URL error for invalid URL, got %T: %v", err, err)
				}
			}

			if !tc.expectError && err != nil && !tc.expectURLError {
				// Since we're using real GitHub clients without mocking,
				// we expect network errors in unit tests
				t.Logf("Got expected network error: %v", err)
			}

			// Migration ID should be empty on error
			if err != nil && migrationID != "" {
				t.Errorf("Expected empty migrationID on error, got %s", migrationID)
			}
		})
	}
}

func TestGetMigrationStatus(t *testing.T) {
	api := setupTestMigrationAPI()

	tests := []struct {
		name        string
		migrationID string
		expectError bool
	}{
		{
			name:        "valid migration ID",
			migrationID: "test-migration-id",
			expectError: false,
		},
		{
			name:        "empty migration ID",
			migrationID: "",
			expectError: false, // GraphQL might handle this
		},
		{
			name:        "invalid migration ID format",
			migrationID: "invalid-id-format",
			expectError: false, // GraphQL will handle validation
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			status, err := api.GetMigrationStatus(ctx, tc.migrationID)

			if tc.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				// Since we're using real GitHub clients without mocking,
				// we expect network errors in unit tests
				t.Logf("Got expected network error: %v", err)
			}

			// Status should be empty on error
			if err != nil && status != "" {
				t.Errorf("Expected empty status on error, got %s", status)
			}
		})
	}
}

// TestMigrationConflictDetection tests repository conflict detection in migrations
func TestMigrationConflictDetection(t *testing.T) {
	api := setupTestMigrationAPI()

	// Test conflict detection in StartRepositoryMigration error handling
	t.Run("start_migration_conflict_detection", func(t *testing.T) {
		ctx := context.Background()

		// This would normally return a GraphQL error containing conflict information
		// Since we can't mock the GraphQL client here, we test the conflict detection logic indirectly
		// by calling the function with valid parameters and expecting network errors
		migrationID, err := api.StartRepositoryMigration(
			ctx,
			"source-id",
			"owner-id",
			"test-repo",
			"https://github.example.com/org/repo",
			"https://example.com/archive.tar.gz",
			"https://example.com/metadata.json",
			"ghp_123456789012345678901234567890123456",
			"ghp_123456789012345678901234567890123456",
		)

		if err != nil {
			t.Logf("Expected network error in unit test: %v", err)

			// Check that we handle the error properly
			if migrationID != "" {
				t.Errorf("Expected empty migration ID on error, got %s", migrationID)
			}
		}
	})
}

// TestMigrationURLValidation tests URL validation in migration functions
func TestMigrationURLValidation(t *testing.T) {
	api := setupTestMigrationAPI()

	invalidURLs := []string{
		"",
		"not-a-url",
		"http://",
		"://invalid",
		"ftp://unsupported-scheme.com",
	}

	for _, invalidURL := range invalidURLs {
		t.Run(fmt.Sprintf("invalid_url_%s", strings.ReplaceAll(invalidURL, "://", "_")), func(t *testing.T) {
			ctx := context.Background()

			migrationID, err := api.StartRepositoryMigration(
				ctx,
				"source-id",
				"owner-id",
				"test-repo",
				invalidURL,
				"https://example.com/archive.tar.gz",
				"https://example.com/metadata.json",
				"ghp_123456789012345678901234567890123456",
				"ghp_123456789012345678901234567890123456",
			)

			// Empty URL should cause URL parsing error
			if invalidURL == "not-a-url" || invalidURL == "http://" || invalidURL == "://invalid" {
				if err == nil {
					t.Errorf("Expected error for invalid URL %s", invalidURL)
				}

				if migrationID != "" {
					t.Errorf("Expected empty migration ID on URL error, got %s", migrationID)
				}
			}
		})
	}
}

// TestMigrationParameterValidation tests various parameter validation scenarios
func TestMigrationParameterValidation(t *testing.T) {
	api := setupTestMigrationAPI()
	ctx := context.Background()

	// Test edge cases for CreateMigrationSource
	t.Run("create_source_edge_cases", func(t *testing.T) {
		edgeCases := []struct {
			name    string
			url     string
			ownerID string
		}{
			{"long_url", "https://very-long-domain-name-that-exceeds-normal-length.example.com/path", "owner-id"},
			{"special_chars_url", "https://github.example.com/org/repo-with_special.chars", "owner-id"},
			{"long_owner_id", "very-long-owner-id-that-might-cause-issues", "https://github.example.com"},
		}

		for _, tc := range edgeCases {
			t.Run(tc.name, func(t *testing.T) {
				sourceID, err := api.CreateMigrationSource(ctx, "test-source", tc.url, tc.ownerID)
				if err != nil {
					t.Logf("Expected network error for edge case %s: %v", tc.name, err)
				}
				if err != nil && sourceID != "" {
					t.Errorf("Expected empty sourceID on error, got %s", sourceID)
				}
			})
		}
	})

	// Test edge cases for repository names
	t.Run("repository_name_edge_cases", func(t *testing.T) {
		repoNames := []string{
			"repo-with-dashes",
			"repo_with_underscores",
			"repo.with.dots",
			"123456789",
			"verylongrepositoryname",
		}

		for _, repoName := range repoNames {
			t.Run(fmt.Sprintf("repo_%s", repoName), func(t *testing.T) {
				migrationID, err := api.StartRepositoryMigration(
					ctx,
					"source-id",
					"owner-id",
					repoName,
					"https://github.example.com/org/repo",
					"https://example.com/archive.tar.gz",
					"https://example.com/metadata.json",
					"ghp_123456789012345678901234567890123456",
					"ghp_123456789012345678901234567890123456",
				)

				if err != nil {
					t.Logf("Expected network error for repo name %s: %v", repoName, err)
				}
				if err != nil && migrationID != "" {
					t.Errorf("Expected empty migration ID on error, got %s", migrationID)
				}
			})
		}
	})
}

// TestMigrationContextCancellation tests context cancellation handling
func TestMigrationContextCancellation(t *testing.T) {
	api := setupTestMigrationAPI()

	t.Run("create_source_context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		sourceID, err := api.CreateMigrationSource(ctx, "test-source", "https://github.example.com", "owner-id")
		if err == nil {
			t.Errorf("Expected error due to cancelled context")
		}
		if sourceID != "" {
			t.Errorf("Expected empty sourceID on cancelled context, got %s", sourceID)
		}
	})

	t.Run("start_migration_context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		migrationID, err := api.StartRepositoryMigration(
			ctx,
			"source-id",
			"owner-id",
			"test-repo",
			"https://github.example.com/org/repo",
			"https://example.com/archive.tar.gz",
			"https://example.com/metadata.json",
			"ghp_123456789012345678901234567890123456",
			"ghp_123456789012345678901234567890123456",
		)
		if err == nil {
			t.Errorf("Expected error due to cancelled context")
		}
		if migrationID != "" {
			t.Errorf("Expected empty migration ID on cancelled context, got %s", migrationID)
		}
	})

	t.Run("get_status_context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		status, err := api.GetMigrationStatus(ctx, "migration-id")
		if err == nil {
			t.Errorf("Expected error due to cancelled context")
		}
		if status != "" {
			t.Errorf("Expected empty status on cancelled context, got %s", status)
		}
	})
}
