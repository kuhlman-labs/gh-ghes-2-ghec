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
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// Server handles HTTP requests for repository migrations
type Server struct {
	migrator *migrator.Migrator
	logger   *slog.Logger
	config   *config.Config
	server   *http.Server
}

// New creates a new server instance
func New(cfg *config.Config, m *migrator.Migrator) *Server {
	s := &Server{
		migrator: m,
		logger:   logging.Get(),
		config:   cfg,
	}

	// Create router with middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/migrate", s.handleMigration)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)

	// Create server with basic config
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: mux,
	}

	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info("Starting server",
		"port", s.config.Server.Port,
	)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server")
	return s.server.Shutdown(ctx)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add request ID to context
		requestID := fmt.Sprintf("%d", time.Now().UnixNano())
		ctx := context.WithValue(r.Context(), "request_id", requestID)

		// Log request
		start := time.Now()
		s.logger.Debug("Incoming request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
		)

		// Add security headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Call next handler
		next.ServeHTTP(w, r.WithContext(ctx))

		// Log response time
		s.logger.Debug("Request completed",
			"request_id", requestID,
			"duration_ms", time.Since(start).Milliseconds(),
			"path", r.URL.Path,
		)
	})
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

	// Limit request body size to 1MB
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Read and decode the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
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

	// Log the migration request details
	s.logger.Info("Migration request received",
		"source_org", req.SourceOrg,
		"target_org", req.TargetOrg,
		"repositories", req.Repositories,
		"ghes_base_url", req.GHESBaseURL,
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
