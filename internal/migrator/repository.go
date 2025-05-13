// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

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
	ownerID, databaseID, err := githubAPI.GetOrganizationID(ctx, req.TargetOrg)
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

				m.updateStatus(
					repoName,
					payload.StatusInProgress,
					"uploading archive to GitHub Owned Storage",
					time.Now(),
					startTime,
				)

				// Upload the archive to GHOS
				archiveName := fmt.Sprintf("%s-%s.tar.gz", repoName, time.Now().Format("20060102-150405"))
				geiURI, err := githubAPI.UploadArchiveToGHOS(ctx, databaseID, archiveURL, archiveName, req.GHCloudToken)
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

				sourceRepoURL := fmt.Sprintf("%s/%s", baseURL, sourceRepo)

				// Start repository migration using the GHOS URI
				// Use a blank string for sourceRepoURL since it's not needed when using GEI URI
				migrationID, err = githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, repoName, sourceRepoURL, geiURI, geiURI, req.GHESToken, req.GHCloudToken)
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
			migrationID, err = githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, repoName, sourceRepoURL, archiveURL, archiveURL, req.GHESToken, req.GHCloudToken)
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
		durationStr := formatDuration(migrationDuration)

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
