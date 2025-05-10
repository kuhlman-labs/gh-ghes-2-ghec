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
		m.logger.Info("Finished all migrations in queue")
		cancel()
	}()

	return nil
}

func (m *Migrator) migrateRepository(ctx context.Context, req *payload.MigrationRequest, repoName string) error {
	// Record start time for this repository migration
	startTime := time.Now()

	// Initialize clients for this migration
	clients, err := config.NewClients(req.GHESToken, req.GHCloudToken)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to initialize clients: %v", err), time.Now(), startTime)
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	// Update GHES base URL
	if err := clients.UpdateGHESBaseURL(req.GetGHESAPIURL()); err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to update GHES base URL: %v", err), time.Now(), startTime)
		return fmt.Errorf("failed to update GHES base URL: %w", err)
	}

	// Create GitHub API instance for this migration
	githubAPI := github.New(clients, m.logger)

	// Update status to in progress with initial stage
	m.updateStatus(repoName, payload.StatusInProgress, "starting migration process", time.Now(), startTime)

	// Validate that source repository exists
	sourceRepo := fmt.Sprintf("%s/%s", req.SourceOrg, repoName)
	m.updateStatus(repoName, payload.StatusInProgress, "validating source repository", time.Now(), startTime)
	err = githubAPI.ValidateRepository(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("source repository not found: %v", err), time.Now(), startTime)
		return fmt.Errorf("source repository not found: %w", err)
	}

	// Get the owner ID for the destination organization
	m.updateStatus(repoName, payload.StatusInProgress, "getting target organization ID", time.Now(), startTime)
	ownerID, err := githubAPI.GetOrganizationID(ctx, req.TargetOrg)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to get owner ID: %v", err), time.Now(), startTime)
		return fmt.Errorf("failed to get owner ID: %w", err)
	}
	m.logger.Debug("Target organization details", "org", req.TargetOrg, "ownerID", ownerID)

	// Get the base URL for the source organization
	baseURL := req.GHESBaseURL
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Create migration source in destination organization
	m.updateStatus(repoName, payload.StatusInProgress, "creating migration source in GHEC", time.Now(), startTime)
	migrationSourceID, err := githubAPI.CreateMigrationSource(ctx, repoName, baseURL, ownerID)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to create migration source: %v", err), time.Now(), startTime)
		return fmt.Errorf("failed to create migration source: %w", err)
	}
	m.logger.Debug("Migration source created", "sourceID", migrationSourceID)

	// Generate migration archive on Source GHES
	m.updateStatus(repoName, payload.StatusInProgress, "generating migration archive", time.Now(), startTime)
	archiveID, err := githubAPI.GenerateMigrationArchive(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to generate migration archive: %v", err), time.Now(), startTime)
		return fmt.Errorf("failed to generate migration archive: %w", err)
	}
	m.logger.Debug("Archive generation initiated", "archiveID", archiveID, "repository", repoName)

	// Wait for migration archive export to complete
	m.updateStatus(repoName, payload.StatusInProgress, "waiting for archive export", time.Now(), startTime)

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
				startTime,
			)
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		// Check status of migration source
		status, err := githubAPI.GetMigrationArchiveStatus(ctx, archiveID, req.SourceOrg)
		if err != nil {
			m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to get migration source status: %v", err), time.Now(), startTime)
			return fmt.Errorf("failed to get migration source status: %w", err)
		}

		// Update status with current state
		m.updateStatus(
			repoName,
			payload.StatusInProgress,
			fmt.Sprintf("archive export state: %s", status),
			time.Now(),
			startTime,
		)

		exportDuration := time.Since(exportStartTime)
		m.logger.Debug("Archive export status",
			"status", status,
			"archiveID", archiveID,
			"repository", repoName,
			"duration", fmt.Sprintf("%dm%ds", int(exportDuration.Minutes()), int(exportDuration.Seconds())%60),
		)

		// Handle different states of migration export
		switch status {
		case "exported":
			// Export completed successfully, proceed to next step
			m.logger.Info("Archive export completed",
				"repository", repoName,
				"archiveID", archiveID,
			)

			// Get the migration archive URL
			archiveURL, err := githubAPI.GetMigrationArchiveURL(ctx, archiveID, req.SourceOrg)
			if err != nil {
				m.logger.Error("Failed to get archive URL",
					"repository", repoName,
					"archiveID", archiveID,
					"error", err,
				)
				m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to get migration archive URL: %v", err), time.Now(), startTime)
				return fmt.Errorf("failed to get migration archive URL: %w", err)
			}

			m.logger.Debug("Archive URL retrieved",
				"repository", repoName,
				"archiveID", archiveID,
			)

			// Update status with archive URL
			m.updateStatus(
				repoName,
				payload.StatusInProgress,
				fmt.Sprintf("archive ready for migration: %s", archiveURL),
				time.Now(),
				startTime,
			)

			goto startRepositoryMigration
		case "failed":
			failureMsg := fmt.Sprintf("migration archive export failed with state: %s", status)
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now(), startTime)
			return fmt.Errorf("migration archive export failed: %s", failureMsg)
		case "pending", "exporting":
			// Continue polling - no additional logging needed as we already logged status above
		default:
			m.logger.Warn("Unknown archive export status",
				"status", status,
				"repository", repoName,
				"archiveID", archiveID,
			)
		}
	}

startRepositoryMigration:
	// Construct the source repository URL from the base URL
	sourceRepoURL := fmt.Sprintf("%s/%s", baseURL, sourceRepo)

	m.logger.Debug("Starting repository migration",
		"sourceURL", sourceRepoURL,
		"repository", repoName,
	)

	// Start repository migration
	m.updateStatus(repoName, payload.StatusInProgress, "starting repository migration", time.Now(), startTime)
	migrationID, err := githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, repoName, sourceRepoURL, req.GHESToken, req.GHCloudToken)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to start repository migration: %v", err), time.Now(), startTime)
		return fmt.Errorf("failed to start repository migration: %w", err)
	}

	// Update status with migration ID
	m.updateStatus(
		repoName,
		payload.StatusInProgress,
		fmt.Sprintf("migration created with ID: %s", migrationID),
		time.Now(),
		startTime,
	)

	// Wait for migration to complete
	m.updateStatus(repoName, payload.StatusInProgress, "waiting for migration to complete", time.Now(), startTime)

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
				startTime,
			)
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		state, err := githubAPI.GetMigrationStatus(ctx, migrationID)
		if err != nil {
			m.updateStatus(repoName, payload.StatusFailed, err.Error(), time.Now(), startTime)
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		// Update status with current state
		m.updateStatus(
			repoName,
			payload.StatusInProgress,
			fmt.Sprintf("migration state: %s", state),
			time.Now(),
			startTime,
		)

		migrationDuration := time.Since(migrationStartTime)
		durationStr := fmt.Sprintf("%dh%dm%ds",
			int(migrationDuration.Hours()),
			int(migrationDuration.Minutes())%60,
			int(migrationDuration.Seconds())%60,
		)

		// Only log at INFO level for significant changes or every 5 minutes
		if state != lastState || consecutiveNoChanges%20 == 0 { // Log after ~5 min (15s * 20) when no change
			m.logger.Info("Migration status",
				"repository", repoName,
				"state", state,
				"duration", durationStr,
			)
		} else {
			m.logger.Debug("Migration status check",
				"repository", repoName,
				"state", state,
				"duration", durationStr,
			)
		}

		// Implement adaptive polling - increase polling interval if state doesn't change
		if state == lastState {
			consecutiveNoChanges++
			// Max out at 1 minute between polls for long-running migrations
			if consecutiveNoChanges > 5 && pollInterval < 1*time.Minute {
				pollInterval = time.Duration(math.Min(float64(pollInterval*2), float64(1*time.Minute)))
				m.logger.Debug("Increasing polling interval",
					"repository", repoName,
					"interval_seconds", int(pollInterval.Seconds()),
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
			totalDuration := time.Since(startTime)
			durationStr := formatDuration(totalDuration)

			m.logger.Info("Migration successful",
				"repository", repoName,
				"duration", durationStr,
				"started_at", startTime.Format(time.RFC3339),
			)
			m.updateStatus(repoName, payload.StatusSucceeded, "migration completed successfully", time.Now(), startTime)
			return nil
		case "FAILED":
			failureMsg := fmt.Sprintf("migration failed with state: %s", state)
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now(), startTime)
			return fmt.Errorf("migration failed: %s", failureMsg)
		case "PENDING", "IN_PROGRESS", "QUEUED":
			// Continue polling - already logged status above
		}
	}
}

// formatDuration returns a human-readable duration string
func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
}

func (m *Migrator) updateStatus(repoName, status, message string, timestamp time.Time, startTime time.Time) {
	m.mu.Lock()

	// Parse the message to determine stage and state
	var stage, state string

	// Extract stage information from message (with improved mapping)
	switch {
	case strings.Contains(message, "starting migration process"):
		stage = "init"
		state = "starting"
	case strings.Contains(message, "validating source repository"):
		stage = "validation"
		state = "checking_source"
	case strings.Contains(message, "getting target organization ID"):
		stage = "validation"
		state = "checking_target"
	case strings.Contains(message, "creating migration source"):
		stage = "setup"
		state = "creating_source"
	case strings.Contains(message, "generating migration archive"):
		stage = "archive"
		state = "generating"
	case strings.Contains(message, "waiting for archive export"):
		stage = "archive"
		state = "waiting"
	case strings.Contains(message, "archive export state:"):
		stage = "archive"
		state = strings.TrimSpace(strings.TrimPrefix(message, "archive export state:"))
	case strings.Contains(message, "archive ready for migration"):
		stage = "archive"
		state = "ready"
	case strings.Contains(message, "starting repository migration"):
		stage = "migration"
		state = "starting"
	case strings.Contains(message, "migration created with ID:"):
		stage = "migration"
		state = "created"
	case strings.Contains(message, "waiting for migration to complete"):
		stage = "migration"
		state = "waiting"
	case strings.Contains(message, "migration state:"):
		stage = "migration"
		state = strings.TrimSpace(strings.TrimPrefix(message, "migration state:"))
	case strings.Contains(message, "migration completed successfully"):
		stage = "migration"
		state = "completed"
	case status == payload.StatusFailed:
		stage = "error"
		state = "failed"
	default:
		// Default values if no matching pattern is found
		stage = "unknown"
		state = "unknown"
	}

	// Check if this is a new status or a change in stage/state
	var isNewOrChanged bool
	var oldStatus *payload.MigrationStatus

	if existing, ok := m.migrations[repoName]; !ok {
		// New status
		isNewOrChanged = true

		m.migrations[repoName] = &payload.MigrationStatus{
			Repository: repoName,
			Status:     status,
			Error:      message,
			UpdatedAt:  timestamp,
			Stage:      stage,
			State:      state,
			StartedAt:  startTime,
		}
	} else {
		// Save old status for comparison
		oldStatus = &payload.MigrationStatus{
			Repository: existing.Repository,
			Status:     existing.Status,
			Error:      existing.Error,
			UpdatedAt:  existing.UpdatedAt,
			Stage:      existing.Stage,
			State:      existing.State,
			StartedAt:  existing.StartedAt,
		}

		// Update existing status
		existing.Status = status
		existing.UpdatedAt = timestamp
		existing.Stage = stage
		existing.State = state

		// Only update error message if there's an error
		if status == payload.StatusFailed {
			existing.Error = message
		}

		// Keep the original start time
		if existing.StartedAt.IsZero() {
			existing.StartedAt = startTime
		}

		// Check if stage or state changed
		isNewOrChanged = (oldStatus.Status != status ||
			oldStatus.Stage != stage ||
			oldStatus.State != state)
	}

	// Calculate duration for completed or failed migrations
	if status == payload.StatusSucceeded || status == payload.StatusFailed {
		if migStatus, ok := m.migrations[repoName]; ok && !migStatus.StartedAt.IsZero() {
			migStatus.Duration = timestamp.Sub(migStatus.StartedAt)
		}
	}

	// Release lock before logging and sending webhook
	m.mu.Unlock()

	// Log status update (only for significant changes to reduce noise)
	if isNewOrChanged {
		if status == payload.StatusSucceeded {
			duration := timestamp.Sub(startTime)
			m.logger.Info("Status updated",
				"repository", repoName,
				"status", status,
				"stage", stage,
				"state", state,
				"total_duration", formatDuration(duration),
			)
		} else {
			m.logger.Info("Status updated",
				"repository", repoName,
				"status", status,
				"stage", stage,
				"state", state,
			)
		}
	} else {
		m.logger.Debug("Status refreshed",
			"repository", repoName,
			"status", status,
			"stage", stage,
			"state", state,
		)
	}

	// Send webhook notification if the status changed
	if isNewOrChanged && m.webhookURL != "" {
		go m.sendWebhookNotification(repoName, nil)
	}
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
		m.logger.Error("Invalid webhook URL",
			"repository", repoName,
			"error", err,
		)
		return
	}

	// Create a payload with the migration details
	webhookPayload := map[string]interface{}{
		"repository": repoName,
		"status":     status.Status,
		"stage":      status.Stage,
		"state":      status.State,
		"timestamp":  status.UpdatedAt,
		"details": map[string]interface{}{
			"stage_description": getStageDescription(status.Stage),
			"state_description": getStateDescription(status.Stage, status.State),
		},
	}

	// Add duration if migration is complete
	if status.Status == payload.StatusSucceeded || status.Status == payload.StatusFailed {
		if !status.StartedAt.IsZero() && status.Duration > 0 {
			webhookPayload["started_at"] = status.StartedAt.Format(time.RFC3339)
			webhookPayload["duration_seconds"] = int(status.Duration.Seconds())
			webhookPayload["duration_string"] = formatDuration(status.Duration)
		}
	}

	// Add error details if present
	if status.Error != "" {
		webhookPayload["error"] = status.Error
	}

	// Add source and target org if available from the request
	if migrationReq != nil {
		webhookPayload["source_org"] = migrationReq.SourceOrg
		webhookPayload["target_org"] = migrationReq.TargetOrg
	}

	payloadBytes, err := json.Marshal(webhookPayload)
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

	httpReq, err := http.NewRequest(http.MethodPost, m.webhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		m.logger.Error("Failed to create webhook request",
			"repository", repoName,
			"error", err,
		)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "ghes-2-ghec")

	m.logger.Debug("Sending webhook",
		"repository", repoName,
		"status", status.Status,
		"stage", status.Stage,
		"state", status.State,
	)

	resp, err := client.Do(httpReq)
	if err != nil {
		m.logger.Error("Webhook send failed",
			"repository", repoName,
			"error", err,
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read the response body for more details
		body, _ := io.ReadAll(resp.Body)
		m.logger.Error("Webhook rejected",
			"repository", repoName,
			"status_code", resp.StatusCode,
			"response", string(body),
		)
	} else {
		m.logger.Debug("Webhook delivered",
			"repository", repoName,
			"status_code", resp.StatusCode,
		)
	}
}

// getStageDescription returns a human-readable description of a migration stage
func getStageDescription(stage string) string {
	switch stage {
	case "init":
		return "Migration initialization"
	case "validation":
		return "Repository validation"
	case "setup":
		return "Migration setup"
	case "archive":
		return "Archive management"
	case "migration":
		return "Repository migration"
	case "error":
		return "Error occurred"
	default:
		return "Unknown stage"
	}
}

// getStateDescription returns a human-readable description of a migration state
func getStateDescription(stage, state string) string {
	// First handle common states across stages
	if state == "failed" {
		return "The operation has failed"
	}

	// Then handle stage-specific states
	switch stage {
	case "validation":
		switch state {
		case "checking_source":
			return "Validating source repository exists"
		case "checking_target":
			return "Validating target organization"
		default:
			return state
		}
	case "setup":
		switch state {
		case "creating_source":
			return "Creating migration source"
		default:
			return state
		}
	case "archive":
		switch state {
		case "generating":
			return "Generating migration archive"
		case "waiting":
			return "Waiting for archive to be created"
		case "exported":
			return "Archive has been exported"
		case "ready":
			return "Archive is ready for migration"
		default:
			return state
		}
	case "migration":
		switch state {
		case "starting":
			return "Starting repository migration"
		case "created":
			return "Migration has been created"
		case "waiting":
			return "Waiting for migration to complete"
		case "IN_PROGRESS":
			return "Migration is in progress"
		case "PENDING":
			return "Migration is pending"
		case "QUEUED":
			return "Migration is queued"
		case "SUCCEEDED":
			return "Migration has succeeded"
		case "FAILED":
			return "Migration has failed"
		case "completed":
			return "Migration completed successfully"
		default:
			return state
		}
	default:
		return state
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
