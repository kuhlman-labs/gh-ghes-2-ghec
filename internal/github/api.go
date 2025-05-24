// Package github provides functionality for interacting with GitHub APIs,
// both for GitHub Enterprise Server (GHES) and GitHub Enterprise Cloud (GHEC).
// It handles authentication, API requests, retries, and migration-specific operations.
package github

import (
	"context"
	"log/slog"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
)

// Repository represents a GitHub repository for wizard operations
type Repository struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Size        int64  `json:"size"`
	Private     bool   `json:"private"`
}

// API is the interface for interacting with GitHub APIs.
// It abstracts the GitHub API operations for better testability and flexibility.
type API interface {
	ValidateRepository(ctx context.Context, org, repo string) error
	ValidateCloudRepository(ctx context.Context, org, repo string) error
	CheckCloudRepositoryExists(ctx context.Context, org, repo string) (bool, error)
	DeleteCloudRepositoryIfExists(ctx context.Context, org, repo string) (bool, error)
	GetOrganizationID(ctx context.Context, org string) (string, int64, error)
	ValidateGHESOrganization(ctx context.Context, org string) error
	ValidateGHCloudOrganization(ctx context.Context, org string) error
	ListOrganizationRepositories(ctx context.Context, org string) ([]Repository, error)
	CreateMigrationSource(ctx context.Context, name, url, ownerID string) (string, error)
	GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error)
	GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error)
	GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error)
	StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, archiveURL, metadataURL, ghesToken, ghCloudToken string) (string, error)
	GetMigrationStatus(ctx context.Context, migrationID string) (string, error)
	UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error)
	GetGHESRateLimit(ctx context.Context) (*RateLimitInfo, error)
	GetGHCloudRateLimit(ctx context.Context) (*RateLimitInfo, error)
	GetRepositorySize(ctx context.Context, org, repo string) (int64, error)

	// IsTestImplementation returns true if this is a test implementation
	// that shouldn't be used for real operations
	IsTestImplementation() bool
}

// GitHubAPI handles GitHub API operations for both GitHub Enterprise Server and GitHub Cloud.
// It provides methods for repository validation, organization management,
// migration source creation, and migration operations.
type GitHubAPI struct {
	clients               *config.Clients
	logger                *slog.Logger
	retryConfig           *utils.RetryConfig
	ghesCircuitBreaker    *utils.CircuitBreaker
	ghCloudCircuitBreaker *utils.CircuitBreaker
}

// New creates a new GitHub API handler with the provided clients and logger.
// It configures default retry policies appropriate for GitHub API interactions.
func New(clients *config.Clients, logger *slog.Logger) API {
	// Create a retry configuration suitable for GitHub API calls
	retryConfig := utils.DefaultRetryConfig(logger).
		WithMaxRetries(5).                    // 6 total attempts
		WithInitialInterval(1 * time.Second). // Start with 1s backoff
		WithMaxInterval(30 * time.Second).    // Cap at 30s
		WithFactor(2.0)                       // Double the wait time each retry (exponential backoff)

	// Create circuit breakers for both GitHub endpoints
	ghesCircuitConfig := utils.DefaultCircuitConfig("github-enterprise-server", logger).
		WithFailureThreshold(5).           // Trip after 5 consecutive failures
		WithResetTimeout(1 * time.Minute). // Stay open for 1 minute before attempting recovery
		WithHalfOpenSuccessThreshold(2).   // Require 2 successful requests to close circuit
		WithMaxConcurrentRequests(20)      // Limit concurrent requests to 20

	ghCloudCircuitConfig := utils.DefaultCircuitConfig("github-cloud", logger).
		WithFailureThreshold(5).           // Trip after 5 consecutive failures
		WithResetTimeout(1 * time.Minute). // Stay open for 1 minute before attempting recovery
		WithHalfOpenSuccessThreshold(2).   // Require 2 successful requests to close circuit
		WithMaxConcurrentRequests(20)      // Limit concurrent requests to 20

	ghesCircuitBreaker := utils.NewCircuitBreaker(ghesCircuitConfig)
	ghCloudCircuitBreaker := utils.NewCircuitBreaker(ghCloudCircuitConfig)

	// Set up state change handlers to log circuit state transitions
	ghesCircuitBreaker.OnStateChange(func(oldState, newState utils.CircuitState) {
		logger.Info("GHES circuit breaker state changed",
			"from", string(oldState),
			"to", string(newState),
		)
	})

	ghCloudCircuitBreaker.OnStateChange(func(oldState, newState utils.CircuitState) {
		logger.Info("GitHub Cloud circuit breaker state changed",
			"from", string(oldState),
			"to", string(newState),
		)
	})

	return &GitHubAPI{
		clients:               clients,
		logger:                logger,
		retryConfig:           retryConfig,
		ghesCircuitBreaker:    ghesCircuitBreaker,
		ghCloudCircuitBreaker: ghCloudCircuitBreaker,
	}
}

// IsTestImplementation returns false for the real GitHubAPI implementation
func (a *GitHubAPI) IsTestImplementation() bool {
	return false
}
