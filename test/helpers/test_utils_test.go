// Package helpers provides common utilities and helpers for testing
package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTestConfig(t *testing.T) {
	config, err := LoadTestConfig()
	require.NoError(t, err, "Should load test config successfully")
	require.NotNil(t, config, "Config should not be nil")

	// Verify some expected configuration values
	assert.Equal(t, 90.0, config.Unit.CoverageThreshold)
	assert.Equal(t, "5m", config.Unit.Timeout)
	assert.True(t, config.Unit.Parallel)
	assert.True(t, config.Unit.Short)

	assert.Equal(t, "15m", config.Integration.Timeout)
	assert.True(t, config.Integration.Database.SQLite.Enabled)
	assert.Equal(t, ":memory:", config.Integration.Database.SQLite.Path)
}

func TestGetProjectRoot(t *testing.T) {
	root := GetProjectRoot()
	assert.NotEmpty(t, root, "Project root should not be empty")
	t.Logf("Project root: %s", root)
}

func TestNewTestSuite(t *testing.T) {
	suite := NewTestSuite(t)
	require.NotNil(t, suite, "Test suite should not be nil")
	require.NotNil(t, suite.config, "Test suite config should not be nil")

	// Test cleanup works without panics
	assert.NotPanics(t, func() {
		suite.Cleanup()
	}, "Cleanup should not panic")
}

func TestGenerateMockRepository(t *testing.T) {
	repo := GenerateMockRepository()
	assert.NotEmpty(t, repo.Name, "Generated repository should have a name")
	assert.NotEmpty(t, repo.FullName, "Generated repository should have a full name")
	assert.NotEmpty(t, repo.Language, "Generated repository should have a language")

	t.Logf("Generated mock repository: Name=%s, FullName=%s, Language=%s",
		repo.Name, repo.FullName, repo.Language)
}

func TestCreateTempDir(t *testing.T) {
	suite := NewTestSuite(t)
	tempDir := suite.CreateTempDir()

	assert.NotEmpty(t, tempDir, "Temp directory should not be empty")
	t.Logf("Created temp directory: %s", tempDir)

	// Cleanup should be called automatically via t.Cleanup
}

func TestSkipIfShort(t *testing.T) {
	// This test should not skip since we're not running with -short
	assert.NotPanics(t, func() {
		SkipIfShort(t, "test reason")
	}, "SkipIfShort should not panic in normal tests")
}
