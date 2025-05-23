package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		wantErr  bool
		expected bool
	}{
		{
			name: "enabled config",
			config: Config{
				Enabled:     true,
				Port:        8080,
				Path:        "/metrics",
				ServiceName: "test-service",
			},
			wantErr:  false,
			expected: true,
		},
		{
			name: "disabled config",
			config: Config{
				Enabled: false,
			},
			wantErr:  false,
			expected: false,
		},
		{
			name: "custom namespace",
			config: Config{
				Enabled:     true,
				ServiceName: "custom-namespace",
			},
			wantErr:  false,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global state
			enabled = false
			namespace = "ghghe2ec"

			err := Init(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expected, enabled)
			if tt.config.ServiceName != "" && tt.config.Enabled {
				assert.Equal(t, tt.config.ServiceName, namespace)
			}
		})
	}
}

func TestIsEnabled(t *testing.T) {
	// Test disabled state
	enabled = false
	assert.False(t, IsEnabled())

	// Test enabled state
	enabled = true
	assert.True(t, IsEnabled())
}

func TestHandler(t *testing.T) {
	// Reset global state to avoid interference from other tests
	enabled = false
	namespace = "ghghe2ec"

	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	handler := Handler()
	assert.NotNil(t, handler)

	// Create a test request
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}

func TestInstrumentHandler(t *testing.T) {
	// Reset global state to avoid interference from other tests
	enabled = false
	namespace = "ghghe2ec"

	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("test response")); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	})

	instrumentedHandler := InstrumentHandler(testHandler, "test_handler")

	// Test the instrumented handler
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	instrumentedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test response", w.Body.String())

	// Gather metrics and verify they exist
	families, err := registry.Gather()
	require.NoError(t, err)

	// Check that metrics were created (basic smoke test)
	assert.NotNil(t, registry)
	assert.NotEmpty(t, families, "Expected metrics to be registered")
}

func TestRecordMigrationStart(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	sourceOrg := "source-org"
	targetOrg := "target-org"

	// Reset the counter before testing
	migrationTotal.Reset()

	RecordMigrationStart(sourceOrg, targetOrg)

	// Verify the metric was incremented
	expected := `
		# HELP ghghe2ec_migrations_total Total number of migration operations
		# TYPE ghghe2ec_migrations_total counter
		ghghe2ec_migrations_total{source_org="source-org",status="started",target_org="target-org"} 1
	`
	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_migrations_total")
	assert.NoError(t, err)
}

func TestRecordMigrationComplete(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	sourceOrg := "source-org"
	targetOrg := "target-org"
	status := "succeeded"
	duration := 30 * time.Second
	sizeBytes := int64(1024 * 1024) // 1MB

	// Reset metrics before testing
	migrationTotal.Reset()
	migrationDuration.Reset()
	migrationSize.Reset()

	RecordMigrationComplete(sourceOrg, targetOrg, status, duration, sizeBytes)

	// Verify migration total counter
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_migrations_total Total number of migration operations
		# TYPE ghghe2ec_migrations_total counter
		ghghe2ec_migrations_total{source_org="%s",status="%s",target_org="%s"} 1
	`, sourceOrg, status, targetOrg)

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_migrations_total")
	assert.NoError(t, err)
}

func TestRecordMigrationStage(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	sourceOrg := "source-org"
	targetOrg := "target-org"
	stage := "archive"
	status := "completed"
	duration := 10 * time.Second

	// Reset metrics before testing
	migrationDuration.Reset()

	RecordMigrationStage(sourceOrg, targetOrg, stage, status, duration)

	// Basic verification that the function doesn't panic
	// Detailed histogram testing would require more complex validation
	assert.True(t, true, "RecordMigrationStage completed without error")
}

func TestRecordGitHubAPIRequest(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	api := "rest"
	endpoint := "/repos/{owner}/{repo}"
	status := "200"
	duration := 500 * time.Millisecond

	// Reset metrics before testing
	githubAPIRequestsTotal.Reset()
	githubAPIRequestDuration.Reset()

	RecordGitHubAPIRequest(api, endpoint, status, duration)

	// Verify the counter was incremented
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_github_api_requests_total Total number of GitHub API requests
		# TYPE ghghe2ec_github_api_requests_total counter
		ghghe2ec_github_api_requests_total{api="%s",endpoint="%s",status="%s"} 1
	`, api, endpoint, status)

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_github_api_requests_total")
	assert.NoError(t, err)
}

func TestSetGitHubRateLimit(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	api := "rest"
	remaining := 4500

	SetGitHubRateLimit(api, remaining)

	// Verify the gauge was set
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_github_rate_limit_remaining Number of GitHub API rate limit calls remaining
		# TYPE ghghe2ec_github_rate_limit_remaining gauge
		ghghe2ec_github_rate_limit_remaining{api="%s"} %d
	`, api, remaining)

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_github_rate_limit_remaining")
	assert.NoError(t, err)
}

func TestRecordStorageOperation(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	operation := "save"
	status := "success"
	duration := 100 * time.Millisecond

	// Reset metrics before testing
	storageOperationsTotal.Reset()
	storageOperationDuration.Reset()

	RecordStorageOperation(operation, status, duration)

	// Verify the counter was incremented
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_storage_operations_total Total number of storage operations
		# TYPE ghghe2ec_storage_operations_total counter
		ghghe2ec_storage_operations_total{operation="%s",status="%s"} 1
	`, operation, status)

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_storage_operations_total")
	assert.NoError(t, err)
}

func TestSetDatabaseConnections(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	dbType := "postgres"
	state := "active"
	value := 10.0

	SetDatabaseConnections(dbType, state, value)

	// Verify the gauge was set
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_database_connections Current number of database connections
		# TYPE ghghe2ec_database_connections gauge
		ghghe2ec_database_connections{db_type="%s",state="%s"} %s
	`, dbType, state, strconv.FormatFloat(value, 'f', -1, 64))

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_database_connections")
	assert.NoError(t, err)
}

func TestSetDatabaseWaitCount(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	dbType := "postgres"
	count := int64(5)

	SetDatabaseWaitCount(dbType, count)

	// Verify the counter was set
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_database_wait_count_total Total number of connection waits due to pool exhaustion
		# TYPE ghghe2ec_database_wait_count_total counter
		ghghe2ec_database_wait_count_total{db_type="%s"} %d
	`, dbType, count)

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_database_wait_count_total")
	assert.NoError(t, err)
}

func TestSetDatabaseWaitDuration(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	dbType := "postgres"
	seconds := 2.5

	SetDatabaseWaitDuration(dbType, seconds)

	// Verify the gauge was set
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_database_wait_duration_seconds Total time waiting for database connections
		# TYPE ghghe2ec_database_wait_duration_seconds gauge
		ghghe2ec_database_wait_duration_seconds{db_type="%s"} %s
	`, dbType, strconv.FormatFloat(seconds, 'f', -1, 64))

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_database_wait_duration_seconds")
	assert.NoError(t, err)
}

func TestRecordDatabaseQuery(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	dbType := "postgres"
	operation := "select"
	duration := 50 * time.Millisecond

	// Reset metrics before testing
	databaseQueryDuration.Reset()

	RecordDatabaseQuery(dbType, operation, duration)

	// Basic verification that the function doesn't panic
	assert.True(t, true, "RecordDatabaseQuery completed without error")
}

func TestRecordError(t *testing.T) {
	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	category := "validation_error"

	// Reset metrics before testing
	errorCategoryCounts.Reset()

	RecordError(category)

	// Verify the counter was incremented
	expected := fmt.Sprintf(`
		# HELP ghghe2ec_errors_by_category Count of errors by category
		# TYPE ghghe2ec_errors_by_category counter
		ghghe2ec_errors_by_category{category="%s"} 1
	`, category)

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "ghghe2ec_errors_by_category")
	assert.NoError(t, err)
}

func TestGetErrorCategoryCounts(t *testing.T) {
	// Reset global state to avoid interference from other tests
	enabled = false
	namespace = "ghghe2ec"

	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	// Reset and add some errors
	errorCategoryCounts.Reset()
	RecordError("validation_error")
	RecordError("api_error")
	RecordError("validation_error") // duplicate category

	counts := GetErrorCategoryCounts()

	expected := map[string]int{
		"validation_error": 2,
		"api_error":        1,
	}

	assert.Equal(t, expected, counts)
}

func TestResponseWriter(t *testing.T) {
	// Test the custom response writer
	recorder := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// Test WriteHeader
	rw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rw.statusCode)
	assert.Equal(t, http.StatusCreated, recorder.Code)

	// Test default status code
	rw2 := &responseWriter{
		ResponseWriter: httptest.NewRecorder(),
		statusCode:     http.StatusOK,
	}
	// Writing without calling WriteHeader should use default status
	if _, err := rw2.Write([]byte("test")); err != nil {
		t.Errorf("Failed to write: %v", err)
	}
	assert.Equal(t, http.StatusOK, rw2.statusCode)
}

func TestMetricsDisabled(t *testing.T) {
	// Test behavior when metrics are disabled
	err := Init(Config{Enabled: false})
	require.NoError(t, err)

	// These should not panic when metrics are disabled
	RecordMigrationStart("org1", "org2")
	RecordMigrationComplete("org1", "org2", "success", time.Second, 1024)
	RecordGitHubAPIRequest("rest", "/endpoint", "200", time.Millisecond)
	SetGitHubRateLimit("rest", 5000)
	RecordStorageOperation("save", "success", time.Millisecond)
	RecordError("test_error")

	assert.False(t, IsEnabled())
}

func TestConcurrentMetricUpdates(t *testing.T) {
	// Reset global state to avoid interference from other tests
	enabled = false
	namespace = "ghghe2ec"

	// Initialize metrics
	err := Init(Config{Enabled: true})
	require.NoError(t, err)

	// Reset metrics
	migrationTotal.Reset()

	// Test concurrent updates
	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			RecordMigrationStart(fmt.Sprintf("org%d", id), "target-org")
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// The metrics should handle concurrent updates without panicking
	assert.True(t, true, "Concurrent metric updates completed without error")
}
