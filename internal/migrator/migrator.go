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
		webhookURL: webhookURL,
		logger:     logger,
		migrations: make(map[string]*payload.MigrationStatus),
	}
}

// StartMigration starts the migration process for the given request
func (m *Migrator) StartMigration(ctx context.Context, req *payload.MigrationRequest, cancel context.CancelFunc) error {
	// Initialize clients for this migration using tokens from the request
	clients, err := config.NewClients(req.GHESToken, req.GHCloudToken)
	if err != nil {
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	// Update GHES base URL
	if err := clients.UpdateGHESBaseURL(req.GetGHESAPIURL()); err != nil {
		return fmt.Errorf("failed to update GHES base URL: %w", err)
	}

	// Track active migrations to know when to cancel the context
	var wg sync.WaitGroup

	// Start migration for each repository
	for _, repo := range req.Repositories {
		wg.Add(1)
		// Launch migration for each repository in a separate goroutine
		go func(repo string) {
			defer wg.Done()
			m.logger.Info("Starting migration for repository", "repository", repo)
			if err := m.migrateRepository(ctx, req, repo); err != nil {
				m.logger.Error("Repository migration failed",
					"repository", repo,
					"error", err,
				)
			}
		}(repo)
	}

	// Start a goroutine to wait for all migrations to complete and then cancel the context
	go func() {
		wg.Wait()
		m.logger.Info("All repository migrations completed, cancelling context")
		cancel()
	}()

	return nil
}

func (m *Migrator) migrateRepository(ctx context.Context, req *payload.MigrationRequest, repoName string) error {
	// Initialize clients for this migration
	clients, err := config.NewClients(req.GHESToken, req.GHCloudToken)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to initialize clients: %v", err), time.Now())
		// Send webhook notification for failure
		go m.sendWebhookNotification(repoName, req)
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	// Update GHES base URL
	if err := clients.UpdateGHESBaseURL(req.GetGHESAPIURL()); err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to update GHES base URL: %v", err), time.Now())
		// Send webhook notification for failure
		go m.sendWebhookNotification(repoName, req)
		return fmt.Errorf("failed to update GHES base URL: %w", err)
	}

	// Create GitHub API instance for this migration
	githubAPI := github.New(clients, m.logger)

	// Update status to in progress with initial stage
	m.updateStatus(repoName, payload.StatusInProgress, "starting migration process - this may take hours for large repositories", time.Now())

	// Validate that source repository exists
	sourceRepo := fmt.Sprintf("%s/%s", req.SourceOrg, repoName)
	m.updateStatus(repoName, payload.StatusInProgress, "validating source repository", time.Now())
	err = githubAPI.ValidateRepository(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("source repository not found: %v", err), time.Now())
		return fmt.Errorf("source repository not found: %w", err)
	}

	// Get the owner ID for the destination organization
	m.updateStatus(repoName, payload.StatusInProgress, "getting target organization ID", time.Now())
	ownerID, err := githubAPI.GetOrganizationID(ctx, req.TargetOrg)
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
	migrationSourceID, err := githubAPI.CreateMigrationSource(ctx, repoName, baseURL, ownerID)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to create migration source: %v", err), time.Now())
		return fmt.Errorf("failed to create migration source: %w", err)
	}
	m.logger.Debug("Migration source ID", "migrationSourceID", migrationSourceID)

	// Generate migration archive on Source GHES
	m.updateStatus(repoName, payload.StatusInProgress, "generating migration archive on GHES...", time.Now())
	archiveID, err := githubAPI.GenerateMigrationArchive(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to generate migration archive: %v", err), time.Now())
		return fmt.Errorf("failed to generate migration archive: %w", err)
	}
	m.logger.Debug("Migration archive ID", "archiveID", archiveID)

	// Wait for migration archive export to complete
	m.updateStatus(repoName, payload.StatusInProgress, "waiting for archive export to complete...", time.Now())

	// Use longer polling intervals for archive export status checks
	// Start with 15 seconds between checks to avoid unnecessary API load
	pollInterval := 15 * time.Second

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
		status, err := githubAPI.GetMigrationArchiveStatus(ctx, archiveID, req.SourceOrg)
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
			archiveURL, err := githubAPI.GetMigrationArchiveURL(ctx, archiveID, req.SourceOrg)
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
	// Construct the source repository URL from the base URL
	sourceRepoURL := fmt.Sprintf("%s/%s", baseURL, sourceRepo)

	m.logger.Debug("Using source repository URL",
		"url", sourceRepoURL,
		"base_url", baseURL,
	)

	// Start repository migration
	m.updateStatus(repoName, payload.StatusInProgress, "starting repository migration to GHEC", time.Now())
	migrationID, err := githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, repoName, sourceRepoURL, req.GHESToken, req.GHCloudToken)
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

	pollInterval = 15 * time.Second

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

		state, err := githubAPI.GetMigrationStatus(ctx, migrationID)
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
			// Max out at 1 minute between polls for long-running migrations
			if consecutiveNoChanges > 5 && pollInterval < 1*time.Minute {
				pollInterval = time.Duration(math.Min(float64(pollInterval*2), float64(1*time.Minute)))
				m.logger.Debug("Increasing polling interval due to stable state",
					"repository", repoName,
					"new_interval_seconds", pollInterval.Seconds(),
					"state", state,
				)
			}
		} else {
			// State changed, reset counter and polling interval
			consecutiveNoChanges = 0
			pollInterval = 15 * time.Second
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
			// Send webhook notification for success
			go m.sendWebhookNotification(repoName, req)
			return nil
		case "FAILED":
			failureMsg := fmt.Sprintf("migration failed with state: %s", state)
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now())
			// Send webhook notification for failure
			go m.sendWebhookNotification(repoName, req)
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
	if strings.Contains(message, "starting migration process") {
		stage = "init"
		state = "starting"
	} else if strings.Contains(message, "validating source repository") {
		stage = "validation"
		state = "checking_source"
	} else if strings.Contains(message, "getting target organization ID") {
		stage = "validation"
		state = "checking_target"
	} else if strings.Contains(message, "creating migration source") {
		stage = "setup"
		state = "creating_source"
	} else if strings.Contains(message, "generating migration archive") {
		stage = "archive"
		state = "generating"
	} else if strings.Contains(message, "checking migration archive status") {
		stage = "archive"
		state = "checking"
	} else if strings.Contains(message, "migration archive is ready") {
		stage = "archive"
		state = "ready"
	} else if strings.Contains(message, "starting repository migration") {
		stage = "migration"
		state = "starting"
	} else if strings.Contains(message, "checking migration status") {
		stage = "migration"
		state = "checking"
	} else if strings.Contains(message, "migration is in progress") {
		stage = "migration"
		state = "in_progress"
	} else if strings.Contains(message, "migration completed successfully") {
		stage = "migration"
		state = "completed"
	} else if status == payload.StatusFailed {
		stage = "error"
		state = "failed"
	}

	// Create a new status or update existing
	if _, ok := m.migrations[repoName]; !ok {
		m.migrations[repoName] = &payload.MigrationStatus{
			Repository: repoName,
			Status:     status,
			Error:      message,
			UpdatedAt:  timestamp,
			Stage:      stage,
			State:      state,
		}
	} else {
		m.migrations[repoName].Status = status
		m.migrations[repoName].UpdatedAt = timestamp
		m.migrations[repoName].Stage = stage
		m.migrations[repoName].State = state

		// Only update error message if there's an error
		if status == payload.StatusFailed {
			m.migrations[repoName].Error = message
		}
	}

	// Log status update
	m.logger.Info("Migration status updated",
		"repository", repoName,
		"status", status,
		"message", message,
		"stage", stage,
		"state", state,
	)
}

func (m *Migrator) sendWebhookNotification(repoName string, migrationReq *payload.MigrationRequest) {
	m.mu.RLock()
	status := m.migrations[repoName]
	m.mu.RUnlock()

	if status == nil {
		return
	}

	// Skip if no webhook URL is configured
	if m.webhookURL == "" {
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

	httpReq, err := http.NewRequest(http.MethodPost, m.webhookURL, bytes.NewBuffer(webhookPayload))
	if err != nil {
		m.logger.Error("Failed to create webhook request",
			"repository", repoName,
			"error", err,
		)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "ghes-2-ghec")

	m.logger.Debug("Sending webhook notification",
		"repository", repoName,
		"webhook_url", m.webhookURL,
		"status", status.Status,
		"stage", status.Stage,
		"state", status.State,
		"payload", string(webhookPayload),
	)

	resp, err := client.Do(httpReq)
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
