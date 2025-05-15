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
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/dashboard"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
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

	// Create router with routes
	mux := http.NewServeMux()

	// API routes with appropriate middleware
	migrateHandler := http.HandlerFunc(s.handleMigration)
	statusHandler := http.HandlerFunc(s.handleStatus)
	healthHandler := http.HandlerFunc(s.handleHealth)

	// Apply middleware stacks to routes based on their needs
	mux.Handle("/migrate", s.withAPIMiddleware(migrateHandler))
	mux.Handle("/status", s.withBaseMiddleware(statusHandler))
	mux.Handle("/health", s.withBaseMiddleware(healthHandler))

	// Initialize and register dashboard if enabled
	if cfg.Server.Dashboard {
		s.initDashboard(mux)
	}

	// Create server with timeouts
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// withBaseMiddleware applies the base middleware stack (used for all endpoints).
// The base middleware includes request logging, tracing and security headers.
//
// Parameters:
//   - next: The handler to wrap with middleware.
//
// Returns:
//   - http.Handler: The handler wrapped with base middleware.
func (s *Server) withBaseMiddleware(next http.Handler) http.Handler {
	// Apply tracing middleware first to capture the entire request lifecycle
	tracedHandler := tracing.TraceHTTP(next, "http_server")

	return CombineMiddleware(tracedHandler,
		s.middleware.LogRequest,
		s.middleware.SecurityHeaders,
		s.middleware.SanitizeInput,
	)
}

// withAPIMiddleware applies the full middleware stack used for API endpoints.
// This includes all base middleware plus JSON validation, request size limiting,
// and optional rate limiting based on configuration.
//
// Parameters:
//   - next: The handler to wrap with middleware.
//
// Returns:
//   - http.Handler: The handler wrapped with API middleware.
func (s *Server) withAPIMiddleware(next http.Handler) http.Handler {
	// Apply rate limiter only if configured
	middlewareStack := []func(http.Handler) http.Handler{
		s.middleware.LogRequest,
		s.middleware.SecurityHeaders,
		s.middleware.SanitizeInput,
		s.middleware.JSONOnly,
		s.middleware.RequestSizeLimit,
	}

	// Add rate limiting if configured (default 60 requests per minute)
	if s.config.Server.RateLimit > 0 {
		middlewareStack = append(middlewareStack,
			s.middleware.RateLimiter(s.config.Server.RateLimit))
	}

	// Apply tracing middleware only if enabled
	if s.config.Tracing.Enabled {
		next = tracing.TraceHTTP(next, "http_api")
	}

	return CombineMiddleware(next, middlewareStack...)
}

// Start starts the HTTP server and begins listening for requests.
// It blocks until the server shuts down or encounters an error.
//
// Returns:
//   - error: An error if the server fails to start or encounters a fatal error.
func (s *Server) Start() error {
	s.logger.Info("Starting server",
		"port", s.config.Server.Port,
		"read_timeout", s.config.Server.ReadTimeout,
		"write_timeout", s.config.Server.WriteTimeout,
	)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server with a timeout context.
// It attempts to complete all in-flight requests before shutting down.
// It also closes any resources used by the migrator, including storage connections.
//
// Parameters:
//   - ctx: Context for shutdown timeout.
//
// Returns:
//   - error: An error if the server fails to shut down gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server")

	// First shut down the HTTP server
	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Error("Error shutting down HTTP server", "error", err)
		// Continue with cleanup even if HTTP shutdown fails
	}

	// Then close migrator resources (including storage)
	if err := s.migrator.Close(); err != nil {
		s.logger.Error("Error closing migrator resources", "error", err)
		return err
	}

	s.logger.Info("Server shutdown completed")
	return nil
}

// handleHealth handles requests to the /health endpoint.
// It returns a simple status response indicating if the server is running.
// This endpoint is useful for load balancers and monitoring systems.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Add trace attribute for health check
	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attribute.String("endpoint", "health"))

	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// handleStatus handles requests to the /status endpoint.
// It returns the status of migrations for all repositories
// or for a specific repository if a "repository" query parameter is provided.
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

	// Check Content-Type
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		s.writeError(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		span.SetStatus(codes.Error, "Unsupported media type")
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "Failed to read request body")
		tracing.RecordError(ctx, err)
		return
	}

	if err := r.Body.Close(); err != nil {
		s.logger.Warn("Failed to close request body", "error", err)
	}

	// Parse request
	var req payload.MigrationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "Invalid JSON payload")
		tracing.RecordError(ctx, err)
		return
	}

	// Add migration request details to span
	span.SetAttributes(
		attribute.String("migration.source_org", req.SourceOrg),
		attribute.String("migration.target_org", req.TargetOrg),
		attribute.Int("migration.repositories_count", len(req.Repositories)),
	)

	// Validate the request
	if err := req.Validate(); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		tracing.RecordError(ctx, err)
		return
	}

	// Check if GHES token is provided when needed
	ghesBaseURL := req.GHESBaseURL
	if ghesBaseURL != "" && req.GHESToken == "" {
		s.writeError(w, r, http.StatusBadRequest, "GHES token is required when using GHES base URL")
		span.SetStatus(codes.Error, "Missing GHES token")
		return
	}

	// Log migration request
	s.logger.Info("Migration request received",
		"source_org", req.SourceOrg,
		"target_org", req.TargetOrg,
		"repo_count", len(req.Repositories),
		"use_ghos", req.UseGHOS,
	)

	// Start migration in a separate goroutine
	go func() {
		// Create a new context with correlation ID for the background task
		bgCtx := logging.ContextWithCorrelationID(context.Background())
		if id := logging.GetCorrelationID(ctx); id != "" {
			bgCtx = context.WithValue(bgCtx, logging.KeyCorrelationID, id)
		}

		// Create a cancel function for the background context
		bgCtx, cancel := context.WithCancel(bgCtx)

		// Start the migration
		err := s.migrator.StartMigration(bgCtx, &req, cancel)
		if err != nil {
			s.logger.Error("Failed to start migration",
				"error", err,
				"source_org", req.SourceOrg,
				"target_org", req.TargetOrg,
			)
			cancel() // Cancel if there was an error starting
		}
	}()

	// Return accepted response
	resp := map[string]interface{}{
		"status":       "accepted",
		"message":      fmt.Sprintf("Migration started for %d repositories", len(req.Repositories)),
		"timestamp":    time.Now(),
		"request_id":   logging.GetCorrelationID(ctx),
		"repositories": req.Repositories,
	}

	span.SetStatus(codes.Ok, "Migration accepted")
	s.writeJSON(w, r, http.StatusAccepted, resp)
}

// sanitizeToken redacts most of a token for logging purposes,
// showing only the first 4 and last 4 characters.
func sanitizeToken(token string) string {
	if len(token) < 8 {
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

// writeJSON writes a JSON response with the given status code and data.
// It handles the error case internally and logs any issues.
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
func (s *Server) initDashboard(mux *http.ServeMux) {
	// Create dashboard handler
	dashboardHandler, err := dashboard.New(s.migrator)
	if err != nil {
		s.logger.Error("Failed to initialize dashboard", "error", err)
		return
	}

	// Register dashboard handlers
	dashboardHandler.RegisterHandlers(mux)
	s.logger.Info("Dashboard enabled and registered")
}
