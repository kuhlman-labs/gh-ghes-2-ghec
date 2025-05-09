package migrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/go-github/v70/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// Migrator handles repository migrations
type Migrator struct {
	clients    *config.Clients
	webhookURL string
	logger     *slog.Logger
	mu         sync.RWMutex
	migrations map[string]*payload.MigrationStatus
}

// New creates a new migrator instance
func New(webhookURL string) *Migrator {
	return &Migrator{
		clients:    config.GetClients(),
		webhookURL: webhookURL,
		logger:     logging.Get(),
		migrations: make(map[string]*payload.MigrationStatus),
	}
}

// StartMigration starts the migration process for the given request
func (m *Migrator) StartMigration(ctx context.Context, req *payload.MigrationRequest) error {
	// Update GHES client base URL
	if err := m.clients.UpdateGHESBaseURL(req.GHESAPIURL); err != nil {
		return fmt.Errorf("invalid GHES API URL: %w", err)
	}

	// Start migrations for each repository
	var wg sync.WaitGroup
	for _, repo := range req.Repositories {
		wg.Add(1)
		go func(repoName string) {
			defer wg.Done()
			if err := m.migrateRepository(ctx, req, repoName); err != nil {
				m.logger.Error("Failed to migrate repository",
					"repository", repoName,
					"error", err,
				)
			}
		}(repo)
	}

	wg.Wait()
	return nil
}

func (m *Migrator) migrateRepository(ctx context.Context, req *payload.MigrationRequest, repoName string) error {
	// Update status to in progress
	m.updateStatus(repoName, payload.StatusInProgress, "")

	// Create migration options
	opts := &github.MigrationOptions{
		LockRepositories:   true,
		ExcludeAttachments: false,
	}

	// Create migration
	migration, _, err := m.clients.GHCloudClient.Migrations.StartMigration(ctx, req.TargetOrg, []string{fmt.Sprintf("%s/%s", req.SourceOrg, repoName)}, opts)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, err.Error())
		return fmt.Errorf("failed to start migration: %w", err)
	}

	// Wait for migration to complete
	for {
		status, _, err := m.clients.GHCloudClient.Migrations.MigrationStatus(ctx, req.TargetOrg, migration.GetID())
		if err != nil {
			m.updateStatus(repoName, payload.StatusFailed, err.Error())
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		switch status.GetState() {
		case "success":
			m.updateStatus(repoName, payload.StatusSucceeded, "")
			return nil
		case "failed":
			// Get failure message from status
			failureMsg := "migration failed"
			if status.GetState() == "failed" {
				failureMsg = fmt.Sprintf("migration failed with state: %s", status.GetState())
			}
			m.updateStatus(repoName, payload.StatusFailed, failureMsg)
			return fmt.Errorf("migration failed: %s", failureMsg)
		}

		select {
		case <-ctx.Done():
			m.updateStatus(repoName, payload.StatusFailed, "migration cancelled")
			return ctx.Err()
		default:
			// Continue polling
		}
	}
}

func (m *Migrator) updateStatus(repoName, status, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.migrations[repoName] = &payload.MigrationStatus{
		Repository: repoName,
		Status:     status,
		Error:      errMsg,
	}

	// Send webhook notification
	if m.webhookURL != "" {
		go m.sendWebhookNotification(repoName)
	}
}

func (m *Migrator) sendWebhookNotification(repoName string) {
	m.mu.RLock()
	status := m.migrations[repoName]
	m.mu.RUnlock()

	if status == nil {
		return
	}

	payload, err := json.Marshal(status)
	if err != nil {
		m.logger.Error("Failed to marshal webhook payload",
			"repository", repoName,
			"error", err,
		)
		return
	}

	resp, err := http.Post(m.webhookURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		m.logger.Error("Failed to send webhook notification",
			"repository", repoName,
			"error", err,
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		m.logger.Error("Webhook notification failed",
			"repository", repoName,
			"status_code", resp.StatusCode,
		)
	}
}
