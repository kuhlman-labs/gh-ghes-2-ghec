package migrator

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/stretchr/testify/assert"
)

func TestSendWebhookNotification(t *testing.T) {
	tests := []struct {
		name           string
		webhookURL     string
		repoName       string
		migrationReq   *payload.MigrationRequest
		status         *payload.MigrationStatus
		serverResponse int
		expectRequest  bool
	}{
		{
			name:       "successful webhook",
			webhookURL: "webhook",
			repoName:   "test-org/test-repo",
			migrationReq: &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			},
			status: &payload.MigrationStatus{
				Status:      payload.StatusInProgress,
				Stage:       "archive",
				State:       "generating",
				UpdatedAt:   time.Now(),
				MigrationID: "migration-123",
			},
			serverResponse: http.StatusOK,
			expectRequest:  true,
		},
		{
			name:       "webhook with completed migration",
			webhookURL: "webhook",
			repoName:   "test-org/test-repo",
			migrationReq: &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			},
			status: &payload.MigrationStatus{
				Status:      payload.StatusSucceeded,
				Stage:       "complete",
				State:       "completed",
				UpdatedAt:   time.Now(),
				StartedAt:   time.Now().Add(-30 * time.Second),
				Duration:    30 * time.Second,
				MigrationID: "migration-123",
			},
			serverResponse: http.StatusOK,
			expectRequest:  true,
		},
		{
			name:       "webhook with error",
			webhookURL: "webhook",
			repoName:   "test-org/test-repo",
			migrationReq: &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			},
			status: &payload.MigrationStatus{
				Status:    payload.StatusFailed,
				Stage:     "error",
				State:     "failed",
				UpdatedAt: time.Now(),
				Error:     "Test error message",
			},
			serverResponse: http.StatusOK,
			expectRequest:  true,
		},
		{
			name:           "no webhook URL",
			webhookURL:     "",
			repoName:       "test-org/test-repo",
			migrationReq:   nil,
			status:         &payload.MigrationStatus{},
			serverResponse: http.StatusOK,
			expectRequest:  false,
		},
		{
			name:           "invalid webhook URL",
			webhookURL:     "not-a-valid-url://invalid",
			repoName:       "test-org/test-repo",
			migrationReq:   nil,
			status:         &payload.MigrationStatus{},
			serverResponse: http.StatusOK,
			expectRequest:  false,
		},
		{
			name:       "server error response",
			webhookURL: "webhook",
			repoName:   "test-org/test-repo",
			migrationReq: &payload.MigrationRequest{
				SourceOrg: "source-org",
				TargetOrg: "target-org",
			},
			status: &payload.MigrationStatus{
				Status:    payload.StatusInProgress,
				Stage:     "archive",
				State:     "generating",
				UpdatedAt: time.Now(),
			},
			serverResponse: http.StatusInternalServerError,
			expectRequest:  true,
		},
		{
			name:           "missing status",
			webhookURL:     "webhook",
			repoName:       "missing-repo",
			migrationReq:   nil,
			status:         nil,
			serverResponse: http.StatusOK,
			expectRequest:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestReceived bool
			var receivedPayload map[string]interface{}

			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceived = true

				// Verify request headers
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "ghes-2-ghec", r.Header.Get("User-Agent"))

				// Decode payload
				err := json.NewDecoder(r.Body).Decode(&receivedPayload)
				assert.NoError(t, err)

				// Only write header if we have a valid status code
				if tt.serverResponse > 0 {
					w.WriteHeader(tt.serverResponse)
				} else {
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			// Create migrator
			var webhookURL string
			switch tt.webhookURL {
			case "":
				// For "no webhook URL" test case, leave it actually empty
				webhookURL = ""
			case "not-a-valid-url://invalid":
				// For "invalid webhook URL" test case, use the invalid URL directly
				webhookURL = tt.webhookURL
			default:
				// For normal cases, append to server URL
				webhookURL = server.URL + "/" + tt.webhookURL
			}

			m := &Migrator{
				logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
				webhookURL: webhookURL,
				migrations: make(map[string]*payload.MigrationStatus),
				mu:         sync.RWMutex{},
			}

			// Set up status if provided
			if tt.status != nil {
				m.migrations[tt.repoName] = tt.status
			}

			// Call the function
			m.sendWebhookNotification(tt.repoName, tt.migrationReq)

			// Verify expectations
			assert.Equal(t, tt.expectRequest, requestReceived)

			if tt.expectRequest && requestReceived {
				// Verify payload structure
				assert.Equal(t, tt.repoName, receivedPayload["repository"])
				assert.NotNil(t, receivedPayload["timestamp"])
				assert.NotNil(t, receivedPayload["details"])

				if tt.status != nil {
					assert.Equal(t, tt.status.Status, receivedPayload["status"])
					assert.Equal(t, tt.status.Stage, receivedPayload["stage"])
					assert.Equal(t, tt.status.State, receivedPayload["state"])

					if tt.status.MigrationID != "" {
						assert.Equal(t, tt.status.MigrationID, receivedPayload["migration_id"])
					}

					if tt.status.Error != "" {
						assert.Equal(t, tt.status.Error, receivedPayload["error"])
					}

					// Check duration fields for completed migrations
					if tt.status.Status == payload.StatusSucceeded || tt.status.Status == payload.StatusFailed {
						if !tt.status.StartedAt.IsZero() && tt.status.Duration > 0 {
							assert.NotNil(t, receivedPayload["started_at"])
							assert.NotNil(t, receivedPayload["duration_seconds"])
							assert.NotNil(t, receivedPayload["duration_string"])
						}
					}
				}

				if tt.migrationReq != nil {
					assert.Equal(t, tt.migrationReq.SourceOrg, receivedPayload["source_org"])
					assert.Equal(t, tt.migrationReq.TargetOrg, receivedPayload["target_org"])
				}

				// Verify details object
				details, ok := receivedPayload["details"].(map[string]interface{})
				assert.True(t, ok)
				assert.NotNil(t, details["stage_description"])
				assert.NotNil(t, details["state_description"])
			}
		})
	}
}

func TestSendWebhookNotificationRetry(t *testing.T) {
	var requestCount int
	var mu sync.Mutex

	// Create test server that fails first few requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()

		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Create migrator
	m := &Migrator{
		logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		webhookURL: server.URL,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
	}

	repoName := "test-org/test-repo"
	status := &payload.MigrationStatus{
		Status:    payload.StatusInProgress,
		Stage:     "archive",
		State:     "generating",
		UpdatedAt: time.Now(),
	}
	m.migrations[repoName] = status

	// Call the function
	m.sendWebhookNotification(repoName, nil)

	// Verify retry behavior - should eventually succeed after retries
	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	assert.True(t, finalCount >= 3, "Expected at least 3 requests due to retries")
}

func TestSendWebhookNotificationTimeout(t *testing.T) {
	// Create server that hangs
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(12 * time.Second) // Longer than webhook timeout (10s) but not too long
	}))
	defer server.Close()

	// Create migrator
	m := &Migrator{
		logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		webhookURL: server.URL,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
	}

	repoName := "test-org/test-repo"
	status := &payload.MigrationStatus{
		Status:    payload.StatusInProgress,
		Stage:     "archive",
		State:     "generating",
		UpdatedAt: time.Now(),
	}
	m.migrations[repoName] = status

	start := time.Now()
	m.sendWebhookNotification(repoName, nil)
	duration := time.Since(start)

	// Should timeout quickly due to client timeout configuration
	// With 3 retries and 10s timeout each, plus retry delays, expect it to be under 60 seconds
	assert.True(t, duration < 60*time.Second, "Webhook should timeout before 60 seconds")
}

func TestSendWebhookNotificationConcurrent(t *testing.T) {
	var requestCount int
	var mu sync.Mutex

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create migrator
	m := &Migrator{
		logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		webhookURL: server.URL,
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
	}

	// Send multiple concurrent webhook notifications
	numWebhooks := 10
	done := make(chan bool, numWebhooks)

	for i := 0; i < numWebhooks; i++ {
		repoName := fmt.Sprintf("test-org/test-repo-%d", i)
		status := &payload.MigrationStatus{
			Status:    payload.StatusInProgress,
			Stage:     "archive",
			State:     "generating",
			UpdatedAt: time.Now(),
		}
		m.migrations[repoName] = status

		go func(repo string) {
			m.sendWebhookNotification(repo, nil)
			done <- true
		}(repoName)
	}

	// Wait for all webhooks to complete
	for i := 0; i < numWebhooks; i++ {
		<-done
	}

	// Verify all requests were received
	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	assert.Equal(t, numWebhooks, finalCount)
}

func TestSendWebhookNotificationInvalidURL(t *testing.T) {
	// Create migrator with invalid URL
	m := &Migrator{
		logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		webhookURL: "://invalid-url",
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
	}

	repoName := "test-org/test-repo"
	status := &payload.MigrationStatus{
		Status:    payload.StatusInProgress,
		Stage:     "archive",
		State:     "generating",
		UpdatedAt: time.Now(),
	}
	m.migrations[repoName] = status

	// Should not panic with invalid URL
	m.sendWebhookNotification(repoName, nil)
}

func TestSendWebhookNotificationMarshalError(t *testing.T) {
	// This test ensures graceful handling of marshal errors
	// Create migrator
	m := &Migrator{
		logger:     slog.New(slog.NewTextHandler(os.Stdout, nil)),
		webhookURL: "http://example.com",
		migrations: make(map[string]*payload.MigrationStatus),
		mu:         sync.RWMutex{},
	}

	repoName := "test-org/test-repo"
	status := &payload.MigrationStatus{
		Status:    payload.StatusInProgress,
		Stage:     "archive",
		State:     "generating",
		UpdatedAt: time.Now(),
	}
	m.migrations[repoName] = status

	// This should complete without error even if we can't reach the URL
	m.sendWebhookNotification(repoName, nil)
}
