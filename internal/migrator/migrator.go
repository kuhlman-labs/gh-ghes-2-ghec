// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
// It handles the entire migration process, status tracking, and webhook notifications.
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
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
)

// Migrator handles repository migrations from GHES to GHEC.
// It tracks migration status, manages concurrent migrations,
// and sends webhook notifications for status updates.
type Migrator struct {
	webhookURL string
	logger     *slog.Logger
	mu         sync.RWMutex
	migrations map[string]*payload.MigrationStatus
}

// New creates a new migrator instance with the provided webhook URL.
// If the webhook URL is invalid, webhook notifications will be disabled.
// Returns a configured Migrator ready to handle repository migrations.
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

// StartMigration starts the migration process for the given request.
// It handles multiple repositories concurrently, tracking their status,
// and coordinates the overall migration process.
//
// The provided context can be used to cancel all migrations, and the cancel function
// will be called when all migrations are complete or have failed.
//
// Returns an error if the migration setup fails.
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

// migrateRepository handles the migration of a single repository.
// It executes all migration stages: validation, setup, archive, and migration.
// Updates the status throughout the process and sends webhook notifications.
//
// Returns an error if any stage of the migration fails.
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

	// Variable to store archive URL, will be set when archive is exported
	var archiveURL string

	// Variable to store the migration ID
	var migrationID string

	// Simplify control flow and avoid goto statements that may cross variable declarations
pollLoop:
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

		// Check archive export status
		status, err := githubAPI.GetMigrationArchiveStatus(ctx, archiveID, req.SourceOrg)
		if err != nil {
			m.updateStatus(repoName, payload.StatusFailed, err.Error(), time.Now(), startTime)
			return fmt.Errorf("failed to get archive export status: %w", err)
		}

		// Update status with current export state
		exportDuration := time.Since(exportStartTime)
		exportDurationStr := formatDuration(exportDuration)

		m.updateStatus(
			repoName,
			payload.StatusInProgress,
			fmt.Sprintf("archive export state: %s", status),
			time.Now(),
			startTime,
		)

		m.logger.Debug("Archive export status",
			"repository", repoName,
			"status", status,
			"duration", exportDurationStr,
		)

		// Log progress every 5 minutes to INFO level (approx. 20 iterations at 15s)
		if int(exportDuration.Minutes())%5 == 0 && int(exportDuration.Seconds())%60 < 15 {
			m.logger.Info("Archive export in progress",
				"repository", repoName,
				"status", status,
				"duration", exportDurationStr,
			)
		}

		// Check migration state and act accordingly
		switch status {
		case "exported":
			// Archive is ready to be downloaded
			m.logger.Info("Migration archive export completed",
				"repository", repoName,
				"duration", exportDurationStr,
			)

			// Get archive URL
			archiveURL, err = githubAPI.GetMigrationArchiveURL(ctx, archiveID, req.SourceOrg)
			if err != nil {
				m.updateStatus(repoName, payload.StatusFailed, err.Error(), time.Now(), startTime)
				return fmt.Errorf("failed to get archive URL: %w", err)
			}

			m.updateStatus(
				repoName,
				payload.StatusInProgress,
				"archive ready for migration",
				time.Now(),
				startTime,
			)

			// If using GitHub Owned Storage, handle the special upload flow
			if req.UseGHOS {
				// Extract organization ID (numeric) from the ownerID
				orgDatabaseID := config.ExtractOrgDatabaseID(ownerID)

				if orgDatabaseID == "" {
					errMsg := "failed to extract organization database ID for GHOS upload"
					m.updateStatus(repoName, payload.StatusFailed, errMsg, time.Now(), startTime)
					return fmt.Errorf("error extracting organization database ID for GHOS upload: %w", err)
				}

				m.updateStatus(
					repoName,
					payload.StatusInProgress,
					"uploading archive to GitHub Owned Storage",
					time.Now(),
					startTime,
				)

				// Upload the archive to GHOS
				archiveName := fmt.Sprintf("%s-%s.tar.gz", repoName, time.Now().Format("20060102-150405"))
				geiURI, err := githubAPI.UploadArchiveToGHOS(ctx, orgDatabaseID, archiveURL, archiveName, req.GHCloudToken)
				if err != nil {
					m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("failed to upload to GHOS: %v", err), time.Now(), startTime)
					return fmt.Errorf("failed to upload archive to GitHub Owned Storage: %w", err)
				}

				m.updateStatus(
					repoName,
					payload.StatusInProgress,
					fmt.Sprintf("archive uploaded to GHOS: %s", geiURI),
					time.Now(),
					startTime,
				)

				// In the GHOS flow, we directly start the migration with the GEI URI
				m.updateStatus(repoName, payload.StatusInProgress, "starting repository migration with GHOS archive", time.Now(), startTime)

				// Start repository migration using the GHOS URI
				// Use a blank string for sourceRepoURL since it's not needed when using GEI URI
				migrationID, err = githubAPI.StartRepositoryMigrationWithGEIURI(ctx, migrationSourceID, ownerID, repoName, geiURI, req.GHESToken, req.GHCloudToken)
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

				// Store migration ID in the status object
				m.mu.Lock()
				if status, exists := m.migrations[repoName]; exists {
					status.MigrationID = migrationID
				}
				m.mu.Unlock()

				// Skip directly to monitoring migration progress
				break pollLoop
			}

			// Regular non-GHOS flow
			// Construct the source repository URL from the base URL
			sourceRepoURL := fmt.Sprintf("%s/%s", baseURL, sourceRepo)

			m.logger.Debug("Starting repository migration",
				"sourceURL", sourceRepoURL,
				"repository", repoName,
			)

			// Start repository migration
			m.updateStatus(repoName, payload.StatusInProgress, "starting repository migration", time.Now(), startTime)
			migrationID, err = githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, repoName, sourceRepoURL, req.GHESToken, req.GHCloudToken)
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

			// Store migration ID in the status object
			m.mu.Lock()
			if status, exists := m.migrations[repoName]; exists {
				status.MigrationID = migrationID
			}
			m.mu.Unlock()

			// Exit the polling loop and move on to monitoring the migration
			break pollLoop

		case "failed":
			failureMsg := fmt.Sprintf("migration archive export failed with state: %s", status)
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now(), startTime)
			return fmt.Errorf("migration archive export failed: %s", failureMsg)
		case "pending", "exporting":
			// Continue polling - no additional logging needed as we already logged status above
			continue
		default:
			m.logger.Warn("Unknown archive export status",
				"status", status,
				"repository", repoName,
				"archiveID", archiveID,
			)
			continue
		}
	}

	// Monitor the migration progress
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
				"migration_id", migrationID,
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
		// New status - Initialize with first stage
		isNewOrChanged = true

		// Determine progression data
		progressData := calculateProgressData(stage, state, nil)

		m.migrations[repoName] = &payload.MigrationStatus{
			Repository:        repoName,
			Status:            status,
			Error:             message,
			UpdatedAt:         timestamp,
			Stage:             stage,
			State:             state,
			StartedAt:         startTime,
			Progress:          progressData.progress,
			StageProgress:     progressData.stageProgress,
			CompletedStages:   progressData.completedStages,
			TotalStages:       len(payload.MigrationStages),
			CurrentStageIndex: progressData.currentStageIndex,
		}
	} else {
		// Save old status for comparison
		oldStatus = &payload.MigrationStatus{
			Status:      existing.Status,
			Stage:       existing.Stage,
			State:       existing.State,
			MigrationID: existing.MigrationID,
		}

		// Calculate progress data based on current stage/state and previous state
		progressData := calculateProgressData(stage, state, existing)

		// Update existing status
		existing.Status = status
		existing.UpdatedAt = timestamp
		existing.Stage = stage
		existing.State = state
		existing.Progress = progressData.progress
		existing.StageProgress = progressData.stageProgress
		existing.CompletedStages = progressData.completedStages
		existing.CurrentStageIndex = progressData.currentStageIndex
		existing.TotalStages = len(payload.MigrationStages)

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
			migStatus.Progress = 100 // Set to 100% when completed
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
				"progress", m.migrations[repoName].Progress,
			)
		} else {
			m.logger.Info("Status updated",
				"repository", repoName,
				"status", status,
				"stage", stage,
				"state", state,
				"progress", m.migrations[repoName].Progress,
			)
		}
	} else {
		m.logger.Info("Current status",
			"repository", repoName,
			"status", status,
			"stage", stage,
			"state", state,
			"progress", m.migrations[repoName].Progress,
		)
	}

	// Send webhook notification if the status changed
	if isNewOrChanged && m.webhookURL != "" {
		go m.sendWebhookNotification(repoName, nil)
	}
}

// progressData holds calculated progress information
type progressData struct {
	progress          int
	stageProgress     int
	completedStages   []string
	currentStageIndex int
}

// calculateProgressData calculates progress information based on stage and state
func calculateProgressData(stage, state string, existing *payload.MigrationStatus) progressData {
	// Define weights for each stage (percentages)
	stageWeights := map[string]int{
		"validation": 10,
		"setup":      10,
		"archive":    30,
		"migration":  50,
	}

	// Initialize result
	result := progressData{
		progress:          0,
		stageProgress:     0,
		completedStages:   []string{},
		currentStageIndex: 0,
	}

	// If it's a new migration, just set the initial progress
	if existing == nil {
		if stage == "init" {
			return result
		}
	} else {
		// Copy existing completed stages
		result.completedStages = append(result.completedStages, existing.CompletedStages...)
	}

	// Find current stage index
	currentStageIndex := -1
	for i, s := range payload.MigrationStages {
		if s == stage {
			currentStageIndex = i
			break
		}
	}

	// If stage not found in the progression (like "init" or "error")
	if currentStageIndex == -1 {
		if stage == "error" {
			// Error state - keep existing progress if available
			if existing != nil {
				return progressData{
					progress:          existing.Progress,
					stageProgress:     0,
					completedStages:   existing.CompletedStages,
					currentStageIndex: existing.CurrentStageIndex,
				}
			}
			return result
		}
		// Init stage - set to 0
		return result
	}

	// Set current stage index (1-based for better UX)
	result.currentStageIndex = currentStageIndex + 1

	// Calculate total progress from completed stages
	cumulativeProgress := 0

	// Mark previous stages as completed
	for i, s := range payload.MigrationStages {
		if i < currentStageIndex {
			// Add to completed stages if not already included
			found := false
			for _, cs := range result.completedStages {
				if cs == s {
					found = true
					break
				}
			}
			if !found {
				result.completedStages = append(result.completedStages, s)
			}

			// Add weight to cumulative progress
			cumulativeProgress += stageWeights[s]
		}
	}

	// Calculate stage progress based on the state
	stageProgress := calculateStageProgress(stage, state)
	result.stageProgress = stageProgress

	// Add weighted stage progress to total
	currentStageWeight := stageWeights[stage]
	stageContribution := (currentStageWeight * stageProgress) / 100

	// Calculate total progress
	result.progress = cumulativeProgress + stageContribution

	// Special cases
	if stage == "migration" && state == "completed" {
		result.progress = 100
		result.stageProgress = 100

		// Add final stage to completed stages if not there
		found := false
		for _, s := range result.completedStages {
			if s == stage {
				found = true
				break
			}
		}
		if !found {
			result.completedStages = append(result.completedStages, stage)
		}
	}

	return result
}

// calculateStageProgress estimates progress within a stage based on the state
func calculateStageProgress(stage, state string) int {
	switch stage {
	case "validation":
		switch state {
		case "checking_source":
			return 25
		case "checking_target":
			return 75
		default:
			return 50
		}
	case "setup":
		switch state {
		case "creating_source":
			return 50
		default:
			return 25
		}
	case "archive":
		switch state {
		case "generating":
			return 10
		case "waiting":
			return 30
		case "exporting":
			return 50
		case "exported":
			return 80
		case "ready":
			return 100
		default:
			// For archive export states like "pending"
			return 40
		}
	case "migration":
		switch state {
		case "starting":
			return 10
		case "created":
			return 20
		case "waiting":
			return 30
		case "QUEUED":
			return 40
		case "PENDING":
			return 50
		case "IN_PROGRESS":
			return 70
		case "SUCCEEDED":
			return 100
		case "completed":
			return 100
		default:
			return 50
		}
	default:
		return 0
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

	// Add migration ID if available
	if status.MigrationID != "" {
		webhookPayload["migration_id"] = status.MigrationID
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

	// Create retry configuration for webhooks
	retryConfig := utils.DefaultRetryConfig(m.logger).
		WithMaxRetries(3).
		WithInitialInterval(2 * time.Second).
		WithMaxInterval(15 * time.Second)

	// Prepare the webhook request
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

	// Execute the webhook request with retry
	err = utils.Retry(context.Background(), retryConfig, "send_webhook", func() error {
		// Create a fresh buffer for each retry
		req := httpReq.Clone(httpReq.Context())
		req.Body = io.NopCloser(bytes.NewBuffer(payloadBytes))

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				m.logger.Warn("Failed to close response body", "error", err)
			}
		}()

		// Check for non-success status codes
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("webhook returned non-success status code %d: %s", resp.StatusCode, string(body))
		}

		return nil
	})

	if err != nil {
		m.logger.Error("Webhook delivery failed after retries",
			"repository", repoName,
			"error", err,
		)
	} else {
		m.logger.Debug("Webhook delivered",
			"repository", repoName,
			"status", status.Status,
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
