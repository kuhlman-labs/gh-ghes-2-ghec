package migrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

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
	errChan := make(chan error, len(req.Repositories))
	var wg sync.WaitGroup

	for _, repo := range req.Repositories {
		wg.Add(1)
		go func(repoName string) {
			defer wg.Done()

			// Create a timeout context for each repository migration
			repoCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			if err := m.migrateRepository(repoCtx, req, repoName); err != nil {
				m.logger.Error("Failed to migrate repository",
					"repository", repoName,
					"error", err,
				)
				errChan <- fmt.Errorf("repository %s: %w", repoName, err)
			}
		}(repo)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errChan)

	// Collect all errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to migrate %d repositories: %v", len(errors), errors)
	}

	return nil
}

func (m *Migrator) migrateRepository(ctx context.Context, req *payload.MigrationRequest, repoName string) error {
	// Update status to in progress with timestamp
	m.updateStatus(repoName, payload.StatusInProgress, "", time.Now())

	// Validate that source repository exists
	sourceRepo := fmt.Sprintf("%s/%s", req.SourceOrg, repoName)
	_, _, err := m.clients.GHESClient.Repositories.Get(ctx, req.SourceOrg, repoName)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, fmt.Sprintf("source repository not found: %v", err), time.Now())
		return fmt.Errorf("source repository not found: %w", err)
	}

	// Create migration options
	opts := &github.MigrationOptions{
		LockRepositories:   true,
		ExcludeAttachments: false,
	}

	// Create migration
	migration, _, err := m.clients.GHCloudClient.Migrations.StartMigration(ctx, req.TargetOrg, []string{sourceRepo}, opts)
	if err != nil {
		m.updateStatus(repoName, payload.StatusFailed, err.Error(), time.Now())
		return fmt.Errorf("failed to start migration: %w", err)
	}

	// Update status with migration ID
	m.updateStatus(
		repoName,
		payload.StatusInProgress,
		fmt.Sprintf("migration ID: %d", migration.GetID()),
		time.Now(),
	)

	// Wait for migration to complete
	pollInterval := 10 * time.Second
	for {
		select {
		case <-ctx.Done():
			m.updateStatus(
				repoName,
				payload.StatusFailed,
				fmt.Sprintf("migration cancelled: %v", ctx.Err()),
				time.Now(),
			)
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}

		status, _, err := m.clients.GHCloudClient.Migrations.MigrationStatus(ctx, req.TargetOrg, migration.GetID())
		if err != nil {
			m.updateStatus(repoName, payload.StatusFailed, err.Error(), time.Now())
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		// Update status with current state
		m.updateStatus(
			repoName,
			payload.StatusInProgress,
			fmt.Sprintf("migration state: %s", status.GetState()),
			time.Now(),
		)

		switch status.GetState() {
		case "success":
			m.updateStatus(repoName, payload.StatusSucceeded, "", time.Now())
			return nil
		case "failed":
			failureMsg := fmt.Sprintf("migration failed with state: %s", status.GetState())
			m.updateStatus(repoName, payload.StatusFailed, failureMsg, time.Now())
			return fmt.Errorf("migration failed: %s", failureMsg)
		case "pending":
			// Continue polling
		}
	}
}

func (m *Migrator) updateStatus(repoName, status, message string, timestamp time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.migrations[repoName] = &payload.MigrationStatus{
		Repository: repoName,
		Status:     status,
		Error:      message,
		UpdatedAt:  timestamp,
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

	webhookPayload, err := json.Marshal(status)
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

	req, err := http.NewRequest(http.MethodPost, m.webhookURL, bytes.NewBuffer(webhookPayload))
	if err != nil {
		m.logger.Error("Failed to create webhook request",
			"repository", repoName,
			"error", err,
		)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gh-ghes-2-ghec-migrator")

	resp, err := client.Do(req)
	if err != nil {
		m.logger.Error("Failed to send webhook notification",
			"repository", repoName,
			"error", err,
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		m.logger.Error("Webhook notification failed",
			"repository", repoName,
			"status_code", resp.StatusCode,
		)
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
