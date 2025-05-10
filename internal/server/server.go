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

// Server handles HTTP requests for repository migrations
type Server struct {
	migrator   *migrator.Migrator
	logger     *slog.Logger
	config     *config.Config
	server     *http.Server
	middleware *Middleware
}

// New creates a new server instance
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

// withBaseMiddleware applies the base middleware stack (used for all endpoints)
func (s *Server) withBaseMiddleware(next http.Handler) http.Handler {
	return CombineMiddleware(next,
		s.middleware.LogRequest,
		s.middleware.SecurityHeaders,
	)
}

// withAPIMiddleware applies the full middleware stack (used for API endpoints)
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

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info("Starting server",
		"port", s.config.Server.Port,
		"read_timeout", s.config.Server.ReadTimeout,
		"write_timeout", s.config.Server.WriteTimeout,
	)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server")
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

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

// Helper function to sanitize token for logging
func sanitizeToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

// writeJSON writes a JSON response with the given status code
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode response",
			"error", err,
			"path", r.URL.Path,
			"request_id", r.Context().Value("request_id"),
		)
	}
}

// writeError writes an error response with the given status code and message
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	s.logger.Error("Request error",
		"status_code", statusCode,
		"message", message,
		"path", r.URL.Path,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
