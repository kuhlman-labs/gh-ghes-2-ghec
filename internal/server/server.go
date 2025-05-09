package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// Server handles HTTP requests for repository migrations
type Server struct {
	migrator *migrator.Migrator
	logger   *slog.Logger
}

// New creates a new server instance
func New(webhookURL string) *Server {
	return &Server{
		migrator: migrator.New(webhookURL),
		logger:   logging.Get(),
	}
}

// Start starts the HTTP server
func (s *Server) Start(port int) error {
	http.HandleFunc("/migrate", s.handleMigration)
	addr := fmt.Sprintf(":%d", port)
	s.logger.Info("Starting server", "port", port)
	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req payload.MigrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Start migration in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		if err := s.migrator.StartMigration(ctx, &req); err != nil {
			s.logger.Error("Migration failed",
				"error", err,
				"source_org", req.SourceOrg,
				"target_org", req.TargetOrg,
			)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "migration started",
	})
}
