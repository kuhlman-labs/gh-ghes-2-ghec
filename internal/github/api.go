// Package github provides functionality for interacting with GitHub APIs,
// both for GitHub Enterprise Server (GHES) and GitHub Enterprise Cloud (GHEC).
// It handles authentication, API requests, retries, and migration-specific operations.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-github/v70/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
	"github.com/shurcooL/githubv4"
)

// API handles GitHub API operations for both GitHub Enterprise Server and GitHub Cloud.
// It provides methods for repository validation, organization management,
// migration source creation, and migration operations.
type API struct {
	clients     *config.Clients
	logger      *slog.Logger
	retryConfig *utils.RetryConfig
}

// New creates a new GitHub API handler with the provided clients and logger.
// It configures default retry policies appropriate for GitHub API interactions.
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

// retryableOperation executes a function with retries based on the API's retry configuration.
// It logs attempts and results, and backs off exponentially between retry attempts.
// The operation name is used for logging and observability.
func (a *API) retryableOperation(ctx context.Context, operation string, fn func() error) error {
	return utils.Retry(ctx, a.retryConfig, operation, fn)
}

// ValidateRepository checks if a repository exists in the source organization.
// It makes a REST API call to the GHES instance and verifies the repository's existence.
// Returns an error if the repository doesn't exist or can't be accessed.
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

// GetOrganizationID retrieves the organization node ID from GitHub Enterprise Cloud.
// It makes a GraphQL API call to fetch the organization's unique identifier,
// which is needed for migration operations.
// Returns the organization ID as a string and any errors encountered.
func (a *API) GetOrganizationID(ctx context.Context, org string) (string, int64, error) {
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
func (a *API) StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, archiveURL, metadataURL, ghesToken, ghCloudToken string) (string, error) {
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

// UploadArchiveToGHOS uploads a migration archive to GitHub Owned Storage
// This is used when customers select Local Storage (GHOS) instead of Azure or S3
// It performs a chunked upload for all archives.
func (a *API) UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error) {
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

	// Download the archive from GHES
	a.logger.Debug("Downloading migration archive from GHES",
		"url", archiveURL,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for archive download: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download archive from GHES: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download archive, received status code: %d", resp.StatusCode)
	}

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

	initResp, err := client.Do(initReq)
	if err != nil {
		return "", fmt.Errorf("failed to initialize multipart upload: %w", err)
	}
	defer func() {
		if err := initResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close init response body", "error", err)
		}
	}()

	if initResp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(initResp.Body)
		return "", fmt.Errorf("failed to initialize multipart upload, received status code: %d, body: %s",
			initResp.StatusCode, string(respBody))
	}

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
		partReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, partURL, bytes.NewReader(buffer[:bytesRead]))
		if err != nil {
			return "", fmt.Errorf("failed to create part request: %w", err)
		}

		partReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
		partReq.Header.Set("Content-Type", "application/octet-stream")
		// Let the HTTP client set the Content-Length header automatically

		partResp, err := client.Do(partReq)
		if err != nil {
			return "", fmt.Errorf("failed to upload part %d: %w", partNumber, err)
		}

		// Get the next path from the response
		if partResp.StatusCode != http.StatusAccepted {
			respBody, _ := io.ReadAll(partResp.Body)
			if err := partResp.Body.Close(); err != nil {
				a.logger.Warn("Failed to close part response body", "error", err)
			}
			return "", fmt.Errorf("failed to upload part %d, received status code: %d, body: %s",
				partNumber, partResp.StatusCode, string(respBody))
		}

		// Get the next path for the next part
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

	completeResp, err := client.Do(completeReq)
	if err != nil {
		return "", fmt.Errorf("failed to complete upload: %w", err)
	}
	defer func() {
		if err := completeResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close complete response body", "error", err)
		}
	}()

	if completeResp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(completeResp.Body)
		return "", fmt.Errorf("failed to complete upload, received status code: %d, body: %s",
			completeResp.StatusCode, string(respBody))
	}

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
