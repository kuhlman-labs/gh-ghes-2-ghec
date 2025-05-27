package helpers

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	// Import database drivers for testing
	_ "github.com/go-sql-driver/mysql" // MySQL driver
	_ "github.com/lib/pq"              // PostgreSQL driver
)

func TestSimpleContainerManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping container test in short mode")
	}

	// Set environment variable to skip database connection testing
	os.Setenv("SKIP_DB_CONNECTION_TEST", "true")
	defer os.Unsetenv("SKIP_DB_CONNECTION_TEST")

	t.Run("PostgresContainerLifecycle", func(t *testing.T) {
		// Create a test suite but don't use automatic cleanup
		suite := &TestSuite{
			t:          t,
			containers: make(map[string]testcontainers.Container),
			testID:     "test-simple",
		}

		// Load config manually
		config, err := LoadTestConfig()
		require.NoError(t, err)
		suite.config = config

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

		// Verify container no longer exists
		assert.False(t, suite.containerExists(ctx, container), "Container should no longer exist")
	})

	t.Run("ContainerExistsCheck", func(t *testing.T) {
		suite := &TestSuite{
			t:          t,
			containers: make(map[string]testcontainers.Container),
			testID:     "test-exists",
		}

		// Test with nil container
		exists := suite.containerExists(context.Background(), nil)
		assert.False(t, exists, "Nil container should not exist")
	})
}
