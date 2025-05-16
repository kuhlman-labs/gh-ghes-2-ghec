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
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/metrics"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
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
		Metrics: struct {
			Enabled     bool   "mapstructure:\"enabled\""
			Port        int    "mapstructure:\"port\""
			Path        string "mapstructure:\"path\""
			ServiceName string "mapstructure:\"service_name\""
		}{
			Enabled: false,
			Path:    "/metrics",
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

func TestHandleHealthCheck(t *testing.T) {
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
				"status": "ok",
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
			req := httptest.NewRequest(tt.method, "/api/healthz", nil)
			w := httptest.NewRecorder()

			server.handleHealthCheck(w, req)

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
			req := httptest.NewRequest(tt.method, "/api/status"+tt.query, nil)
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
			expectedStatus: http.StatusAccepted,
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

			req := httptest.NewRequest(tt.method, "/api/migrate", bytes.NewReader(body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()

			server.handleMigration(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestWithMiddleware(t *testing.T) {
	server := setupTestServer(t)

	// Simple handler for testing middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("test"))
		if err != nil {
			panic(err)
		}
	})

	// Apply middleware
	wrappedHandler := server.withMiddleware(handler)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	// Process the request
	wrappedHandler.ServeHTTP(w, req)

	// Verify the response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test", w.Body.String())
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

func TestMetricsEndpoint(t *testing.T) {
	// Create config with metrics enabled
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
		},
		Metrics: struct {
			Enabled     bool   "mapstructure:\"enabled\""
			Port        int    "mapstructure:\"port\""
			Path        string "mapstructure:\"path\""
			ServiceName string "mapstructure:\"service_name\""
		}{
			Enabled: true,
			Path:    "/metrics",
		},
	}

	// Initialize a mock metrics system
	metricsCfg := metrics.Config{
		Enabled: true,
		Path:    "/metrics",
	}
	_ = metrics.Init(metricsCfg)

	// Create migrator and server
	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	m := migrator.NewMigrator(logger, githubAPI, storageProvider, "", cfg, nil, nil)
	server := New(cfg, m)

	// Test that metrics endpoint is mounted
	assert.NotNil(t, server.server.Handler)

	// Test metrics middleware is applied
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	// Use the server's mux directly
	server.server.Handler.ServeHTTP(w, req)

	// Verify that metrics endpoint returned success
	assert.Less(t, w.Code, 400)
}

func TestDashboardInitialization(t *testing.T) {
	// Create config with dashboard enabled
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
			Dashboard:    true,
		},
	}

	// Create migrator and server
	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	m := migrator.NewMigrator(logger, githubAPI, storageProvider, "", cfg, nil, nil)
	server := New(cfg, m)

	// Test that server was created successfully
	assert.NotNil(t, server.server.Handler)
}

// Helper function to set up a test server
func setupTestServer(t *testing.T) *Server {
	// Initialize config before creating a server
	err := config.Init()
	require.NoError(t, err, "Failed to initialize config")

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
			RateLimit:    60,
		},
		Metrics: struct {
			Enabled     bool   "mapstructure:\"enabled\""
			Port        int    "mapstructure:\"port\""
			Path        string "mapstructure:\"path\""
			ServiceName string "mapstructure:\"service_name\""
		}{
			Enabled: false,
			Path:    "/metrics",
		},
		Tracing: struct {
			Enabled     bool    "mapstructure:\"enabled\""
			Endpoint    string  "mapstructure:\"endpoint\""
			ServiceName string  "mapstructure:\"service_name\""
			SampleRate  float64 "mapstructure:\"sample_rate\""
		}{
			Enabled: false,
		},
	}

	logger := logging.Get()
	githubAPI := github.NewNoopAPI(logger)
	storageProvider := &storage.NoopStorage{}
	m := migrator.NewMigrator(logger, githubAPI, storageProvider, "", cfg, nil, nil)

	return New(cfg, m)
}
