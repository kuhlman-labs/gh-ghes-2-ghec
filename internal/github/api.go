// Package github provides functionality for interacting with GitHub APIs,
// both for GitHub Enterprise Server (GHES) and GitHub Enterprise Cloud (GHEC).
// It handles authentication, API requests, retries, and migration-specific operations.
package github

import (
	"bufio"
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

// UploadArchiveToGHOS uploads a migration archive to GitHub Owned Storage
// This is used when customers use GitHub Owned Storage (GHOS) instead of Azure or S3
// For archives >5GiB, it performs a chunked upload
func (a *API) UploadArchiveToGHOS(ctx context.Context, organizationID, archiveURL, archiveName, ghCloudToken string) (string, error) {
	// Log the start of the upload to GHOS
	a.logger.Info("Starting archive upload to GitHub Owned Storage",
		"api", "GHOS_Upload",
		"organization_id", organizationID,
		"archive_name", archiveName,
	)

	startTime := time.Now()

	// Create a client for downloading the archive
	client := &http.Client{
		Timeout: 30 * time.Minute, // Long timeout for potentially large files
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

	// Initialize chunked upload
	initURL := fmt.Sprintf("https://uploads.github.com/organizations/%s/gei/archive/init?name=%s",
		organizationID, url.QueryEscape(archiveName))

	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, initURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create init request: %w", err)
	}

	initReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Content-Length", fmt.Sprintf("%d", totalSize))

	initResp, err := client.Do(initReq)
	if err != nil {
		return "", fmt.Errorf("failed to initialize chunked upload: %w", err)
	}
	defer func() {
		if err := initResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close init response body", "error", err)
		}
	}()

	if initResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(initResp.Body)
		return "", fmt.Errorf("failed to initialize chunked upload, received status code: %d, body: %s",
			initResp.StatusCode, string(respBody))
	}

	var initResponse struct {
		UploadID  string `json:"upload_id"`
		ChunkSize int64  `json:"chunk_size"`
	}

	err = json.NewDecoder(initResp.Body).Decode(&initResponse)
	if err != nil {
		return "", fmt.Errorf("failed to parse init response: %w", err)
	}

	// Calculate number of chunks
	chunkSize := initResponse.ChunkSize
	numChunks := (totalSize + chunkSize - 1) / chunkSize

	// Create a buffered reader for the response body
	reader := bufio.NewReaderSize(resp.Body, int(chunkSize))

	// Upload chunks
	for i := int64(0); i < numChunks; i++ {
		chunkStart := i * chunkSize
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > totalSize {
			chunkEnd = totalSize
		}

		// Read chunk
		chunk := make([]byte, chunkEnd-chunkStart)
		_, err := io.ReadFull(reader, chunk)
		if err != nil {
			return "", fmt.Errorf("failed to read chunk %d: %w", i, err)
		}

		// Upload chunk
		chunkURL := fmt.Sprintf("https://uploads.github.com/organizations/%s/gei/archive/chunk?upload_id=%s&chunk=%d",
			organizationID, initResponse.UploadID, i)

		chunkReq, err := http.NewRequestWithContext(ctx, http.MethodPut, chunkURL, bytes.NewReader(chunk))
		if err != nil {
			return "", fmt.Errorf("failed to create chunk request: %w", err)
		}

		chunkReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
		chunkReq.Header.Set("Content-Type", "application/octet-stream")
		chunkReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(chunk)))

		chunkResp, err := client.Do(chunkReq)
		if err != nil {
			return "", fmt.Errorf("failed to upload chunk %d: %w", i, err)
		}
		defer func() {
			if err := chunkResp.Body.Close(); err != nil {
				a.logger.Warn("Failed to close chunk response body", "error", err)
			}
		}()

		if chunkResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to upload chunk %d, received status code: %d", i, chunkResp.StatusCode)
		}

		// Log progress
		progress := float64(i+1) / float64(numChunks) * 100
		a.logger.Debug("Upload progress",
			"chunk", i+1,
			"total_chunks", numChunks,
			"progress", fmt.Sprintf("%.1f%%", progress),
		)
	}

	// Complete the upload
	completeURL := fmt.Sprintf("https://uploads.github.com/organizations/%s/gei/archive/complete?upload_id=%s",
		organizationID, initResponse.UploadID)

	completeReq, err := http.NewRequestWithContext(ctx, http.MethodPost, completeURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create complete request: %w", err)
	}

	completeReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))

	completeResp, err := client.Do(completeReq)
	if err != nil {
		return "", fmt.Errorf("failed to complete upload: %w", err)
	}
	defer func() {
		if err := completeResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close complete response body", "error", err)
		}
	}()

	if completeResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(completeResp.Body)
		return "", fmt.Errorf("failed to complete upload, received status code: %d, body: %s",
			completeResp.StatusCode, string(respBody))
	}

	// Parse the response to get the GEI URI
	var response struct {
		GUID      string    `json:"guid"`
		NodeID    string    `json:"node_id"`
		Name      string    `json:"name"`
		Size      int64     `json:"size"`
		URI       string    `json:"uri"`
		CreatedAt time.Time `json:"created_at"`
	}

	err = json.NewDecoder(completeResp.Body).Decode(&response)
	if err != nil {
		return "", fmt.Errorf("failed to parse complete response: %w", err)
	}

	duration := time.Since(startTime)

	a.logger.Info("Successfully uploaded archive to GitHub Owned Storage",
		"api", "GHOS_Upload",
		"duration_ms", duration.Milliseconds(),
		"uri", response.URI,
		"guid", response.GUID,
		"size", response.Size,
	)

	return response.URI, nil
}

// StartRepositoryMigrationWithGEIURI starts a repository migration in GHEC using a GEI URI from GitHub Owned Storage
func (a *API) StartRepositoryMigrationWithGEIURI(ctx context.Context, sourceID, ownerID, repoName, geiURI, ghesToken, ghCloudToken string) (string, error) {
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

	// Create input parameters for GraphQL mutation
	continueOnError := githubv4.Boolean(true)
	accessToken := githubv4.String(ghesToken)
	gitHubPat := githubv4.String(ghCloudToken)
	targetRepoVisibility := githubv4.String("private")

	// Create variables map for the GraphQL mutation
	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"sourceId":             githubv4.ID(sourceID),
			"ownerId":              githubv4.ID(ownerID),
			"repositoryName":       githubv4.String(repoName),
			"continueOnError":      continueOnError,
			"accessToken":          accessToken,
			"gitHubPat":            gitHubPat,
			"targetRepoVisibility": targetRepoVisibility,
			"githubArchiveId":      geiURI, // This is the key field for GHOS migrations
		},
	}

	// Log the input parameters for debugging
	a.logger.Debug("Starting repository migration with GHOS URI",
		"api", "GHEC_GraphQL",
		"method", "Mutate(startRepositoryMigration)",
		"sourceId", sourceID,
		"ownerId", ownerID,
		"repositoryName", repoName,
		"geiURI", geiURI,
	)

	startTime := time.Now()

	err := a.retryableOperation(ctx, "start_repository_migration_ghos", func() error {
		// Use the raw variables map rather than the typed input to include the githubArchiveId field
		return a.clients.GHCloudGraphQL.Mutate(ctx, &mutation, variables, nil)
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Migration start failed with GHOS URI",
			"api", "GHEC_GraphQL",
			"method", "Mutate(startRepositoryMigration)",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"error_details", strings.ReplaceAll(err.Error(), "\n", " "),
			"repository", repoName,
		)
		return "", fmt.Errorf("failed to start repository migration with GHOS URI: %w", err)
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
	a.logger.Info("Repository migration started with GHOS URI",
		"api", "GHEC_GraphQL",
		"method", "Mutate(startRepositoryMigration)",
		"duration_ms", duration.Milliseconds(),
		"migrationId", migrationID,
		"repository", repoName,
		"sourceId", mutation.StartRepositoryMigration.RepositoryMigration.MigrationSource.ID,
	)

	return migrationID, nil
}
