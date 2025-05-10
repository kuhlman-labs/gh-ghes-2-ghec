package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-github/v70/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
	"github.com/shurcooL/githubv4"
)

// API handles GitHub API operations
type API struct {
	clients     *config.Clients
	logger      *slog.Logger
	retryConfig *utils.RetryConfig
}

// New creates a new GitHub API handler
func New(clients *config.Clients, logger *slog.Logger) *API {
	// Create a retry configuration suitable for GitHub API calls
	retryConfig := utils.DefaultRetryConfig(logger).
		WithMaxRetries(2).                    // 3 total attempts
		WithInitialInterval(1 * time.Second). // Start with 1s backoff
		WithMaxInterval(5 * time.Second)      // Cap at 5s

	return &API{
		clients:     clients,
		logger:      logger,
		retryConfig: retryConfig,
	}
}

// retryableOperation executes a function with retries based on the API's retry configuration
func (a *API) retryableOperation(ctx context.Context, operation string, fn func() error) error {
	return utils.Retry(ctx, a.retryConfig, operation, fn)
}

// ValidateRepository checks if a repository exists in the source organization
func (a *API) ValidateRepository(ctx context.Context, org, repo string) error {
	startTime := time.Now()
	a.logger.Debug("Validating repository",
		"api", "GHES_REST",
		"method", "Repositories.Get",
		"org", org,
		"repo", repo,
	)

	var respStatus int
	err := a.retryableOperation(ctx, "validate_repository", func() error {
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

// GetOrganizationID retrieves the organization ID from GitHub Enterprise Cloud
func (a *API) GetOrganizationID(ctx context.Context, org string) (string, error) {
	var query struct {
		Organization struct {
			ID string `graphql:"id"`
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

	err := a.retryableOperation(ctx, "get_organization_id", func() error {
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
		return "", fmt.Errorf("failed to get organization: %w", err)
	}

	// check if the organization ID is empty
	if query.Organization.ID == "" {
		a.logger.Error("Organization ID is empty",
			"api", "GHEC_GraphQL",
			"method", "Query(organization)",
			"duration_ms", duration.Milliseconds(),
			"org", org,
		)
		return "", fmt.Errorf("organization ID is empty")
	}

	a.logger.Debug("Organization ID retrieved",
		"api", "GHEC_GraphQL",
		"method", "Query(organization)",
		"duration_ms", duration.Milliseconds(),
		"org", org,
		"id", query.Organization.ID,
	)
	return query.Organization.ID, nil
}

// CreateMigrationSource creates a migration source in GitHub Enterprise Cloud
func (a *API) CreateMigrationSource(ctx context.Context, name, url, ownerID string) (string, error) {
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

	err := a.retryableOperation(ctx, "create_migration_source", func() error {
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
func (a *API) GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error) {
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

	err := a.retryableOperation(ctx, "generate_migration_archive", func() error {
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
func (a *API) GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error) {
	a.logger.Debug("Getting archive status",
		"api", "GHES_REST",
		"method", "Migrations.MigrationStatus",
		"migrationID", migrationID,
		"org", orgName,
	)

	startTime := time.Now()

	var status *github.Migration
	var respStatus int

	err := a.retryableOperation(ctx, "get_migration_archive_status", func() error {
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
func (a *API) GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error) {
	a.logger.Debug("Getting migration archive URL",
		"api", "GHES_REST",
		"method", "Migrations.MigrationArchiveURL",
		"migrationId", archiveID,
		"org", orgName,
	)

	startTime := time.Now()

	var archiveURL string

	err := a.retryableOperation(ctx, "get_migration_archive_url", func() error {
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
func (a *API) StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, ghesToken, ghCloudToken string) (string, error) {
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

	err = a.retryableOperation(ctx, "start_repository_migration", func() error {
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
func (a *API) GetMigrationStatus(ctx context.Context, migrationID string) (string, error) {
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

	err := a.retryableOperation(ctx, "get_migration_status", func() error {
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
	case "FAILED":
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
