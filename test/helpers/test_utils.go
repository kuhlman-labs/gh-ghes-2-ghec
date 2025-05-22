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

	suite := &TestSuite{
		t:          t,
		config:     config,
		containers: make(map[string]testcontainers.Container),
		tempDirs:   make([]string, 0),
		cleanup:    make([]func(), 0),
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

	data, err := os.ReadFile(configPath)
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
	ts.AddCleanup(func() { db.Close() })

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
			WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(30 * time.Second),
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			return "", fmt.Errorf("failed to start postgres container: %w", err)
		}

		ts.containers["postgres"] = container
		ts.AddCleanup(func() { container.Terminate(ctx) })

		host, err := container.Host(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get postgres host: %w", err)
		}

		port, err := container.MappedPort(ctx, "5432")
		if err != nil {
			return "", fmt.Errorf("failed to get postgres port: %w", err)
		}

		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			ts.config.Integration.Database.Postgres.Username,
			ts.config.Integration.Database.Postgres.Password,
			host,
			port.Port(),
			ts.config.Integration.Database.Postgres.Database), nil

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
			WaitingFor: wait.ForListeningPort("3306/tcp").WithStartupTimeout(60 * time.Second),
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			return "", fmt.Errorf("failed to start mysql container: %w", err)
		}

		ts.containers["mysql"] = container
		ts.AddCleanup(func() { container.Terminate(ctx) })

		host, err := container.Host(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get mysql host: %w", err)
		}

		port, err := container.MappedPort(ctx, "3306")
		if err != nil {
			return "", fmt.Errorf("failed to get mysql port: %w", err)
		}

		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
			ts.config.Integration.Database.MySQL.Username,
			ts.config.Integration.Database.MySQL.Password,
			host,
			port.Port(),
			ts.config.Integration.Database.MySQL.Database), nil

	default:
		return "", fmt.Errorf("unsupported database type: %s", dbType)
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

	err = os.WriteFile(configPath, data, 0644)
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

	// Run cleanup functions in reverse order
	for i := len(ts.cleanup) - 1; i >= 0; i-- {
		ts.cleanup[i]()
	}

	// Clean up temp directories
	for _, dir := range ts.tempDirs {
		os.RemoveAll(dir)
	}

	// Clean up containers
	ctx := context.Background()
	for _, container := range ts.containers {
		container.Terminate(ctx)
	}

	// Check for goroutine leaks in unit tests
	if ts.config.Unit.Short {
		goleak.VerifyNone(ts.t)
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

// GenerateMockRepository generates a mock repository using faker
func GenerateMockRepository() MockRepository {
	var repo MockRepository
	faker.FakeData(&repo)

	// Provide fallback values if faker doesn't generate data
	if repo.Name == "" {
		repo.Name = "test-repo"
	}
	if repo.FullName == "" {
		repo.FullName = "test-org/test-repo"
	}
	if repo.Language == "" {
		repo.Language = "Go"
	}
	if repo.Description == "" {
		repo.Description = "A test repository for testing purposes"
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

	// Restore original stdout/stderr
	stdoutW.Close()
	stderrW.Close()
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
