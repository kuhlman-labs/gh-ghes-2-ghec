// Package integration provides integration tests for database interactions
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/test/helpers"
)

// TestDatabaseIntegration tests database operations across different database types
func TestDatabaseIntegration(t *testing.T) {
	helpers.SkipIfShort(t, "integration tests require database setup")

	suite := helpers.NewTestSuite(t)

	tests := []struct {
		name   string
		dbType string
	}{
		{"SQLite", "sqlite"},
		{"PostgreSQL", "postgres"},
		{"MySQL", "mysql"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDatabaseOperations(t, suite, tt.dbType)
		})
	}
}

func testDatabaseOperations(t *testing.T, suite *helpers.TestSuite, dbType string) {
	var connectionString string
	var err error

	switch dbType {
	case "sqlite":
		connectionString = ":memory:"
	case "postgres", "mysql":
		connectionString, err = suite.SetupTestDatabase(dbType)
		require.NoError(t, err, "Failed to setup test database")
	}

	// Create storage configuration
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             dbType,
		ConnectionString: connectionString,
		TablePrefix:      "test_",
		Timeout:          30,
	}

	// Initialize storage provider
	storageProvider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage provider")
	defer func() {
		if err := storageProvider.Close(); err != nil {
			t.Logf("Failed to close storage provider: %v", err)
		}
	}()

	// Test schema initialization
	t.Run("SchemaOperations", func(t *testing.T) {
		testSchemaOperations(t, storageProvider)
	})

	// Test migration status operations
	t.Run("MigrationStatusOperations", func(t *testing.T) {
		testMigrationStatusOperations(t, storageProvider)
	})

	// Test concurrent operations
	t.Run("ConcurrentOperations", func(t *testing.T) {
		testConcurrentOperations(t, storageProvider)
	})

	// Test archive operations
	t.Run("ArchiveOperations", func(t *testing.T) {
		testArchiveOperations(t, storageProvider)
	})
}

func testSchemaOperations(t *testing.T, provider storage.MigrationStorage) {
	ctx := context.Background()

	// Test schema creation
	err := provider.Initialize(ctx)
	assert.NoError(t, err, "Schema initialization should succeed")

	// Test schema recreation (should be idempotent)
	err = provider.Initialize(ctx)
	assert.NoError(t, err, "Schema reinitialization should succeed")

	// Test database check and repair
	result, err := provider.CheckAndRepairDatabase(ctx)
	assert.NoError(t, err, "Database check should succeed")
	assert.NotEmpty(t, result, "Check result should not be empty")
}

func testMigrationStatusOperations(t *testing.T, provider storage.MigrationStorage) {
	ctx := context.Background()

	// Create test migration statuses
	statuses := []*payload.MigrationStatus{
		{
			Repository: "test-org/repo1",
			Status:     payload.StatusInProgress,
			Stage:      "validation",
			State:      "checking",
			Progress:   25,
			StartedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
		{
			Repository: "test-org/repo2",
			Status:     payload.StatusSucceeded,
			Stage:      "complete",
			State:      "finished",
			Progress:   100,
			StartedAt:  time.Now().Add(-1 * time.Hour),
			UpdatedAt:  time.Now().Add(-30 * time.Minute),
			Duration:   30 * time.Minute,
		},
	}

	// Test status creation
	for _, status := range statuses {
		err := provider.SaveMigrationStatus(ctx, status)
		assert.NoError(t, err, "Saving migration status should succeed")
	}

	// Test status retrieval
	retrievedStatus, err := provider.GetMigrationStatus(ctx, "test-org/repo1")
	assert.NoError(t, err, "Retrieving migration status should succeed")
	assert.Equal(t, "test-org/repo1", retrievedStatus.Repository)
	assert.Equal(t, payload.StatusInProgress, retrievedStatus.Status)

	// Test status update
	retrievedStatus.Status = payload.StatusSucceeded
	retrievedStatus.Progress = 100
	err = provider.SaveMigrationStatus(ctx, retrievedStatus)
	assert.NoError(t, err, "Updating migration status should succeed")

	updatedStatus, err := provider.GetMigrationStatus(ctx, "test-org/repo1")
	assert.NoError(t, err, "Retrieving updated status should succeed")
	assert.Equal(t, payload.StatusSucceeded, updatedStatus.Status)
	assert.Equal(t, 100, updatedStatus.Progress)

	// Test listing all statuses
	allStatuses, err := provider.GetAllMigrationStatuses(ctx)
	assert.NoError(t, err, "Listing all migration statuses should succeed")
	assert.Len(t, allStatuses, 2, "Should have 2 migration statuses")

	// Test status deletion
	err = provider.DeleteMigrationStatus(ctx, "test-org/repo1")
	assert.NoError(t, err, "Deleting migration status should succeed")

	// Verify status is deleted (should return nil, nil for not found)
	deletedStatus, err := provider.GetMigrationStatus(ctx, "test-org/repo1")
	assert.NoError(t, err, "Retrieving deleted status should not return an error")
	assert.Nil(t, deletedStatus, "Deleted status should be nil")
}

func testConcurrentOperations(t *testing.T, provider storage.MigrationStorage) {
	ctx := context.Background()

	const numGoroutines = 10
	const numOperationsPerGoroutine = 5

	// Test concurrent status creation
	errChan := make(chan error, numGoroutines*numOperationsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			for j := 0; j < numOperationsPerGoroutine; j++ {
				status := &payload.MigrationStatus{
					Repository: fmt.Sprintf("test-org/concurrent-repo-%d-%d", goroutineID, j),
					Status:     payload.StatusInProgress,
					Stage:      "validation",
					State:      "checking",
					Progress:   0,
					StartedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}

				err := provider.SaveMigrationStatus(ctx, status)
				errChan <- err
			}
		}(i)
	}

	// Collect all errors
	for i := 0; i < numGoroutines*numOperationsPerGoroutine; i++ {
		err := <-errChan
		assert.NoError(t, err, "Concurrent status creation should succeed")
	}

	// Verify all statuses were created
	allStatuses, err := provider.GetAllMigrationStatuses(ctx)
	assert.NoError(t, err, "Listing statuses after concurrent creation should succeed")
	assert.GreaterOrEqual(t, len(allStatuses), numGoroutines*numOperationsPerGoroutine,
		"Should have at least the expected number of statuses")
}

func testArchiveOperations(t *testing.T, provider storage.MigrationStorage) {
	ctx := context.Background()

	// Create test migration attempt
	attempt := &payload.MigrationStatus{
		Repository: "test-org/archive-repo",
		Status:     payload.StatusSucceeded,
		Stage:      "complete",
		State:      "finished",
		Progress:   100,
		StartedAt:  time.Now().Add(-2 * time.Hour),
		UpdatedAt:  time.Now().Add(-1 * time.Hour),
		Duration:   time.Hour,
		Error:      "",
	}

	// Archive the completed migration attempt
	err := provider.ArchiveMigrationAttempt(ctx, attempt)
	assert.NoError(t, err, "Archiving migration attempt should succeed")

	// Retrieve archived attempts
	archivedAttempts, err := provider.GetArchivedMigrationAttempts(ctx, "test-org/archive-repo")
	assert.NoError(t, err, "Retrieving archived attempts should succeed")
	assert.Len(t, archivedAttempts, 1, "Should have 1 archived attempt")
	assert.Equal(t, "test-org/archive-repo", archivedAttempts[0].Repository)
	assert.Equal(t, payload.StatusSucceeded, archivedAttempts[0].Status)

	// Archive another attempt for the same repository
	attempt2 := &payload.MigrationStatus{
		Repository: "test-org/archive-repo",
		Status:     payload.StatusFailed,
		Stage:      "archive",
		State:      "failed",
		Progress:   75,
		StartedAt:  time.Now().Add(-4 * time.Hour),
		UpdatedAt:  time.Now().Add(-3 * time.Hour),
		Duration:   time.Hour,
		Error:      "Test error",
	}

	err = provider.ArchiveMigrationAttempt(ctx, attempt2)
	assert.NoError(t, err, "Archiving second attempt should succeed")

	// Verify multiple archived attempts
	allArchivedAttempts, err := provider.GetArchivedMigrationAttempts(ctx, "test-org/archive-repo")
	assert.NoError(t, err, "Retrieving all archived attempts should succeed")
	assert.Len(t, allArchivedAttempts, 2, "Should have 2 archived attempts")
}

// TestStorageProviderCreation tests storage provider creation with different configurations
func TestStorageProviderCreation(t *testing.T) {
	tests := []struct {
		name     string
		config   *storage.StorageConfig
		wantType string
	}{
		{
			name: "Disabled Storage",
			config: &storage.StorageConfig{
				Enabled: false,
			},
			wantType: "*storage.NoopStorage",
		},
		{
			name: "SQLite Storage",
			config: &storage.StorageConfig{
				Enabled:          true,
				Type:             "sqlite",
				ConnectionString: ":memory:",
			},
			wantType: "SQLite",
		},
		{
			name: "Default to SQLite",
			config: &storage.StorageConfig{
				Enabled:          true,
				Type:             "unknown",
				ConnectionString: ":memory:",
			},
			wantType: "SQLite",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := storage.NewStorageProvider(tt.config)
			assert.NoError(t, err, "Creating storage provider should succeed")
			assert.NotNil(t, provider, "Provider should not be nil")
			defer func() {
				if err := provider.Close(); err != nil {
					t.Logf("Failed to close storage provider: %v", err)
				}
			}()

			// Test basic operations
			ctx := context.Background()
			err = provider.Initialize(ctx)
			assert.NoError(t, err, "Initializing provider should succeed")
		})
	}
}

// TestDatabaseFailureRecovery tests how the storage handles various failure scenarios
func TestDatabaseFailureRecovery(t *testing.T) {
	helpers.SkipIfShort(t, "integration tests require database setup")

	// Test with SQLite for simpler failure simulation
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "test_",
		Timeout:          1, // Short timeout to simulate failures
	}

	provider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage provider")
	defer func() {
		if err := provider.Close(); err != nil {
			t.Logf("Failed to close storage provider: %v", err)
		}
	}()

	ctx := context.Background()

	// Initialize schema
	err = provider.Initialize(ctx)
	require.NoError(t, err, "Schema initialization should succeed")

	// Test recovery after various operations
	t.Run("RecoveryAfterOperations", func(t *testing.T) {
		// Create a status
		status := &payload.MigrationStatus{
			Repository: "test-org/recovery-repo",
			Status:     payload.StatusInProgress,
			Stage:      "validation",
			State:      "checking",
			Progress:   50,
			StartedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		err := provider.SaveMigrationStatus(ctx, status)
		assert.NoError(t, err, "Saving status should succeed")

		// Test database check and repair
		result, err := provider.CheckAndRepairDatabase(ctx)
		assert.NoError(t, err, "Database check should succeed after operations")
		assert.NotEmpty(t, result, "Check result should contain information")

		// Verify data integrity after check
		retrievedStatus, err := provider.GetMigrationStatus(ctx, "test-org/recovery-repo")
		assert.NoError(t, err, "Data should be accessible after database check")
		assert.Equal(t, status.Repository, retrievedStatus.Repository)
	})
}
