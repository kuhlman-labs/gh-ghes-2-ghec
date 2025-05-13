// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
)

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
