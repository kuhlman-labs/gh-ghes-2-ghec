// Package server provides HTTP server functionality for the migration API,
// including request handlers, middleware, and server configuration.
// It implements a RESTful API for initiating and monitoring repository migrations.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/dashboard"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/metrics"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Server handles HTTP requests for repository migrations.
// It routes requests, applies middleware, manages authentication,
// and interacts with the migrator package to handle actual migrations.
type Server struct {
	migrator   *migrator.Migrator
	logger     *slog.Logger
	config     *config.Config
	server     *http.Server
	middleware *Middleware
}

// New creates a new server instance with the provided configuration and migrator.
// It sets up routes, applies middleware, and configures server timeouts.
//
// Parameters:
//   - cfg: Server configuration including port, timeouts, and rate limits.
//   - m: The migrator instance that will handle repository migrations.
//
// Returns:
//   - *Server: A configured server ready to handle HTTP requests.
func New(cfg *config.Config, m *migrator.Migrator) *Server {
	s := &Server{
		migrator:   m,
		logger:     logging.Get(),
		config:     cfg,
		middleware: NewMiddleware(),
	}

	// Set up HTTP routes
	mux := http.NewServeMux()

	// API routes with middleware
	migrateHandler := s.withMiddleware(http.HandlerFunc(s.handleMigration))
	statusHandler := s.withMiddleware(http.HandlerFunc(s.handleStatus))
	healthHandler := s.withMiddleware(http.HandlerFunc(s.handleHealthCheck))
	retryHandler := s.withMiddleware(http.HandlerFunc(s.handleRetryMigration))

	mux.Handle("/api/migrate", migrateHandler)
	mux.Handle("/api/status", statusHandler)
	mux.Handle("/api/healthz", healthHandler)
	mux.Handle("/api/retry", retryHandler)

	// Add metrics endpoint if enabled
	if cfg.Metrics.Enabled {
		mux.Handle(cfg.Metrics.Path, metrics.Handler())
		s.logger.Info("Metrics endpoint enabled", "path", cfg.Metrics.Path)
	}

	// Initialize and mount the dashboard if enabled
	if cfg.Server.Dashboard {
		if err := s.initDashboard(mux); err != nil {
			s.logger.Error("Failed to initialize dashboard", "error", err)
		}
	}

	// Create HTTP server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      tracing.TraceHTTP(metrics.InstrumentHandler(mux, "server"), "http_request"),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	return s
}

// withMiddleware applies all necessary middleware to a handler
func (s *Server) withMiddleware(handler http.Handler) http.Handler {
	// First apply request logging
	handler = s.middleware.LogRequest(handler)

	// Then security headers
	handler = s.middleware.SecurityHeaders(handler)

	// Then sanitize input
	handler = s.middleware.SanitizeInput(handler)

	// Then JSON validation for api endpoints
	handler = s.middleware.JSONOnly(handler)

	// Then request size limits
	handler = s.middleware.RequestSizeLimit(handler)

	// Apply rate limiting if configured
	if s.config.Server.RateLimit > 0 {
		handler = s.middleware.RateLimiter(s.config.Server.RateLimit)(handler)
	}

	// Instrument with metrics
	handler = metrics.InstrumentHandler(handler, "api")

	// Finally apply tracing
	if s.config.Tracing.Enabled {
		handler = tracing.TraceHTTP(handler, "api_request")
	}

	return handler
}

// Start begins listening for HTTP requests on the configured port.
// It returns an error if the server fails to start.
func (s *Server) Start() error {
	s.logger.Info("Starting server", "port", s.config.Server.Port)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server without interrupting active connections.
// It waits for the configured shutdown timeout before forcibly closing connections.
//
// Parameters:
//   - ctx: Context for controlling the shutdown process.
//
// Returns:
//   - error: An error if the shutdown process fails.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server")
	// First, shutdown the HTTP server
	err := s.server.Shutdown(ctx)
	if err != nil {
		s.logger.Error("Error shutting down HTTP server", "error", err)
	}

	// Then attempt to close migrator resources
	if err := s.migrator.Close(); err != nil {
		s.logger.Error("Error closing migrator resources", "error", err)
		return err
	}

	return nil
}

// handleHealthCheck handles requests to the /api/healthz endpoint.
// It returns a 200 OK response if the server is healthy.
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	_, span := tracing.StartSpan(r.Context(), "health_check")
	defer span.End()

	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleStatus handles requests to the /api/status endpoint.
// It returns the status of a specific repository migration or all migrations.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_, span := tracing.StartSpan(r.Context(), "get_migration_status")
	defer span.End()

	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		span.SetStatus(codes.Error, "Method not allowed")
		return
	}

	// Get repository full name (org/repo) from query parameter
	repoFullName := r.URL.Query().Get("repository")
	if repoFullName != "" {
		span.SetAttributes(attribute.String("repository_full_name", repoFullName))
	}

	if repoFullName != "" {
		// Get status for specific repository
		status := s.migrator.GetMigrationStatus(repoFullName)
		if status == nil {
			errMsg := fmt.Sprintf("No migration found for repository %s", repoFullName)
			s.writeError(w, r, http.StatusNotFound, errMsg)
			span.SetStatus(codes.Error, errMsg)
			return
		}

		// Add migration status details to span
		span.SetAttributes(
			attribute.String("migration.id", status.MigrationID),
			attribute.String("migration.status", status.Status),
			attribute.Int("migration.progress", status.Progress),
		)

		s.writeJSON(w, r, http.StatusOK, status)
		return
	}

	// Return all statuses
	statuses := s.migrator.GetAllMigrationStatuses()
	span.SetAttributes(attribute.Int("migration.count", len(statuses)))
	s.writeJSON(w, r, http.StatusOK, statuses)
}

// handleMigration handles requests to the /migrate endpoint.
// It validates the request, starts the migration process in the background,
// and returns an acceptance response. The actual migration happens asynchronously.
func (s *Server) handleMigration(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracing.StartSpan(r.Context(), "start_migration")
	defer span.End()

	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		span.SetStatus(codes.Error, "Method not allowed")
		return
	}

	// Read and parse the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "Failed to read request body")
		tracing.RecordError(ctx, err)
		return
	}

	var migrationReq payload.MigrationRequest
	if err := json.Unmarshal(body, &migrationReq); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "Invalid JSON in request body")
		tracing.RecordError(ctx, err)
		return
	}

	// Log the migration request details
	s.logger.Info("Received migration request",
		"source_org", migrationReq.SourceOrg,
		"target_org", migrationReq.TargetOrg,
		"repositories_count", len(migrationReq.Repositories),
		"delete_if_exists", migrationReq.DeleteIfExists,
		"use_ghos", migrationReq.UseGHOS)

	// Add migration details to span
	span.SetAttributes(
		attribute.String("source_org", migrationReq.SourceOrg),
		attribute.String("target_org", migrationReq.TargetOrg),
		attribute.Int("repositories_count", len(migrationReq.Repositories)),
		attribute.Bool("use_ghos", migrationReq.UseGHOS),
		attribute.Bool("delete_if_exists", migrationReq.DeleteIfExists),
	)

	// Validate required fields
	if err := migrationReq.Validate(); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		tracing.RecordError(ctx, err)
		return
	}

	// Start the migration process in a goroutine
	go func() {
		// Create a new background context with cancellation ability
		bgCtx := logging.ContextWithCorrelationID(context.Background())
		if id := logging.GetCorrelationID(ctx); id != "" {
			bgCtx = context.WithValue(bgCtx, logging.KeyCorrelationID, id)
		}

		// Create a cancel function for the background context
		bgCtx, cancel := context.WithCancel(bgCtx)

		// Start the migration
		if err := s.migrator.StartMigration(bgCtx, &migrationReq, cancel); err != nil {
			s.logger.Error("Migration failed",
				"source_org", migrationReq.SourceOrg,
				"target_org", migrationReq.TargetOrg,
				"repos_count", len(migrationReq.Repositories),
				"error", err,
			)

			// Record metrics for failed migration
			metrics.RecordMigrationComplete(
				migrationReq.SourceOrg,
				migrationReq.TargetOrg,
				"failed",
				time.Second, // Minimal duration for immediate failures
				0,
			)

			cancel() // Cancel if there was an error starting
		}
	}()

	// Record metrics for migration start
	metrics.RecordMigrationStart(migrationReq.SourceOrg, migrationReq.TargetOrg)

	// Return accepted response
	response := map[string]interface{}{
		"status":       "accepted",
		"message":      fmt.Sprintf("Migration request accepted for %d repositories", len(migrationReq.Repositories)),
		"timestamp":    time.Now(),
		"request_id":   logging.GetCorrelationID(ctx),
		"repositories": migrationReq.Repositories,
	}
	s.writeJSON(w, r, http.StatusAccepted, response)
}

// handleRetryMigration handles requests to retry a failed migration.
// It accepts a repository name as a query parameter and attempts to retry the migration.
func (s *Server) handleRetryMigration(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracing.StartSpan(r.Context(), "retry_migration_api")
	defer span.End()

	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		span.SetStatus(codes.Error, "Method not allowed")
		return
	}

	// Get repository name from query parameter
	repoFullName := r.URL.Query().Get("repository")
	if repoFullName == "" {
		errMsg := "Missing repository parameter"
		s.writeError(w, r, http.StatusBadRequest, errMsg)
		span.SetStatus(codes.Error, errMsg)
		return
	}

	span.SetAttributes(attribute.String("repository", repoFullName))

	// Read request body for additional parameters if present
	var params struct {
		GHESToken    string `json:"ghes_token"`
		GHCloudToken string `json:"gh_cloud_token"`
		GHESBaseURL  string `json:"ghes_base_url"`
		TargetOrg    string `json:"target_org"`
	}

	// Try to read the body but don't fail if it's not present
	body, err := io.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		_ = json.Unmarshal(body, &params)
	}

	// Attempt to retry the migration with provided parameters
	err = s.migrator.RetryMigration(ctx, repoFullName, params.GHESToken, params.GHCloudToken, params.GHESBaseURL, params.TargetOrg)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to retry migration: %v", err)
		s.writeError(w, r, http.StatusBadRequest, errMsg)
		span.SetStatus(codes.Error, errMsg)
		return
	}

	// Return success response
	response := struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		Status     string `json:"status"`
		Repository string `json:"repository"`
	}{
		Success:    true,
		Message:    "Migration retry initiated successfully",
		Status:     "in_progress",
		Repository: repoFullName,
	}

	s.writeJSON(w, r, http.StatusOK, response)
}

// writeJSON writes a JSON response with the given status code and data.
// It sets the appropriate Content-Type header and handles error logging.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, statusCode int, data interface{}) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response",
			"error", err,
			"status_code", statusCode,
		)
		tracing.RecordError(ctx, err)
		span.SetStatus(codes.Error, "Failed to encode JSON response")
	}

	// Add response information to span
	span.SetAttributes(attribute.Int("http.status_code", statusCode))
}

// writeError writes a JSON error response with the given status code and message.
// It also logs the error at the appropriate level based on the status code.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)

	// Log client errors at INFO level, server errors at ERROR level
	if statusCode >= 500 {
		s.logger.Error("Server error",
			"status_code", statusCode,
			"message", message,
		)
	} else {
		s.logger.Info("Client error",
			"status_code", statusCode,
			"message", message,
		)
	}

	// Add error information to span
	span.SetAttributes(
		attribute.Int("http.status_code", statusCode),
		attribute.String("error.message", message),
	)
	span.SetStatus(codes.Error, message)

	// Construct error response
	errorResp := map[string]interface{}{
		"status":     "error",
		"message":    message,
		"code":       statusCode,
		"timestamp":  time.Now(),
		"request_id": logging.GetCorrelationID(ctx),
	}

	// Write JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(errorResp); err != nil {
		s.logger.Error("Failed to encode error response",
			"error", err,
		)
		tracing.RecordError(ctx, err)
	}
}

// initDashboard initializes and registers the dashboard handlers
func (s *Server) initDashboard(mux *http.ServeMux) error {
	// Create a new dashboard with the migrator
	dashHandler, err := dashboard.New(s.migrator)
	if err != nil {
		return fmt.Errorf("failed to create dashboard: %w", err)
	}

	// Dashboard Handler has its own RegisterHandlers method
	dashHandler.RegisterHandlers(mux)

	// Add the errors dashboard
	dashboard.RegisterErrorsDashboard(mux)
	s.logger.Info("Errors dashboard initialized", "path", "/dashboard/errors")

	s.logger.Info("Dashboard initialized", "path", "/")
	return nil
}
