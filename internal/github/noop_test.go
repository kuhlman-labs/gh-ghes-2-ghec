package github

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewNoopAPI(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)

	if api == nil {
		t.Fatal("NewNoopAPI should return a non-nil API")
	}

	// Type assertion to check it's actually a NoopAPI
	if _, ok := api.(*NoopAPI); !ok {
		t.Error("NewNoopAPI should return a *NoopAPI")
	}
}

func TestNoopAPI_ValidateRepository(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	err := api.ValidateRepository(ctx, "testorg", "testrepo")
	if err == nil {
		t.Error("ValidateRepository should return an error")
	}

	expectedErrorMsg := "ValidateRepository not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_ValidateCloudRepository(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	err := api.ValidateCloudRepository(ctx, "testorg", "testrepo")
	if err == nil {
		t.Error("ValidateCloudRepository should return an error")
	}

	expectedErrorMsg := "ValidateCloudRepository not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_CheckCloudRepositoryExists(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	exists, err := api.CheckCloudRepositoryExists(ctx, "testorg", "testrepo")
	if err == nil {
		t.Error("CheckCloudRepositoryExists should return an error")
	}

	if exists {
		t.Error("CheckCloudRepositoryExists should return false")
	}

	expectedErrorMsg := "CheckCloudRepositoryExists not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_DeleteCloudRepositoryIfExists(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	deleted, err := api.DeleteCloudRepositoryIfExists(ctx, "testorg", "testrepo")
	if err == nil {
		t.Error("DeleteCloudRepositoryIfExists should return an error")
	}

	if deleted {
		t.Error("DeleteCloudRepositoryIfExists should return false")
	}

	expectedErrorMsg := "DeleteCloudRepositoryIfExists not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_GetOrganizationID(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	orgName, orgID, err := api.GetOrganizationID(ctx, "testorg")
	if err == nil {
		t.Error("GetOrganizationID should return an error")
	}

	if orgName != "" {
		t.Error("GetOrganizationID should return empty string for org name")
	}

	if orgID != 0 {
		t.Error("GetOrganizationID should return 0 for org ID")
	}

	expectedErrorMsg := "GetOrganizationID not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_CreateMigrationSource(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	sourceID, err := api.CreateMigrationSource(ctx, "test-source", "https://example.com", "owner123")
	if err == nil {
		t.Error("CreateMigrationSource should return an error")
	}

	if sourceID != "" {
		t.Error("CreateMigrationSource should return empty string")
	}

	expectedErrorMsg := "CreateMigrationSource not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_GenerateMigrationArchive(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	migrationID, err := api.GenerateMigrationArchive(ctx, "testorg", "testrepo")
	if err == nil {
		t.Error("GenerateMigrationArchive should return an error")
	}

	if migrationID != 0 {
		t.Error("GenerateMigrationArchive should return 0")
	}

	expectedErrorMsg := "GenerateMigrationArchive not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_GetMigrationArchiveStatus(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	status, err := api.GetMigrationArchiveStatus(ctx, 123, "testorg")
	if err == nil {
		t.Error("GetMigrationArchiveStatus should return an error")
	}

	if status != "" {
		t.Error("GetMigrationArchiveStatus should return empty string")
	}

	expectedErrorMsg := "GetMigrationArchiveStatus not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_GetMigrationArchiveURL(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	url, err := api.GetMigrationArchiveURL(ctx, 123, "testorg")
	if err == nil {
		t.Error("GetMigrationArchiveURL should return an error")
	}

	if url != "" {
		t.Error("GetMigrationArchiveURL should return empty string")
	}

	expectedErrorMsg := "GetMigrationArchiveURL not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_StartRepositoryMigration(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	migrationID, err := api.StartRepositoryMigration(ctx, "src123", "owner456", "testrepo",
		"https://source.com", "https://archive.com", "https://metadata.com", "token1", "token2")
	if err == nil {
		t.Error("StartRepositoryMigration should return an error")
	}

	if migrationID != "" {
		t.Error("StartRepositoryMigration should return empty string")
	}

	expectedErrorMsg := "StartRepositoryMigration not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_GetMigrationStatus(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	status, err := api.GetMigrationStatus(ctx, "migration123")
	if err == nil {
		t.Error("GetMigrationStatus should return an error")
	}

	if status != "" {
		t.Error("GetMigrationStatus should return empty string")
	}

	expectedErrorMsg := "GetMigrationStatus not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_UploadArchiveToGHOS(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	uploadID, err := api.UploadArchiveToGHOS(ctx, 123, "https://archive.com", "archive.tar.gz", "token")
	if err == nil {
		t.Error("UploadArchiveToGHOS should return an error")
	}

	if uploadID != "" {
		t.Error("UploadArchiveToGHOS should return empty string")
	}

	expectedErrorMsg := "UploadArchiveToGHOS not implemented in NoopAPI"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error message to contain '%s', got: %s", expectedErrorMsg, err.Error())
	}
}

func TestNoopAPI_GetGHESRateLimit(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	rateLimit, err := api.GetGHESRateLimit(ctx)
	if err != nil {
		t.Errorf("GetGHESRateLimit should not return an error, got: %v", err)
	}

	if rateLimit == nil {
		t.Error("GetGHESRateLimit should return a non-nil rate limit")
	}

	if rateLimit.Limit != 5000 {
		t.Errorf("Expected rate limit to be 5000, got: %d", rateLimit.Limit)
	}

	if rateLimit.Remaining != 4950 {
		t.Errorf("Expected remaining to be 4950, got: %d", rateLimit.Remaining)
	}

	if rateLimit.Used != 50 {
		t.Errorf("Expected used to be 50, got: %d", rateLimit.Used)
	}

	if rateLimit.Reset.Before(time.Now()) {
		t.Error("Reset time should be in the future")
	}
}

func TestNoopAPI_GetGHCloudRateLimit(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	rateLimit, err := api.GetGHCloudRateLimit(ctx)
	if err != nil {
		t.Errorf("GetGHCloudRateLimit should not return an error, got: %v", err)
	}

	if rateLimit == nil {
		t.Error("GetGHCloudRateLimit should return a non-nil rate limit")
	}

	if rateLimit.Limit != 5000 {
		t.Errorf("Expected rate limit to be 5000, got: %d", rateLimit.Limit)
	}

	if rateLimit.Remaining != 4950 {
		t.Errorf("Expected remaining to be 4950, got: %d", rateLimit.Remaining)
	}

	if rateLimit.Used != 50 {
		t.Errorf("Expected used to be 50, got: %d", rateLimit.Used)
	}

	if rateLimit.Reset.Before(time.Now()) {
		t.Error("Reset time should be in the future")
	}
}

func TestNoopAPI_GetRepositorySize(t *testing.T) {
	logger := slog.Default()
	api := NewNoopAPI(logger)
	ctx := context.Background()

	size, err := api.GetRepositorySize(ctx, "testorg", "testrepo")
	if err != nil {
		t.Errorf("GetRepositorySize should not return an error, got: %v", err)
	}

	expectedSize := int64(50 * 1024 * 1024) // 50MB
	if size != expectedSize {
		t.Errorf("Expected repository size to be %d, got: %d", expectedSize, size)
	}
}
