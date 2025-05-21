// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
// It handles the entire migration process, status tracking, and webhook notifications.
package migrator

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/queue"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/tracing"
	"go.opentelemetry.io/otel/trace"
)

// Migrator handles the entire migration process for multiple repositories.
// It manages state, interacts with GitHub APIs, and provides status updates.
type Migrator struct {
	logger        *slog.Logger
	githubAPI     github.API
	storage       storage.MigrationStorage
	migrations    map[string]*payload.MigrationStatus // Keyed by sourceRepoFullName (org/repo)
	mu            sync.RWMutex
	webhookURL    string
	config        *config.Config
	httpClient    *http.Client
	traceProvider trace.TracerProvider
	queueManager  *QueueManagerIntegration // New field for queue manager integration
}

// NewMigrator creates a new Migrator instance.
// It initializes the migrator with necessary dependencies like logger, GitHub API client,
// storage backend, and configuration.
func NewMigrator(
	logger *slog.Logger,
	githubAPI github.API,
	storage storage.MigrationStorage,
	webhookURL string,
	cfg *config.Config,
	httpClient *http.Client,
	tp trace.TracerProvider,
) *Migrator {
	// Use default HTTP client if none provided
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	// Use default logger if none provided
	if logger == nil {
		logger = slog.Default()
	}

	m := &Migrator{
		logger:        logger,
		githubAPI:     githubAPI, // Can be nil, will be created per migration
		storage:       storage,
		migrations:    make(map[string]*payload.MigrationStatus), // Keyed by sourceRepoFullName
		webhookURL:    webhookURL,
		config:        cfg,
		httpClient:    httpClient,
		traceProvider: tp, // Can be nil
	}

	// Load existing migration statuses from storage
	if storage != nil && cfg != nil && cfg.Storage.Enabled {
		if err := m.loadMigrationsFromStorage(); err != nil {
			logger.Error("Failed to load existing migration statuses from storage", "error", err)
			// Depending on policy, might want to halt or continue with an empty in-memory state.
		}
	}

	// Initialize queue manager if enabled in config
	if cfg != nil && cfg.Queue.Enabled {
		m.queueManager = NewQueueManagerIntegration(m, logger, cfg)
		logger.Info("Queue manager integration initialized",
			"max_queue_size", cfg.Queue.MaxQueueSize,
			"max_archive_threads", cfg.Queue.MaxArchiveThreads,
			"max_migration_threads", cfg.Queue.MaxMigrationThreads)
	} else {
		logger.Info("Queue manager is disabled in configuration")
	}

	return m
}

// loadMigrationsFromStorage loads all migration statuses from the persistent storage
// into the in-memory map. This is typically called on migrator startup.
func (m *Migrator) loadMigrationsFromStorage() error {
	m.logger.Info("Loading existing migration statuses from storage...")

	storageTimeoutSeconds := m.config.Storage.Timeout
	if storageTimeoutSeconds == 0 {
		// Use a default timeout if not specified in config, e.g., from a package constant or hardcoded
		// For example, if config has a defaultDbTimeout constant (it does: 120s)
		// This needs to be accessible, e.g. config.DefaultDbTimeoutSeconds or similar
		// For now, let's use a reasonable default like 120 seconds if not defined.
		// Ideally, this default comes from the config package itself if exported.
		// Assuming config.defaultDbTimeout (120) is conceptually what we want:
		storageTimeoutSeconds = 120 // Defaulting to 120s, as seen in config.go defaults
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(storageTimeoutSeconds)*time.Second)
	defer cancel()

	statuses, err := m.storage.GetAllMigrationStatuses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all migration statuses from storage: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for sourceRepoFullName, status := range statuses { // status.Repository is already sourceRepoFullName from storage
		m.migrations[sourceRepoFullName] = status // Key by sourceRepoFullName
		m.logger.Debug("Loaded status from storage", "repository_full_name", sourceRepoFullName, "status", status.Status)
	}
	m.logger.Info("Successfully loaded migration statuses from storage", "count", len(m.migrations))
	return nil
}

// StartMigration initiates the migration process for the repositories specified in the request.
// It handles each repository in a separate goroutine.
// It also implements the retry logic: if a migration for a repo previously failed,
// it archives the failed attempt and starts a new one.
func (m *Migrator) StartMigration(ctx context.Context, req *payload.MigrationRequest, cancelFunc context.CancelFunc) error {
	if req == nil {
		return fmt.Errorf("migration request cannot be nil")
	}
	m.logger.Info("Starting migration process for request",
		"source_org", req.SourceOrg,
		"target_org", req.TargetOrg,
		"repo_count", len(req.Repositories),
	)

	// If queue manager is enabled, use it
	if m.queueManager != nil && m.config.Queue.Enabled {
		m.logger.Info("Using queue manager for migration",
			"source_org", req.SourceOrg,
			"target_org", req.TargetOrg,
			"repo_count", len(req.Repositories))

		// First, ensure the queue manager is started
		if m.queueManager != nil {
			m.queueManager.Start()
		}

		// Enqueue the migration with default priority
		return m.queueManager.EnqueueMigration(ctx, req, m.config.Queue.DefaultPriority)
	}

	// Fall back to the original direct migration if queueing is disabled
	m.logger.Info("Queue manager is disabled, using direct migration approach",
		"source_org", req.SourceOrg,
		"target_org", req.TargetOrg,
		"repo_count", len(req.Repositories))

	var wg sync.WaitGroup
	var multiErr *multierror.Error

	for _, repoName := range req.Repositories {
		if repoName == "" {
			m.logger.Warn("Empty repository name in request, skipping.")
			multiErr = multierror.Append(multiErr, fmt.Errorf("empty repository name provided for source_org %s", req.SourceOrg))
			continue
		}
		// Construct the full repository name (org/repo)
		sourceRepoFullName := req.SourceOrg + "/" + repoName

		wg.Add(1)
		go func(rfn string, currentReq *payload.MigrationRequest, parentCtx context.Context) {
			defer wg.Done()

			repoCtx, repoCancel := context.WithCancel(parentCtx)
			defer repoCancel()

			var attemptStartTime time.Time

			m.mu.Lock()
			existingStatus, exists := m.migrations[rfn]

			if exists {
				switch existingStatus.Status {
				case payload.StatusInProgress:
					m.logger.Warn("Migration already in progress, skipping", "repository_full_name", rfn)
					m.mu.Unlock()
					return // Skip this goroutine

				case payload.StatusFailed:
					m.logger.Info("Found previous failed migration. This will be treated as a retry attempt.", "repository_full_name", rfn)
					attemptStartTime = time.Now() // New start time for this attempt

					// Archive the current failed status
					statusToArchive := deepCopyMigrationStatus(existingStatus)
					archiveCtx, archiveCtxCancel := context.WithTimeout(context.Background(), 30*time.Second)
					if err := m.storage.ArchiveMigrationAttempt(archiveCtx, statusToArchive); err != nil {
						m.logger.Error("Failed to archive previous failed migration status", "repository_full_name", rfn, "error", err)
					} else {
						m.logger.Info("Successfully archived previous failed migration status", "repository_full_name", rfn)
					}
					archiveCtxCancel()

					// Reset the existingStatus object for the new attempt
					existingStatus.Repository = rfn // Ensure it's the full name
					existingStatus.Status = payload.StatusInProgress
					existingStatus.Error = ""
					existingStatus.StartedAt = attemptStartTime // CRITICAL: Reset start time
					existingStatus.UpdatedAt = attemptStartTime
					existingStatus.Duration = 0
					existingStatus.MigrationID = ""
					existingStatus.Progress = 0
					existingStatus.StageProgress = 0
					existingStatus.Stage = "init"
					existingStatus.State = "starting"
					existingStatus.CompletedStages = []string{}
					m.logger.Info("Status for repository reset for retry attempt.", "repository_full_name", rfn, "new_start_time", existingStatus.StartedAt.Format(time.RFC3339))

				case payload.StatusSucceeded:
					m.logger.Info("Migration for repository has already succeeded.", "repository_full_name", rfn)
					m.mu.Unlock()
					return // Skip this goroutine

				default:
					// Handle any other status
					m.logger.Info("Found status in unknown state, treating as new migration", "repository_full_name", rfn, "status", existingStatus.Status)
					attemptStartTime = time.Now()
					initialStatus := &payload.MigrationStatus{
						Repository:     rfn, // Use sourceRepoFullName
						Status:         payload.StatusInProgress,
						StartedAt:      attemptStartTime,
						UpdatedAt:      attemptStartTime,
						Stage:          "init",
						State:          "starting",
						TotalStages:    len(payload.MigrationStages), // Initialize TotalStages
						TargetOrg:      currentReq.TargetOrg,         // Save target org for future use
						GHESBaseURL:    currentReq.GHESBaseURL,       // Save GHES URL for future use
						UseGHOS:        currentReq.UseGHOS,           // Save GHOS setting
						DeleteIfExists: currentReq.DeleteIfExists,    // Save DeleteIfExists setting
					}
					m.migrations[rfn] = initialStatus // Replace with new status
				}
			} else {
				// Brand new migration for this sourceRepoFullName
				attemptStartTime = time.Now()
				initialStatus := &payload.MigrationStatus{
					Repository:     rfn, // Use sourceRepoFullName
					Status:         payload.StatusInProgress,
					StartedAt:      attemptStartTime,
					UpdatedAt:      attemptStartTime,
					Stage:          "init",
					State:          "starting",
					TotalStages:    len(payload.MigrationStages), // Initialize TotalStages
					TargetOrg:      currentReq.TargetOrg,         // Save target org for future use
					GHESBaseURL:    currentReq.GHESBaseURL,       // Save GHES URL for future use
					UseGHOS:        currentReq.UseGHOS,           // Save GHOS setting
					DeleteIfExists: currentReq.DeleteIfExists,    // Save DeleteIfExists setting
				}
				m.migrations[rfn] = initialStatus // Use sourceRepoFullName
				m.logger.Info("Initialized new migration status", "repository_full_name", rfn)
			}
			m.mu.Unlock() // Release lock before starting the long-running task

			// Initial status update to ensure it's logged and persisted, especially for new migrations.
			// For retries, this re-affirms the reset status.
			m.updateStatus(rfn, payload.StatusInProgress, "Starting migration process", time.Now(), attemptStartTime)

			// Perform the actual migration steps
			if err := m.performMigration(repoCtx, currentReq, rfn, attemptStartTime, repoCancel); err != nil {
				m.logger.Error("Migration failed for repository", "repository_full_name", rfn, "error", err)
				// The error is already logged and status updated within performMigration/migrateRepository.
				// We use multierror here if StartMigration itself needs to return an aggregated error.
				// However, since migrations run in goroutines, direct error return from here isn't standard.
				// Status updates and logging are the primary error reporting for individual goroutines.
				m.mu.Lock() // Lock for appending to shared multiErr
				multiErr = multierror.Append(multiErr, fmt.Errorf("migration for %s failed: %w", rfn, err))
				m.mu.Unlock()
			}
		}(sourceRepoFullName, req, ctx) // Pass rfn, currentReq, and parentCtx
	}

	// Wait for all repository migration goroutines to complete.
	// Note: This waits for the goroutines to finish their performMigration call,
	// but StartMigration itself might return before all actual migrations are truly 'done' (succeeded/failed).
	// The cancelFunc passed to StartMigration is the primary way to signal shutdown to these goroutines.
	go func() {
		wg.Wait()
		// Get the correlation ID from the context for tracking
		requestID := logging.GetCorrelationID(ctx)
		m.logger.Info("All migration tasks initiated by this StartMigration call have completed processing.",
			"request_id", requestID,
			"source_org", req.SourceOrg,
			"target_org", req.TargetOrg,
			"repo_count", len(req.Repositories),
		)
		// If cancelFunc is tied to the lifecycle of this specific StartMigration batch and not a global server shutdown,
		// it could be called here, but typically cancelFunc is for broader context cancellation.
		// cancelFunc() // Potentially call cancel if it's for this batch.
	}()

	// Return any immediate errors encountered during setup.
	// Asynchronous errors from goroutines are handled via status updates and logging.
	return multiErr.ErrorOrNil()
}

// updateStatus is implemented in status.go

func (m *Migrator) performMigration(
	ctx context.Context,
	req *payload.MigrationRequest,
	sourceRepoFullName string,
	attemptStartTime time.Time,
	cancelFunc context.CancelFunc,
) error {
	m.logger.Info("Performing migration", "repository_full_name", sourceRepoFullName, "started_at", attemptStartTime.Format(time.RFC3339))
	defer m.logger.Info("Finished performing migration", "repository_full_name", sourceRepoFullName)
	defer cancelFunc() // Ensure cancellation is signaled when this specific migration attempt concludes

	// Parse sourceRepoFullName to orgName and repoName for API calls
	parts := strings.SplitN(sourceRepoFullName, "/", 2)
	if len(parts) != 2 {
		errMsg := fmt.Sprintf("invalid repository full name format: %s", sourceRepoFullName)
		m.logger.Error(errMsg)
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
		return fmt.Errorf("%s", errMsg)
	}
	orgName := parts[0]
	repoName := parts[1]

	// Ensure SourceOrg from request matches the org from sourceRepoFullName
	if req.SourceOrg != orgName {
		errMsg := fmt.Sprintf("mismatch between request SourceOrg ('%s') and repository's organization ('%s')", req.SourceOrg, orgName)
		m.logger.Error(errMsg, "repository_full_name", sourceRepoFullName)
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, errMsg, time.Now(), attemptStartTime)
		return fmt.Errorf("%s", errMsg)
	}

	// Log initial status (already done in StartMigration before goroutine, but can be updated here if needed)
	m.updateStatus(sourceRepoFullName, payload.StatusInProgress, "Migration process initiated", time.Now(), attemptStartTime)

	err := m.migrateRepository(ctx, req, repoName, sourceRepoFullName, attemptStartTime)
	if err != nil {
		// Capture the raw error message - this will preserve the detailed GitHub API error information
		fullErrorMessage := err.Error()

		// Check for specific error types and provide more useful messages but preserve the raw error details
		errLower := strings.ToLower(fullErrorMessage)

		// Repository conflict error
		if strings.Contains(errLower, "already exists") || strings.Contains(errLower, "conflict") {
			conflictMsg := fmt.Sprintf("Repository already exists: %s", fullErrorMessage)
			m.logger.Error("Migration failed due to repository conflict",
				"repository_full_name", sourceRepoFullName,
				"error", err)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, conflictMsg, time.Now(), attemptStartTime)
			return err
		}

		// Authentication error
		if strings.Contains(errLower, "unauthorized") || strings.Contains(errLower, "authentication") ||
			strings.Contains(errLower, "credential") || strings.Contains(errLower, "token") {
			authMsg := fmt.Sprintf("Authentication failed: %s", fullErrorMessage)
			m.logger.Error("Migration failed due to authentication error",
				"repository_full_name", sourceRepoFullName,
				"error", err)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, authMsg, time.Now(), attemptStartTime)
			return err
		}

		// Permission error
		if strings.Contains(errLower, "permission") || strings.Contains(errLower, "forbidden") ||
			strings.Contains(errLower, "access denied") {
			permMsg := fmt.Sprintf("Permission denied: %s", fullErrorMessage)
			m.logger.Error("Migration failed due to permission error",
				"repository_full_name", sourceRepoFullName,
				"error", err)
			m.updateStatus(sourceRepoFullName, payload.StatusFailed, permMsg, time.Now(), attemptStartTime)
			return err
		}

		// For other error types, include the full raw error message to preserve all GitHub API details
		m.logger.Error("Migration attempt failed for repository",
			"repository_full_name", sourceRepoFullName,
			"error", err)

		// Create a more user-friendly error message but include the full raw error
		errorMsg := fmt.Sprintf("Migration failed: %v", fullErrorMessage)
		m.updateStatus(sourceRepoFullName, payload.StatusFailed, errorMsg, time.Now(), attemptStartTime)
		return err // Propagate error for potential cleanup or higher-level error aggregation if any.
	}

	m.logger.Info("Migration attempt completed for repository", "repository_full_name", sourceRepoFullName)
	return nil
}

// GetMigrationStatus retrieves the current status of a specific migration.
// It takes sourceRepoFullName (e.g., "org/repo") as input.
func (m *Migrator) GetMigrationStatus(sourceRepoFullName string) *payload.MigrationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status, exists := m.migrations[sourceRepoFullName]
	if !exists {
		return nil
	}
	// Return a copy to prevent modification of the internal map's value by the caller
	return deepCopyMigrationStatus(status)
}

// GetAllMigrationStatuses retrieves the statuses of all migrations.
// The returned map is keyed by sourceRepoFullName (e.g., "org/repo").
func (m *Migrator) GetAllMigrationStatuses() map[string]*payload.MigrationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a deep copy of the map and its values
	statusesCopy := make(map[string]*payload.MigrationStatus, len(m.migrations))
	for key, status := range m.migrations { // Key is already sourceRepoFullName
		statusesCopy[key] = deepCopyMigrationStatus(status)
	}
	return statusesCopy
}

// Close handles graceful shutdown of the migrator, such as closing storage connections.
func (m *Migrator) Close() error {
	m.logger.Info("Closing migrator...")

	// Stop the queue manager if it's running
	if m.queueManager != nil {
		m.queueManager.Stop()
	}

	if m.storage != nil {
		if err := m.storage.Close(); err != nil {
			return fmt.Errorf("failed to close storage: %w", err)
		}
	}
	m.logger.Info("Migrator closed successfully.")
	return nil
}

// deepCopyMigrationStatus creates a deep copy of a MigrationStatus object.
// This is important when returning status from public methods to prevent callers
// from modifying the internal state, and when archiving.
func deepCopyMigrationStatus(original *payload.MigrationStatus) *payload.MigrationStatus {
	if original == nil {
		return nil
	}
	copied := *original // Shallow copy for most fields

	// Deep copy slice fields
	if original.CompletedStages != nil {
		copied.CompletedStages = make([]string, len(original.CompletedStages))
		copy(copied.CompletedStages, original.CompletedStages)
	}
	// Add any other slice, map, or pointer fields here if payload.MigrationStatus evolves
	return &copied
}

// GetArchivedMigrationAttempts retrieves all historical migration attempts for a specific repository
// directly from the storage layer.
// It takes sourceRepoFullName (e.g., "org/repo") as input.
func (m *Migrator) GetArchivedMigrationAttempts(sourceRepoFullName string) ([]*payload.MigrationStatus, error) {
	m.logger.Debug("Migrator: Attempting to get archived migration attempts from storage", "repository_full_name", sourceRepoFullName)
	if sourceRepoFullName == "" {
		return nil, fmt.Errorf("repository full name cannot be empty")
	}

	if !m.config.Storage.Enabled {
		m.logger.Info("Storage is not enabled, cannot retrieve archived attempts.", "repository_full_name", sourceRepoFullName)
		return nil, nil // Or an error indicating storage is disabled, depending on desired contract
	}

	// Use a timeout from the storage config, or a default.
	storageTimeoutSeconds := m.config.Storage.Timeout
	if storageTimeoutSeconds == 0 {
		storageTimeoutSeconds = 120 // Default seen elsewhere, e.g. loadMigrationsFromStorage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(storageTimeoutSeconds)*time.Second)
	defer cancel()

	archivedAttempts, err := m.storage.GetArchivedMigrationAttempts(ctx, sourceRepoFullName)
	if err != nil {
		m.logger.Error("Migrator: Failed to get archived migration attempts from storage", "repository_full_name", sourceRepoFullName, "error", err)
		return nil, fmt.Errorf("failed to get archived migration attempts for %s from storage: %w", sourceRepoFullName, err)
	}

	m.logger.Debug("Migrator: Successfully retrieved archived migration attempts from storage", "repository_full_name", sourceRepoFullName, "count", len(archivedAttempts))
	return archivedAttempts, nil
}

// RetryMigration initiates a retry for a failed migration.
// It validates that the repository exists, archives the current failed status,
// and starts a new migration attempt. It uses stored configuration values from
// the original migration status, but can be updated with new credentials.
//
// Parameters:
//   - ctx: Context for controlling the retry operation (should be a background context with no timeout)
//   - sourceRepoFullName: Full repository name (org/repo) to retry
//   - ghesToken: Optional GitHub Enterprise Server token (can be empty to use stored values)
//   - ghCloudToken: Optional GitHub Cloud token (can be empty to use stored values)
//   - ghesBaseURL: Optional GitHub Enterprise Server URL (can be empty to use stored values)
//   - targetOrg: Optional target organization (can be empty to use stored values)
//
// Returns:
//   - error: An error if the retry fails or the repository isn't in a failed state
func (m *Migrator) RetryMigration(ctx context.Context, sourceRepoFullName string, ghesToken, ghCloudToken, ghesBaseURL, targetOrg string) error {
	m.logger.Info("Attempting to retry migration", "repository", sourceRepoFullName)

	_, span := tracing.StartSpan(ctx, "retry_migration")
	defer span.End()

	// We need to acquire a write lock since we'll be modifying the migrations map
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Check if the repository exists in our migrations map
	existingStatus, exists := m.migrations[sourceRepoFullName]
	if !exists {
		err := fmt.Errorf("no migration found for repository %s", sourceRepoFullName)
		span.RecordError(err)
		return err
	}

	// 2. Check if the repository is in a failed state
	if existingStatus.Status != payload.StatusFailed {
		err := fmt.Errorf("repository %s is not in a failed state, current status: %s", sourceRepoFullName, existingStatus.Status)
		span.RecordError(err)
		return err
	}

	// 3. Archive the current failed status
	attemptStartTime := time.Now()
	statusToArchive := deepCopyMigrationStatus(existingStatus)

	// Create a context with timeout for archiving
	archiveCtx, archiveCtxCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer archiveCtxCancel()

	if err := m.storage.ArchiveMigrationAttempt(archiveCtx, statusToArchive); err != nil {
		m.logger.Error("Failed to archive previous failed migration status", "repository", sourceRepoFullName, "error", err)
		span.RecordError(err)
		// Continue with the retry even if archiving fails - not a blocking error
	} else {
		m.logger.Info("Successfully archived previous failed migration status", "repository", sourceRepoFullName)
	}

	// 4. Reset the migration status for a new attempt
	existingStatus.Status = payload.StatusInProgress
	existingStatus.Error = ""
	existingStatus.StartedAt = attemptStartTime
	existingStatus.UpdatedAt = attemptStartTime
	existingStatus.Duration = 0
	existingStatus.MigrationID = ""
	existingStatus.Progress = 0
	existingStatus.StageProgress = 0
	existingStatus.Stage = "preparation"
	existingStatus.State = "starting"
	existingStatus.CompletedStages = []string{}

	// Update GHES URL from parameter if provided
	if ghesBaseURL != "" {
		existingStatus.GHESBaseURL = ghesBaseURL
	}

	// Update target org from parameter if provided
	if targetOrg != "" {
		existingStatus.TargetOrg = targetOrg
	}

	// 5. Parse the repository name to get the org and repo
	parts := strings.Split(sourceRepoFullName, "/")
	if len(parts) != 2 {
		err := fmt.Errorf("invalid repository name format: %s", sourceRepoFullName)
		span.RecordError(err)
		return err
	}

	orgName := parts[0]
	repoName := parts[1]

	// 6. Create a request using stored and provided values
	req := &payload.MigrationRequest{
		SourceOrg:      orgName,
		TargetOrg:      existingStatus.TargetOrg,
		GHESToken:      ghesToken,
		GHCloudToken:   ghCloudToken,
		GHESBaseURL:    existingStatus.GHESBaseURL,
		UseGHOS:        existingStatus.UseGHOS,
		Repositories:   []string{repoName},
		DeleteIfExists: existingStatus.DeleteIfExists, // Preserve DeleteIfExists flag from original request
	}

	m.logger.Info("Retry migration configuration",
		"repository", sourceRepoFullName,
		"delete_if_exists", req.DeleteIfExists,
		"use_ghos", req.UseGHOS)

	// 7. If queue manager is enabled, use it for the retry
	if m.queueManager != nil && m.config.Queue.Enabled {
		m.logger.Info("Using queue manager for retry migration", "repository", sourceRepoFullName)

		// Create a truly detached background context with no parent timeouts or deadlines
		migrationCtx := context.Background()

		// Add correlation ID for tracking
		if id := logging.GetCorrelationID(ctx); id != "" {
			migrationCtx = context.WithValue(migrationCtx, logging.KeyCorrelationID, id)
		}

		// Use a higher priority for retries to ensure they're processed before new migrations
		retryPriority := m.config.Queue.DefaultPriority + 10
		if retryPriority > queue.PriorityHigh {
			retryPriority = queue.PriorityHigh
		}

		// Enqueue the retry with higher priority
		return m.queueManager.EnqueueMigration(migrationCtx, req, retryPriority)
	}

	// 7. Start the migration in a new goroutine with a detached context (direct mode)
	go func(rfn string, startTime time.Time) {
		// Create a truly detached background context with no parent timeouts or deadlines
		migrationCtx := context.Background()

		// Create a separate cancel function not tied to any timeout
		migrationCtx, migrationCancel := context.WithCancel(migrationCtx)
		defer migrationCancel()

		// Add correlation ID for tracking
		if id := logging.GetCorrelationID(ctx); id != "" {
			migrationCtx = context.WithValue(migrationCtx, logging.KeyCorrelationID, id)
		}

		// Log the start of the retry operation
		m.logger.Info("Starting retry migration operation in background",
			"repository", rfn,
			"start_time", startTime.Format(time.RFC3339))

		if err := m.performMigration(migrationCtx, req, rfn, startTime, migrationCancel); err != nil {
			// Error handling is already done within performMigration, but we can add additional logging
			// Include the error type in the log to help with troubleshooting
			m.logger.Error("Failed to retry migration",
				"repository", rfn,
				"error", err)
		}
	}(sourceRepoFullName, attemptStartTime)

	m.logger.Info("Migration retry initiated successfully", "repository", sourceRepoFullName)
	return nil
}

// GetQueueStats retrieves statistics about the migration queue
// if queue manager is enabled.
func (m *Migrator) GetQueueStats() map[string]interface{} {
	if m.queueManager == nil || !m.config.Queue.Enabled {
		return map[string]interface{}{
			"queue_enabled": false,
		}
	}

	stats := m.queueManager.GetQueueStats()
	stats["queue_enabled"] = true
	return stats
}

// GetQueuedRepositories returns a slice of repository names currently queued (waiting for a worker)
func (m *Migrator) GetQueuedRepositories() []string {
	if m.queueManager != nil && m.config.Queue.Enabled {
		return m.queueManager.GetQueuedRepositories()
	}
	return []string{}
}
