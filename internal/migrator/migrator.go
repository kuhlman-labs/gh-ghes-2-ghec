package migrator

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
	"sync"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/shurcooL/githubv4"
)

// Migrator handles repository migrations
type Migrator struct {
	clients    *config.Clients
	webhookURL string
	logger     *slog.Logger
	mu         sync.RWMutex
	migrations map[string]*payload.MigrationStatus
}

// New creates a new migrator instance
func New(webhookURL string) *Migrator {
	logger := logging.Get()

	// Validate webhook URL if provided
	if webhookURL != "" {
		_, err := url.Parse(webhookURL)
		if err != nil {
			logger.Warn("Invalid webhook URL provided, notifications will be disabled",
				"webhook_url", webhookURL,
				"error", err,
			)
			webhookURL = "" // Disable webhook notifications
		}
	}

	return &Migrator{
		clients:    config.GetClients(),
		webhookURL: webhookURL,
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
	}
}

// StartMigration starts the migration process for the given request
func (m *Migrator) StartMigration(ctx context.Context, req *payload.MigrationRequest) error {
	// Update GHES client base URL with the REST API URL
	apiURL := req.GetGHESAPIURL()
	if err := m.clients.UpdateGHESBaseURL(apiURL); err != nil {
		return fmt.Errorf("invalid GHES API URL: %w", err)
	}

	// Start migrations for each repository
	errChan := make(chan error, len(req.Repositories))
	var wg sync.WaitGroup

	for _, repo := range req.Repositories {
		wg.Add(1)
		go func(repoName string) {
			defer wg.Done()

			// Create a timeout context for each repository migration
			repoCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			if err := m.migrateRepository(repoCtx, req, repoName); err != nil {
				m.logger.Error("Failed to migrate repository",
					"repository", repoName,
					"error", err,
				)
				errChan <- fmt.Errorf("repository %s: %w", repoName, err)
			}
		}(repo)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errChan)

	// Collect all errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to migrate %d repositories: %v", len(errors), errors)
	}

	return nil
}

func (m *Migrator) migrateRepository(ctx context.Context, req *payload.MigrationRequest, repoName string) error {
	// Update status to in progress with timestamp
	m.updateStatus(repoName, payload.StatusInProgress, "", time.Now())

	// Validate that source repository exists
	sourceRepo := fmt.Sprintf("%s/%s", req.SourceOrg, repoName)
	_, _, err := m.clients.GHESClient.Repositories.Get(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("source repository not found: %v", err), time.Now())
		return fmt.Errorf("source repository not found: %w", err)
	}

	// Get the owner ID for the destination organization
	ownerID, err := m.getOwnerID(ctx, req.TargetOrg)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to get owner ID: %v", err), time.Now())
		return fmt.Errorf("failed to get owner ID: %w", err)
	}
	m.logger.Debug("Owner ID", "ownerID", ownerID)

	// Get the base URL for the migration source (without /api/v3)
	baseURL := req.GHESBaseURL
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Create migration source
	migrationSourceID, err := m.createMigrationSource(ctx, repoName, baseURL, ownerID)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to create migration archive: %v", err), time.Now())
		return fmt.Errorf("failed to create migration archive: %w", err)
	}
	m.logger.Debug("Migration source ID", "migrationSourceID", migrationSourceID)

	sourceRepoURL := fmt.Sprintf("%s/%s", baseURL, sourceRepo)

	m.logger.Debug("Using source repository URL",
		"url", sourceRepoURL,
		"base_url", baseURL,
	)

	// Start repository migration
	migrationID, err := m.startRepositoryMigration(ctx, migrationSourceID, ownerID, repoName, sourceRepoURL)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to start repository migration: %v", err), time.Now())
		return fmt.Errorf("failed to start repository migration: %w", err)
	}

	// Update status with migration ID
	m.updateStatus(
		repoName,
		payload.StatusInProgress,
		fmt.Sprintf("migration ID: %s", migrationID),
		time.Now(),
	)

	// Wait for migration to complete
	pollInterval := 10 * time.Second
	for {
		select {
		case <-ctx.Done():
			m.updateStatus(
				repoName,
				payload.StatusFailed,
				fmt.Sprintf("migration cancelled: %v", ctx.Err()),
				time.Now(),
			)
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		state, err := m.getMigrationStatus(ctx, migrationID)
		if err != nil {
			m.updateStatus(repoName, payload.StatusFailed, err.Error(), time.Now())
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		// Update status with current state
		m.updateStatus(
			repoName,
			payload.StatusInProgress,
			fmt.Sprintf("migration state: %s", state),
			time.Now(),
		)

		switch state {
		case "SUCCEEDED":
			m.updateStatus(repoName, payload.StatusSucceeded, "", time.Now())
			return nil
		case "FAILED":
			failureMsg := fmt.Sprintf("migration failed with state: %s", state)
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now())
			return fmt.Errorf("migration failed: %s", failureMsg)
		case "PENDING", "IN_PROGRESS":
			// Continue polling
		}
	}
}

func (m *Migrator) updateStatus(repoName, status, message string, timestamp time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.migrations[repoName] = &payload.MigrationStatus{
		Repository: repoName,
		Status:     status,
		Error:      message,
		UpdatedAt:  timestamp,
	}

	// Send webhook notification if URL is configured
	if m.webhookURL != "" && m.webhookURL != "http://localhost:8080/webhook" {
		go m.sendWebhookNotification(repoName)
	}
}

func (m *Migrator) sendWebhookNotification(repoName string) {
	m.mu.RLock()
	status := m.migrations[repoName]
	m.mu.RUnlock()

	if status == nil {
		return
	}

	// Validate webhook URL
	_, err := url.Parse(m.webhookURL)
	if err != nil {
		m.logger.Error("Invalid webhook URL, skipping notification",
			"repository", repoName,
			"webhook_url", m.webhookURL,
			"error", err,
		)
		return
	}

	webhookPayload, err := json.Marshal(status)
	if err != nil {
		m.logger.Error("Failed to marshal webhook payload",
			"repository", repoName,
			"error", err,
		)
		return
	}

	// Create an HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest(http.MethodPost, m.webhookURL, bytes.NewBuffer(webhookPayload))
	if err != nil {
		m.logger.Error("Failed to create webhook request",
			"repository", repoName,
			"error", err,
		)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ghes-2-ghec")

	m.logger.Debug("Sending webhook notification",
		"repository", repoName,
		"webhook_url", m.webhookURL,
		"status", status.Status,
	)

	resp, err := client.Do(req)
	if err != nil {
		m.logger.Error("Failed to send webhook notification",
			"repository", repoName,
			"webhook_url", m.webhookURL,
			"error", err,
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read the response body for more details
		body, _ := io.ReadAll(resp.Body)
		m.logger.Error("Webhook notification failed",
			"repository", repoName,
			"webhook_url", m.webhookURL,
			"status_code", resp.StatusCode,
			"response_body", string(body),
		)
	} else {
		m.logger.Debug("Webhook notification sent successfully",
			"repository", repoName,
			"status_code", resp.StatusCode,
		)
	}
}

// GetMigrationStatus returns the current status of a repository migration
func (m *Migrator) GetMigrationStatus(repoName string) *payload.MigrationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if status, exists := m.migrations[repoName]; exists {
		return status
	}

	return nil
}

// GetAllMigrationStatuses returns the current status of all repository migrations
func (m *Migrator) GetAllMigrationStatuses() map[string]*payload.MigrationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a copy to avoid race conditions
	statuses := make(map[string]*payload.MigrationStatus, len(m.migrations))
	for k, v := range m.migrations {
		statuses[k] = v
	}

	return statuses
}

// getOwnerID gets the owner ID for the destination organization
func (m *Migrator) getOwnerID(ctx context.Context, org string) (string, error) {
	var query struct {
		Organization struct {
			ID string `graphql:"id"`
		} `graphql:"organization(login: $login)"`
	}

	variables := map[string]interface{}{
		"login": githubv4.String(org),
	}

	m.logger.Debug("Querying organization ID", "org", org)

	err := m.clients.GHCloudGraphQL.Query(ctx, &query, variables)
	if err != nil {
		m.logger.Error("Failed to get organization ID",
			"error", err,
			"org", org,
		)
		return "", fmt.Errorf("failed to get organization: %w", err)
	}

	// check if the organization ID is empty
	if query.Organization.ID == "" {
		m.logger.Error("Organization ID is empty", "org", org)
		return "", fmt.Errorf("organization ID is empty")
	}

	m.logger.Debug("Organization ID retrieved successfully",
		"org", org,
		"id", query.Organization.ID,
	)

	return query.Organization.ID, nil
}

// createMigrationSource creates a migration source in GitHub Enterprise Cloud
func (m *Migrator) createMigrationSource(ctx context.Context, name, url, ownerID string) (string, error) {
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

	// Log the input parameters
	m.logger.Debug("Creating migration source",
		"name", name,
		"url", url,
		"ownerId", ownerID,
		"type", "GITHUB_ARCHIVE",
	)

	// Create string pointer for URL
	urlPtr := githubv4.String(url)

	input := githubv4.CreateMigrationSourceInput{
		Name:    githubv4.String(name),
		URL:     &urlPtr,
		OwnerID: githubv4.ID(ownerID),
		Type:    githubv4.MigrationSourceTypeGitHubArchive,
	}

	err := m.clients.GHCloudGraphQL.Mutate(ctx, &mutation, input, nil)
	if err != nil {
		m.logger.Error("Failed to create migration source",
			"error", err,
			"variables", fmt.Sprintf("%+v", input),
		)
		return "", fmt.Errorf("failed to create migration source: %w", err)
	}

	// Check if the migration source ID is empty
	if mutation.CreateMigrationSource.MigrationSource.ID == "" {
		m.logger.Error("Empty migration source ID returned",
			"mutation_response", fmt.Sprintf("%+v", mutation),
		)
		return "", fmt.Errorf("createMigrationSource returned empty ID")
	}

	m.logger.Info("Migration source created successfully",
		"sourceId", mutation.CreateMigrationSource.MigrationSource.ID,
		"name", mutation.CreateMigrationSource.MigrationSource.Name,
		"type", mutation.CreateMigrationSource.MigrationSource.Type,
	)

	return mutation.CreateMigrationSource.MigrationSource.ID, nil
}

// startRepositoryMigration starts a repository migration
func (m *Migrator) startRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL string) (string, error) {
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

	// Get the access tokens from the config
	cfg := config.Get()

	// Log the input parameters for debugging
	m.logger.Debug("Starting repository migration",
		"sourceId", sourceID,
		"ownerId", ownerID,
		"repositoryName", repoName,
		"sourceRepositoryUrl", sourceRepoURL,
	)

	continueOnError := githubv4.Boolean(true)
	accessToken := githubv4.String(cfg.GitHub.GHESToken)
	gitHubPat := githubv4.String(cfg.GitHub.GHCloudToken)
	targetRepoVisibility := githubv4.String("private")

	// Parse the source repository URL
	parsedURL, err := url.Parse(sourceRepoURL)
	if err != nil {
		m.logger.Error("Failed to parse source repository URL",
			"error", err,
			"url", sourceRepoURL,
		)
		return "", fmt.Errorf("invalid source repository URL: %w", err)
	}

	// Create URI from parsed URL
	sourceRepoURI := githubv4.URI{URL: parsedURL}

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

	err = m.clients.GHCloudGraphQL.Mutate(ctx, &mutation, input, nil)
	if err != nil {
		m.logger.Error("GraphQL mutation error",
			"error", err,
			"variables", fmt.Sprintf("%+v", input),
		)
		return "", fmt.Errorf("failed to start repository migration: %w", err)
	}

	// Check if the mutation response is valid
	if mutation.StartRepositoryMigration.RepositoryMigration.ID == "" {
		m.logger.Error("Empty migration ID returned",
			"mutation_response", fmt.Sprintf("%+v", mutation),
			"variables", fmt.Sprintf("%+v", input),
		)
		return "", fmt.Errorf("startRepositoryMigration returned empty migration ID")
	}

	m.logger.Info("Repository migration started successfully",
		"migrationId", mutation.StartRepositoryMigration.RepositoryMigration.ID,
		"repository", repoName,
	)

	return mutation.StartRepositoryMigration.RepositoryMigration.ID, nil
}

// getMigrationStatus gets the current status of a repository migration
func (m *Migrator) getMigrationStatus(ctx context.Context, migrationID string) (string, error) {
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

	m.logger.Debug("Querying migration status", "migrationId", migrationID)

	err := m.clients.GHCloudGraphQL.Query(ctx, &query, variables)
	if err != nil {
		m.logger.Error("Failed to get migration status",
			"error", err,
			"migrationId", migrationID,
		)
		return "", fmt.Errorf("failed to get migration status: %w", err)
	}

	// If there's a failure reason, include it in the error
	if query.Node.Migration.FailureReason != "" {
		m.logger.Error("Migration failed",
			"migrationId", migrationID,
			"state", query.Node.Migration.State,
			"failureReason", query.Node.Migration.FailureReason,
		)
		return query.Node.Migration.State, fmt.Errorf("migration failed: %s", query.Node.Migration.FailureReason)
	}

	m.logger.Debug("Migration status retrieved",
		"migrationId", migrationID,
		"state", query.Node.Migration.State,
	)

	return query.Node.Migration.State, nil
}
