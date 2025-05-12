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
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/validation"
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
// The base middleware includes request logging and security headers.
//
// Parameters:
//   - next: The handler to wrap with middleware.
//
// Returns:
//   - http.Handler: The handler wrapped with base middleware.
func (s *Server) withBaseMiddleware(next http.Handler) http.Handler {
	return CombineMiddleware(next,
		s.middleware.LogRequest,
		s.middleware.SecurityHeaders,
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
		s.middleware.JSONOnly,
		s.middleware.RequestSizeLimit,
	}

	// Add rate limiting if configured (default 60 requests per minute)
	if s.config.Server.RateLimit > 0 {
		middlewareStack = append(middlewareStack,
			s.middleware.RateLimiter(s.config.Server.RateLimit))
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
//
// Parameters:
//   - ctx: Context for shutdown timeout.
//
// Returns:
//   - error: An error if the server fails to shut down gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server")
	return s.server.Shutdown(ctx)
}

// handleHealth handles requests to the /health endpoint.
// It returns a simple status response indicating if the server is running.
// This endpoint is useful for load balancers and monitoring systems.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// handleStatus handles requests to the /status endpoint.
// It returns the status of migrations for all repositories
// or for a specific repository if a "repository" query parameter is provided.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get repository name from query parameter
	repoName := r.URL.Query().Get("repository")

	if repoName != "" {
		// Get status for specific repository
		status := s.migrator.GetMigrationStatus(repoName)
		if status == nil {
			s.writeError(w, r, http.StatusNotFound, fmt.Sprintf("No migration found for repository %s", repoName))
			return
		}
		s.writeJSON(w, r, http.StatusOK, status)
		return
	}

	// Return all statuses
	statuses := s.migrator.GetAllMigrationStatuses()
	s.writeJSON(w, r, http.StatusOK, statuses)
}

// handleMigration handles requests to the /migrate endpoint.
// It validates the request, starts the migration process in the background,
// and returns an acceptance response. The actual migration happens asynchronously.
func (s *Server) handleMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check Content-Type
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		s.writeError(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	// Limit request body size to 1MB
	r.Body = http.MaxBytesReader(w, r.Body, validation.MaxRequestBodySizeBytes)

	// Read and decode the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}

	// Check if request body is empty
	if len(body) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "Request body is empty")
		return
	}

	var req payload.MigrationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Test connection to GHES if needed (optional, can be resource-intensive)
	// if r.URL.Query().Get("test_connection") == "true" {
	//    if err := validation.TestGHESURL(req.GHESBaseURL, req.GHESToken); err != nil {
	//        s.writeError(w, r, http.StatusBadRequest, fmt.Sprintf("GHES connection test failed: %v", err))
	//        return
	//    }
	// }

	// Sanitize tokens for logging (only show first/last few characters)
	sanitizedGHESToken := sanitizeToken(req.GHESToken)
	sanitizedGHCloudToken := sanitizeToken(req.GHCloudToken)

	// Log the migration request details with sanitized tokens
	s.logger.Info("Migration request received",
		"source_org", req.SourceOrg,
		"target_org", req.TargetOrg,
		"repositories", len(req.Repositories),
		"ghes_base_url", req.GHESBaseURL,
		"ghes_token", sanitizedGHESToken,
		"gh_cloud_token", sanitizedGHCloudToken,
	)

	// Start migration in background
	go func() {
		var ctx context.Context
		var cancel context.CancelFunc

		// Use the custom timeout from the request if provided, otherwise use no timeout
		if req.MaxDuration != "" {
			// We already validated the duration in the request validation
			maxDuration := req.GetMaxDuration()
			s.logger.Info("Using custom timeout",
				"max_duration", req.MaxDuration,
				"repositories", len(req.Repositories),
			)
			ctx, cancel = context.WithTimeout(context.Background(), maxDuration)
		} else {
			// No timeout - for very large repositories
			s.logger.Info("No timeout configured",
				"repositories", len(req.Repositories),
			)
			ctx, cancel = context.WithCancel(context.Background())
		}

		// Migrator will take ownership of the context and handle its lifecycle
		if err := s.migrator.StartMigration(ctx, &req, cancel); err != nil {
			s.logger.Error("Failed to start migration",
				"error", err,
				"source_org", req.SourceOrg,
				"target_org", req.TargetOrg,
			)
			// Only cancel if there was an error starting the migration
			cancel()
		}
	}()

	s.writeJSON(w, r, http.StatusAccepted, map[string]string{
		"status": "migration started",
	})
}

// sanitizeToken masks a token for secure logging, showing only the first and last few characters.
// This prevents accidental exposure of sensitive credentials in logs.
//
// Parameters:
//   - token: The token to sanitize.
//
// Returns:
//   - string: The sanitized token with middle characters replaced by "...".
func sanitizeToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

// writeJSON writes a JSON response with the given status code and data.
// It handles serialization of the data and sets appropriate headers.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
//   - statusCode: HTTP status code to return.
//   - data: Data to serialize as JSON.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			s.logger.Error("Failed to encode JSON response",
				"error", err,
				"path", r.URL.Path,
			)
		}
	}
}

// writeError writes a JSON error response with the given status code and message.
// It also logs the error for monitoring and debugging.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
//   - statusCode: HTTP status code to return.
//   - message: Error message to include in the response.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	s.logger.Error("Request error",
		"status", statusCode,
		"path", r.URL.Path,
		"method", r.Method,
		"error", message,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := fmt.Fprintf(w, `{"error": %q}`, message); err != nil {
		s.logger.Warn("Failed to write error response", "error", err)
	}
}
