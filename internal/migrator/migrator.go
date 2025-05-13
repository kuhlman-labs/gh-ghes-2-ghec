// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
// It handles the entire migration process, status tracking, and webhook notifications.
package migrator

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
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
