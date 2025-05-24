package migrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
)

// MockGitHubAPI implements the github.API interface for testing
type MockGitHubAPI struct {
	// Control flags for different behaviors
	ValidateRepositoryError          error
	CheckCloudRepositoryExistsResult bool
	CheckCloudRepositoryExistsError  error
	DeleteCloudRepositoryResult      bool
	DeleteCloudRepositoryError       error
	GetOrganizationIDResult          GetOrgIDResult
	CreateMigrationSourceResult      CreateSourceResult
	GenerateMigrationArchiveResult   GenerateArchiveResult
	GetMigrationArchiveStatusResults []string // Queue of statuses to return
	GetMigrationArchiveStatusErrors  []error  // Queue of errors to return
	GetMigrationArchiveURLResult     GetArchiveURLResult
	StartRepositoryMigrationResult   StartMigrationResult
	GetMigrationStatusResults        []string // Queue of statuses to return
	GetMigrationStatusErrors         []error  // Queue of errors to return
	UploadArchiveToGHOSResult        UploadGHOSResult
	GetRepositorySizeResult          GetRepoSizeResult

	// Tracking calls for verification
	ValidateRepositoryCalls            []ValidateRepoCall
	CheckCloudRepositoryExistsCalls    []CheckRepoCall
	DeleteCloudRepositoryIfExistsCalls []DeleteRepoCall
	GetOrganizationIDCalls             []string
	CreateMigrationSourceCalls         []CreateMigrationSourceCall
	GenerateMigrationArchiveCalls      []GenerateArchiveCall
	GetMigrationArchiveStatusCalls     []GetArchiveStatusCall
	GetMigrationArchiveURLCalls        []GetArchiveURLCall
	StartRepositoryMigrationCalls      []StartMigrationCall
	GetMigrationStatusCalls            []string
	UploadArchiveToGHOSCalls           []UploadGHOSCall
	GetRepositorySizeCalls             []GetRepoSizeCall

	// Counters for queue operations
	archiveStatusCallCount   int
	migrationStatusCallCount int

	mu sync.Mutex
}

// Result types for cleaner mock handling
type GetOrgIDResult struct {
	OwnerID    string
	DatabaseID int64
	Error      error
}

type CreateSourceResult struct {
	SourceID string
	Error    error
}

type GenerateArchiveResult struct {
	ArchiveID int64
	Error     error
}

type GetArchiveURLResult struct {
	URL   string
	Error error
}

type StartMigrationResult struct {
	MigrationID string
	Error       error
}

type UploadGHOSResult struct {
	URI   string
	Error error
}

type GetRepoSizeResult struct {
	Size  int64
	Error error
}

// Call tracking structs
type ValidateRepoCall struct {
	Org  string
	Repo string
}

type CheckRepoCall struct {
	Org  string
	Repo string
}

type DeleteRepoCall struct {
	Org  string
	Repo string
}

type CreateMigrationSourceCall struct {
	Name    string
	URL     string
	OwnerID string
}

type GenerateArchiveCall struct {
	OrgName  string
	RepoName string
}

type GetArchiveStatusCall struct {
	MigrationID int64
	OrgName     string
}

type GetArchiveURLCall struct {
	ArchiveID int64
	OrgName   string
}

type StartMigrationCall struct {
	SourceID     string
	OwnerID      string
	RepoName     string
	SourceURL    string
	ArchiveURL   string
	MetadataURL  string
	GHESToken    string
	GHCloudToken string
}

type UploadGHOSCall struct {
	DatabaseID   int64
	ArchiveURL   string
	ArchiveName  string
	GHCloudToken string
}

type GetRepoSizeCall struct {
	Org  string
	Repo string
}

// Mock implementations
func (m *MockGitHubAPI) ValidateRepository(ctx context.Context, org, repo string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ValidateRepositoryCalls = append(m.ValidateRepositoryCalls, ValidateRepoCall{Org: org, Repo: repo})
	return m.ValidateRepositoryError
}

func (m *MockGitHubAPI) ValidateCloudRepository(ctx context.Context, org, repo string) error {
	return nil // Not used in repository.go
}

func (m *MockGitHubAPI) CheckCloudRepositoryExists(ctx context.Context, org, repo string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CheckCloudRepositoryExistsCalls = append(m.CheckCloudRepositoryExistsCalls, CheckRepoCall{Org: org, Repo: repo})
	return m.CheckCloudRepositoryExistsResult, m.CheckCloudRepositoryExistsError
}

func (m *MockGitHubAPI) DeleteCloudRepositoryIfExists(ctx context.Context, org, repo string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeleteCloudRepositoryIfExistsCalls = append(m.DeleteCloudRepositoryIfExistsCalls, DeleteRepoCall{Org: org, Repo: repo})
	return m.DeleteCloudRepositoryResult, m.DeleteCloudRepositoryError
}

func (m *MockGitHubAPI) GetOrganizationID(ctx context.Context, org string) (string, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetOrganizationIDCalls = append(m.GetOrganizationIDCalls, org)
	result := m.GetOrganizationIDResult
	return result.OwnerID, result.DatabaseID, result.Error
}

func (m *MockGitHubAPI) CreateMigrationSource(ctx context.Context, name, url, ownerID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateMigrationSourceCalls = append(m.CreateMigrationSourceCalls, CreateMigrationSourceCall{
		Name: name, URL: url, OwnerID: ownerID,
	})
	result := m.CreateMigrationSourceResult
	return result.SourceID, result.Error
}

func (m *MockGitHubAPI) GenerateMigrationArchive(ctx context.Context, orgName, repoName string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GenerateMigrationArchiveCalls = append(m.GenerateMigrationArchiveCalls, GenerateArchiveCall{
		OrgName: orgName, RepoName: repoName,
	})
	result := m.GenerateMigrationArchiveResult
	return result.ArchiveID, result.Error
}

func (m *MockGitHubAPI) GetMigrationArchiveStatus(ctx context.Context, migrationID int64, orgName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetMigrationArchiveStatusCalls = append(m.GetMigrationArchiveStatusCalls, GetArchiveStatusCall{
		MigrationID: migrationID, OrgName: orgName,
	})

	// Check if we have an error for this call
	var err error
	if m.archiveStatusCallCount < len(m.GetMigrationArchiveStatusErrors) {
		err = m.GetMigrationArchiveStatusErrors[m.archiveStatusCallCount]
	}

	// Return queued responses
	if m.archiveStatusCallCount < len(m.GetMigrationArchiveStatusResults) {
		status := m.GetMigrationArchiveStatusResults[m.archiveStatusCallCount]
		m.archiveStatusCallCount++
		return status, err
	}

	// If we have an error but no status, return the error
	if err != nil {
		m.archiveStatusCallCount++
		return "", err
	}

	return "exported", nil // Default to success
}

func (m *MockGitHubAPI) GetMigrationArchiveURL(ctx context.Context, archiveID int64, orgName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetMigrationArchiveURLCalls = append(m.GetMigrationArchiveURLCalls, GetArchiveURLCall{
		ArchiveID: archiveID, OrgName: orgName,
	})
	result := m.GetMigrationArchiveURLResult
	return result.URL, result.Error
}

func (m *MockGitHubAPI) StartRepositoryMigration(ctx context.Context, sourceID, ownerID, repoName, sourceRepoURL, archiveURL, metadataURL, ghesToken, ghCloudToken string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StartRepositoryMigrationCalls = append(m.StartRepositoryMigrationCalls, StartMigrationCall{
		SourceID: sourceID, OwnerID: ownerID, RepoName: repoName, SourceURL: sourceRepoURL,
		ArchiveURL: archiveURL, MetadataURL: metadataURL, GHESToken: ghesToken, GHCloudToken: ghCloudToken,
	})
	result := m.StartRepositoryMigrationResult
	return result.MigrationID, result.Error
}

func (m *MockGitHubAPI) GetMigrationStatus(ctx context.Context, migrationID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetMigrationStatusCalls = append(m.GetMigrationStatusCalls, migrationID)

	// Return queued responses
	if m.migrationStatusCallCount < len(m.GetMigrationStatusResults) {
		status := m.GetMigrationStatusResults[m.migrationStatusCallCount]
		var err error
		if m.migrationStatusCallCount < len(m.GetMigrationStatusErrors) {
			err = m.GetMigrationStatusErrors[m.migrationStatusCallCount]
		}
		m.migrationStatusCallCount++
		return status, err
	}
	return "SUCCEEDED", nil // Default to success
}

func (m *MockGitHubAPI) UploadArchiveToGHOS(ctx context.Context, databaseID int64, archiveURL, archiveName, ghCloudToken string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UploadArchiveToGHOSCalls = append(m.UploadArchiveToGHOSCalls, UploadGHOSCall{
		DatabaseID: databaseID, ArchiveURL: archiveURL, ArchiveName: archiveName, GHCloudToken: ghCloudToken,
	})
	result := m.UploadArchiveToGHOSResult
	return result.URI, result.Error
}

func (m *MockGitHubAPI) GetGHESRateLimit(ctx context.Context) (*github.RateLimitInfo, error) {
	return nil, nil // Not used in repository.go
}

func (m *MockGitHubAPI) GetGHCloudRateLimit(ctx context.Context) (*github.RateLimitInfo, error) {
	return nil, nil // Not used in repository.go
}

func (m *MockGitHubAPI) GetRepositorySize(ctx context.Context, org, repo string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetRepositorySizeCalls = append(m.GetRepositorySizeCalls, GetRepoSizeCall{Org: org, Repo: repo})
	result := m.GetRepositorySizeResult
	return result.Size, result.Error
}

func (m *MockGitHubAPI) ValidateGHESOrganization(ctx context.Context, org string) error {
	return nil // Not used in repository.go
}

func (m *MockGitHubAPI) ValidateGHCloudOrganization(ctx context.Context, org string) error {
	return nil // Not used in repository.go
}

func (m *MockGitHubAPI) ListOrganizationRepositories(ctx context.Context, org string) ([]github.Repository, error) {
	return nil, nil // Not used in repository.go
}

// IsTestImplementation returns true since MockGitHubAPI is a test implementation
func (m *MockGitHubAPI) IsTestImplementation() bool {
	return true
}

// Helper function to create a test migrator with mocked dependencies
func createTestMigrator() (*Migrator, *MockGitHubAPI, *storage.NoopStorage) {
	mockAPI := &MockGitHubAPI{
		GetOrganizationIDResult: GetOrgIDResult{
			OwnerID:    "owner-123",
			DatabaseID: 123,
			Error:      nil,
		},
		CreateMigrationSourceResult: CreateSourceResult{
			SourceID: "source-123",
			Error:    nil,
		},
		GenerateMigrationArchiveResult: GenerateArchiveResult{
			ArchiveID: 456,
			Error:     nil,
		},
		GetMigrationArchiveURLResult: GetArchiveURLResult{
			URL:   "https://example.com/archive.tar.gz",
			Error: nil,
		},
		StartRepositoryMigrationResult: StartMigrationResult{
			MigrationID: "migration-789",
			Error:       nil,
		},
		UploadArchiveToGHOSResult: UploadGHOSResult{
			URI:   "ghos-uri-123",
			Error: nil,
		},
		GetRepositorySizeResult: GetRepoSizeResult{
			Size:  1048576, // 1MB
			Error: nil,
		},
	}

	noopStorage := &storage.NoopStorage{}

	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Proxy: config.ProxyConfig{
				Enabled: false,
			},
		},
		Storage: config.StorageConfig{
			Enabled: false,
		},
		Queue: config.QueueConfig{
			Enabled: false,
		},
	}

	migrator := &Migrator{
		logger:        slog.Default(),
		githubAPI:     mockAPI,
		storage:       noopStorage,
		migrations:    make(map[string]*payload.MigrationStatus),
		mu:            sync.RWMutex{},
		webhookURL:    "",
		config:        cfg,
		traceProvider: nil,
	}

	return migrator, mockAPI, noopStorage
}

// Helper function to create a test migration request
func createTestMigrationRequest() *payload.MigrationRequest {
	return &payload.MigrationRequest{
		SourceOrg:      "source-org",
		TargetOrg:      "target-org",
		Repositories:   []string{"test-repo"},
		GHESToken:      "ghp_123456789012345678901234567890123456",
		GHCloudToken:   "ghp_123456789012345678901234567890123456",
		GHESBaseURL:    "https://ghes.example.com",
		DeleteIfExists: false,
		UseGHOS:        false,
	}
}

func TestMigrator_migrateRepository_Success(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up successful responses
	mockAPI.GetMigrationArchiveStatusResults = []string{"exported"}
	mockAPI.GetMigrationStatusResults = []string{"SUCCEEDED"}

	err := migrator.migrateRepository(
		context.Background(),
		req,
		"test-repo",
		"source-org/test-repo",
		attemptStartTime,
	)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify API calls were made
	if len(mockAPI.ValidateRepositoryCalls) != 1 {
		t.Errorf("Expected 1 ValidateRepository call, got %d", len(mockAPI.ValidateRepositoryCalls))
	}

	if len(mockAPI.CheckCloudRepositoryExistsCalls) != 2 { // Once in prepare, once in start
		t.Errorf("Expected 2 CheckCloudRepositoryExists calls, got %d", len(mockAPI.CheckCloudRepositoryExistsCalls))
	}

	if len(mockAPI.GenerateMigrationArchiveCalls) != 1 {
		t.Errorf("Expected 1 GenerateMigrationArchive call, got %d", len(mockAPI.GenerateMigrationArchiveCalls))
	}

	if len(mockAPI.StartRepositoryMigrationCalls) != 1 {
		t.Errorf("Expected 1 StartRepositoryMigration call, got %d", len(mockAPI.StartRepositoryMigrationCalls))
	}

	// Check final status
	status := migrator.GetMigrationStatus("source-org/test-repo")
	if status == nil {
		t.Error("Expected migration status to exist")
	} else if status.Status != payload.StatusSucceeded {
		t.Errorf("Expected status %s, got %s", payload.StatusSucceeded, status.Status)
	}
}

func TestMigrator_migrateRepository_WithGHOS(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	req.UseGHOS = true
	attemptStartTime := time.Now()

	// Set up successful responses
	mockAPI.GetMigrationArchiveStatusResults = []string{"exported"}
	mockAPI.GetMigrationStatusResults = []string{"SUCCEEDED"}

	err := migrator.migrateRepository(
		context.Background(),
		req,
		"test-repo",
		"source-org/test-repo",
		attemptStartTime,
	)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify GHOS upload was called
	if len(mockAPI.UploadArchiveToGHOSCalls) != 1 {
		t.Errorf("Expected 1 UploadArchiveToGHOS call, got %d", len(mockAPI.UploadArchiveToGHOSCalls))
	}

	// Verify the upload call had correct parameters
	if len(mockAPI.UploadArchiveToGHOSCalls) > 0 {
		uploadCall := mockAPI.UploadArchiveToGHOSCalls[0]
		if uploadCall.DatabaseID != 123 {
			t.Errorf("Expected database ID 123, got %d", uploadCall.DatabaseID)
		}
		if uploadCall.ArchiveName != "test-repo" {
			t.Errorf("Expected archive name 'test-repo', got %s", uploadCall.ArchiveName)
		}
	}
}

func TestMigrator_migrateRepository_WithDeleteIfExists(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	req.DeleteIfExists = true
	attemptStartTime := time.Now()

	// Set up responses - repository exists but should be deleted
	mockAPI.CheckCloudRepositoryExistsResult = true
	mockAPI.DeleteCloudRepositoryResult = true
	mockAPI.GetMigrationArchiveStatusResults = []string{"exported"}
	mockAPI.GetMigrationStatusResults = []string{"SUCCEEDED"}

	err := migrator.migrateRepository(
		context.Background(),
		req,
		"test-repo",
		"source-org/test-repo",
		attemptStartTime,
	)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify repository deletion was attempted
	if len(mockAPI.DeleteCloudRepositoryIfExistsCalls) != 2 { // Once in prepare, once in start
		t.Errorf("Expected 2 DeleteCloudRepositoryIfExists calls, got %d", len(mockAPI.DeleteCloudRepositoryIfExistsCalls))
	}
}

func TestMigrator_prepareForMigration_SourceRepositoryNotFound(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up error for source repository validation
	mockAPI.ValidateRepositoryError = errors.New("repository not found")

	err := migrator.prepareForMigration(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for source repository not found")
	}

	if !errors.Is(err, mockAPI.ValidateRepositoryError) {
		t.Errorf("Expected wrapped validation error, got %v", err)
	}

	// Check that status was updated to failed
	status := migrator.GetMigrationStatus("source-org/test-repo")
	if status == nil {
		t.Error("Expected migration status to exist")
	} else if status.Status != payload.StatusFailed {
		t.Errorf("Expected status %s, got %s", payload.StatusFailed, status.Status)
	}
}

func TestMigrator_prepareForMigration_RepositoryConflict(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	req.DeleteIfExists = false // Don't delete if exists
	attemptStartTime := time.Now()

	// Set up responses - repository exists in target but delete not allowed
	mockAPI.CheckCloudRepositoryExistsResult = true

	err := migrator.prepareForMigration(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for repository conflict")
	}

	expectedError := "repository conflict"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}

	// Check that status was updated to failed
	status := migrator.GetMigrationStatus("source-org/test-repo")
	if status == nil {
		t.Error("Expected migration status to exist")
	} else if status.Status != payload.StatusFailed {
		t.Errorf("Expected status %s, got %s", payload.StatusFailed, status.Status)
	}
}

func TestMigrator_prepareForMigration_DeleteFailure(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	req.DeleteIfExists = true
	attemptStartTime := time.Now()

	// Set up responses - repository exists but deletion fails
	mockAPI.CheckCloudRepositoryExistsResult = true
	mockAPI.DeleteCloudRepositoryError = errors.New("deletion failed")

	err := migrator.prepareForMigration(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for deletion failure")
	}

	expectedError := "failed to delete existing repository"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}
}

func TestMigrator_prepareForMigration_MigrationSourceCreationConflict(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up conflict error in migration source creation
	conflictErr := &apierrors.ClassifiedError{
		Category: apierrors.CategoryResourceConflict,
		Err:      errors.New("Repository already exists"),
	}
	mockAPI.CreateMigrationSourceResult = CreateSourceResult{
		SourceID: "",
		Error:    conflictErr,
	}

	err := migrator.prepareForMigration(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for migration source creation conflict")
	}

	expectedError := "repository conflict"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}
}

func TestMigrator_processArchive_ArchiveGenerationFailure(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up error for archive generation
	mockAPI.GenerateMigrationArchiveResult = GenerateArchiveResult{
		ArchiveID: 0,
		Error:     errors.New("archive generation failed"),
	}

	_, _, err := migrator.processArchive(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for archive generation failure")
	}

	expectedError := "failed to generate migration archive"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}
}

func TestMigrator_processArchive_ArchiveExportStatusFailure(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up error for archive status check
	mockAPI.GetMigrationArchiveStatusResults = []string{}
	mockAPI.GetMigrationArchiveStatusErrors = []error{errors.New("status check failed")}

	_, _, err := migrator.processArchive(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for archive status check failure")
	}

	expectedError := "failed to get archive export status"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}
}

func TestMigrator_processArchive_ArchiveExportFailed(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up failed archive export
	mockAPI.GetMigrationArchiveStatusResults = []string{"failed"}

	_, _, err := migrator.processArchive(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for failed archive export")
	}

	expectedError := "migration archive export failed with state: failed"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}
}

func TestMigrator_processArchive_ArchiveURLRetrievalFailure(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up successful export but URL retrieval failure
	mockAPI.GetMigrationArchiveStatusResults = []string{"exported"}
	mockAPI.GetMigrationArchiveURLResult = GetArchiveURLResult{
		URL:   "",
		Error: errors.New("URL retrieval failed"),
	}

	_, _, err := migrator.processArchive(
		context.Background(),
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for archive URL retrieval failure")
	}

	expectedError := "failed to get archive URL"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}
}

func TestMigrator_processArchive_ProgressiveStatus(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Set up progressive status changes: pending -> exporting -> exported
	mockAPI.GetMigrationArchiveStatusResults = []string{"pending", "exporting", "exported"}
	mockAPI.GetMigrationStatusResults = []string{"SUCCEEDED"}

	// Use a context with timeout to prevent infinite loop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	migrationID, archiveID, err := migrator.processArchive(
		ctx,
		mockAPI,
		req,
		"test-repo",
		attemptStartTime,
	)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if migrationID == "" {
		t.Error("Expected migration ID to be returned")
	}

	if archiveID == 0 {
		t.Error("Expected archive ID to be returned")
	}

	// Verify status was checked multiple times
	if len(mockAPI.GetMigrationArchiveStatusCalls) < 3 {
		t.Errorf("Expected at least 3 status checks, got %d", len(mockAPI.GetMigrationArchiveStatusCalls))
	}
}

func TestMigrator_monitorMigration_Success(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	attemptStartTime := time.Now()

	// Set up successful migration status
	mockAPI.GetMigrationStatusResults = []string{"SUCCEEDED"}

	err := migrator.monitorMigration(
		context.Background(),
		mockAPI,
		"migration-789",
		"test-repo",
		"source-org/test-repo",
		attemptStartTime,
	)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Check final status
	status := migrator.GetMigrationStatus("source-org/test-repo")
	if status == nil {
		t.Error("Expected migration status to exist")
	} else if status.Status != payload.StatusSucceeded {
		t.Errorf("Expected status %s, got %s", payload.StatusSucceeded, status.Status)
	}
}

func TestMigrator_monitorMigration_Failure(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	attemptStartTime := time.Now()

	// Set up failed migration status
	mockAPI.GetMigrationStatusResults = []string{"FAILED"}

	err := migrator.monitorMigration(
		context.Background(),
		mockAPI,
		"migration-789",
		"test-repo",
		"source-org/test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for failed migration")
	}

	expectedError := "migration failed with state: FAILED"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error containing %q, got %v", expectedError, err)
	}

	// Check final status
	status := migrator.GetMigrationStatus("source-org/test-repo")
	if status == nil {
		t.Error("Expected migration status to exist")
	} else if status.Status != payload.StatusFailed {
		t.Errorf("Expected status %s, got %s", payload.StatusFailed, status.Status)
	}
}

func TestMigrator_monitorMigration_ProgressiveStatus(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	attemptStartTime := time.Now()

	// Reset the mock's counter to ensure we start from the beginning of the queue
	mockAPI.migrationStatusCallCount = 0

	// Set up progressive status changes: PENDING -> IN_PROGRESS -> SUCCEEDED
	mockAPI.GetMigrationStatusResults = []string{"PENDING", "IN_PROGRESS", "SUCCEEDED"}

	// Use a context with timeout to prevent infinite loop - but extend it to allow for polling
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := migrator.monitorMigration(
		ctx,
		mockAPI,
		"migration-789",
		"test-repo",
		"source-org/test-repo",
		attemptStartTime,
	)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify status was checked multiple times
	if len(mockAPI.GetMigrationStatusCalls) < 3 {
		t.Errorf("Expected at least 3 status checks, got %d", len(mockAPI.GetMigrationStatusCalls))
	}

	// Check final status
	status := migrator.GetMigrationStatus("source-org/test-repo")
	if status == nil {
		t.Error("Expected migration status to exist")
	} else if status.Status != payload.StatusSucceeded {
		t.Errorf("Expected status %s, got %s", payload.StatusSucceeded, status.Status)
	}
}

func TestMigrator_monitorMigration_ContextCancellation(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	attemptStartTime := time.Now()

	// Set up long-running status that would loop
	mockAPI.GetMigrationStatusResults = []string{"PENDING", "PENDING", "PENDING"}

	// Create a context that gets cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := migrator.monitorMigration(
		ctx,
		mockAPI,
		"migration-789",
		"test-repo",
		"source-org/test-repo",
		attemptStartTime,
	)

	if err == nil {
		t.Error("Expected error for context cancellation")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context deadline exceeded error, got %v", err)
	}

	// Check that status was updated to failed
	status := migrator.GetMigrationStatus("source-org/test-repo")
	if status == nil {
		t.Error("Expected migration status to exist")
	} else if status.Status != payload.StatusFailed {
		t.Errorf("Expected status %s, got %s", payload.StatusFailed, status.Status)
	}
}

func TestMigrator_prepareForMigration_RepositorySizeHandling(t *testing.T) {
	migrator, mockAPI, _ := createTestMigrator()
	req := createTestMigrationRequest()
	attemptStartTime := time.Now()

	// Test both successful and failed repository size retrieval
	tests := []struct {
		name        string
		sizeResult  GetRepoSizeResult
		expectError bool
	}{
		{
			name: "successful size retrieval",
			sizeResult: GetRepoSizeResult{
				Size:  1048576, // 1MB
				Error: nil,
			},
			expectError: false,
		},
		{
			name: "failed size retrieval",
			sizeResult: GetRepoSizeResult{
				Size:  0,
				Error: errors.New("size retrieval failed"),
			},
			expectError: false, // Should not fail the overall process
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock
			mockAPI.GetRepositorySizeCalls = nil
			mockAPI.GetRepositorySizeResult = tt.sizeResult

			// Clear previous status
			migrator.mu.Lock()
			delete(migrator.migrations, "source-org/test-repo")
			migrator.mu.Unlock()

			err := migrator.prepareForMigration(
				context.Background(),
				mockAPI,
				req,
				"test-repo",
				attemptStartTime,
			)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			// Verify size retrieval was attempted
			if len(mockAPI.GetRepositorySizeCalls) != 1 {
				t.Errorf("Expected 1 GetRepositorySize call, got %d", len(mockAPI.GetRepositorySizeCalls))
			}

			// Check if size was stored (only for successful retrieval)
			if tt.sizeResult.Error == nil {
				status := migrator.GetMigrationStatus("source-org/test-repo")
				if status != nil && status.RepositorySize != tt.sizeResult.Size {
					t.Errorf("Expected repository size %d, got %d", tt.sizeResult.Size, status.RepositorySize)
				}
			}
		})
	}
}

// Test the internal updateStatus method behavior through public methods
func TestMigrator_StatusUpdates(t *testing.T) {
	migrator, _, _ := createTestMigrator()
	repoFullName := "source-org/test-repo"
	attemptStartTime := time.Now()

	// Test status update
	migrator.updateStatus(repoFullName, payload.StatusInProgress, "test message", time.Now(), attemptStartTime)

	status := migrator.GetMigrationStatus(repoFullName)
	if status == nil {
		t.Error("Expected migration status to exist")
		return
	}

	if status.Status != payload.StatusInProgress {
		t.Errorf("Expected status %s, got %s", payload.StatusInProgress, status.Status)
	}

	if status.Repository != repoFullName {
		t.Errorf("Expected repository %s, got %s", repoFullName, status.Repository)
	}

	if status.Error != "test message" {
		t.Errorf("Expected error message 'test message', got %s", status.Error)
	}
}

// Test edge cases and error conditions
func TestMigrator_EdgeCases(t *testing.T) {
	t.Run("empty repository name", func(t *testing.T) {
		migrator, _, _ := createTestMigrator()
		req := createTestMigrationRequest()
		attemptStartTime := time.Now()

		err := migrator.migrateRepository(
			context.Background(),
			req,
			"", // empty repo name
			"source-org/",
			attemptStartTime,
		)

		// Should handle gracefully - though the implementation may vary
		// At minimum, should not panic
		_ = err // Test doesn't specify expected behavior for empty names
	})

	t.Run("nil migration request", func(t *testing.T) {
		migrator, _, _ := createTestMigrator()
		attemptStartTime := time.Now()

		// This will likely panic in the real implementation, but we test anyway
		defer func() {
			if r := recover(); r != nil {
				// Expected panic due to nil request
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		err := migrator.migrateRepository(
			context.Background(),
			nil, // nil request
			"test-repo",
			"source-org/test-repo",
			attemptStartTime,
		)

		// Should handle gracefully
		if err == nil {
			t.Error("Expected error for nil migration request")
		}
	})
}

// Test concurrent access to migration status
func TestMigrator_ConcurrentAccess(t *testing.T) {
	migrator, _, _ := createTestMigrator()
	repoFullName := "source-org/test-repo"
	attemptStartTime := time.Now()

	// Start multiple goroutines updating status
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			message := fmt.Sprintf("concurrent update %d", i)
			migrator.updateStatus(repoFullName, payload.StatusInProgress, message, time.Now(), attemptStartTime)
		}(i)
	}

	// Start multiple goroutines reading status
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := migrator.GetMigrationStatus(repoFullName)
			_ = status // Just ensure no panic
		}()
	}

	wg.Wait()

	// Verify final state is consistent
	status := migrator.GetMigrationStatus(repoFullName)
	if status == nil {
		t.Error("Expected migration status to exist after concurrent updates")
	}
}
