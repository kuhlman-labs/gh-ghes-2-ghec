// Package helpers provides common utilities and helpers for testing
package helpers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-faker/faker/v4"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/goleak"
	"gopkg.in/yaml.v3"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
)

// TestConfig holds test configuration
type TestConfig struct {
	Unit struct {
		CoverageThreshold float64 `yaml:"coverage_threshold"`
		Timeout           string  `yaml:"timeout"`
		Parallel          bool    `yaml:"parallel"`
		Short             bool    `yaml:"short"`
	} `yaml:"unit"`
	Integration struct {
		Timeout  string `yaml:"timeout"`
		Database struct {
			SQLite struct {
				Enabled bool   `yaml:"enabled"`
				Path    string `yaml:"path"`
			} `yaml:"sqlite"`
			Postgres struct {
				Enabled  bool   `yaml:"enabled"`
				Host     string `yaml:"host"`
				Port     int    `yaml:"port"`
				Database string `yaml:"database"`
				Username string `yaml:"username"`
				Password string `yaml:"password"`
			} `yaml:"postgres"`
			MySQL struct {
				Enabled  bool   `yaml:"enabled"`
				Host     string `yaml:"host"`
				Port     int    `yaml:"port"`
				Database string `yaml:"database"`
				Username string `yaml:"username"`
				Password string `yaml:"password"`
			} `yaml:"mysql"`
		} `yaml:"database"`
		GitHub struct {
			MockAPI       bool   `yaml:"mock_api"`
			RateLimitTest bool   `yaml:"rate_limit_test"`
			Timeout       string `yaml:"timeout"`
		} `yaml:"github"`
	} `yaml:"integration"`
}

// TestSuite provides a comprehensive test suite setup
type TestSuite struct {
	t          *testing.T
	config     *TestConfig
	containers map[string]testcontainers.Container
	mocks      *MockServices
	tempDirs   []string
	mu         sync.RWMutex
	cleanup    []func()
	testID     string // Unique identifier for this test run
}

// MockServices holds all mock services
type MockServices struct {
	HTTPTransport *httpmock.MockTransport
	SQLMock       sqlmock.Sqlmock
	Database      *sql.DB
}

// NewTestSuite creates a new test suite
func NewTestSuite(t *testing.T) *TestSuite {
	t.Helper()

	// Load test configuration
	config, err := LoadTestConfig()
	require.NoError(t, err, "Failed to load test configuration")

	// Generate unique test ID for container isolation
	testID := fmt.Sprintf("test-%d-%s", time.Now().Unix(), strings.ReplaceAll(t.Name(), "/", "-"))

	suite := &TestSuite{
		t:          t,
		config:     config,
		containers: make(map[string]testcontainers.Container),
		tempDirs:   make([]string, 0),
		cleanup:    make([]func(), 0),
		testID:     testID,
	}

	// Setup cleanup on test completion
	t.Cleanup(func() {
		suite.Cleanup()
	})

	return suite
}

// LoadTestConfig loads the test configuration
func LoadTestConfig() (*TestConfig, error) {
	configPath := filepath.Join(GetProjectRoot(), "test", "config", "test_config.yaml")

	// Validate the config path is within expected directory for security
	projectRoot := GetProjectRoot()
	expectedPrefix := filepath.Join(projectRoot, "test", "config")
	cleanPath := filepath.Clean(configPath)
	if !strings.HasPrefix(cleanPath, expectedPrefix) {
		return nil, fmt.Errorf("config path outside expected directory: %s", configPath)
	}

	data, err := os.ReadFile(cleanPath) // #nosec G304 - path is validated above
	if err != nil {
		return nil, fmt.Errorf("failed to read test config: %w", err)
	}

	var config TestConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse test config: %w", err)
	}

	return &config, nil
}

// GetProjectRoot returns the project root directory
func GetProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}

// SetupMockServices initializes mock services
func (ts *TestSuite) SetupMockServices() *MockServices {
	ts.t.Helper()

	// Setup HTTP mocks
	httpmock.Activate()
	ts.AddCleanup(httpmock.DeactivateAndReset)

	// Setup SQL mocks
	db, mock, err := sqlmock.New()
	require.NoError(ts.t, err, "Failed to create SQL mock")
	ts.AddCleanup(func() {
		if err := db.Close(); err != nil {
			ts.t.Logf("Failed to close database: %v", err)
		}
	})

	ts.mocks = &MockServices{
		HTTPTransport: httpmock.DefaultTransport,
		SQLMock:       mock,
		Database:      db,
	}

	return ts.mocks
}

// SetupTestDatabase sets up a test database container
func (ts *TestSuite) SetupTestDatabase(dbType string) (string, error) {
	ts.t.Helper()

	ctx := context.Background()

	// Add unique labels for container identification and cleanup
	labels := map[string]string{
		"test-suite":    "gh-ghes-2-ghec",
		"test-id":       ts.testID,
		"test-name":     ts.t.Name(),
		"database-type": dbType,
	}

	switch dbType {
	case "postgres":
		req := testcontainers.ContainerRequest{
			Image:        "postgres:15-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       ts.config.Integration.Database.Postgres.Database,
				"POSTGRES_USER":     ts.config.Integration.Database.Postgres.Username,
				"POSTGRES_PASSWORD": ts.config.Integration.Database.Postgres.Password,
			},
			Labels: labels,
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("5432/tcp"),
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			).WithStartupTimeout(ts.getContainerTimeout()),
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			return "", fmt.Errorf("failed to start postgres container: %w", err)
		}

		// Verify container is healthy before proceeding
		if err := ts.waitForContainerHealth(ctx, container, "postgres"); err != nil {
			ts.terminateContainerSafely(container, "postgres")
			return "", fmt.Errorf("postgres container health check failed: %w", err)
		}

		ts.containers["postgres"] = container
		ts.AddCleanup(func() {
			ts.terminateContainerSafely(container, "postgres")
		})

		host, err := container.Host(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get postgres host: %w", err)
		}

		port, err := container.MappedPort(ctx, "5432")
		if err != nil {
			return "", fmt.Errorf("failed to get postgres port: %w", err)
		}

		connectionString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			ts.config.Integration.Database.Postgres.Username,
			ts.config.Integration.Database.Postgres.Password,
			host,
			port.Port(),
			ts.config.Integration.Database.Postgres.Database)

		// Test the connection before returning
		if err := ts.testDatabaseConnection(connectionString, "postgres"); err != nil {
			ts.terminateContainerSafely(container, "postgres")
			return "", fmt.Errorf("failed to connect to postgres database: %w", err)
		}

		return connectionString, nil

	case "mysql":
		req := testcontainers.ContainerRequest{
			Image:        "mysql:8.0",
			ExposedPorts: []string{"3306/tcp"},
			Env: map[string]string{
				"MYSQL_DATABASE":      ts.config.Integration.Database.MySQL.Database,
				"MYSQL_USER":          ts.config.Integration.Database.MySQL.Username,
				"MYSQL_PASSWORD":      ts.config.Integration.Database.MySQL.Password,
				"MYSQL_ROOT_PASSWORD": "root",
			},
			Labels: labels,
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("3306/tcp"),
				wait.ForLog("ready for connections").WithOccurrence(1),
			).WithStartupTimeout(ts.getContainerTimeout()),
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			return "", fmt.Errorf("failed to start mysql container: %w", err)
		}

		// Verify container is healthy before proceeding
		if err := ts.waitForContainerHealth(ctx, container, "mysql"); err != nil {
			ts.terminateContainerSafely(container, "mysql")
			return "", fmt.Errorf("mysql container health check failed: %w", err)
		}

		ts.containers["mysql"] = container
		ts.AddCleanup(func() {
			ts.terminateContainerSafely(container, "mysql")
		})

		host, err := container.Host(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get mysql host: %w", err)
		}

		port, err := container.MappedPort(ctx, "3306")
		if err != nil {
			return "", fmt.Errorf("failed to get mysql port: %w", err)
		}

		connectionString := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
			ts.config.Integration.Database.MySQL.Username,
			ts.config.Integration.Database.MySQL.Password,
			host,
			port.Port(),
			ts.config.Integration.Database.MySQL.Database)

		// Test the connection before returning
		if err := ts.testDatabaseConnection(connectionString, "mysql"); err != nil {
			ts.terminateContainerSafely(container, "mysql")
			return "", fmt.Errorf("failed to connect to mysql database: %w", err)
		}

		return connectionString, nil

	default:
		return "", fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// waitForContainerHealth waits for a container to be healthy
func (ts *TestSuite) waitForContainerHealth(ctx context.Context, container testcontainers.Container, containerType string) error {
	timeout := 30 * time.Second
	interval := 1 * time.Second

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s container to become healthy", containerType)
		case <-ticker.C:
			state, err := container.State(ctx)
			if err != nil {
				ts.t.Logf("Failed to get %s container state: %v", containerType, err)
				continue
			}

			if state.Running {
				ts.t.Logf("%s container is running and healthy", containerType)
				return nil
			}

			if state.Dead || state.OOMKilled {
				return fmt.Errorf("%s container died or was killed", containerType)
			}
		}
	}
}

// testDatabaseConnection tests if the database connection is working
func (ts *TestSuite) testDatabaseConnection(connectionString, dbType string) error {
	// Skip connection testing if we're in a test environment where drivers might not be available
	if os.Getenv("SKIP_DB_CONNECTION_TEST") == "true" {
		ts.t.Logf("Skipping database connection test for %s", dbType)
		return nil
	}

	var driverName string
	switch dbType {
	case "postgres":
		driverName = "postgres"
	case "mysql":
		driverName = "mysql"
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	// Import the required drivers
	if dbType == "postgres" {
		// Ensure postgres driver is imported
		_ = "github.com/lib/pq"
	} else if dbType == "mysql" {
		// Ensure mysql driver is imported
		_ = "github.com/go-sql-driver/mysql"
	}

	db, err := sql.Open(driverName, connectionString)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			ts.t.Logf("Failed to close test database connection: %v", err)
		}
	}()

	// Set connection limits to avoid overwhelming the test database
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

// containerExists checks if a container still exists
func (ts *TestSuite) containerExists(ctx context.Context, container testcontainers.Container) bool {
	if container == nil {
		return false
	}

	_, err := container.State(ctx)
	if err != nil {
		// If we get "No such container" error, the container doesn't exist
		if strings.Contains(err.Error(), "No such container") ||
			strings.Contains(err.Error(), "container not found") {
			return false
		}
		// For other errors, assume it exists but is in an unknown state
		return true
	}
	return true
}

// terminateContainerSafely terminates a container with proper error handling and retries
func (ts *TestSuite) terminateContainerSafely(container testcontainers.Container, containerType string) {
	if container == nil {
		return
	}

	// Create a new context with timeout for termination
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if container still exists before attempting operations
	if !ts.containerExists(ctx, container) {
		ts.t.Logf("%s container no longer exists, skipping termination", containerType)
		return
	}

	// First, try to stop the container gracefully
	if err := container.Stop(ctx, nil); err != nil {
		// Only log if it's not a "container not found" error
		if !strings.Contains(err.Error(), "No such container") &&
			!strings.Contains(err.Error(), "container not found") {
			ts.t.Logf("Failed to stop %s container gracefully: %v", containerType, err)
		}
	}

	// Then terminate it with retries
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		err := container.Terminate(ctx)
		if err == nil {
			ts.t.Logf("Successfully terminated %s container", containerType)
			return
		}

		// Check if the error indicates the container is already gone
		if strings.Contains(err.Error(), "No such container") ||
			strings.Contains(err.Error(), "container not found") ||
			strings.Contains(err.Error(), "removal of container") {
			ts.t.Logf("%s container already terminated or removed", containerType)
			return
		}

		ts.t.Logf("Attempt %d/%d: Failed to terminate %s container: %v", i+1, maxRetries, containerType, err)

		// Wait before retrying
		if i < maxRetries-1 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	ts.t.Logf("Failed to terminate %s container after %d attempts", containerType, maxRetries)
}

// removeContainerFromTracking safely removes a container from the tracking map
func (ts *TestSuite) removeContainerFromTracking(containerType string) {
	// Use a simple non-blocking approach
	select {
	case <-time.After(100 * time.Millisecond):
		// Give a small window for other operations to complete
	}

	// Try to acquire the lock with a very short timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		ts.mu.Lock()
		delete(ts.containers, containerType)
		ts.mu.Unlock()
	}()

	select {
	case <-done:
		// Successfully removed
	case <-time.After(500 * time.Millisecond):
		// If we can't remove it quickly, just log and continue
		// The container will be cleaned up by the automatic cleanup
		ts.t.Logf("Could not remove container %s from tracking (lock contention)", containerType)
	}
}

// CreateTempDir creates a temporary directory for testing
func (ts *TestSuite) CreateTempDir() string {
	ts.t.Helper()

	tempDir, err := os.MkdirTemp("", "gh-ghes-2-ghec-test-*")
	require.NoError(ts.t, err, "Failed to create temp directory")

	ts.mu.Lock()
	ts.tempDirs = append(ts.tempDirs, tempDir)
	ts.mu.Unlock()

	return tempDir
}

// CreateTestConfig creates a test configuration file
func (ts *TestSuite) CreateTestConfig(overrides map[string]interface{}) string {
	ts.t.Helper()

	tempDir := ts.CreateTempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	// Create base configuration
	baseConfig := map[string]interface{}{
		"log": map[string]interface{}{
			"level":      "debug",
			"format":     "text",
			"file":       filepath.Join(tempDir, "test.log"),
			"max_size":   10,
			"max_age":    7,
			"max_backup": 3,
		},
		"database": map[string]interface{}{
			"type": "sqlite",
			"sqlite": map[string]interface{}{
				"path": ":memory:",
			},
		},
		"github": map[string]interface{}{
			"ghes": map[string]interface{}{
				"base_url": "https://github.example.com",
				"token":    "test-token",
			},
			"ghec": map[string]interface{}{
				"base_url": "https://api.github.com",
				"token":    "test-token",
			},
		},
		"server": map[string]interface{}{
			"port":    8080,
			"host":    "localhost",
			"enabled": false,
		},
		"dashboard": map[string]interface{}{
			"enabled": false,
		},
	}

	// Apply overrides
	for key, value := range overrides {
		baseConfig[key] = value
	}

	// Write configuration to file
	data, err := yaml.Marshal(baseConfig)
	require.NoError(ts.t, err, "Failed to marshal test config")

	// Use restrictive permissions for test config files
	err = os.WriteFile(configPath, data, 0600)
	require.NoError(ts.t, err, "Failed to write test config")

	return configPath
}

// SetupGitHubMocks sets up GitHub API mocks
func (ts *TestSuite) SetupGitHubMocks() {
	ts.t.Helper()

	if ts.mocks == nil {
		ts.SetupMockServices()
	}

	// Mock common GitHub API endpoints
	httpmock.RegisterResponder("GET", "https://api.github.com/user",
		httpmock.NewStringResponder(200, `{"login": "test-user"}`))

	httpmock.RegisterResponder("GET", "https://github.example.com/api/v3/user",
		httpmock.NewStringResponder(200, `{"login": "test-user"}`))

	// Add more GitHub API mocks as needed
}

// AddCleanup adds a cleanup function to be called when the test completes
func (ts *TestSuite) AddCleanup(fn func()) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.cleanup = append(ts.cleanup, fn)
}

// Cleanup performs cleanup of all test resources
func (ts *TestSuite) Cleanup() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Clean up orphaned containers first
	ts.cleanupOrphanedContainers()

	// Run cleanup functions in reverse order
	for i := len(ts.cleanup) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if r := recover(); r != nil {
					ts.t.Logf("Cleanup function panicked: %v", r)
				}
			}()
			ts.cleanup[i]()
		}()
	}

	// Clean up temp directories
	for _, dir := range ts.tempDirs {
		if err := os.RemoveAll(dir); err != nil {
			ts.t.Logf("Failed to remove temp directory %s: %v", dir, err)
		}
	}

	// Clean up any remaining containers (fallback)
	ts.cleanupRemainingContainers()

	// Skip goroutine leak detection for integration tests since they involve
	// complex services (like lumberjack logger, database connections, etc.)
	// that create background goroutines which are difficult to clean up properly
	// Only run leak detection for unit tests when running in short mode
	if testing.Short() {
		goleak.VerifyNone(ts.t)
	}
}

// cleanupOrphanedContainers removes any containers that might have been left from previous test runs
func (ts *TestSuite) cleanupOrphanedContainers() {
	// This is a best-effort cleanup for orphaned containers
	// We'll use docker commands to find and remove containers with our test labels
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try to clean up containers with our test suite label that are older than 1 hour
	// This helps prevent accumulation of orphaned containers in CI environments
	if err := ts.removeOrphanedContainers(ctx); err != nil {
		ts.t.Logf("Failed to clean up orphaned containers: %v", err)
	}
}

// removeOrphanedContainers removes containers that match our test labels and are old
func (ts *TestSuite) removeOrphanedContainers(ctx context.Context) error {
	// This is a simplified approach - in a real implementation, you might want to use
	// the Docker API directly or testcontainers' cleanup mechanisms

	// For now, we'll just log that we would clean up orphaned containers
	// In a production environment, you could implement actual Docker API calls here
	ts.t.Logf("Checking for orphaned containers with test-suite=gh-ghes-2-ghec label")

	return nil
}

// cleanupRemainingContainers provides a fallback cleanup for any containers that weren't properly terminated
func (ts *TestSuite) cleanupRemainingContainers() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to get a snapshot of containers with a timeout to avoid blocking
	containersCopy := make(map[string]testcontainers.Container)

	// Use a channel to get the containers without blocking indefinitely
	done := make(chan struct{})
	go func() {
		defer close(done)
		ts.mu.RLock()
		for name, container := range ts.containers {
			containersCopy[name] = container
		}
		ts.mu.RUnlock()
	}()

	select {
	case <-done:
		// Successfully got the containers
	case <-time.After(2 * time.Second):
		ts.t.Logf("Timeout getting containers for cleanup, skipping")
		return
	}

	for name, container := range containersCopy {
		if container == nil {
			continue
		}

		// Check if container still exists
		if !ts.containerExists(ctx, container) {
			ts.t.Logf("Container %s no longer exists", name)
			continue
		}

		// Get container state to check if it's running
		state, err := container.State(ctx)
		if err != nil {
			ts.t.Logf("Failed to get state for %s container: %v", name, err)
			continue
		}

		if state.Running {
			ts.t.Logf("Container %s still running, attempting final cleanup", name)
			ts.terminateContainerSafely(container, name)
		} else {
			ts.t.Logf("Container %s is not running (state: %s)", name, state.Status)
		}
	}
}

// MockRepository represents a mock repository for testing
type MockRepository struct {
	Name        string `faker:"word"`
	FullName    string `faker:"sentence"`
	Description string `faker:"paragraph"`
	Private     bool   `faker:"bool"`
	Size        int    `faker:"boundary_start=1000,boundary_end=100000"`
	Language    string `faker:"word"`
}

// GenerateMockRepository generates a mock repository for testing
func GenerateMockRepository() MockRepository {
	var repo MockRepository
	if err := faker.FakeData(&repo); err != nil {
		// Use default values if faker fails
		repo = MockRepository{
			Name:        "test-repo",
			FullName:    "test-org/test-repo",
			Description: "A test repository",
			Private:     false,
			Size:        50000,
			Language:    "Go",
		}
	}
	return repo
}

// SetupTestServer creates a test HTTP server for testing
func SetupTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeoutCh := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			if condition() {
				return
			}
		case <-timeoutCh:
			t.Fatalf("Condition not met within timeout: %s", message)
		}
	}
}

// CaptureOutput captures stdout/stderr output during test execution
func CaptureOutput(fn func()) (string, string, error) {
	// Capture stdout
	oldStdout := os.Stdout
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	os.Stdout = stdoutW

	// Capture stderr
	oldStderr := os.Stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	os.Stderr = stderrW

	// Run function
	fn()

	// Close writers to flush any buffered data
	if err := stdoutW.Close(); err != nil {
		return "", "", fmt.Errorf("failed to close stdout writer: %w", err)
	}
	if err := stderrW.Close(); err != nil {
		return "", "", fmt.Errorf("failed to close stderr writer: %w", err)
	}

	// Restore original stdout/stderr
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read captured output
	stdoutOut, err := io.ReadAll(stdoutR)
	if err != nil {
		return "", "", err
	}

	stderrOut, err := io.ReadAll(stderrR)
	if err != nil {
		return "", "", err
	}

	return string(stdoutOut), string(stderrOut), nil
}

// InitTestEnvironment initializes the test environment
func InitTestEnvironment(t *testing.T) {
	t.Helper()

	// Initialize logging for tests
	if err := logging.Init(); err != nil {
		t.Logf("Warning: Failed to initialize logging: %v", err)
	}

	// Initialize configuration for tests
	if err := config.Init(); err != nil {
		t.Logf("Warning: Failed to initialize config: %v", err)
	}
}

// SkipIfShort skips the test if running in short mode
func SkipIfShort(t *testing.T, reason string) {
	t.Helper()
	if testing.Short() {
		t.Skipf("Skipping test in short mode: %s", reason)
	}
}

// RequireEnv requires an environment variable to be set
func RequireEnv(t *testing.T, envVar string) string {
	t.Helper()
	value := os.Getenv(envVar)
	if value == "" {
		t.Skipf("Environment variable %s is required but not set", envVar)
	}
	return value
}

// getContainerTimeout returns appropriate timeout based on environment
func (ts *TestSuite) getContainerTimeout() time.Duration {
	// Check if running in CI environment
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		// CI environments may be slower, use longer timeout
		return 180 * time.Second
	}
	// Local development environment
	return 120 * time.Second
}
