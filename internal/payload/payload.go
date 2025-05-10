// Package payload defines the data structures for migration requests and status responses,
// including validation and utility methods for handling these structures.
package payload

import (
	"fmt"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/validation"
)

// MigrationRequest represents a request to migrate repositories from GHES to GHEC.
// It contains all the necessary information to perform the migration, including
// source and target organizations, repository list, API URLs, and authentication tokens.
type MigrationRequest struct {
	SourceOrg    string   `json:"source_org"`
	TargetOrg    string   `json:"target_org"`
	Repositories []string `json:"repositories"`
	GHESBaseURL  string   `json:"ghes_base_url"`          // Base URL of GHES instance (e.g., https://github.example.com)
	GHESToken    string   `json:"ghes_token"`             // Token for GitHub Enterprise Server
	GHCloudToken string   `json:"gh_cloud_token"`         // Token for GitHub Enterprise Cloud
	MaxDuration  string   `json:"max_duration,omitempty"` // Optional maximum duration for the migration (e.g., "24h", "48h")
	UseGHOS      bool     `json:"use_ghos,omitempty"`     // Use GitHub Owned Storage (GHOS) for migration archives
}

// Validate performs comprehensive validation of the migration request.
// It checks that all required fields are present and valid, including:
// - Organization names
// - Repository names
// - URLs
// - Tokens
// - Duration format
// Returns an error if any validation fails.
func (r *MigrationRequest) Validate() error {
	// Validate organization names
	if err := validation.ValidateOrganizationName(r.SourceOrg); err != nil {
		return fmt.Errorf("source_org: %w", err)
	}

	if err := validation.ValidateOrganizationName(r.TargetOrg); err != nil {
		return fmt.Errorf("target_org: %w", err)
	}

	// Validate source and target are different
	if strings.EqualFold(r.SourceOrg, r.TargetOrg) {
		return fmt.Errorf("source_org and target_org must be different")
	}

	// Validate repositories
	if err := validation.ValidateRepositoryList(r.Repositories); err != nil {
		return fmt.Errorf("repositories: %w", err)
	}

	// Validate GHES base URL
	if err := validation.ValidateURL(r.GHESBaseURL); err != nil {
		return fmt.Errorf("ghes_base_url: %w", err)
	}

	// Validate tokens
	if err := validation.ValidateGitHubToken(r.GHESToken); err != nil {
		return fmt.Errorf("ghes_token: %w", err)
	}

	if err := validation.ValidateGitHubToken(r.GHCloudToken); err != nil {
		return fmt.Errorf("gh_cloud_token: %w", err)
	}

	// Validate max duration if provided
	if r.MaxDuration != "" {
		_, err := validation.ValidateDuration(r.MaxDuration)
		if err != nil {
			return fmt.Errorf("max_duration: %w", err)
		}
	}

	return nil
}

// GetMaxDuration returns the parsed maximum duration for the migration.
// If no duration is specified or validation fails, it returns a default duration.
// The default duration is defined in the validation package.
func (r *MigrationRequest) GetMaxDuration() time.Duration {
	duration, err := validation.ValidateDuration(r.MaxDuration)
	if err != nil {
		// Should not happen due to validation, but return default if it does
		return time.Duration(validation.DefaultMaxDuration) * time.Hour
	}

	return duration
}

// GetGHESAPIURL returns the REST API URL for the GHES instance.
// It ensures the base URL has no trailing slash and appends the API path.
func (r *MigrationRequest) GetGHESAPIURL() string {
	baseURL := strings.TrimSuffix(r.GHESBaseURL, "/")
	return baseURL + "/api/v3/"
}

// GetGHESGraphQLURL returns the GraphQL API URL for the GHES instance.
// It ensures the base URL has no trailing slash and appends the GraphQL endpoint path.
func (r *MigrationRequest) GetGHESGraphQLURL() string {
	baseURL := strings.TrimSuffix(r.GHESBaseURL, "/")
	return baseURL + "/api/graphql"
}

// MigrationStatus represents the status of a repository migration process.
// It contains detailed information about the migration progress, including
// current stage, state, timing, and progress metrics.
type MigrationStatus struct {
	Repository        string        `json:"repository"`
	Status            string        `json:"status"`
	Error             string        `json:"error,omitempty"`
	UpdatedAt         time.Time     `json:"updated_at"`
	Stage             string        `json:"stage,omitempty"`               // Current stage of the migration process
	State             string        `json:"state,omitempty"`               // Current state within the stage
	StartedAt         time.Time     `json:"started_at,omitempty"`          // When the migration was started
	Duration          time.Duration `json:"duration_seconds,omitempty"`    // How long the migration took to complete
	MigrationID       string        `json:"migration_id,omitempty"`        // GitHub migration ID when available
	Progress          int           `json:"progress,omitempty"`            // Overall progress percentage (0-100)
	StageProgress     int           `json:"stage_progress,omitempty"`      // Progress percentage within current stage (0-100)
	CompletedStages   []string      `json:"completed_stages,omitempty"`    // List of completed stages
	TotalStages       int           `json:"total_stages,omitempty"`        // Total number of stages in the migration process
	CurrentStageIndex int           `json:"current_stage_index,omitempty"` // Index of current stage (1-based)
}

// MigrationStages defines the sequential stages in the migration process.
// These stages are processed in order during a repository migration.
var MigrationStages = []string{
	"validation", // Repository validation
	"setup",      // Migration setup and source creation
	"archive",    // Archive generation and export
	"migration",  // Repository migration to target
}

// Status constants for migration state.
const (
	StatusInProgress = "in_progress" // Migration is currently in progress
	StatusSucceeded  = "succeeded"   // Migration completed successfully
	StatusFailed     = "failed"      // Migration failed
)
