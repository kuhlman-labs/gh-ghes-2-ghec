package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
			RateLimit:    60,
		},
	}
	m := &migrator.Migrator{}

	server := New(cfg, m)
	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
	assert.Equal(t, m, server.migrator)
	assert.NotNil(t, server.logger)
	assert.NotNil(t, server.middleware)
	assert.NotNil(t, server.server)
}

func TestHandleHealth(t *testing.T) {
	server := setupTestServer(t)

	tests := []struct {
		name           string
		method         string
		expectedStatus int
		expectedBody   map[string]string
	}{
		{
			name:           "GET request",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			expectedBody: map[string]string{
				"status": "healthy",
			},
		},
		{
			name:           "POST request",
			method:         http.MethodPost,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/health", nil)
			w := httptest.NewRecorder()

			server.handleHealth(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.expectedBody != nil {
				var response map[string]string
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.expectedBody, response)
			}
		})
	}
}

func TestHandleStatus(t *testing.T) {
	server := setupTestServer(t)

	tests := []struct {
		name           string
		method         string
		query          string
		expectedStatus int
	}{
		{
			name:           "GET request without repository",
			method:         http.MethodGet,
			query:          "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "GET request with repository",
			method:         http.MethodGet,
			query:          "?repository=test-repo",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "POST request",
			method:         http.MethodPost,
			query:          "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/status"+tt.query, nil)
			w := httptest.NewRecorder()

			server.handleStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleMigration(t *testing.T) {
	server := setupTestServer(t)

	validRequest := payload.MigrationRequest{
		SourceOrg:    "source-org",
		TargetOrg:    "target-org",
		Repositories: []string{"repo1", "repo2"},
		GHESBaseURL:  "https://ghes.example.com",
		GHESToken:    "ghes-token-0123456789012345678901234567",
		GHCloudToken: "ghcloud-token-0123456789012345678901234",
	}

	tests := []struct {
		name           string
		method         string
		contentType    string
		body           interface{}
		expectedStatus int
	}{
		{
			name:           "valid POST request",
			method:         http.MethodPost,
			contentType:    "application/json",
			body:           validRequest,
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "GET request",
			method:         http.MethodGet,
			contentType:    "application/json",
			body:           validRequest,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "invalid content type",
			method:         http.MethodPost,
			contentType:    "text/plain",
			body:           validRequest,
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "empty body",
			method:         http.MethodPost,
			contentType:    "application/json",
			body:           nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON",
			method:         http.MethodPost,
			contentType:    "application/json",
			body:           "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			var err error

			if tt.body != nil {
				body, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(tt.method, "/migrate", bytes.NewReader(body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()

			server.handleMigration(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestServerShutdown(t *testing.T) {
	server := setupTestServer(t)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start server in a goroutine
	go func() {
		err := server.Start()
		assert.Error(t, err, http.ErrServerClosed)
	}()

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown the server
	err := server.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestSanitizeToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{
			name:     "long token",
			token:    "0123456789012345678901234567890123456789",
			expected: "0123...6789",
		},
		{
			name:     "short token",
			token:    "123456",
			expected: "***",
		},
		{
			name:     "empty token",
			token:    "",
			expected: "***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeToken(tt.token)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function to set up a test server
func setupTestServer(t *testing.T) *Server {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
			RateLimit:    60,
		},
	}
	m := migrator.New("")
	return New(cfg, m)
}
