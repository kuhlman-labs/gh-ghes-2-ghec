package payload

import (
	"fmt"
)

// MigrationRequest represents a request to migrate repositories
type MigrationRequest struct {
	SourceOrg    string   `json:"source_org"`
	TargetOrg    string   `json:"target_org"`
	Repositories []string `json:"repositories"`
	GHESAPIURL   string   `json:"ghes_api_url"`
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
	if r.GHESAPIURL == "" {
		return fmt.Errorf("ghes_api_url is required")
	}
	return nil
}

// MigrationStatus represents the status of a repository migration
type MigrationStatus struct {
	Repository string `json:"repository"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

const (
	StatusInProgress = "in_progress"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
)
