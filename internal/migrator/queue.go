// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/queue"
)

// QueueManagerIntegration handles the integration between the Migrator and the QueueManager
type QueueManagerIntegration struct {
	migrator     *Migrator
	queueManager *queue.QueueManager
	logger       *slog.Logger
	mu           sync.Mutex
}

// NewQueueManagerIntegration creates a new QueueManagerIntegration instance
func NewQueueManagerIntegration(migrator *Migrator, logger *slog.Logger, cfg *config.Config) *QueueManagerIntegration {
	if logger == nil {
		logger = slog.Default()
	}

	qmi := &QueueManagerIntegration{
		migrator: migrator,
		logger:   logger,
	}

	// Create and configure the queue manager
	qm := queue.NewQueueManager(
		logger,
		cfg.Queue.MaxQueueSize,
		cfg.Queue.MaxArchiveThreads,
		cfg.Queue.MaxMigrationThreads,
		qmi.handleArchiveJob,
		qmi.handleMigrationJob,
	)

	qmi.queueManager = qm
	return qmi
}

// Start starts the queue manager
func (qmi *QueueManagerIntegration) Start() {
	qmi.queueManager.Start()
	qmi.logger.Info("Queue manager integration started")
}

// Stop stops the queue manager
func (qmi *QueueManagerIntegration) Stop() {
	qmi.queueManager.Stop()
	qmi.logger.Info("Queue manager integration stopped")
}

// EnqueueMigration adds a migration request to the queue
func (qmi *QueueManagerIntegration) EnqueueMigration(
	ctx context.Context,
	req *payload.MigrationRequest,
	priority int,
) error {
	qmi.mu.Lock()
	defer qmi.mu.Unlock()

	qmi.logger.Info("Enqueueing migration request",
		"source_org", req.SourceOrg,
		"target_org", req.TargetOrg,
		"repos_count", len(req.Repositories),
		"priority", priority,
		"delete_if_exists", req.DeleteIfExists)

	// Initialize API clients for validation
	clients, err := config.NewClients(&config.ClientsConfig{
		GHESToken:    req.GHESToken,
		GHCloudToken: req.GHCloudToken,
		Proxy:        qmi.migrator.config.GitHub.Proxy,
	})
	if err != nil {
		qmi.logger.Error("Failed to initialize clients for pre-enqueue validation",
			"error", err)
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	// Update GHES base URL
	if err := clients.UpdateGHESBaseURL(req.GetGHESAPIURL()); err != nil {
		qmi.logger.Error("Failed to update GHES base URL for pre-enqueue validation",
			"error", err)
		return fmt.Errorf("failed to update GHES base URL: %w", err)
	}

	// Create GitHub API instance for validation
	githubAPI := github.New(clients, qmi.logger)

	// Process each repository as a separate job
	for _, repoName := range req.Repositories {
		// Skip empty repository names
		if repoName == "" {
			qmi.logger.Warn("Empty repository name in request, skipping.")
			continue
		}

		// Create the full repository name
		sourceRepoFullName := req.SourceOrg + "/" + repoName

		// Create a job-specific request (deep copy but with a single repository)
		jobReq := &payload.MigrationRequest{
			SourceOrg:      req.SourceOrg,
			TargetOrg:      req.TargetOrg,
			GHESToken:      req.GHESToken,
			GHCloudToken:   req.GHCloudToken,
			GHESBaseURL:    req.GHESBaseURL,
			UseGHOS:        req.UseGHOS,
			DeleteIfExists: req.DeleteIfExists,
			Repositories:   []string{repoName},
		}

		// Validate repository before enqueueing
		valid, err := qmi.validateRepositoryForQueue(ctx, githubAPI, jobReq, sourceRepoFullName)
		if err != nil {
			qmi.logger.Error("Pre-enqueue validation failed",
				"repository", sourceRepoFullName,
				"error", err)

			// Create a status entry for the failed validation
			attemptStartTime := time.Now()
			qmi.migrator.updateStatus(
				sourceRepoFullName,
				payload.StatusFailed,
				fmt.Sprintf("Pre-enqueue validation failed: %v", err),
				time.Now(),
				attemptStartTime)

			continue
		}

		if !valid {
			qmi.logger.Info("Repository skipped due to pre-enqueue validation",
				"repository", sourceRepoFullName)
			continue
		}

		// Enqueue the job for archive generation (first phase)
		err = qmi.queueManager.EnqueueArchiveJob(sourceRepoFullName, jobReq, priority)
		if err != nil {
			qmi.logger.Error("Failed to enqueue archive job",
				"repository", sourceRepoFullName,
				"error", err)
			continue
		}
	}

	return nil
}

// validateRepositoryForQueue validates a repository before enqueueing it
// Returns: valid (bool), error
func (qmi *QueueManagerIntegration) validateRepositoryForQueue(
	ctx context.Context,
	githubAPI github.API,
	req *payload.MigrationRequest,
	sourceRepoFullName string,
) (bool, error) {
	// Extract repo name from full name
	parts := strings.Split(sourceRepoFullName, "/")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid repository name format: %s", sourceRepoFullName)
	}
	repoName := parts[1]

	// Create a temporary status entry in the migrations map
	attemptStartTime := time.Now()
	qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress, "pre-enqueue validation", time.Now(), attemptStartTime)

	// 1. Validate that source repository exists
	qmi.logger.Info("Pre-enqueue validation: checking source repository",
		"repo", sourceRepoFullName,
		"delete_if_exists", req.DeleteIfExists)

	err := githubAPI.ValidateRepository(ctx, req.SourceOrg, repoName)
	if err != nil {
		// Source repository must exist
		errorMsg := fmt.Sprintf("source repository not found: %v", err)
		qmi.logger.Error("Pre-enqueue validation: source repository validation failed",
			"repo", sourceRepoFullName,
			"error", err)
		qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
		return false, fmt.Errorf("source repository not found: %w", err)
	}

	qmi.logger.Info("Pre-enqueue validation: source repository validated successfully",
		"repo", sourceRepoFullName)

	// 2. Check if repository exists in the target organization
	exists, err := githubAPI.CheckCloudRepositoryExists(ctx, req.TargetOrg, repoName)
	if err != nil {
		// This is an actual error (not a 404)
		errorMsg := fmt.Sprintf("failed to check target repository: %v", err)
		qmi.logger.Error("Pre-enqueue validation: target repository check failed with error",
			"repo", fmt.Sprintf("%s/%s", req.TargetOrg, repoName),
			"error", err)
		qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
		return false, err
	}

	if exists {
		// Repository exists in target organization
		qmi.logger.Info("Pre-enqueue validation: repository exists in target organization",
			"repo", fmt.Sprintf("%s/%s", req.TargetOrg, repoName),
			"delete_if_exists", req.DeleteIfExists)

		if req.DeleteIfExists {
			// If DeleteIfExists flag is set, try to delete the repository
			qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress,
				fmt.Sprintf("pre-enqueue validation: repository exists in target organization, attempting to delete: %s/%s",
					req.TargetOrg, repoName),
				time.Now(), attemptStartTime)

			qmi.logger.Info("Pre-enqueue validation: attempting to delete existing repository",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, repoName),
				"delete_if_exists", req.DeleteIfExists)

			deleted, err := githubAPI.DeleteCloudRepositoryIfExists(ctx, req.TargetOrg, repoName)
			if err != nil {
				// Failed to delete repository
				errorMsg := fmt.Sprintf("Failed to delete existing repository in target organization: %v", err)
				qmi.logger.Error("Pre-enqueue validation: failed to delete existing repository",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, repoName),
					"error", err)
				qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
				return false, fmt.Errorf("failed to delete existing repository: %w", err)
			}

			if deleted {
				qmi.logger.Info("Pre-enqueue validation: successfully deleted existing repository",
					"repo", fmt.Sprintf("%s/%s", req.TargetOrg, repoName))
				qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress,
					fmt.Sprintf("pre-enqueue validation: successfully deleted existing repository: %s/%s",
						req.TargetOrg, repoName),
					time.Now(), attemptStartTime)
			}
		} else {
			// DeleteIfExists flag is not set, fail with conflict error
			conflictMsg := fmt.Sprintf("Repository %s/%s already exists in target organization", req.TargetOrg, repoName)
			qmi.logger.Error("Pre-enqueue validation: repository already exists in target organization",
				"repo", fmt.Sprintf("%s/%s", req.TargetOrg, repoName))
			qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
			return false, fmt.Errorf("repository conflict: %s", conflictMsg)
		}
	} else {
		// Repository doesn't exist in target, which is good
		qmi.logger.Info("Pre-enqueue validation: target repository does not exist",
			"repo", fmt.Sprintf("%s/%s", req.TargetOrg, repoName))
	}

	// Repository passed validation
	qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress,
		"pre-enqueue validation successful, queuing for archive generation",
		time.Now(), attemptStartTime)
	return true, nil
}

// handleArchiveJob processes an archive generation job
func (qmi *QueueManagerIntegration) handleArchiveJob(job *queue.MigrationJob) error {
	// Remove lock around entire function to allow concurrent processing
	qmi.logger.Info("Processing archive job", "repository", job.Repository)

	req, ok := job.Data.(*payload.MigrationRequest)
	if !ok || req == nil {
		return fmt.Errorf("invalid job data: expected *payload.MigrationRequest")
	}

	// Create a background context for the migration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare a migration status for the job
	sourceRepoFullName := job.Repository
	attemptStartTime := time.Now()

	// Initialize clients for this migration
	clients, err := config.NewClients(&config.ClientsConfig{
		GHESToken:    req.GHESToken,
		GHCloudToken: req.GHCloudToken,
		Proxy:        qmi.migrator.config.GitHub.Proxy,
	})
	if err != nil {
		errMsg := fmt.Sprintf("failed to initialize clients: %v", err)
		qmi.logger.Error(errMsg, "repository", sourceRepoFullName)
		qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	// Update GHES base URL
	if err := clients.UpdateGHESBaseURL(req.GetGHESAPIURL()); err != nil {
		errMsg := fmt.Sprintf("failed to update GHES base URL: %v", err)
		qmi.logger.Error(errMsg, "repository", sourceRepoFullName)
		qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
		return fmt.Errorf("failed to update GHES base URL: %w", err)
	}

	// Create GitHub API instance for this migration
	githubAPI := github.New(clients, qmi.logger)

	// Update status to in progress with initial stage
	qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress, "starting archive generation", time.Now(), attemptStartTime)

	// Generate migration archive on Source GHES
	qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress, "generating migration archive", time.Now(), attemptStartTime)
	archiveID, err := githubAPI.GenerateMigrationArchive(ctx, req.SourceOrg, req.Repositories[0])
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate migration archive: %v", err)
		qmi.logger.Error(errMsg, "repository", sourceRepoFullName)
		qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
		return fmt.Errorf("failed to generate migration archive: %w", err)
	}
	qmi.logger.Debug("Archive generation initiated", "archiveID", archiveID, "repository", sourceRepoFullName)

	// Wait for migration archive export to complete
	qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress, "waiting for archive export", time.Now(), attemptStartTime)

	// Use longer polling intervals for archive export status checks
	pollInterval := 15 * time.Second
	exportStartTime := time.Now()

	// Poll for archive export completion
	for {
		select {
		case <-ctx.Done():
			errMsg := fmt.Sprintf("archive export cancelled: %v", ctx.Err())
			qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		// Check archive export status
		status, err := githubAPI.GetMigrationArchiveStatus(ctx, archiveID, req.SourceOrg)
		if err != nil {
			errMsg := fmt.Sprintf("failed to get archive export status: %v", err)
			qmi.logger.Error(errMsg, "repository", sourceRepoFullName)
			qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
			return fmt.Errorf("failed to get archive export status: %w", err)
		}

		elapsedExport := time.Since(exportStartTime)
		qmi.logger.Debug("Archive export status",
			"status", status,
			"repository", sourceRepoFullName,
			"elapsed", elapsedExport.String(),
		)

		// Update status message with current state and wait time
		qmi.migrator.updateStatus(
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
			archiveURL, err := githubAPI.GetMigrationArchiveURL(ctx, archiveID, req.SourceOrg)
			if err != nil {
				errMsg := fmt.Sprintf("failed to get archive URL: %v", err)
				qmi.logger.Error(errMsg, "repository", sourceRepoFullName)
				qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
				return fmt.Errorf("failed to get archive URL: %w", err)
			}
			qmi.logger.Debug("Archive URL retrieved", "repository", sourceRepoFullName)

			// Store the archive URL in the request data for the migration phase
			req.ArchiveURL = archiveURL

			// Enqueue the migration job (second phase)
			err = qmi.queueManager.EnqueueMigrationJob(sourceRepoFullName, req, job.Priority)
			if err != nil {
				errMsg := fmt.Sprintf("Failed to enqueue migration job: %v", err)
				qmi.logger.Error(errMsg, "repository", sourceRepoFullName)
				qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
				return err
			}

			return nil

		case "failed":
			failureMsg := fmt.Sprintf("migration archive export failed with state: %s", status)
			qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, failureMsg, time.Now(), attemptStartTime)
			return fmt.Errorf("%s", failureMsg)

		case "pending", "exporting":
			// Continue polling - no additional logging needed as we already logged status above
			continue

		default:
			qmi.logger.Warn("Unknown archive export status",
				"status", status,
				"repository", sourceRepoFullName,
				"archiveID", archiveID,
			)
			continue
		}
	}
}

// handleMigrationJob processes a migration job
func (qmi *QueueManagerIntegration) handleMigrationJob(job *queue.MigrationJob) error {
	// Remove lock around entire function to allow concurrent processing
	qmi.logger.Info("Processing migration job", "repository", job.Repository)

	req, ok := job.Data.(*payload.MigrationRequest)
	if !ok || req == nil {
		return fmt.Errorf("invalid job data: expected *payload.MigrationRequest")
	}

	// Create a background context for the migration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare migration data
	sourceRepoFullName := job.Repository
	attemptStartTime := time.Now()

	// Use a smaller critical section for updating status
	func() {
		qmi.mu.Lock()
		defer qmi.mu.Unlock()
		qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress, "Starting migration import", time.Now(), attemptStartTime)
	}()

	// Call the migrator's migration code directly
	err := qmi.migrator.performMigration(ctx, req, sourceRepoFullName, attemptStartTime, cancel)
	if err != nil {
		// Error handling is already done in performMigration
		return err
	}

	qmi.logger.Info("Migration job completed successfully", "repository", sourceRepoFullName)
	return nil
}

// GetQueueStats returns statistics about the queue
func (qmi *QueueManagerIntegration) GetQueueStats() map[string]interface{} {
	qmi.mu.Lock()
	defer qmi.mu.Unlock()
	return qmi.queueManager.GetQueueStats()
}

// GetQueuedRepositories returns a slice of repository names currently queued (waiting for a worker)
func (qmi *QueueManagerIntegration) GetQueuedRepositories() []string {
	return qmi.queueManager.GetQueuedRepositories()
}
