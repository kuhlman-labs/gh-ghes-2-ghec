// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// migrateRepository performs the actual migration process for a single repository.
// This includes:
// - Validating the repository exists
// - Creating migration source in the destination organization
// - Generating and monitoring migration archive
// - Starting the migration process
// - Monitoring migration progress
func (m *Migrator) migrateRepository(
	ctx context.Context,
	req *payload.MigrationRequest,
	sourceRepoName string,
	repoFullName string,
	attemptStartTime time.Time,
) error {
	var githubAPI github.API

	// Use existing GitHub API client if available (typically injected for testing)
	// In production, no client should be injected, so we'll always create real clients
	if m.githubAPI != nil {
		githubAPI = m.githubAPI
		m.logger.Debug("Using injected GitHub API client",
			"repository", repoFullName,
			"is_test", m.githubAPI.IsTestImplementation())
	}

	if githubAPI == nil {
		// Initialize clients for this migration
		clients, err := config.NewClients(&config.ClientsConfig{
			GHESToken:    req.GHESToken,
			GHCloudToken: req.GHCloudToken,
			Proxy:        m.config.GitHub.Proxy,
		})
		if err != nil {
			m.updateStatus(repoFullName, payload.StatusFailed, fmt.Sprintf("failed to initialize clients: %v", err), time.Now(), attemptStartTime)
			return fmt.Errorf("failed to initialize clients: %w", err)
		}

		// Update GHES base URL
		if err := clients.UpdateGHESBaseURL(req.GetGHESAPIURL()); err != nil {
			m.updateStatus(repoFullName, payload.StatusFailed, fmt.Sprintf("failed to update GHES base URL: %v", err), time.Now(), attemptStartTime)
			return fmt.Errorf("failed to update GHES base URL: %w", err)
		}

		// Create GitHub API instance for this migration
		githubAPI = github.New(clients, m.logger)
		m.logger.Debug("Created new GitHub API client for migration",
			"repository", repoFullName)
	}

	// Update status to in progress with initial stage
	m.updateStatus(repoFullName, payload.StatusInProgress, "starting migration process", time.Now(), attemptStartTime)

	// Validate source repository existence and prepare for migration
	if err := m.prepareForMigration(ctx, githubAPI, req, sourceRepoName, attemptStartTime); err != nil {
		return err
	}

	// Generate and process migration archive
	migrationID, _, err := m.processArchive(ctx, githubAPI, req, sourceRepoName, attemptStartTime)
	if err != nil {
		return err
	}

	// Monitor the migration progress
	return m.monitorMigration(ctx, githubAPI, migrationID, sourceRepoName, repoFullName, attemptStartTime)
}

// prepareForMigration validates the repository exists and creates the migration source.
// Returns the owner ID, database ID, and migration source ID needed for later steps.
func (m *Migrator) prepareForMigration(
	ctx context.Context,
	githubAPI github.API,
	req *payload.MigrationRequest,
	sourceRepoName string,
	attemptStartTime time.Time,
) error {
	// Validate that source repository exists
	sourceRepoFullName := fmt.Sprintf("%s/%s", req.SourceOrg, sourceRepoName)

	// Log the DeleteIfExists flag at the beginning for debugging
	m.logger.Info("Starting repository preparation with configuration details",
		"repository_full_name", sourceRepoFullName,
		"delete_if_exists", req.DeleteIfExists,
		"use_ghos", req.UseGHOS)

	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "validating source repository", time.Now(), attemptStartTime)
	err := githubAPI.ValidateRepository(ctx, req.SourceOrg, sourceRepoName)
	if err != nil {
		// Source repository must exist - this is a critical error
		errorMsg := fmt.Sprintf("source repository not found: %v", err)
		m.logger.Error("Source repository validation failed",
			"repo", sourceRepoFullName,
			"error", err)
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
		return fmt.Errorf("source repository not found: %w", err)
	}

	m.logger.Info("Source repository validated successfully",
		"repo", sourceRepoFullName)

	// Retrieve repository size for estimation
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "estimating repository size", time.Now(), attemptStartTime)
	repoSize, err := githubAPI.GetRepositorySize(ctx, req.SourceOrg, sourceRepoName)
	if err != nil {
		// Log the error but continue with the migration - size estimation is not critical
		m.logger.Warn("Failed to get repository size",
			"repo", sourceRepoFullName,
			"error", err)
	} else {
		sizeCategory := payload.GetSizeCategory(repoSize)
		m.logger.Info("Repository size retrieved",
			"repo", sourceRepoFullName,
			"size_bytes", repoSize,
			"size_category", sizeCategory)

		// Update the migration status with the repository size information
		m.mu.Lock()
		if status, exists := m.migrations[sourceRepoFullName]; exists {
			status.RepositorySize = repoSize
			status.SizeCategory = sizeCategory
		}
		m.mu.Unlock()

		// Update status to show the size information to the user
		m.updateStatus(sourceRepoFullName, payload.StatusInProgress,
			fmt.Sprintf("repository size: %s (%.2f MB)", sizeCategory, float64(repoSize)/(1024*1024)),
			time.Now(), attemptStartTime)
	}

	// Check if repository exists in the target organization
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "checking if repository exists in target organization", time.Now(), attemptStartTime)
	exists, err := githubAPI.CheckCloudRepositoryExists(ctx, req.TargetOrg, sourceRepoName)
	if err != nil {
		// Real error (not a 404)
		errorMsg := fmt.Sprintf("failed to check target repository: %v", err)
		m.logger.Error("Target repository check failed with error",
			"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
			"error", err)
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
		return err
	}

	if exists {
		// Repository exists in target organization
		m.logger.Info("Repository exists check result",
			"repository", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
			"exists", true,
			"delete_if_exists_flag", req.DeleteIfExists) // Explicitly log the flag value

		if req.DeleteIfExists {
			// If DeleteIfExists flag is set, try to delete the repository
			m.updateStatus(sourceRepoFullName, payload.StatusInProgress,
				fmt.Sprintf("repository exists in target organization, attempting to delete: %s/%s",
					req.TargetOrg, sourceRepoName),
				time.Now(), attemptStartTime)

			m.logger.Info("Repository exists in target organization, attempting to delete",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
				"delete_if_exists", req.DeleteIfExists)

			deleted, err := githubAPI.DeleteCloudRepositoryIfExists(ctx, req.TargetOrg, sourceRepoName)
			if err != nil {
				// Failed to delete repository
				errorMsg := fmt.Sprintf("Failed to delete existing repository in target organization: %v", err)
				m.logger.Error("Failed to delete existing repository",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
					"error", err)
				m.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
				return fmt.Errorf("failed to delete existing repository: %w", err)
			}

			if deleted {
				m.logger.Info("Successfully deleted existing repository in target organization",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName))
				m.updateStatus(sourceRepoFullName, payload.StatusInProgress,
					fmt.Sprintf("successfully deleted existing repository: %s/%s", req.TargetOrg, sourceRepoName),
					time.Now(), attemptStartTime)
			}
		} else {
			// DeleteIfExists flag is not set, fail with conflict error
			conflictMsg := fmt.Sprintf("Repository %s/%s already exists in target organization", req.TargetOrg, sourceRepoName)
			m.logger.Error("Repository already exists in target organization",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName))
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
			return fmt.Errorf("repository conflict: %s", conflictMsg)
		}
	} else {
		// Repository doesn't exist in target, which is good
		m.logger.Info("Target repository validation successful - repository does not exist in target organization",
			"source_repo", sourceRepoFullName,
			"target_org", req.TargetOrg)
	}

	// Get the owner ID for the destination organization
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "getting target organization ID", time.Now(), attemptStartTime)
	ownerID, _, err := githubAPI.GetOrganizationID(ctx, req.TargetOrg)
	if err != nil {
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, fmt.Sprintf("failed to get owner ID: %v", err), time.Now(), attemptStartTime)
		return fmt.Errorf("failed to get owner ID: %w", err)
	}
	m.logger.Debug("Target organization details", "org", req.TargetOrg, "ownerID", ownerID)

	// Get the base URL for the source organization
	baseURL := req.GHESBaseURL
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Create migration source in destination organization
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "creating migration source in GHEC", time.Now(), attemptStartTime)
	migrationSourceID, err := githubAPI.CreateMigrationSource(ctx, sourceRepoName, baseURL, ownerID)
	if err != nil {
		// Check for repository conflict errors in the migrationSource creation
		var classifiedErr *apierrors.ClassifiedError
		if errors.As(err, &classifiedErr) && classifiedErr.Category == apierrors.CategoryResourceConflict {
			conflictMsg := fmt.Sprintf("Repository %s/%s already exists in target organization", req.TargetOrg, sourceRepoName)
			m.logger.Error("Repository conflict detected during migration source creation",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
				"error", classifiedErr)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
			return fmt.Errorf("repository conflict: %s", conflictMsg)
		}

		m.updateStatus(sourceRepoFullName, payload.StatusFailed, fmt.Sprintf("failed to create migration source: %v", err), time.Now(), attemptStartTime)
		return fmt.Errorf("failed to create migration source: %w", err)
	}
	m.logger.Debug("Migration source created", "sourceID", migrationSourceID)

	return nil
}

// processArchive handles generating the migration archive and starting the migration.
// It waits for the archive to be exported and then initiates the actual migration.
// Returns the migration ID for monitoring.
func (m *Migrator) processArchive(
	ctx context.Context,
	githubAPI github.API,
	req *payload.MigrationRequest,
	sourceRepoName string,
	attemptStartTime time.Time,
) (string, int64, error) {
	sourceRepoFullName := fmt.Sprintf("%s/%s", req.SourceOrg, sourceRepoName)
	// Generate migration archive on Source GHES
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "generating migration archive", time.Now(), attemptStartTime)
	archiveID, err := githubAPI.GenerateMigrationArchive(ctx, req.SourceOrg, sourceRepoName)
	if err != nil {
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, fmt.Sprintf("failed to generate migration archive: %v", err), time.Now(), attemptStartTime)
		return "", 0, fmt.Errorf("failed to generate migration archive: %w", err)
	}
	m.logger.Debug("Archive generation initiated", "archiveID", archiveID, "repository", sourceRepoName)

	// Wait for migration archive export to complete
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "waiting for archive export", time.Now(), attemptStartTime)

	// Use shorter polling intervals for testing, longer for production
	pollInterval := 15 * time.Second
	if m.githubAPI != nil {
		// When using injected GitHub API (testing mode), use much shorter interval
		pollInterval = 100 * time.Millisecond
	}
	exportStartTime := time.Now()
	var archiveURL string
	var migrationID string

	// Poll for archive export completion
	for {
		select {
		case <-ctx.Done():
			m.updateStatus(
				sourceRepoFullName,
				payload.StatusFailed,
				fmt.Sprintf("archive export cancelled: %v", ctx.Err()),
				time.Now(),
				attemptStartTime,
			)
			return "", archiveID, ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		// Check archive export status
		status, err := githubAPI.GetMigrationArchiveStatus(ctx, archiveID, req.SourceOrg)
		if err != nil {
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, err.Error(), time.Now(), attemptStartTime)
			return "", archiveID, fmt.Errorf("failed to get archive export status: %w", err)
		}

		elapsedExport := time.Since(exportStartTime)
		m.logger.Debug("Archive export status",
			"status", status,
			"repository", sourceRepoName,
			"elapsed", elapsedExport.String(),
		)

		// Update status message with current state and wait time
		m.updateStatus(
			sourceRepoFullName,
			payload.StatusInProgress,
			fmt.Sprintf("waiting for archive export (status: %s, elapsed: %s)", status, elapsedExport.Round(time.Second)),
			time.Now(),
			attemptStartTime,
		)

		// Check status and take appropriate action
		switch status {
		case "exported":
			// Get archive URL
			archiveURL, err = githubAPI.GetMigrationArchiveURL(ctx, archiveID, req.SourceOrg)
			if err != nil {
				m.updateStatus(sourceRepoFullName, payload.StatusFailed, err.Error(), time.Now(), attemptStartTime)
				return "", archiveID, fmt.Errorf("failed to get archive URL: %w", err)
			}
			m.logger.Debug("Archive URL retrieved", "repository", sourceRepoName)

			// Start the migration using the appropriate method
			migrationID, err = m.startMigration(ctx, githubAPI, req, sourceRepoName, archiveURL, attemptStartTime)
			if err != nil {
				return "", archiveID, err
			}
			return migrationID, archiveID, nil

		case "failed":
			failureMsg := fmt.Sprintf("migration archive export failed with state: %s", status)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, failureMsg, time.Now(), attemptStartTime)
			return "", archiveID, fmt.Errorf("%s", failureMsg)

		case "pending", "exporting":
			// Continue polling - no additional logging needed as we already logged status above
			continue

		default:
			m.logger.Warn("Unknown archive export status",
				"status", status,
				"repository", sourceRepoName,
				"archiveID", archiveID,
			)
			continue
		}
	}
}

// startMigration starts the migration process using either the GHOS upload or direct repository migration.
// Returns the migration ID for monitoring.
func (m *Migrator) startMigration(
	ctx context.Context,
	githubAPI github.API,
	req *payload.MigrationRequest,
	sourceRepoName string,
	archiveURL string,
	attemptStartTime time.Time,
) (string, error) {
	sourceRepoFullName := fmt.Sprintf("%s/%s", req.SourceOrg, sourceRepoName)

	// Log migration details at the start
	m.logger.Info("Starting repository migration with configuration details",
		"repository_full_name", sourceRepoFullName,
		"delete_if_exists", req.DeleteIfExists,
		"use_ghos", req.UseGHOS)

	// Double-check that repository doesn't exist in target to avoid race conditions
	exists, err := githubAPI.CheckCloudRepositoryExists(ctx, req.TargetOrg, sourceRepoName)
	if err != nil {
		// Real error (not a 404)
		errorMsg := fmt.Sprintf("failed to check target repository: %v", err)
		m.logger.Error("Target repository check failed with error during migration start",
			"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
			"error", err)
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
		return "", err
	}

	if exists {
		// Repository exists in target organization
		m.logger.Info("Repository exists check result during migration start",
			"repository", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
			"exists", true,
			"delete_if_exists_flag", req.DeleteIfExists) // Explicitly log the flag value

		if req.DeleteIfExists {
			// If DeleteIfExists flag is set, try to delete the repository
			m.updateStatus(sourceRepoFullName, payload.StatusInProgress,
				fmt.Sprintf("repository exists in target organization (race condition detected), attempting to delete: %s/%s",
					req.TargetOrg, sourceRepoName),
				time.Now(), attemptStartTime)

			m.logger.Info("Repository exists in target organization (race condition detected), attempting to delete",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
				"delete_if_exists", req.DeleteIfExists)

			deleted, err := githubAPI.DeleteCloudRepositoryIfExists(ctx, req.TargetOrg, sourceRepoName)
			if err != nil {
				// Failed to delete repository
				errorMsg := fmt.Sprintf("Failed to delete existing repository in target organization: %v", err)
				m.logger.Error("Failed to delete existing repository during migration start",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
					"error", err)
				m.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
				return "", fmt.Errorf("failed to delete existing repository: %w", err)
			}

			if deleted {
				m.logger.Info("Successfully deleted existing repository in target organization during migration start",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName))
				m.updateStatus(sourceRepoFullName, payload.StatusInProgress,
					fmt.Sprintf("successfully deleted existing repository: %s/%s", req.TargetOrg, sourceRepoName),
					time.Now(), attemptStartTime)
			}
		} else {
			// DeleteIfExists flag is not set, fail with conflict error
			conflictMsg := fmt.Sprintf("Repository %s/%s already exists in target organization", req.TargetOrg, sourceRepoName)
			m.logger.Error("Repository already exists in target organization (detected during start migration)",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName))
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
			return "", fmt.Errorf("repository conflict: %s", conflictMsg)
		}
	} else {
		// Repository doesn't exist in target, which is good
		m.logger.Info("Repository doesn't exist in target organization, proceeding with migration",
			"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName))
	}

	// Get the necessary IDs
	ownerID, databaseID, err := githubAPI.GetOrganizationID(ctx, req.TargetOrg)
	if err != nil {
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, fmt.Sprintf("failed to get owner ID: %v", err), time.Now(), attemptStartTime)
		return "", fmt.Errorf("failed to get owner ID: %w", err)
	}

	// Get migration source ID
	var migrationSourceID string
	// This would ideally come from stored information, but for now we'll recreate it
	baseURL := strings.TrimSuffix(req.GHESBaseURL, "/")
	migrationSourceID, err = githubAPI.CreateMigrationSource(ctx, sourceRepoName, baseURL, ownerID)
	if err != nil {
		// Check for repository conflict errors in the migrationSource creation
		var classifiedErr *apierrors.ClassifiedError
		if errors.As(err, &classifiedErr) && classifiedErr.Category == apierrors.CategoryResourceConflict {
			conflictMsg := fmt.Sprintf("Repository %s/%s already exists in target organization", req.TargetOrg, sourceRepoName)
			m.logger.Error("Repository conflict detected during migration source creation",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
				"error", classifiedErr)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
			return "", fmt.Errorf("repository conflict: %s", conflictMsg)
		}
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, fmt.Sprintf("failed to create migration source: %v", err), time.Now(), attemptStartTime)
		return "", fmt.Errorf("failed to create migration source: %w", err)
	}

	var migrationID string
	var geiURI string

	// Check if we're migrating using GHOS
	if req.UseGHOS {
		// Upload archive to GHOS
		m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "uploading archive to GHOS", time.Now(), attemptStartTime)
		geiURI, err = githubAPI.UploadArchiveToGHOS(ctx, databaseID, archiveURL, sourceRepoName, req.GHCloudToken)
		if err != nil {
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, fmt.Sprintf("failed to upload archive to GHOS: %v", err), time.Now(), attemptStartTime)
			return "", fmt.Errorf("failed to upload archive to GHOS: %w", err)
		}

		// Now that we have the GEI URI, start the actual migration
		m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "starting repository migration with GHOS archive", time.Now(), attemptStartTime)

		// Construct the source repository URL from the base URL
		sourceRepoURL := fmt.Sprintf("%s/%s/%s", baseURL, req.SourceOrg, sourceRepoName)

		// Start the migration using the GEI URI as both the archive URL and metadata URL
		migrationID, err = githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, sourceRepoName, sourceRepoURL, geiURI, geiURI, req.GHESToken, req.GHCloudToken)
		if err != nil {
			// Check specifically for repository conflict errors
			var classifiedErr *apierrors.ClassifiedError
			if errors.As(err, &classifiedErr) && classifiedErr.Category == apierrors.CategoryResourceConflict {
				// Format a more specific error message for repository conflicts
				conflictMsg := fmt.Sprintf("Repository %s/%s already exists in target organization", req.TargetOrg, sourceRepoName)
				m.logger.Error("Repository conflict detected during migration start with GHOS",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
					"error", classifiedErr)
				m.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
				return "", fmt.Errorf("repository conflict: %s", conflictMsg)
			}

			errMsg := fmt.Sprintf("failed to start repository migration with GHOS archive: %v", err)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
			return "", fmt.Errorf("failed to start repository migration with GHOS archive: %w", err)
		}
	} else {
		// Regular non-GHOS flow
		// Construct the source repository URL from the base URL
		sourceRepoURL := fmt.Sprintf("%s/%s/%s", baseURL, req.SourceOrg, sourceRepoName)

		m.logger.Debug("Starting repository migration",
			"sourceURL", sourceRepoURL,
			"repository", sourceRepoName,
		)

		// Start repository migration
		m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "starting repository migration", time.Now(), attemptStartTime)
		migrationID, err = githubAPI.StartRepositoryMigration(ctx, migrationSourceID, ownerID, sourceRepoName, sourceRepoURL, archiveURL, archiveURL, req.GHESToken, req.GHCloudToken)
		if err != nil {
			// Check specifically for repository conflict errors
			var classifiedErr *apierrors.ClassifiedError
			if errors.As(err, &classifiedErr) && classifiedErr.Category == apierrors.CategoryResourceConflict {
				// Format a more specific error message for repository conflicts
				conflictMsg := fmt.Sprintf("Repository %s/%s already exists in target organization", req.TargetOrg, sourceRepoName)
				m.logger.Error("Repository conflict detected during direct migration start",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, sourceRepoName),
					"error", classifiedErr)
				m.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
				return "", fmt.Errorf("repository conflict: %s", conflictMsg)
			}

			errMsg := fmt.Sprintf("failed to start repository migration: %v", err)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
			return "", fmt.Errorf("failed to start repository migration: %w", err)
		}
	}

	// Update status with migration ID
	m.updateStatus(
		sourceRepoFullName,
		payload.StatusInProgress,
		fmt.Sprintf("migration created with ID: %s", migrationID),
		time.Now(),
		attemptStartTime,
	)

	// Store migration ID in the status object
	m.mu.Lock()
	if status, exists := m.migrations[sourceRepoFullName]; exists {
		status.MigrationID = migrationID
	}
	m.mu.Unlock()

	return migrationID, nil
}

// monitorMigration polls the GitHub API to monitor migration progress.
// It updates the status as the migration progresses and implements adaptive polling.
func (m *Migrator) monitorMigration(
	ctx context.Context,
	githubAPI github.API,
	migrationID string,
	sourceRepoName string,
	sourceRepoFullName string,
	attemptStartTime time.Time,
) error {
	// Monitor the migration progress
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "waiting for migration to complete", time.Now(), attemptStartTime)

	// Use shorter polling intervals for testing, longer for production
	pollInterval := 15 * time.Second
	if m.githubAPI != nil {
		// When using injected GitHub API (testing mode), use much shorter interval
		pollInterval = 100 * time.Millisecond
	}
	migrationStartTime := time.Now()

	// Initialize adaptive polling
	consecutiveNoChanges := 0
	lastState := ""

	for {
		select {
		case <-ctx.Done():
			m.updateStatus(
				sourceRepoFullName,
				payload.StatusFailed,
				fmt.Sprintf("migration cancelled: %v", ctx.Err()),
				time.Now(),
				attemptStartTime,
			)
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		state, err := githubAPI.GetMigrationStatus(ctx, migrationID)
		if err != nil {
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, err.Error(), time.Now(), attemptStartTime)
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		// Update status with current state
		elapsedMigration := time.Since(migrationStartTime)
		m.updateStatus(
			sourceRepoFullName,
			payload.StatusInProgress,
			fmt.Sprintf("migration in progress (state: %s, elapsed: %s)", state, elapsedMigration.Round(time.Second)),
			time.Now(),
			attemptStartTime,
		)

		m.logger.Debug("Migration status",
			"state", state,
			"repository", sourceRepoName,
			"elapsed", elapsedMigration.String(),
		)

		// Handle the different states
		// GetMigrationStatus returns uppercase state values
		switch state {
		case "SUCCEEDED":
			m.updateStatus(sourceRepoFullName, payload.StatusSucceeded, "migration completed successfully", time.Now(), attemptStartTime)
			return nil
		case "FAILED", "FAILED_VALIDATION":
			failureMsg := fmt.Sprintf("migration failed with state: %s", state)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, failureMsg, time.Now(), attemptStartTime)
			return fmt.Errorf("%s", failureMsg)
		case "PENDING", "IN_PROGRESS", "QUEUED", "PENDING_VALIDATION":
			// Adaptive polling - if state hasn't changed, gradually back off
			if state == lastState {
				consecutiveNoChanges++
				// After 3 consecutive same status, increase poll interval (up to 2 minutes max)
				if consecutiveNoChanges > 3 && pollInterval < 2*time.Minute {
					pollInterval = pollInterval * 5 / 4 // Increase by 25%
					m.logger.Debug("Increasing poll interval",
						"repository", sourceRepoName,
						"new_interval", pollInterval.String(),
					)
				}
			} else {
				// State changed, reset counter and poll interval
				consecutiveNoChanges = 0
				// Reset to initial interval (preserve test mode interval)
				if m.githubAPI != nil {
					// Testing mode - use short interval
					pollInterval = 100 * time.Millisecond
				} else {
					// Production mode - use normal interval
					pollInterval = 15 * time.Second
				}
				lastState = state
			}
			continue
		default:
			m.logger.Warn("Unknown migration state",
				"state", state,
				"repository", sourceRepoName,
				"migrationID", migrationID,
			)
			continue
		}
	}
}
