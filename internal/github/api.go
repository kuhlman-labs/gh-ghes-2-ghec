// Package github provides functionality for interacting with GitHub APIs,
// both for GitHub Enterprise Server (GHES) and GitHub Enterprise Cloud (GHEC).
// It handles authentication, API requests, retries, and migration-specific operations.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-github/v70/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
	"github.com/shurcooL/githubv4"
)

// API is the interface for interacting with GitHub APIs.
// It abstracts the GitHub API operations for better testability and flexibility.
type API interface {
	ValidateRepository(ctx context.Context, org, repo string) error
	GetOrganizationID(ctx context.Context, org string) (string, int64, error)
	CreateMigrationSource(ctx context.Context, name, url, ownerID string) (string, error)
	GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error)
	GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error)
	GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error)
	StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, archiveURL, metadataURL, ghesToken, ghCloudToken string) (string, error)
	GetMigrationStatus(ctx context.Context, migrationID string) (string, error)
	UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error)
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

// retryableOperation executes a function with retries based on the API's retry configuration.
// It logs attempts and results, and backs off exponentially between retry attempts.
// The operation name is used for logging and observability.
func (a *GitHubAPI) retryableOperation(ctx context.Context, operation string, fn func() error) error {
	return utils.Retry(ctx, a.retryConfig, operation, fn)
}

// circuitProtectedGhesOperation executes a function with circuit breaker protection for GHES API calls.
// It wraps the function with the GHES circuit breaker to prevent cascading failures.
//
// Parameters:
//   - ctx: Context for cancellation control
//   - operation: A name for the operation (for logging)
//   - fn: The function to execute with circuit breaker protection
//
// Returns:
//   - error: Error from the function or circuit breaker if circuit is open
func (a *GitHubAPI) circuitProtectedGhesOperation(ctx context.Context, operation string, fn func() error) error {
	return a.ghesCircuitBreaker.Execute(func() error {
		// Use a retry operation within the circuit breaker
		err := a.retryableOperation(ctx, operation, fn)
		if err != nil {
			// Classify the error for better handling
			return a.classifyGitHubError(err)
		}
		return nil
	})
}

// circuitProtectedGhCloudOperation executes a function with circuit breaker protection for GitHub Cloud API calls.
// It wraps the function with the GitHub Cloud circuit breaker to prevent cascading failures.
//
// Parameters:
//   - ctx: Context for cancellation control
//   - operation: A name for the operation (for logging)
//   - fn: The function to execute with circuit breaker protection
//
// Returns:
//   - error: Error from the function or circuit breaker if circuit is open
func (a *GitHubAPI) circuitProtectedGhCloudOperation(ctx context.Context, operation string, fn func() error) error {
	return a.ghCloudCircuitBreaker.Execute(func() error {
		// Use a retry operation within the circuit breaker
		err := a.retryableOperation(ctx, operation, fn)
		if err != nil {
			// Classify the error for better handling
			return a.classifyGitHubError(err)
		}
		return nil
	})
}

// classifyGitHubError converts GitHub API errors to classified errors.
// This provides a consistent error classification for better handling.
func (a *GitHubAPI) classifyGitHubError(err error) error {
	if err == nil {
		return nil
	}

	// Handle GitHub API errors
	var respErr *github.ErrorResponse
	if errors.As(err, &respErr) {
		// Create HTTP status error with appropriate metadata
		httpErr := apierrors.NewHTTPStatusError(
			respErr.Response.StatusCode,
			respErr.Response.Request.URL.String(),
			respErr.Response.Request.Method,
		)

		// Create a message that includes GitHub error message
		msg := fmt.Sprintf("GitHub API error: %s", respErr.Message)
		if len(respErr.Errors) > 0 {
			errDetails := make([]string, 0, len(respErr.Errors))
			for _, e := range respErr.Errors {
				if e.Message != "" {
					errDetails = append(errDetails, e.Message)
				}
			}
			if len(errDetails) > 0 {
				msg += fmt.Sprintf(" - %s", strings.Join(errDetails, ", "))
			}
		}

		// Return the error with appropriate classification
		category := apierrors.Classify(httpErr)
		classifiedErr := apierrors.WrapWithCategory(err, category, msg)

		// Report the error for metrics and dashboard
		apierrors.ReportError(classifiedErr)

		return classifiedErr
	}

	// Handle other GitHub-specific errors
	// For now, we'll just use the standard classification
	category := apierrors.Classify(err)
	classifiedErr := apierrors.NewClassifiedError(err, category)

	// Report the error for metrics and dashboard
	apierrors.ReportError(classifiedErr)

	return classifiedErr
}

// retryableHTTP returns a function that executes HTTP requests with retry logic.
// It uses the RetryMiddleware from utils package to handle retries for HTTP requests.
// This is particularly useful for direct HTTP client operations not using the GitHub SDK.
//
// Parameters:
//   - client: The HTTP client to wrap with retry logic
//   - operation: A name for the operation being retried (for logging)
//
// Returns:
//   - A function that will execute an HTTP request with retries
func (a *GitHubAPI) retryableHTTP(client *http.Client, operation string) func(req *http.Request) (*http.Response, error) {
	httpExecutor := utils.RetryMiddleware(client, a.retryConfig, operation)

	return func(req *http.Request) (*http.Response, error) {
		resp, err := httpExecutor(req)
		if err != nil {
			// Classify HTTP errors for better handling
			return resp, a.classifyHTTPError(err, req)
		}

		// Check if response status code indicates an error
		if resp != nil && resp.StatusCode >= 400 {
			err = apierrors.NewHTTPStatusError(resp.StatusCode, req.URL.String(), req.Method)
			return resp, a.classifyHTTPError(err, req)
		}

		return resp, nil
	}
}

// classifyHTTPError converts HTTP errors to classified errors
func (a *GitHubAPI) classifyHTTPError(err error, req *http.Request) error {
	if err == nil {
		return nil
	}

	// If it's already an HTTPStatusError, just classify it
	var httpErr *apierrors.HTTPStatusError
	if errors.As(err, &httpErr) {
		category := apierrors.Classify(httpErr)
		classifiedErr := apierrors.NewClassifiedError(err, category)

		// Report the error for metrics and dashboard
		apierrors.ReportError(classifiedErr)

		return classifiedErr
	}

	// Handle URL errors (network, timeout, etc.)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		msg := fmt.Sprintf("HTTP %s request to %s failed", req.Method, req.URL.String())
		category := apierrors.Classify(err)
		classifiedErr := apierrors.WrapWithCategory(err, category, msg)

		// Report the error for metrics and dashboard
		apierrors.ReportError(classifiedErr)

		return classifiedErr
	}

	// For other errors, just use the standard classification
	category := apierrors.Classify(err)
	classifiedErr := apierrors.NewClassifiedError(err, category)

	// Report the error for metrics and dashboard
	apierrors.ReportError(classifiedErr)

	return classifiedErr
}

// circuitProtectedGhesHTTP returns a function that executes HTTP requests with circuit breaker
// and retry protection for GHES API calls that use direct HTTP operations.
//
// Parameters:
//   - client: The HTTP client to wrap
//   - operation: A name for the operation (for logging)
//
// Returns:
//   - A function that will execute an HTTP request with circuit breaker and retry protection
func (a *GitHubAPI) circuitProtectedGhesHTTP(client *http.Client, operation string) func(req *http.Request) (*http.Response, error) {
	// Get the standard retryable HTTP executor
	retryableExecutor := a.retryableHTTP(client, operation)

	// Return a function that first checks the circuit breaker state
	return func(req *http.Request) (*http.Response, error) {
		var resp *http.Response
		var err error

		// Execute within circuit breaker protection
		cbErr := a.ghesCircuitBreaker.Execute(func() error {
			resp, err = retryableExecutor(req)
			return err
		})

		if cbErr != nil {
			return nil, cbErr
		}

		return resp, nil
	}
}

// circuitProtectedGhCloudHTTP returns a function that executes HTTP requests with circuit breaker
// and retry protection for GitHub Cloud API calls that use direct HTTP operations.
//
// Parameters:
//   - client: The HTTP client to wrap
//   - operation: A name for the operation (for logging)
//
// Returns:
//   - A function that will execute an HTTP request with circuit breaker and retry protection
func (a *GitHubAPI) circuitProtectedGhCloudHTTP(client *http.Client, operation string) func(req *http.Request) (*http.Response, error) {
	// Get the standard retryable HTTP executor
	retryableExecutor := a.retryableHTTP(client, operation)

	// Return a function that first checks the circuit breaker state
	return func(req *http.Request) (*http.Response, error) {
		var resp *http.Response
		var err error

		// Execute within circuit breaker protection
		cbErr := a.ghCloudCircuitBreaker.Execute(func() error {
			resp, err = retryableExecutor(req)
			return err
		})

		if cbErr != nil {
			return nil, cbErr
		}

		return resp, nil
	}
}

// ValidateRepository checks if a repository exists in the source organization.
// It makes a REST API call to the GHES instance and verifies the repository's existence.
// Returns an error if the repository doesn't exist or can't be accessed.
func (a *GitHubAPI) ValidateRepository(ctx context.Context, org, repo string) error {
	startTime := time.Now()
	a.logger.Debug("Validating repository",
		"api", "GHES_REST",
		"method", "Repositories.Get",
		"org", org,
		"repo", repo,
	)

	var respStatus int
	err := a.circuitProtectedGhesOperation(ctx, "validate_repository", func() error {
		_, resp, err := a.clients.GHESClient.Repositories.Get(ctx, org, repo)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Repository validation failed",
			"api", "GHES_REST",
			"method", "Repositories.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
			"error", err,
		)
		return fmt.Errorf("repository not found: %w", err)
	}

	a.logger.Debug("Repository validation successful",
		"api", "GHES_REST",
		"method", "Repositories.Get",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"org", org,
		"repo", repo,
	)
	return nil
}

// GetOrganizationID retrieves the organization node ID from GitHub Enterprise Cloud.
// It makes a GraphQL API call to fetch the organization's unique identifier,
// which is needed for migration operations.
// Returns the organization ID as a string and any errors encountered.
func (a *GitHubAPI) GetOrganizationID(ctx context.Context, org string) (string, int64, error) {
	var query struct {
		Organization struct {
			ID         string `graphql:"id"`
			DatabaseID int64  `graphql:"databaseId"`
		} `graphql:"organization(login: $login)"`
	}

	variables := map[string]interface{}{
		"login": githubv4.String(org),
	}

	a.logger.Debug("Querying organization ID",
		"api", "GHEC_GraphQL",
		"method", "Query(organization)",
		"org", org,
	)

	startTime := time.Now()

	err := a.circuitProtectedGhCloudOperation(ctx, "get_organization_id", func() error {
		return a.clients.GHCloudGraphQL.Query(ctx, &query, variables)
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get organization ID",
			"api", "GHEC_GraphQL",
			"method", "Query(organization)",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"org", org,
		)
		return "", 0, fmt.Errorf("failed to get organization: %w", err)
	}

	// check if the organization ID is empty
	if query.Organization.ID == "" {
		a.logger.Error("Organization ID is empty",
			"api", "GHEC_GraphQL",
			"method", "Query(organization)",
			"duration_ms", duration.Milliseconds(),
			"org", org,
		)
		return "", 0, fmt.Errorf("organization ID is empty")
	}

	a.logger.Debug("Organization ID retrieved",
		"api", "GHEC_GraphQL",
		"method", "Query(organization)",
		"duration_ms", duration.Milliseconds(),
		"org", org,
		"id", query.Organization.ID,
		"databaseId", query.Organization.DatabaseID,
	)
	return query.Organization.ID, query.Organization.DatabaseID, nil
}

// CreateMigrationSource creates a migration source in GitHub Enterprise Cloud.
// A migration source defines where repositories will be migrated from.
// This function uses the GraphQL API to create a GITHUB_ARCHIVE type source
// and returns the created source's ID.
func (a *GitHubAPI) CreateMigrationSource(ctx context.Context, name, url, ownerID string) (string, error) {
	var mutation struct {
		CreateMigrationSource struct {
			MigrationSource struct {
				ID   string
				Name string
				URL  string
				Type string
			}
		} `graphql:"createMigrationSource(input: $input)"`
	}

	// Create string pointer for URL
	urlPtr := githubv4.String(url)

	input := githubv4.CreateMigrationSourceInput{
		Name:    githubv4.String(name),
		URL:     &urlPtr,
		OwnerID: githubv4.ID(ownerID),
		Type:    githubv4.MigrationSourceTypeGitHubArchive,
	}

	// Log the input parameters
	a.logger.Debug("Creating migration source",
		"api", "GHEC_GraphQL",
		"method", "Mutate(createMigrationSource)",
		"name", name,
		"ownerId", githubv4.ID(ownerID),
		"type", "GITHUB_ARCHIVE",
	)

	startTime := time.Now()

	err := a.circuitProtectedGhCloudOperation(ctx, "create_migration_source", func() error {
		return a.clients.GHCloudGraphQL.Mutate(ctx, &mutation, input, nil)
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to create migration source",
			"api", "GHEC_GraphQL",
			"method", "Mutate(createMigrationSource)",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"error_details", strings.ReplaceAll(err.Error(), "\n", " "),
		)
		return "", fmt.Errorf("failed to create migration source: %w", err)
	}

	// Check if the migration source ID is empty
	if mutation.CreateMigrationSource.MigrationSource.ID == "" {
		a.logger.Error("Empty migration source ID returned",
			"api", "GHEC_GraphQL",
			"method", "Mutate(createMigrationSource)",
			"duration_ms", duration.Milliseconds(),
		)
		return "", fmt.Errorf("createMigrationSource returned empty ID")
	}

	a.logger.Info("Migration source created",
		"api", "GHEC_GraphQL",
		"method", "Mutate(createMigrationSource)",
		"duration_ms", duration.Milliseconds(),
		"sourceId", mutation.CreateMigrationSource.MigrationSource.ID,
		"name", mutation.CreateMigrationSource.MigrationSource.Name,
		"type", mutation.CreateMigrationSource.MigrationSource.Type,
	)

	return mutation.CreateMigrationSource.MigrationSource.ID, nil
}

// GenerateMigrationArchive generates a migration archive for a repository on GHES
func (a *GitHubAPI) GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error) {
	repos := []string{repoName}
	opts := &github.MigrationOptions{
		LockRepositories: false,
	}

	a.logger.Debug("Generating migration archive",
		"api", "GHES_REST",
		"method", "Migrations.StartMigration",
		"org", orgName,
		"repo", repoName,
	)

	startTime := time.Now()

	var archive *github.Migration
	var respStatus int

	err := a.circuitProtectedGhesOperation(ctx, "generate_migration_archive", func() error {
		var resp *github.Response
		var err error
		archive, resp, err = a.clients.GHESClient.Migrations.StartMigration(ctx, orgName, repos, opts)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to create migration archive",
			"api", "GHES_REST",
			"method", "Migrations.StartMigration",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"error", err,
			"org", orgName,
			"repo", repoName,
		)
		return 0, fmt.Errorf("failed to create migration archive: %w", err)
	}

	archiveID := archive.GetID()
	a.logger.Debug("Migration archive created",
		"api", "GHES_REST",
		"method", "Migrations.StartMigration",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"archive_id", archiveID,
		"org", orgName,
		"repo", repoName,
	)

	return archiveID, nil
}

// GetMigrationArchiveStatus gets the status of a migration archive export on GHES
func (a *GitHubAPI) GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error) {
	a.logger.Debug("Getting archive status",
		"api", "GHES_REST",
		"method", "Migrations.MigrationStatus",
		"migrationID", migrationID,
		"org", orgName,
	)

	startTime := time.Now()

	var status *github.Migration
	var respStatus int

	err := a.circuitProtectedGhesOperation(ctx, "get_migration_archive_status", func() error {
		var resp *github.Response
		var err error
		status, resp, err = a.clients.GHESClient.Migrations.MigrationStatus(ctx, orgName, migrationID)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get archive status",
			"api", "GHES_REST",
			"method", "Migrations.MigrationStatus",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"error", err,
			"migrationID", migrationID,
			"org", orgName,
		)
		return "", fmt.Errorf("failed to get migration archive status: %w", err)
	}

	state := *status.State

	// Log additional details for failed migrations
	if state == "failed" {
		a.logger.Error("Archive export failed",
			"api", "GHES_REST",
			"method", "Migrations.MigrationStatus",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"migrationID", migrationID,
			"org", orgName,
			"state", state,
		)
	}

	a.logger.Debug("Archive status retrieved",
		"api", "GHES_REST",
		"method", "Migrations.MigrationStatus",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"migrationID", migrationID,
		"org", orgName,
		"state", state,
	)

	return state, nil
}

// GetMigrationArchiveURL gets the archive URL of a migration source
func (a *GitHubAPI) GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error) {
	a.logger.Debug("Getting migration archive URL",
		"api", "GHES_REST",
		"method", "Migrations.MigrationArchiveURL",
		"migrationId", archiveID,
		"org", orgName,
	)

	startTime := time.Now()

	var archiveURL string

	err := a.circuitProtectedGhesOperation(ctx, "get_migration_archive_url", func() error {
		var err error
		archiveURL, err = a.clients.GHESClient.Migrations.MigrationArchiveURL(ctx, orgName, archiveID)
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get archive URL",
			"api", "GHES_REST",
			"method", "Migrations.MigrationArchiveURL",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"archiveId", archiveID,
			"org", orgName,
		)
		return "", fmt.Errorf("failed to create request for migration archive URL: %w", err)
	}

	a.logger.Debug("Archive URL retrieved",
		"api", "GHES_REST",
		"method", "Migrations.MigrationArchiveURL",
		"duration_ms", duration.Milliseconds(),
		"archiveId", archiveID,
		"org", orgName,
	)

	return archiveURL, nil
}

// StartRepositoryMigration starts a repository migration in GHEC
func (a *GitHubAPI) StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, archiveURL, metadataURL, ghesToken, ghCloudToken string) (string, error) {
	var mutation struct {
		StartRepositoryMigration struct {
			RepositoryMigration struct {
				ID              string
				MigrationSource struct {
					ID   string
					Name string
					Type string
				}
				SourceURL string
			}
		} `graphql:"startRepositoryMigration(input: $input)"`
	}

	// Parse the source repository URL
	parsedURL, err := url.Parse(sourceRepoURL)
	if err != nil {
		a.logger.Error("Failed to parse source repository URL",
			"error", err,
			"url", sourceRepoURL,
		)
		return "", fmt.Errorf("invalid source repository URL: %w", err)
	}

	// Create URI from parsed URL
	sourceRepoURI := githubv4.URI{URL: parsedURL}

	// Create input parameters for GraphQL mutation
	continueOnError := githubv4.Boolean(true)
	accessToken := githubv4.String(ghesToken)
	gitHubPat := githubv4.String(ghCloudToken)
	targetRepoVisibility := githubv4.String("private")
	gitArchiveURL := githubv4.String(archiveURL)
	metadataArchiveURL := githubv4.String(metadataURL)

	// Create the input variable
	input := githubv4.StartRepositoryMigrationInput{
		SourceID:             githubv4.ID(sourceID),
		OwnerID:              githubv4.ID(ownerID),
		RepositoryName:       githubv4.String(repoName),
		ContinueOnError:      &continueOnError,
		AccessToken:          &accessToken,
		GitHubPat:            &gitHubPat,
		TargetRepoVisibility: &targetRepoVisibility,
		SourceRepositoryURL:  sourceRepoURI,
		GitArchiveURL:        &gitArchiveURL,
		MetadataArchiveURL:   &metadataArchiveURL,
	}

	// Log the input parameters for debugging
	a.logger.Debug("Starting repository migration",
		"api", "GHEC_GraphQL",
		"method", "Mutate(startRepositoryMigration)",
		"sourceId", sourceID,
		"ownerId", ownerID,
		"repositoryName", repoName,
		"sourceRepositoryUrl", sourceRepoURL,
	)

	startTime := time.Now()

	err = a.circuitProtectedGhCloudOperation(ctx, "start_repository_migration", func() error {
		return a.clients.GHCloudGraphQL.Mutate(ctx, &mutation, input, nil)
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Migration start failed",
			"api", "GHEC_GraphQL",
			"method", "Mutate(startRepositoryMigration)",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"error_details", strings.ReplaceAll(err.Error(), "\n", " "),
			"repository", repoName,
		)
		return "", fmt.Errorf("failed to start repository migration: %w", err)
	}

	// Check if the mutation response is valid
	if mutation.StartRepositoryMigration.RepositoryMigration.ID == "" {
		a.logger.Error("Empty migration ID returned",
			"api", "GHEC_GraphQL",
			"method", "Mutate(startRepositoryMigration)",
			"duration_ms", duration.Milliseconds(),
			"repository", repoName,
		)
		return "", fmt.Errorf("startRepositoryMigration returned empty migration ID")
	}

	migrationID := mutation.StartRepositoryMigration.RepositoryMigration.ID
	a.logger.Info("Repository migration started",
		"api", "GHEC_GraphQL",
		"method", "Mutate(startRepositoryMigration)",
		"duration_ms", duration.Milliseconds(),
		"migrationId", migrationID,
		"repository", repoName,
		"sourceId", mutation.StartRepositoryMigration.RepositoryMigration.MigrationSource.ID,
	)

	return migrationID, nil
}

// GetMigrationStatus gets the current status of a repository migration from GHEC
func (a *GitHubAPI) GetMigrationStatus(ctx context.Context, migrationID string) (string, error) {
	var query struct {
		Node struct {
			Migration struct {
				ID              string `graphql:"id"`
				SourceURL       string `graphql:"sourceUrl"`
				MigrationSource struct {
					Name string `graphql:"name"`
				} `graphql:"migrationSource"`
				State         string `graphql:"state"`
				FailureReason string `graphql:"failureReason"`
			} `graphql:"... on Migration"`
		} `graphql:"node(id: $id)"`
	}

	variables := map[string]interface{}{
		"id": githubv4.ID(migrationID),
	}

	a.logger.Debug("Checking migration status",
		"api", "GHEC_GraphQL",
		"method", "Query(node/Migration)",
		"migrationId", migrationID,
	)

	startTime := time.Now()

	err := a.circuitProtectedGhCloudOperation(ctx, "get_migration_status", func() error {
		return a.clients.GHCloudGraphQL.Query(ctx, &query, variables)
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get migration status",
			"api", "GHEC_GraphQL",
			"method", "Query(node/Migration)",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"migrationId", migrationID,
		)
		return "", fmt.Errorf("failed to get migration status: %w", err)
	}

	// Get the state from the response
	state := query.Node.Migration.State

	// Validate state and handle empty state
	if state == "" {
		a.logger.Error("Empty migration state returned",
			"api", "GHEC_GraphQL",
			"method", "Query(node/Migration)",
			"duration_ms", duration.Milliseconds(),
			"migrationId", migrationID,
		)
		return "", fmt.Errorf("empty migration state returned")
	}

	// Log appropriate messages based on state
	switch state {
	case "PENDING":
		a.logger.Debug("Migration is pending",
			"api", "GHEC_GraphQL",
			"duration_ms", duration.Milliseconds(),
			"migrationId", migrationID,
		)
	case "PENDING_VALIDATION":
		a.logger.Debug("Migration is pending validation",
			"api", "GHEC_GraphQL",
			"duration_ms", duration.Milliseconds(),
			"migrationId", migrationID,
		)
	case "IN_PROGRESS":
		a.logger.Debug("Migration is in progress",
			"api", "GHEC_GraphQL",
			"duration_ms", duration.Milliseconds(),
			"migrationId", migrationID,
		)
	case "SUCCEEDED":
		a.logger.Info("Migration succeeded",
			"api", "GHEC_GraphQL",
			"duration_ms", duration.Milliseconds(),
			"migrationId", migrationID,
		)
	case "FAILED", "FAILED_VALIDATION":
		failureReason := query.Node.Migration.FailureReason
		if failureReason != "" {
			a.logger.Error("Migration failed",
				"api", "GHEC_GraphQL",
				"duration_ms", duration.Milliseconds(),
				"migrationId", migrationID,
				"failureReason", failureReason,
			)
			return state, fmt.Errorf("migration failed: %s", failureReason)
		} else {
			a.logger.Error("Migration failed with unknown reason",
				"api", "GHEC_GraphQL",
				"duration_ms", duration.Milliseconds(),
				"migrationId", migrationID,
			)
			return state, fmt.Errorf("migration failed with unknown reason")
		}
	case "QUEUED":
		a.logger.Debug("Migration is queued",
			"api", "GHEC_GraphQL",
			"duration_ms", duration.Milliseconds(),
			"migrationId", migrationID,
		)
	default:
		a.logger.Warn("Unknown migration state",
			"api", "GHEC_GraphQL",
			"duration_ms", duration.Milliseconds(),
			"migrationId", migrationID,
			"state", state,
		)
	}

	return state, nil
}

// UploadArchiveToGHOS uploads a migration archive to GitHub Owned Storage
// This is used when customers select Local Storage (GHOS) instead of Azure or S3
// It performs a chunked upload for all archives.
func (a *GitHubAPI) UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error) {
	// Log the start of the upload to GHOS
	a.logger.Info("Starting archive upload to GitHub Owned Storage",
		"api", "GHOS_Upload",
		"database_id", databaseID,
		"archive_name", archiveName,
	)

	startTime := time.Now()

	// Create a client for downloading the archive
	client := &http.Client{
		Timeout: 120 * time.Minute, // Long timeout for potentially large files
	}

	// Create circuit-protected HTTP clients for GHES and GitHub Cloud
	executeGhesRequest := a.circuitProtectedGhesHTTP(client, "ghos_download")
	executeGhCloudRequest := a.circuitProtectedGhCloudHTTP(client, "ghos_upload")

	// Download the archive from GHES
	a.logger.Debug("Downloading migration archive from GHES",
		"url", archiveURL,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for archive download: %w", err)
	}

	resp, err := executeGhesRequest(req)
	if err != nil {
		return "", fmt.Errorf("failed to download archive from GHES: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close response body", "error", err)
		}
	}()

	// Get the total size of the archive
	totalSize := resp.ContentLength
	if totalSize == -1 {
		return "", fmt.Errorf("could not determine archive size")
	}

	// Step 1: Initialize multipart upload
	a.logger.Debug("Initializing multipart upload",
		"database_id", databaseID,
		"archive_size", totalSize,
	)

	uploadURL := fmt.Sprintf("https://uploads.github.com/organizations/%d/gei/archive/blobs/uploads", databaseID)

	// Prepare JSON body exactly as shown in the Ruby example
	initBody := map[string]interface{}{
		"content_type": "application/octet-stream",
		"name":         archiveName,
		"size":         totalSize,
	}

	initBodyBytes, err := json.Marshal(initBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal initialization body: %w", err)
	}

	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(initBodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create init request: %w", err)
	}

	initReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
	initReq.Header.Set("Content-Type", "application/json")

	// Make this request reusable for retries
	initReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(initBodyBytes)), nil
	}

	initResp, err := executeGhCloudRequest(initReq)
	if err != nil {
		return "", fmt.Errorf("failed to initialize multipart upload: %w", err)
	}
	defer func() {
		if err := initResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close init response body", "error", err)
		}
	}()

	// Get the location header from the response for the next part upload
	nextPath := initResp.Header.Get("Location")
	if nextPath == "" {
		return "", fmt.Errorf("no location header found in initialization response")
	}

	// Extract the GUID from the location header
	// The path should contain guid=<guid> as a parameter
	locationURL, err := url.Parse(nextPath)
	if err != nil {
		return "", fmt.Errorf("failed to parse location header: %w", err)
	}

	query := locationURL.Query()
	guid := query.Get("guid")
	if guid == "" {
		return "", fmt.Errorf("no guid found in location header")
	}

	a.logger.Debug("Multipart upload initialized",
		"guid", guid,
		"next_path", nextPath,
	)

	// Create a buffer for reading the archive in chunks
	// GitHub recommends 100 MiB chunks
	const partSize = 100 * 1024 * 1024 // 100 MiB
	buffer := make([]byte, partSize)

	// Calculate total number of parts
	numParts := (totalSize + partSize - 1) / partSize

	// Track the last path for completing the upload
	var lastPath string

	// Step 2-3: Upload parts
	for partNumber := int64(1); partNumber <= numParts; partNumber++ {
		// Save the current path as the last path before getting a new one
		lastPath = nextPath

		// Read the next chunk from the archive
		bytesRead, err := io.ReadFull(resp.Body, buffer)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return "", fmt.Errorf("failed to read part %d: %w", partNumber, err)
		}

		// If we didn't read anything, we're done
		if bytesRead == 0 {
			break
		}

		// Upload the part
		a.logger.Debug("Uploading part",
			"part_number", partNumber,
			"total_parts", numParts,
			"bytes", bytesRead,
		)

		partURL := fmt.Sprintf("https://uploads.github.com%s", nextPath)
		partData := buffer[:bytesRead]
		partReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, partURL, bytes.NewReader(partData))
		if err != nil {
			return "", fmt.Errorf("failed to create part request: %w", err)
		}

		partReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
		partReq.Header.Set("Content-Type", "application/octet-stream")

		// Make this request reusable for retries
		partReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(partData)), nil
		}

		partResp, err := executeGhCloudRequest(partReq)
		if err != nil {
			return "", fmt.Errorf("failed to upload part %d: %w", partNumber, err)
		}

		// Get the next path from the response
		nextPath = partResp.Header.Get("Location")
		if nextPath == "" && partNumber < numParts {
			if err := partResp.Body.Close(); err != nil {
				a.logger.Warn("Failed to close part response body", "error", err)
			}
			return "", fmt.Errorf("no location header found in part %d response", partNumber)
		}

		if err := partResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close part response body", "error", err)
		}

		// Log progress
		progress := float64(partNumber) / float64(numParts) * 100
		a.logger.Debug("Upload progress",
			"part", partNumber,
			"total_parts", numParts,
			"progress", fmt.Sprintf("%.1f%%", progress),
		)
	}

	// Step 4: Complete the multipart upload
	a.logger.Debug("Completing multipart upload",
		"guid", guid,
	)

	completeURL := fmt.Sprintf("https://uploads.github.com%s", lastPath)
	completeReq, err := http.NewRequestWithContext(ctx, http.MethodPut, completeURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create complete request: %w", err)
	}

	completeReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
	completeReq.Header.Set("Content-Type", "application/octet-stream")

	completeResp, err := executeGhCloudRequest(completeReq)
	if err != nil {
		return "", fmt.Errorf("failed to complete upload: %w", err)
	}
	defer func() {
		if err := completeResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close complete response body", "error", err)
		}
	}()

	// Construct the GEI URI from the GUID
	geiURI := fmt.Sprintf("gei://archive/%s", guid)

	duration := time.Since(startTime)

	a.logger.Info("Successfully uploaded archive to GitHub Owned Storage",
		"api", "GHOS_Upload",
		"duration_ms", duration.Milliseconds(),
		"uri", geiURI,
		"guid", guid,
		"size", totalSize,
	)

	return geiURI, nil
}
