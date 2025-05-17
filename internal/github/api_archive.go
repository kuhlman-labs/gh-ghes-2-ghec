package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/go-github/v70/github"
)

// GenerateMigrationArchive generates a migration archive for a repository on GHES
func (a *GitHubAPI) GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error) {
	repos := []string{repoName}
	opts := &github.MigrationOptions{
		LockRepositories: false,
	}

	a.logger.Debug("Generating migration archive",
		"api", "GHES_REST",
		"method", "Migrations.StartMigration",
		"org", orgName,
		"repo", repoName,
	)

	startTime := time.Now()

	var archive *github.Migration
	var respStatus int

	err := a.circuitProtectedGhesOperation(ctx, "generate_migration_archive", func() error {
		var resp *github.Response
		var err error
		archive, resp, err = a.clients.GHESClient.Migrations.StartMigration(ctx, orgName, repos, opts)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to create migration archive",
			"api", "GHES_REST",
			"method", "Migrations.StartMigration",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"error", err,
			"org", orgName,
			"repo", repoName,
		)
		return 0, fmt.Errorf("failed to create migration archive: %w", err)
	}

	archiveID := archive.GetID()
	a.logger.Debug("Migration archive created",
		"api", "GHES_REST",
		"method", "Migrations.StartMigration",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"archive_id", archiveID,
		"org", orgName,
		"repo", repoName,
	)

	return archiveID, nil
}

// GetMigrationArchiveStatus gets the status of a migration archive export on GHES
func (a *GitHubAPI) GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error) {
	a.logger.Debug("Getting archive status",
		"api", "GHES_REST",
		"method", "Migrations.MigrationStatus",
		"migrationID", migrationID,
		"org", orgName,
	)

	startTime := time.Now()

	var status *github.Migration
	var respStatus int

	err := a.circuitProtectedGhesOperation(ctx, "get_migration_archive_status", func() error {
		var resp *github.Response
		var err error
		status, resp, err = a.clients.GHESClient.Migrations.MigrationStatus(ctx, orgName, migrationID)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get archive status",
			"api", "GHES_REST",
			"method", "Migrations.MigrationStatus",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"error", err,
			"migrationID", migrationID,
			"org", orgName,
		)
		return "", fmt.Errorf("failed to get migration archive status: %w", err)
	}

	state := *status.State

	// Log additional details for failed migrations
	if state == "failed" {
		a.logger.Error("Archive export failed",
			"api", "GHES_REST",
			"method", "Migrations.MigrationStatus",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"migrationID", migrationID,
			"org", orgName,
			"state", state,
		)
	}

	a.logger.Debug("Archive status retrieved",
		"api", "GHES_REST",
		"method", "Migrations.MigrationStatus",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"migrationID", migrationID,
		"org", orgName,
		"state", state,
	)

	return state, nil
}

// GetMigrationArchiveURL gets the archive URL of a migration source
func (a *GitHubAPI) GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error) {
	a.logger.Debug("Getting migration archive URL",
		"api", "GHES_REST",
		"method", "Migrations.MigrationArchiveURL",
		"migrationId", archiveID,
		"org", orgName,
	)

	startTime := time.Now()

	var archiveURL string

	err := a.circuitProtectedGhesOperation(ctx, "get_migration_archive_url", func() error {
		var err error
		archiveURL, err = a.clients.GHESClient.Migrations.MigrationArchiveURL(ctx, orgName, archiveID)
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get archive URL",
			"api", "GHES_REST",
			"method", "Migrations.MigrationArchiveURL",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"archiveId", archiveID,
			"org", orgName,
		)
		return "", fmt.Errorf("failed to create request for migration archive URL: %w", err)
	}

	a.logger.Debug("Archive URL retrieved",
		"api", "GHES_REST",
		"method", "Migrations.MigrationArchiveURL",
		"duration_ms", duration.Milliseconds(),
		"archiveId", archiveID,
		"org", orgName,
	)

	return archiveURL, nil
}

// UploadArchiveToGHOS uploads a migration archive to GitHub Owned Storage
// This is used when customers select Local Storage (GHOS) instead of Azure or S3
// It performs a chunked upload for all archives.
func (a *GitHubAPI) UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error) {
	// Log the start of the upload to GHOS
	a.logger.Info("Starting archive upload to GitHub Owned Storage",
		"api", "GHOS_Upload",
		"database_id", databaseID,
		"archive_name", archiveName,
	)

	startTime := time.Now()

	// Create a client for downloading the archive
	client := &http.Client{
		Timeout: 120 * time.Minute, // Long timeout for potentially large files
	}

	// Create circuit-protected HTTP clients for GHES and GitHub Cloud
	executeGhesRequest := a.circuitProtectedGhesHTTP(client, "ghos_download")
	executeGhCloudRequest := a.circuitProtectedGhCloudHTTP(client, "ghos_upload")

	// Download the archive from GHES
	a.logger.Debug("Downloading migration archive from GHES",
		"url", archiveURL,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for archive download: %w", err)
	}

	resp, err := executeGhesRequest(req)
	if err != nil {
		return "", fmt.Errorf("failed to download archive from GHES: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close response body", "error", err)
		}
	}()

	// Get the total size of the archive
	totalSize := resp.ContentLength
	if totalSize == -1 {
		return "", fmt.Errorf("could not determine archive size")
	}

	// Step 1: Initialize multipart upload
	a.logger.Debug("Initializing multipart upload",
		"database_id", databaseID,
		"archive_size", totalSize,
	)

	uploadURL := fmt.Sprintf("https://uploads.github.com/organizations/%d/gei/archive/blobs/uploads", databaseID)

	// Prepare JSON body exactly as shown in the Ruby example
	initBody := map[string]interface{}{
		"content_type": "application/octet-stream",
		"name":         archiveName,
		"size":         totalSize,
	}

	initBodyBytes, err := json.Marshal(initBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal initialization body: %w", err)
	}

	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(initBodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create init request: %w", err)
	}

	initReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
	initReq.Header.Set("Content-Type", "application/json")

	// Make this request reusable for retries
	initReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(initBodyBytes)), nil
	}

	initResp, err := executeGhCloudRequest(initReq)
	if err != nil {
		return "", fmt.Errorf("failed to initialize multipart upload: %w", err)
	}
	defer func() {
		if err := initResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close init response body", "error", err)
		}
	}()

	// Get the location header from the response for the next part upload
	nextPath := initResp.Header.Get("Location")
	if nextPath == "" {
		return "", fmt.Errorf("no location header found in initialization response")
	}

	// Extract the GUID from the location header
	// The path should contain guid=<guid> as a parameter
	locationURL, err := url.Parse(nextPath)
	if err != nil {
		return "", fmt.Errorf("failed to parse location header: %w", err)
	}

	query := locationURL.Query()
	guid := query.Get("guid")
	if guid == "" {
		return "", fmt.Errorf("no guid found in location header")
	}

	a.logger.Debug("Multipart upload initialized",
		"guid", guid,
		"next_path", nextPath,
	)

	// Create a buffer for reading the archive in chunks
	// GitHub recommends 100 MiB chunks
	const partSize = 100 * 1024 * 1024 // 100 MiB
	buffer := make([]byte, partSize)

	// Calculate total number of parts
	numParts := (totalSize + partSize - 1) / partSize

	// Track the last path for completing the upload
	var lastPath string

	// Step 2-3: Upload parts
	for partNumber := int64(1); partNumber <= numParts; partNumber++ {
		// Save the current path as the last path before getting a new one
		lastPath = nextPath

		// Read the next chunk from the archive
		bytesRead, err := io.ReadFull(resp.Body, buffer)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return "", fmt.Errorf("failed to read part %d: %w", partNumber, err)
		}

		// If we didn't read anything, we're done
		if bytesRead == 0 {
			break
		}

		// Upload the part
		a.logger.Debug("Uploading part",
			"part_number", partNumber,
			"total_parts", numParts,
			"bytes", bytesRead,
		)

		partURL := fmt.Sprintf("https://uploads.github.com%s", nextPath)
		partData := buffer[:bytesRead]
		partReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, partURL, bytes.NewReader(partData))
		if err != nil {
			return "", fmt.Errorf("failed to create part request: %w", err)
		}

		partReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
		partReq.Header.Set("Content-Type", "application/octet-stream")

		// Make this request reusable for retries
		partReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(partData)), nil
		}

		partResp, err := executeGhCloudRequest(partReq)
		if err != nil {
			return "", fmt.Errorf("failed to upload part %d: %w", partNumber, err)
		}

		// Get the next path from the response
		nextPath = partResp.Header.Get("Location")
		if nextPath == "" && partNumber < numParts {
			if err := partResp.Body.Close(); err != nil {
				a.logger.Warn("Failed to close part response body", "error", err)
			}
			return "", fmt.Errorf("no location header found in part %d response", partNumber)
		}

		if err := partResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close part response body", "error", err)
		}

		// Log progress
		progress := float64(partNumber) / float64(numParts) * 100
		a.logger.Debug("Upload progress",
			"part", partNumber,
			"total_parts", numParts,
			"progress", fmt.Sprintf("%.1f%%", progress),
		)
	}

	// Step 4: Complete the multipart upload
	a.logger.Debug("Completing multipart upload",
		"guid", guid,
	)

	completeURL := fmt.Sprintf("https://uploads.github.com%s", lastPath)
	completeReq, err := http.NewRequestWithContext(ctx, http.MethodPut, completeURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create complete request: %w", err)
	}

	completeReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ghCloudToken))
	completeReq.Header.Set("Content-Type", "application/octet-stream")

	completeResp, err := executeGhCloudRequest(completeReq)
	if err != nil {
		return "", fmt.Errorf("failed to complete upload: %w", err)
	}
	defer func() {
		if err := completeResp.Body.Close(); err != nil {
			a.logger.Warn("Failed to close complete response body", "error", err)
		}
	}()

	// Construct the GEI URI from the GUID
	geiURI := fmt.Sprintf("gei://archive/%s", guid)

	duration := time.Since(startTime)

	a.logger.Info("Successfully uploaded archive to GitHub Owned Storage",
		"api", "GHOS_Upload",
		"duration_ms", duration.Milliseconds(),
		"uri", geiURI,
		"guid", guid,
		"size", totalSize,
	)

	return geiURI, nil
}
