// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"context"
	"fmt"
	"log/slog"
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
		"priority", priority)

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
			SourceOrg:    req.SourceOrg,
			TargetOrg:    req.TargetOrg,
			GHESToken:    req.GHESToken,
			GHCloudToken: req.GHCloudToken,
			GHESBaseURL:  req.GHESBaseURL,
			UseGHOS:      req.UseGHOS,
			Repositories: []string{repoName},
		}

		// Enqueue the job for archive generation (first phase)
		err := qmi.queueManager.EnqueueArchiveJob(sourceRepoFullName, jobReq, priority)
		if err != nil {
			qmi.logger.Error("Failed to enqueue archive job",
				"repository", sourceRepoFullName,
				"error", err)
			continue
		}
	}

	return nil
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
