// Package github provides functionality for interacting with GitHub APIs.
package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// NoopAPI is a no-operation implementation of the GitHub API.
// It's useful for testing or when actual GitHub API operations are not needed.
// All methods return appropriate errors indicating they are not implemented.
type NoopAPI struct {
	logger *slog.Logger
}

// NewNoopAPI creates a new no-operation GitHub API with the provided logger.
func NewNoopAPI(logger *slog.Logger) API {
	return &NoopAPI{
		logger: logger,
	}
}

// ValidateRepository is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) ValidateRepository(ctx context.Context, org, repo string) error {
	n.logger.Warn("NoopAPI: ValidateRepository called but not implemented",
		"org", org,
		"repo", repo)
	return fmt.Errorf("ValidateRepository not implemented in NoopAPI")
}

// ValidateCloudRepository is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) ValidateCloudRepository(ctx context.Context, org, repo string) error {
	n.logger.Warn("NoopAPI: ValidateCloudRepository called but not implemented",
		"org", org,
		"repo", repo)
	return fmt.Errorf("ValidateCloudRepository not implemented in NoopAPI")
}

// CheckCloudRepositoryExists is a no-op implementation that logs the call and returns false.
func (n *NoopAPI) CheckCloudRepositoryExists(ctx context.Context, org, repo string) (bool, error) {
	n.logger.Warn("NoopAPI: CheckCloudRepositoryExists called but not implemented",
		"org", org,
		"repo", repo)
	return false, fmt.Errorf("CheckCloudRepositoryExists not implemented in NoopAPI")
}

// DeleteCloudRepositoryIfExists is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) DeleteCloudRepositoryIfExists(ctx context.Context, org, repo string) (bool, error) {
	n.logger.Warn("NoopAPI: DeleteCloudRepositoryIfExists called but not implemented",
		"org", org,
		"repo", repo)
	return false, fmt.Errorf("DeleteCloudRepositoryIfExists not implemented in NoopAPI")
}

// GetOrganizationID is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) GetOrganizationID(ctx context.Context, org string) (string, int64, error) {
	n.logger.Warn("NoopAPI: GetOrganizationID called but not implemented",
		"org", org)
	return "", 0, fmt.Errorf("GetOrganizationID not implemented in NoopAPI")
}

// CreateMigrationSource is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) CreateMigrationSource(ctx context.Context, name, url, ownerID string) (string, error) {
	n.logger.Warn("NoopAPI: CreateMigrationSource called but not implemented",
		"name", name,
		"url", url)
	return "", fmt.Errorf("CreateMigrationSource not implemented in NoopAPI")
}

// GenerateMigrationArchive is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error) {
	n.logger.Warn("NoopAPI: GenerateMigrationArchive called but not implemented",
		"orgName", orgName,
		"repoName", repoName)
	return 0, fmt.Errorf("GenerateMigrationArchive not implemented in NoopAPI")
}

// GetMigrationArchiveStatus is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error) {
	n.logger.Warn("NoopAPI: GetMigrationArchiveStatus called but not implemented",
		"migrationID", migrationID,
		"orgName", orgName)
	return "", fmt.Errorf("GetMigrationArchiveStatus not implemented in NoopAPI")
}

// GetMigrationArchiveURL is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error) {
	n.logger.Warn("NoopAPI: GetMigrationArchiveURL called but not implemented",
		"archiveID", archiveID,
		"orgName", orgName)
	return "", fmt.Errorf("GetMigrationArchiveURL not implemented in NoopAPI")
}

// StartRepositoryMigration is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, archiveURL, metadataURL, ghesToken, ghCloudToken string) (string, error) {
	n.logger.Warn("NoopAPI: StartRepositoryMigration called but not implemented",
		"repoName", repoName)
	return "", fmt.Errorf("StartRepositoryMigration not implemented in NoopAPI")
}

// GetMigrationStatus is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) GetMigrationStatus(ctx context.Context, migrationID string) (string, error) {
	n.logger.Warn("NoopAPI: GetMigrationStatus called but not implemented",
		"migrationID", migrationID)
	return "", fmt.Errorf("GetMigrationStatus not implemented in NoopAPI")
}

// UploadArchiveToGHOS is a no-op implementation that logs the call and returns an error.
func (n *NoopAPI) UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error) {
	n.logger.Warn("NoopAPI: UploadArchiveToGHOS called but not implemented",
		"archiveURL", archiveURL,
		"archiveName", archiveName)
	return "", fmt.Errorf("UploadArchiveToGHOS not implemented in NoopAPI")
}

// GetGHESRateLimit is a no-op implementation that returns a placeholder rate limit.
func (n *NoopAPI) GetGHESRateLimit(ctx context.Context) (*RateLimitInfo, error) {
	n.logger.Debug("NoopAPI: GetGHESRateLimit called, returning placeholder values")
	return &RateLimitInfo{
		Limit:     5000,
		Remaining: 4950,
		Reset:     time.Now().Add(1 * time.Hour),
		Used:      50,
	}, nil
}

// GetGHCloudRateLimit is a no-op implementation that returns a placeholder rate limit.
func (n *NoopAPI) GetGHCloudRateLimit(ctx context.Context) (*RateLimitInfo, error) {
	n.logger.Debug("NoopAPI: GetGHCloudRateLimit called, returning placeholder values")
	return &RateLimitInfo{
		Limit:     5000,
		Remaining: 4950,
		Reset:     time.Now().Add(1 * time.Hour),
		Used:      50,
	}, nil
}
