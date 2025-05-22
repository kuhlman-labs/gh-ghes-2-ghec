// Package e2e provides end-to-end tests for full migration scenarios
package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/test/helpers"
)

// TestE2EEnvironmentSetup tests that we can set up the basic E2E test environment
func TestE2EEnvironmentSetup(t *testing.T) {
	helpers.SkipIfShort(t, "end-to-end tests require external services")

	// Check if we have the required environment variables for integration tests
	ghesToken := os.Getenv("GHES_TOKEN")
	ghecToken := os.Getenv("GHEC_TOKEN")
	if ghesToken == "" || ghecToken == "" {
		t.Skip("GHES_TOKEN and GHEC_TOKEN environment variables required for E2E tests")
	}

	suite := helpers.NewTestSuite(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Setup test configuration
	configPath := suite.CreateTestConfig(map[string]interface{}{
		"github": map[string]interface{}{
			"ghes": map[string]interface{}{
				"base_url": getEnvOrDefault("GHES_BASE_URL", "https://github.example.com"),
				"token":    ghesToken,
			},
			"ghec": map[string]interface{}{
				"base_url": "https://api.github.com",
				"token":    ghecToken,
			},
		},
		"storage": map[string]interface{}{
			"enabled": true,
			"type":    "sqlite",
			"sqlite": map[string]interface{}{
				"path": ":memory:",
			},
		},
	})

	// Test configuration initialization
	require.FileExists(t, configPath, "Configuration file should exist")

	// Initialize logging
	err := logging.Init()
	require.NoError(t, err, "Failed to initialize logging")

	logger := logging.Get()
	require.NotNil(t, logger, "Logger should be initialized")

	// Test storage setup
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "e2e_test_",
		Timeout:          30,
	}

	storageProvider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage")
	defer storageProvider.Close()

	err = storageProvider.Initialize(ctx)
	require.NoError(t, err, "Failed to initialize storage schema")

	// Test basic storage operations
	testStatus := &payload.MigrationStatus{
		Repository: "test-org/e2e-test-repo",
		Status:     payload.StatusInProgress,
		Stage:      "validation",
		State:      "checking",
		Progress:   0,
		StartedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err = storageProvider.SaveMigrationStatus(ctx, testStatus)
	assert.NoError(t, err, "Should be able to save migration status")

	retrievedStatus, err := storageProvider.GetMigrationStatus(ctx, "test-org/e2e-test-repo")
	assert.NoError(t, err, "Should be able to retrieve migration status")
	assert.Equal(t, testStatus.Repository, retrievedStatus.Repository)

	t.Log("E2E environment setup completed successfully")
}

// TestE2ETestDataGeneration tests the generation of test data for E2E scenarios
func TestE2ETestDataGeneration(t *testing.T) {
	helpers.SkipIfShort(t, "end-to-end tests require external services")

	suite := helpers.NewTestSuite(t)

	// Test mock repository generation
	for i := 0; i < 5; i++ {
		mockRepo := helpers.GenerateMockRepository()
		assert.NotEmpty(t, mockRepo.Name, "Generated repository should have a name")
		assert.NotEmpty(t, mockRepo.FullName, "Generated repository should have a full name")
		t.Logf("Generated mock repository: %s (%s)", mockRepo.Name, mockRepo.Language)
	}

	// Test configuration variations
	testConfigs := []map[string]interface{}{
		{
			"test_scenario":    "simple_migration",
			"repositories":     []string{"simple-repo"},
			"delete_if_exists": false,
		},
		{
			"test_scenario":    "bulk_migration",
			"repositories":     []string{"repo1", "repo2", "repo3"},
			"delete_if_exists": true,
		},
		{
			"test_scenario":    "complex_migration",
			"repositories":     []string{"repo-with-lfs", "repo-with-issues"},
			"delete_if_exists": false,
		},
	}

	for _, testConfig := range testConfigs {
		configPath := suite.CreateTestConfig(testConfig)
		assert.FileExists(t, configPath, "Test configuration should be created")
		t.Logf("Created test configuration for scenario: %s", testConfig["test_scenario"])
	}
}

// TestE2EMigrationStatusFlow tests the complete migration status flow
func TestE2EMigrationStatusFlow(t *testing.T) {
	helpers.SkipIfShort(t, "end-to-end tests require external services")

	ctx := context.Background()

	// Setup storage
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "e2e_flow_",
		Timeout:          30,
	}

	storageProvider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage")
	defer storageProvider.Close()

	err = storageProvider.Initialize(ctx)
	require.NoError(t, err, "Failed to initialize storage schema")

	// Test complete migration status flow
	repoName := "test-org/status-flow-repo"
	startTime := time.Now()

	// Test progression through migration stages
	stages := []struct {
		status   string
		stage    string
		state    string
		progress int
	}{
		{payload.StatusInProgress, "validation", "checking_source", 5},
		{payload.StatusInProgress, "validation", "checking_target", 10},
		{payload.StatusInProgress, "setup", "creating_source", 20},
		{payload.StatusInProgress, "archive", "generating", 30},
		{payload.StatusInProgress, "archive", "waiting", 40},
		{payload.StatusInProgress, "archive", "ready", 50},
		{payload.StatusInProgress, "migration", "starting", 60},
		{payload.StatusInProgress, "migration", "in_progress", 80},
		{payload.StatusSucceeded, "migration", "completed", 100},
	}

	for i, stage := range stages {
		migrationStatus := &payload.MigrationStatus{
			Repository:        repoName,
			Status:            stage.status,
			Stage:             stage.stage,
			State:             stage.state,
			Progress:          stage.progress,
			StartedAt:         startTime,
			UpdatedAt:         time.Now(),
			CurrentStageIndex: i + 1,
			TotalStages:       len(payload.MigrationStages),
		}

		err = storageProvider.SaveMigrationStatus(ctx, migrationStatus)
		assert.NoError(t, err, "Should save migration status for stage: %s", stage.stage)

		// Verify status was saved correctly
		retrievedStatus, err := storageProvider.GetMigrationStatus(ctx, repoName)
		assert.NoError(t, err, "Should retrieve migration status for stage: %s", stage.stage)
		assert.Equal(t, stage.status, retrievedStatus.Status)
		assert.Equal(t, stage.stage, retrievedStatus.Stage)
		assert.Equal(t, stage.progress, retrievedStatus.Progress)

		t.Logf("Stage %d: %s/%s - Progress: %d%%", i+1, stage.stage, stage.state, stage.progress)
	}

	// Test final migration completion
	finalStatus, err := storageProvider.GetMigrationStatus(ctx, repoName)
	assert.NoError(t, err, "Should retrieve final migration status")
	assert.Equal(t, payload.StatusSucceeded, finalStatus.Status)
	assert.Equal(t, 100, finalStatus.Progress)

	// Test archiving the completed migration
	err = storageProvider.ArchiveMigrationAttempt(ctx, finalStatus)
	assert.NoError(t, err, "Should archive completed migration")

	// Verify archived migration
	archivedAttempts, err := storageProvider.GetArchivedMigrationAttempts(ctx, repoName)
	assert.NoError(t, err, "Should retrieve archived attempts")
	assert.Len(t, archivedAttempts, 1, "Should have one archived attempt")
	assert.Equal(t, payload.StatusSucceeded, archivedAttempts[0].Status)
}

// TestE2EFailureScenarios tests various failure scenarios in E2E context
func TestE2EFailureScenarios(t *testing.T) {
	helpers.SkipIfShort(t, "end-to-end tests require external services")

	ctx := context.Background()

	// Setup storage
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "e2e_failure_",
		Timeout:          30,
	}

	storageProvider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage")
	defer storageProvider.Close()

	err = storageProvider.Initialize(ctx)
	require.NoError(t, err, "Failed to initialize storage schema")

	failureScenarios := []struct {
		name         string
		repository   string
		failureStage string
		errorMessage string
	}{
		{
			name:         "ValidationFailure",
			repository:   "test-org/validation-fail-repo",
			failureStage: "validation",
			errorMessage: "Repository not found or access denied",
		},
		{
			name:         "ArchiveFailure",
			repository:   "test-org/archive-fail-repo",
			failureStage: "archive",
			errorMessage: "Archive generation failed due to size limits",
		},
		{
			name:         "MigrationFailure",
			repository:   "test-org/migration-fail-repo",
			failureStage: "migration",
			errorMessage: "Target repository already exists and delete_if_exists is false",
		},
	}

	for _, scenario := range failureScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Create failed migration status
			failedStatus := &payload.MigrationStatus{
				Repository: scenario.repository,
				Status:     payload.StatusFailed,
				Stage:      scenario.failureStage,
				State:      "failed",
				Error:      scenario.errorMessage,
				Progress:   50, // Failed mid-way
				StartedAt:  time.Now().Add(-5 * time.Minute),
				UpdatedAt:  time.Now(),
			}

			err = storageProvider.SaveMigrationStatus(ctx, failedStatus)
			assert.NoError(t, err, "Should save failed migration status")

			// Verify failure was recorded correctly
			retrievedStatus, err := storageProvider.GetMigrationStatus(ctx, scenario.repository)
			assert.NoError(t, err, "Should retrieve failed migration status")
			assert.Equal(t, payload.StatusFailed, retrievedStatus.Status)
			assert.Equal(t, scenario.failureStage, retrievedStatus.Stage)
			assert.Equal(t, scenario.errorMessage, retrievedStatus.Error)

			// Archive the failed attempt
			err = storageProvider.ArchiveMigrationAttempt(ctx, retrievedStatus)
			assert.NoError(t, err, "Should archive failed migration")

			t.Logf("Processed failure scenario: %s in stage %s", scenario.name, scenario.failureStage)
		})
	}
}

// Helper functions

func getEnvOrDefault(envVar, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}
