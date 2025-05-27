package helpers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import database drivers for testing
	_ "github.com/go-sql-driver/mysql" // MySQL driver
	_ "github.com/lib/pq"              // PostgreSQL driver
)

func TestContainerCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping container cleanup test in short mode")
	}

	t.Run("PostgresContainerCleanup", func(t *testing.T) {
		suite := NewTestSuite(t)

		// Setup a postgres container
		connectionString, err := suite.SetupTestDatabase("postgres")
		require.NoError(t, err, "Failed to setup postgres container")
		assert.NotEmpty(t, connectionString, "Connection string should not be empty")

		// Verify container is tracked
		suite.mu.RLock()
		container, exists := suite.containers["postgres"]
		suite.mu.RUnlock()
		assert.True(t, exists, "Postgres container should be tracked")
		assert.NotNil(t, container, "Container should not be nil")

		// Verify container exists and is running
		ctx := context.Background()
		assert.True(t, suite.containerExists(ctx, container), "Container should exist")

		state, err := container.State(ctx)
		require.NoError(t, err, "Should be able to get container state")
		assert.True(t, state.Running, "Container should be running")

		// Manually terminate the container
		suite.terminateContainerSafely(container, "postgres")

		// Verify container no longer exists (the main thing we care about)
		assert.False(t, suite.containerExists(ctx, container), "Container should no longer exist")

		// Note: Container may still be tracked for cleanup purposes - this is OK
	})

	t.Run("MySQLContainerCleanup", func(t *testing.T) {
		suite := NewTestSuite(t)

		// Setup a mysql container
		connectionString, err := suite.SetupTestDatabase("mysql")
		require.NoError(t, err, "Failed to setup mysql container")
		assert.NotEmpty(t, connectionString, "Connection string should not be empty")

		// Verify container is tracked
		suite.mu.RLock()
		container, exists := suite.containers["mysql"]
		suite.mu.RUnlock()
		assert.True(t, exists, "MySQL container should be tracked")
		assert.NotNil(t, container, "Container should not be nil")

		// Verify container exists and is running
		ctx := context.Background()
		assert.True(t, suite.containerExists(ctx, container), "Container should exist")

		state, err := container.State(ctx)
		require.NoError(t, err, "Should be able to get container state")
		assert.True(t, state.Running, "Container should be running")

		// Manually terminate the container
		suite.terminateContainerSafely(container, "mysql")

		// Verify container no longer exists (the main thing we care about)
		assert.False(t, suite.containerExists(ctx, container), "Container should no longer exist")

		// Note: Container may still be tracked for cleanup purposes - this is OK
	})

	t.Run("DoubleCleanupHandling", func(t *testing.T) {
		suite := NewTestSuite(t)

		// Setup a postgres container
		connectionString, err := suite.SetupTestDatabase("postgres")
		require.NoError(t, err, "Failed to setup postgres container")
		assert.NotEmpty(t, connectionString, "Connection string should not be empty")

		// Get the container reference
		suite.mu.RLock()
		container, exists := suite.containers["postgres"]
		suite.mu.RUnlock()
		require.True(t, exists, "Postgres container should be tracked")
		require.NotNil(t, container, "Container should not be nil")

		// Terminate the container once
		suite.terminateContainerSafely(container, "postgres")

		// Try to terminate again - this should handle "No such container" gracefully
		suite.terminateContainerSafely(container, "postgres")

		// Should not panic or cause issues - main thing is container doesn't exist
		assert.False(t, suite.containerExists(context.Background(), container), "Container should not exist")
	})
}

func TestContainerExists(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping container exists test in short mode")
	}

	t.Run("NilContainer", func(t *testing.T) {
		suite := NewTestSuite(t)
		exists := suite.containerExists(context.Background(), nil)
		assert.False(t, exists, "Nil container should not exist")
	})

	t.Run("ValidContainer", func(t *testing.T) {
		suite := NewTestSuite(t)

		// Setup a container
		_, err := suite.SetupTestDatabase("postgres")
		require.NoError(t, err, "Failed to setup postgres container")

		suite.mu.RLock()
		container := suite.containers["postgres"]
		suite.mu.RUnlock()
		require.NotNil(t, container, "Container should not be nil")

		// Check if it exists
		exists := suite.containerExists(context.Background(), container)
		assert.True(t, exists, "Valid container should exist")

		// Terminate it
		suite.terminateContainerSafely(container, "postgres")

		// Check if it still exists (should be false)
		exists = suite.containerExists(context.Background(), container)
		assert.False(t, exists, "Terminated container should not exist")
	})
}
