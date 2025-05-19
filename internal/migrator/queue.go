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
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare a migration status for the job
	sourceRepoFullName := job.Repository
	attemptStartTime := time.Now()

	// Use a smaller critical section for updating status
	func() {
		qmi.mu.Lock()
		defer qmi.mu.Unlock()
		qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusInProgress, "Starting archive generation", attemptStartTime, attemptStartTime)
	}()

	// The code that would normally live in StartMigration/performMigration for archive generation
	// goes here...
	// (This is simplified - in a real implementation, you'd need to extract the archive
	// generation logic from the migrator's code and call it here)

	// For now, let's implement a placeholder that moves to the next phase
	qmi.logger.Info("Archive generation completed (simulated)", "repository", sourceRepoFullName)

	// Enqueue the migration job (second phase)
	err := qmi.queueManager.EnqueueMigrationJob(sourceRepoFullName, req, job.Priority)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to enqueue migration job: %v", err)
		qmi.logger.Error(errMsg, "repository", sourceRepoFullName)

		// Use a smaller critical section for updating status
		func() {
			qmi.mu.Lock()
			defer qmi.mu.Unlock()
			qmi.migrator.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
		}()
		return err
	}

	return nil
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
