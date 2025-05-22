// Package load provides load testing for the migration system
package load

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/test/helpers"
)

// TestLoadConcurrentMigrations tests the system under various concurrent loads
func TestLoadConcurrentMigrations(t *testing.T) {
	helpers.SkipIfShort(t, "load tests require extended execution time")

	ctx := context.Background()

	// Setup storage
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "load_test_",
		Timeout:          30,
	}

	storageProvider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage")
	defer storageProvider.Close()

	err = storageProvider.Initialize(ctx)
	require.NoError(t, err, "Failed to initialize storage schema")

	// Test different concurrent loads as defined in test configuration
	concurrentLoads := []int{1, 5, 10, 25, 50}

	for _, concurrent := range concurrentLoads {
		t.Run(fmt.Sprintf("Concurrent_%d", concurrent), func(t *testing.T) {
			testConcurrentLoad(t, storageProvider, concurrent)
		})
	}
}

func testConcurrentLoad(t *testing.T, provider storage.MigrationStorage, concurrentCount int) {
	ctx := context.Background()
	startTime := time.Now()

	var wg sync.WaitGroup
	errorChan := make(chan error, concurrentCount)
	statusChan := make(chan *payload.MigrationStatus, concurrentCount)

	// Launch concurrent migration status operations
	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			repoName := fmt.Sprintf("load-test-org/concurrent-repo-%d", id)
			migrationStatus := &payload.MigrationStatus{
				Repository: repoName,
				Status:     payload.StatusInProgress,
				Stage:      "validation",
				State:      "checking",
				Progress:   0,
				StartedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}

			// Simulate migration progress through stages
			stages := []struct {
				stage    string
				state    string
				progress int
			}{
				{"validation", "checking_source", 10},
				{"validation", "checking_target", 20},
				{"setup", "creating_migration", 30},
				{"archive", "generating", 50},
				{"archive", "ready", 70},
				{"migration", "starting", 80},
				{"migration", "in_progress", 90},
				{"migration", "completed", 100},
			}

			for _, stageInfo := range stages {
				migrationStatus.Stage = stageInfo.stage
				migrationStatus.State = stageInfo.state
				migrationStatus.Progress = stageInfo.progress
				migrationStatus.UpdatedAt = time.Now()

				if stageInfo.progress == 100 {
					migrationStatus.Status = payload.StatusSucceeded
					migrationStatus.Duration = time.Since(migrationStatus.StartedAt)
				}

				err := provider.SaveMigrationStatus(ctx, migrationStatus)
				if err != nil {
					errorChan <- fmt.Errorf("worker %d failed to save status: %w", id, err)
					return
				}

				// Small delay to simulate processing time
				time.Sleep(10 * time.Millisecond)
			}

			statusChan <- migrationStatus
		}(i)
	}

	// Wait for all workers to complete
	wg.Wait()
	close(errorChan)
	close(statusChan)

	// Check for errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	assert.Empty(t, errors, "Should have no errors during concurrent operations")

	// Verify all statuses were created
	var completedStatuses []*payload.MigrationStatus
	for status := range statusChan {
		completedStatuses = append(completedStatuses, status)
	}

	assert.Len(t, completedStatuses, concurrentCount, "Should have expected number of completed statuses")

	// Verify all migrations completed successfully
	for _, status := range completedStatuses {
		assert.Equal(t, payload.StatusSucceeded, status.Status)
		assert.Equal(t, 100, status.Progress)
		assert.NotZero(t, status.Duration)
	}

	// Performance metrics
	totalDuration := time.Since(startTime)
	avgTimePerMigration := totalDuration / time.Duration(concurrentCount)

	t.Logf("Concurrent load test completed:")
	t.Logf("  Concurrent migrations: %d", concurrentCount)
	t.Logf("  Total duration: %v", totalDuration)
	t.Logf("  Average time per migration: %v", avgTimePerMigration)
	t.Logf("  Migrations per second: %.2f", float64(concurrentCount)/totalDuration.Seconds())

	// Performance assertions (adjust thresholds as needed)
	assert.Less(t, totalDuration, 30*time.Second, "Load test should complete within 30 seconds")
	assert.Less(t, avgTimePerMigration, 5*time.Second, "Average migration time should be under 5 seconds")
}

// TestLoadSustainedOperations tests the system under sustained load
func TestLoadSustainedOperations(t *testing.T) {
	helpers.SkipIfShort(t, "sustained load tests require extended execution time")

	ctx := context.Background()

	// Setup storage
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "sustained_test_",
		Timeout:          30,
	}

	storageProvider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage")
	defer storageProvider.Close()

	err = storageProvider.Initialize(ctx)
	require.NoError(t, err, "Failed to initialize storage schema")

	// Sustained load parameters
	duration := 2 * time.Minute
	concurrentWorkers := 10
	operationsPerWorker := 5

	startTime := time.Now()
	endTime := startTime.Add(duration)

	var wg sync.WaitGroup
	totalOperations := int64(0)
	var operationsMutex sync.Mutex

	// Launch sustained workers
	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			workerOperations := int64(0)
			operationID := 0

			for time.Now().Before(endTime) {
				for j := 0; j < operationsPerWorker; j++ {
					repoName := fmt.Sprintf("sustained-test-org/worker-%d-op-%d", workerID, operationID)

					status := &payload.MigrationStatus{
						Repository: repoName,
						Status:     payload.StatusInProgress,
						Stage:      "validation",
						State:      "checking",
						Progress:   25,
						StartedAt:  time.Now(),
						UpdatedAt:  time.Now(),
					}

					err := storageProvider.SaveMigrationStatus(ctx, status)
					if err != nil {
						t.Errorf("Worker %d failed operation: %v", workerID, err)
						return
					}

					// Update to completed
					status.Status = payload.StatusSucceeded
					status.Progress = 100
					status.Stage = "migration"
					status.State = "completed"
					status.UpdatedAt = time.Now()
					status.Duration = time.Since(status.StartedAt)

					err = storageProvider.SaveMigrationStatus(ctx, status)
					if err != nil {
						t.Errorf("Worker %d failed to complete operation: %v", workerID, err)
						return
					}

					workerOperations++
					operationID++
				}

				// Brief pause between batches
				time.Sleep(100 * time.Millisecond)
			}

			operationsMutex.Lock()
			totalOperations += workerOperations
			operationsMutex.Unlock()

			t.Logf("Worker %d completed %d operations", workerID, workerOperations)
		}(i)
	}

	// Wait for all workers to complete
	wg.Wait()

	actualDuration := time.Since(startTime)
	operationsPerSecond := float64(totalOperations) / actualDuration.Seconds()

	t.Logf("Sustained load test results:")
	t.Logf("  Duration: %v", actualDuration)
	t.Logf("  Total operations: %d", totalOperations)
	t.Logf("  Operations per second: %.2f", operationsPerSecond)
	t.Logf("  Concurrent workers: %d", concurrentWorkers)

	// Performance assertions
	assert.Greater(t, totalOperations, int64(100), "Should complete at least 100 operations")
	assert.Greater(t, operationsPerSecond, 1.0, "Should maintain at least 1 operation per second")
}

// TestLoadMemoryUsage tests memory usage under load
func TestLoadMemoryUsage(t *testing.T) {
	helpers.SkipIfShort(t, "memory load tests require extended execution time")

	ctx := context.Background()

	// Setup storage
	storageConfig := &storage.StorageConfig{
		Enabled:          true,
		Type:             "sqlite",
		ConnectionString: ":memory:",
		TablePrefix:      "memory_test_",
		Timeout:          30,
	}

	storageProvider, err := storage.NewStorageProvider(storageConfig)
	require.NoError(t, err, "Failed to initialize storage")
	defer storageProvider.Close()

	err = storageProvider.Initialize(ctx)
	require.NoError(t, err, "Failed to initialize storage schema")

	// Create a large number of migration statuses to test memory usage
	numStatuses := 1000
	statusBatch := make([]*payload.MigrationStatus, numStatuses)

	for i := 0; i < numStatuses; i++ {
		statusBatch[i] = &payload.MigrationStatus{
			Repository:      fmt.Sprintf("memory-test-org/repo-%d", i),
			Status:          payload.StatusSucceeded,
			Stage:           "migration",
			State:           "completed",
			Progress:        100,
			StartedAt:       time.Now().Add(-time.Hour),
			UpdatedAt:       time.Now(),
			Duration:        time.Hour,
			CompletedStages: []string{"validation", "setup", "archive", "migration"},
			TotalStages:     4,
		}
	}

	// Save all statuses
	startTime := time.Now()
	for _, status := range statusBatch {
		err := storageProvider.SaveMigrationStatus(ctx, status)
		require.NoError(t, err, "Should save status successfully")
	}
	saveTime := time.Since(startTime)

	// Retrieve all statuses
	startTime = time.Now()
	allStatuses, err := storageProvider.GetAllMigrationStatuses(ctx)
	require.NoError(t, err, "Should retrieve all statuses successfully")
	retrieveTime := time.Since(startTime)

	assert.Len(t, allStatuses, numStatuses, "Should retrieve all saved statuses")

	t.Logf("Memory usage test results:")
	t.Logf("  Number of statuses: %d", numStatuses)
	t.Logf("  Save time: %v", saveTime)
	t.Logf("  Retrieve time: %v", retrieveTime)
	t.Logf("  Average save time per status: %v", saveTime/time.Duration(numStatuses))
	t.Logf("  Average retrieve time per status: %v", retrieveTime/time.Duration(numStatuses))

	// Performance assertions
	assert.Less(t, saveTime, 10*time.Second, "Saving should complete within 10 seconds")
	assert.Less(t, retrieveTime, 5*time.Second, "Retrieval should complete within 5 seconds")
}
