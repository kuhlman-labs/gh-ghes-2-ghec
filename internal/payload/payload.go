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
	GHESBaseURL  string   `json:"ghes_base_url"`          // Base URL of GHES instance (e.g., https://github.example.com)
	GHESToken    string   `json:"ghes_token"`             // Token for GitHub Enterprise Server
	GHCloudToken string   `json:"gh_cloud_token"`         // Token for GitHub Enterprise Cloud
	MaxDuration  string   `json:"max_duration,omitempty"` // Optional maximum duration for the migration (e.g., "24h", "48h")
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
	if r.GHESToken == "" {
		return fmt.Errorf("ghes_token is required")
	}
	if r.GHCloudToken == "" {
		return fmt.Errorf("gh_cloud_token is required")
	}

	// Validate max duration if provided
	if r.MaxDuration != "" {
		_, err := time.ParseDuration(r.MaxDuration)
		if err != nil {
			return fmt.Errorf("invalid max_duration format: %w", err)
		}
	}

	return nil
}

// GetMaxDuration returns the parsed maximum duration or a default value
func (r *MigrationRequest) GetMaxDuration() time.Duration {
	if r.MaxDuration == "" {
		// Default to 24 hours if not specified
		return 24 * time.Hour
	}

	duration, err := time.ParseDuration(r.MaxDuration)
	if err != nil {
		// Should not happen due to validation, but return default if it does
		return 24 * time.Hour
	}

	return duration
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
	Stage      string    `json:"stage,omitempty"` // Current stage of the migration process
	State      string    `json:"state,omitempty"` // Current state within the stage
}

const (
	StatusInProgress = "in_progress"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
)
