package payload

import (
	"fmt"
	"strings"
	"time"
)

// MigrationRequest represents a request to migrate repositories
type MigrationRequest struct {
	SourceOrg    string   `json:"source_org"`
	TargetOrg    string   `json:"target_org"`
	Repositories []string `json:"repositories"`
	GHESBaseURL  string   `json:"ghes_base_url"` // Base URL of GHES instance (e.g., https://github.example.com)
}

// Validate validates the migration request
func (r *MigrationRequest) Validate() error {
	if r.SourceOrg == "" {
		return fmt.Errorf("source_org is required")
	}
	if r.TargetOrg == "" {
		return fmt.Errorf("target_org is required")
	}
	if len(r.Repositories) == 0 {
		return fmt.Errorf("repositories is required")
	}
	if r.GHESBaseURL == "" {
		return fmt.Errorf("ghes_base_url is required")
	}
	return nil
}

// GetGHESAPIURL returns the REST API URL for the GHES instance
func (r *MigrationRequest) GetGHESAPIURL() string {
	baseURL := strings.TrimSuffix(r.GHESBaseURL, "/")
	return baseURL + "/api/v3/"
}

// GetGHESGraphQLURL returns the GraphQL API URL for the GHES instance
func (r *MigrationRequest) GetGHESGraphQLURL() string {
	baseURL := strings.TrimSuffix(r.GHESBaseURL, "/")
	return baseURL + "/api/graphql"
}

// MigrationStatus represents the status of a repository migration
type MigrationStatus struct {
	Repository string    `json:"repository"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const (
	StatusInProgress = "in_progress"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
)
