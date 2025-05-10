package migrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// Migrator handles repository migrations
type Migrator struct {
	clients    *config.Clients
	githubAPI  *github.API
	webhookURL string
	logger     *slog.Logger
	mu         sync.RWMutex
	migrations map[string]*payload.MigrationStatus
}

// New creates a new migrator instance
func New(webhookURL string) *Migrator {
	logger := logging.Get()
	clients := config.GetClients()

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
		clients:    clients,
		githubAPI:  github.New(clients, logger),
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

			// We use the parent context without additional timeout
			// since migrations can take hours for large repositories
			if err := m.migrateRepository(ctx, req, repoName); err != nil {
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
	// Update status to in progress with initial stage
	m.updateStatus(repoName, payload.StatusInProgress, "starting migration process - this may take hours for large repositories", time.Now())

	// Validate that source repository exists
	sourceRepo := fmt.Sprintf("%s/%s", req.SourceOrg, repoName)
	m.updateStatus(repoName, payload.StatusInProgress, "validating source repository", time.Now())
	err := m.githubAPI.ValidateRepository(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("source repository not found: %v", err), time.Now())
		return fmt.Errorf("source repository not found: %w", err)
	}

	// Get the owner ID for the destination organization
	m.updateStatus(repoName, payload.StatusInProgress, "getting target organization ID", time.Now())
	ownerID, err := m.githubAPI.GetOrganizationID(ctx, req.TargetOrg)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to get owner ID: %v", err), time.Now())
		return fmt.Errorf("failed to get owner ID: %w", err)
	}
	m.logger.Debug("Owner ID", "ownerID", ownerID)

	// Get the base URL for the source organization
	baseURL := req.GHESBaseURL
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Create migration source in destination organization
	m.updateStatus(repoName, payload.StatusInProgress, "creating migration source in GHEC", time.Now())
	migrationSourceID, err := m.githubAPI.CreateMigrationSource(ctx, repoName, baseURL, ownerID)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to create migration source: %v", err), time.Now())
		return fmt.Errorf("failed to create migration source: %w", err)
	}
	m.logger.Debug("Migration source ID", "migrationSourceID", migrationSourceID)

	// Generate migration archive on Source GHES
	m.updateStatus(repoName, payload.StatusInProgress, "generating migration archive on GHES - large repositories may take 30+ minutes", time.Now())
	archiveID, err := m.githubAPI.GenerateMigrationArchive(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to generate migration archive: %v", err), time.Now())
		return fmt.Errorf("failed to generate migration archive: %w", err)
	}
	m.logger.Debug("Migration archive ID", "archiveID", archiveID)

	// Wait for migration archive export to complete
	m.updateStatus(repoName, payload.StatusInProgress, "waiting for archive export to complete - this may take 30+ minutes for large repositories", time.Now())

	// Use longer polling intervals for archive export status checks
	// Start with 30 seconds between checks to avoid unnecessary API load
	pollInterval := 30 * time.Second

	// Track how long we've been waiting
	exportStartTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			m.updateStatus(
				repoName,
				payload.StatusFailed,
				fmt.Sprintf("archive export cancelled: %v", ctx.Err()),
				time.Now(),
			)
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		// Check status of migration source
		status, err := m.githubAPI.GetMigrationArchiveStatus(ctx, archiveID, req.SourceOrg)
		if err != nil {
			m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to get migration source status: %v", err), time.Now())
			return fmt.Errorf("failed to get migration source status: %w", err)
		}

		// Update status with current state
		m.updateStatus(
			repoName,
			payload.StatusInProgress,
			fmt.Sprintf("archive export state: %s", status),
			time.Now(),
		)

		exportDuration := time.Since(exportStartTime)
		m.logger.Info("Migration archive export status",
			"status", status,
			"archiveID", archiveID,
			"repository", repoName,
			"waiting_time", fmt.Sprintf("%dm%ds", int(exportDuration.Minutes()), int(exportDuration.Seconds())%60),
		)

		// Handle different states of migration export
		switch status {
		case "exported":
			// Export completed successfully, proceed to next step
			m.logger.Info("Migration archive export completed successfully",
				"archiveID", archiveID,
				"repository", repoName,
			)

			// Get the migration archive URL
			archiveURL, err := m.githubAPI.GetMigrationArchiveURL(ctx, archiveID, req.SourceOrg)
			if err != nil {
				m.logger.Error("Failed to get migration archive URL",
					"error", err,
					"archiveID", archiveID,
					"repository", repoName,
				)
				m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to get migration archive URL: %v", err), time.Now())
				return fmt.Errorf("failed to get migration archive URL: %w", err)
			}

			m.logger.Info("Migration archive URL retrieved",
				"url", archiveURL,
				"archiveID", archiveID,
				"repository", repoName,
			)

			// Update status with archive URL
			m.updateStatus(
				repoName,
				payload.StatusInProgress,
				fmt.Sprintf("archive URL: %s", archiveURL),
				time.Now(),
			)

			goto startRepositoryMigration
		case "failed":
			failureMsg := fmt.Sprintf("migration archive export failed with state: %s", status)
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now())
			return fmt.Errorf("migration archive export failed: %s", failureMsg)
		case "pending", "exporting":
			// Continue polling
			m.logger.Debug("Waiting for migration archive export to complete",
				"status", status,
				"archiveID", archiveID,
				"repository", repoName,
			)
		default:
			m.logger.Warn("Unknown migration archive export status",
				"status", status,
				"archiveID", archiveID,
				"repository", repoName,
			)
		}
	}

startRepositoryMigration:
	// Ensure the source repository URL is properly formatted
	// Construct the source repository URL from the base URL
	apiURL := strings.TrimSuffix(req.GetGHESAPIURL(), "/")
	sourceRepoURL := fmt.Sprintf("%s/repos/%s", apiURL, sourceRepo)

	m.logger.Debug("Using source repository URL",
		"url", sourceRepoURL,
		"base_url", baseURL,
	)

	// Start repository migration
	m.updateStatus(repoName, payload.StatusInProgress, "starting repository migration to GHEC", time.Now())
	migrationID, err := m.githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, repoName, sourceRepoURL)
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
	m.updateStatus(repoName, payload.StatusInProgress, "waiting for migration to complete - this can take several hours for large repositories", time.Now())

	// Use a longer polling interval for migration status checks
	// Start with 1 minute between checks for larger repositories
	pollInterval = 60 * time.Second

	// Track how long we've been waiting for the migration
	migrationStartTime := time.Now()

	// Initialize adaptive polling
	consecutiveNoChanges := 0
	lastState := ""

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

		state, err := m.githubAPI.GetMigrationStatus(ctx, migrationID)
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

		migrationDuration := time.Since(migrationStartTime)
		m.logger.Info("Repository migration status",
			"state", state,
			"migrationId", migrationID,
			"repository", repoName,
			"waiting_time", fmt.Sprintf("%dh%dm%ds",
				int(migrationDuration.Hours()),
				int(migrationDuration.Minutes())%60,
				int(migrationDuration.Seconds())%60,
			),
		)

		// Implement adaptive polling - increase polling interval if state doesn't change
		if state == lastState {
			consecutiveNoChanges++
			// Max out at 5 minutes between polls for long-running migrations
			if consecutiveNoChanges > 5 && pollInterval < 5*time.Minute {
				pollInterval = time.Duration(math.Min(float64(pollInterval*2), float64(5*time.Minute)))
				m.logger.Debug("Increasing polling interval due to stable state",
					"repository", repoName,
					"new_interval_seconds", pollInterval.Seconds(),
					"state", state,
				)
			}
		} else {
			// State changed, reset counter and polling interval
			consecutiveNoChanges = 0
			pollInterval = 60 * time.Second
			lastState = state
		}

		switch state {
		case "SUCCEEDED":
			// Migration completed successfully
			m.logger.Info("Repository migration completed successfully",
				"migrationId", migrationID,
				"repository", repoName,
			)
			m.updateStatus(repoName, payload.StatusSucceeded, "migration completed successfully", time.Now())
			return nil
		case "FAILED":
			failureMsg := fmt.Sprintf("migration failed with state: %s", state)
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now())
			return fmt.Errorf("migration failed: %s", failureMsg)
		case "PENDING", "IN_PROGRESS", "QUEUED":
			// Continue polling
		}
	}
}

func (m *Migrator) updateStatus(repoName, status, message string, timestamp time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Parse the message to determine stage and state
	var stage, state string

	// Extract stage information from message
	switch {
	case strings.Contains(message, "validating source repository"):
		stage = "validate_repository"
		state = "in_progress"
	case strings.Contains(message, "getting target organization ID"):
		stage = "get_organization_id"
		state = "in_progress"
	case strings.Contains(message, "creating migration source"):
		stage = "create_migration_source"
		state = "in_progress"
	case strings.Contains(message, "generating migration archive"):
		stage = "generate_migration_archive"
		state = "in_progress"
	case strings.Contains(message, "archive export state:"):
		stage = "archive_export"
		stateParts := strings.Split(message, "archive export state: ")
		if len(stateParts) > 1 {
			state = stateParts[1]
		}
	case strings.Contains(message, "archive URL:"):
		stage = "archive_url_retrieved"
		state = "success"
	case strings.Contains(message, "starting repository migration"):
		stage = "start_repository_migration"
		state = "in_progress"
	case strings.Contains(message, "migration ID:"):
		stage = "migration_started"
		state = "in_progress"
	case strings.Contains(message, "migration state:"):
		stage = "migration_progress"
		stateParts := strings.Split(message, "migration state: ")
		if len(stateParts) > 1 {
			state = stateParts[1]
		}
	case strings.Contains(message, "migration completed successfully"):
		stage = "migration_completed"
		state = "succeeded"
	case status == payload.StatusFailed:
		// For failed status, extract the specific stage if available
		if strings.Contains(message, "source repository not found") {
			stage = "validate_repository"
		} else if strings.Contains(message, "failed to get owner ID") {
			stage = "get_organization_id"
		} else if strings.Contains(message, "failed to create migration source") {
			stage = "create_migration_source"
		} else if strings.Contains(message, "failed to generate migration archive") {
			stage = "generate_migration_archive"
		} else if strings.Contains(message, "failed to get migration source status") {
			stage = "archive_export"
		} else if strings.Contains(message, "archive export failed") {
			stage = "archive_export"
		} else if strings.Contains(message, "failed to get migration archive URL") {
			stage = "archive_url_retrieved"
		} else if strings.Contains(message, "failed to start repository migration") {
			stage = "start_repository_migration"
		} else if strings.Contains(message, "migration failed") {
			stage = "migration_progress"
		} else {
			stage = "unknown_failure"
		}
		state = "failed"
	default:
		stage = "in_progress"
		state = "in_progress"
	}

	// Ensure we always have a state value
	if state == "" {
		if status == payload.StatusSucceeded {
			state = "success"
		} else if status == payload.StatusFailed {
			state = "failed"
		} else {
			state = "in_progress"
		}
	}

	m.migrations[repoName] = &payload.MigrationStatus{
		Repository: repoName,
		Status:     status,
		Error:      message,
		UpdatedAt:  timestamp,
		Stage:      stage,
		State:      state,
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
		"stage", status.Stage,
		"state", status.State,
		"payload", string(webhookPayload),
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
			"stage", status.Stage,
			"state", status.State,
			"payload", string(webhookPayload),
		)
	} else {
		m.logger.Debug("Webhook notification sent successfully",
			"repository", repoName,
			"status_code", resp.StatusCode,
			"stage", status.Stage,
			"state", status.State,
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
