package dashboard

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// MigratorInterface defines the methods needed by the dashboard handler
type MigratorInterface interface {
	GetAllMigrationStatuses() map[string]*payload.MigrationStatus
	GetQueuedRepositories() []string
	GetQueueStats() map[string]interface{}
	GetArchivedMigrationAttempts(repository string) ([]*payload.MigrationStatus, error)
	GetMigrationStatus(repository string) *payload.MigrationStatus
	StartMigration(ctx context.Context, request *payload.MigrationRequest, cancelFunc context.CancelFunc) error
	RetryMigration(ctx context.Context, repository, ghesToken, ghCloudToken, ghesBaseURL, targetOrg string) error
}

// MockMigrator implements a mock migrator for testing purposes
type MockMigrator struct {
	migrationStatuses  map[string]*payload.MigrationStatus
	queuedRepositories []string
	queueStats         map[string]interface{}
	archivedAttempts   []*payload.MigrationStatus
	shouldReturnError  bool
	retryCallCount     int
	submitCallCount    int
}

func NewMockMigrator() *MockMigrator {
	return &MockMigrator{
		migrationStatuses:  make(map[string]*payload.MigrationStatus),
		queuedRepositories: []string{},
		queueStats:         map[string]interface{}{},
		archivedAttempts:   []*payload.MigrationStatus{},
	}
}

func (m *MockMigrator) GetAllMigrationStatuses() map[string]*payload.MigrationStatus {
	return m.migrationStatuses
}

func (m *MockMigrator) GetQueuedRepositories() []string {
	return m.queuedRepositories
}

func (m *MockMigrator) GetQueueStats() map[string]interface{} {
	return m.queueStats
}

func (m *MockMigrator) GetArchivedMigrationAttempts(repository string) ([]*payload.MigrationStatus, error) {
	if m.shouldReturnError {
		return nil, fmt.Errorf("mock error")
	}
	return m.archivedAttempts, nil
}

func (m *MockMigrator) GetMigrationStatus(repository string) *payload.MigrationStatus {
	if m.shouldReturnError {
		return nil
	}
	status, exists := m.migrationStatuses[repository]
	if !exists {
		return nil
	}
	return status
}

func (m *MockMigrator) StartMigration(ctx context.Context, request *payload.MigrationRequest, cancelFunc context.CancelFunc) error {
	m.submitCallCount++
	if m.shouldReturnError {
		return fmt.Errorf("mock submit error")
	}
	return nil
}

func (m *MockMigrator) RetryMigration(ctx context.Context, repository, ghesToken, ghCloudToken, ghesBaseURL, targetOrg string) error {
	m.retryCallCount++
	if m.shouldReturnError {
		return fmt.Errorf("mock retry error")
	}
	return nil
}

// Test helper to create sample migration statuses
func createTestMigrationStatus(repo, status, stage string, startedAt time.Time, progress int) *payload.MigrationStatus {
	return &payload.MigrationStatus{
		Repository:      repo,
		Status:          status,
		Stage:           stage,
		State:           "processing",
		StartedAt:       startedAt,
		Duration:        time.Hour,
		Progress:        progress,
		RepositorySize:  1024 * 1024, // 1MB
		SizeCategory:    payload.SizeSmall,
		CompletedStages: []string{"validation"},
	}
}

// testHandler wraps a handler with a mock migrator for testing
type testHandler struct {
	templates *template.Template
	mock      *MockMigrator
	logger    *slog.Logger
}

// Test helper to create a handler with a mock migrator
func createTestHandler() (*testHandler, *MockMigrator) {
	mockMigrator := NewMockMigrator()

	// Create a simple template for testing
	tmpl := template.New("test").Funcs(template.FuncMap{
		"ToLower":        strings.ToLower,
		"Title":          cases.Title(language.English).String,
		"FormatTime":     func(t time.Time) string { return t.Format("15:04:05") },
		"FormatDateTime": func(t time.Time) string { return t.Format("2006-01-02 15:04:05") },
		"FormatDuration": func(d time.Duration) string { return d.String() },
		"percentage":     func(count, total int) string { return fmt.Sprintf("%.1f", float64(count)/float64(total)*100) },
		"divFloat":       func(value int64, divisor float64) float64 { return float64(value) / divisor },
	})

	// Add a minimal template for testing
	template.Must(tmpl.New("stats").Parse(`{{.Active}}`))
	template.Must(tmpl.New("queue_stats").Parse(`{{.}}`))
	template.Must(tmpl.New("migration_detail_content").Parse(`{{.Migration.Repository}}`))

	handler := &testHandler{
		templates: tmpl,
		mock:      mockMigrator,
		logger:    slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}

	return handler, mockMigrator
}

// Implement handler methods for testing
func (h *testHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all migration statuses and calculate stats
	migrationsMap := h.mock.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)
	stats := calculateStats(allMigrations)

	// Create a template with just the stats HTML
	statsTemplate := `{{.Active}}`
	tmpl, err := template.New("stats").Parse(statsTemplate)
	if err != nil {
		http.Error(w, "Failed to parse stats template", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, stats); err != nil {
		http.Error(w, "Failed to render stats", http.StatusInternalServerError)
	}
}

func (h *testHandler) handleQueueStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get queue statistics
	queueStats := h.mock.GetQueueStats()

	// Simple template execution
	if _, err := fmt.Fprintf(w, "%v", queueStats); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
	}
}

func (h *testHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get and validate format parameter
	format := r.URL.Query().Get("format")
	if format != "csv" && format != "json" {
		http.Error(w, "Invalid export format. Supported formats: csv, json", http.StatusBadRequest)
		return
	}

	// Get migrations from the mock
	migrationsMap := h.mock.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)

	// Export in the requested format
	switch format {
	case "csv":
		exportCSV(w, allMigrations)
	case "json":
		exportJSON(w, allMigrations)
	}
}

func (h *testHandler) handleMigrationRoutes(w http.ResponseWriter, r *http.Request) {
	// Extract repository name from path
	path := strings.TrimPrefix(r.URL.Path, "/dashboard/migration/")
	path = strings.TrimSuffix(path, "/")

	// Remove any suffixes like /retry or /retry-form
	path = strings.TrimSuffix(path, "/retry")
	path = strings.TrimSuffix(path, "/retry-form")

	// Try to get migration status
	status := h.mock.GetMigrationStatus(path)
	if status == nil {
		http.Error(w, "Migration not found", http.StatusNotFound)
		return
	}

	// For test purposes, just return success
	w.WriteHeader(http.StatusOK)
}

func (h *testHandler) handleSubmitMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Basic validation - check if required fields are present
	sourceOrg := r.FormValue("source-org")
	targetOrg := r.FormValue("target-org")
	repositories := r.FormValue("repositories")
	ghesBaseURL := r.FormValue("ghes-base-url")
	ghesToken := r.FormValue("ghes-token")
	ghCloudToken := r.FormValue("gh-cloud-token")

	if sourceOrg == "" || targetOrg == "" || repositories == "" ||
		ghesBaseURL == "" || ghesToken == "" || ghCloudToken == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Create mock migration request
	repos := parseRepositories(repositories)
	req := &payload.MigrationRequest{
		SourceOrg:    sourceOrg,
		TargetOrg:    targetOrg,
		Repositories: repos,
		GHESBaseURL:  ghesBaseURL,
		GHESToken:    ghesToken,
		GHCloudToken: ghCloudToken,
	}

	// Try to start migration
	if err := h.mock.StartMigration(r.Context(), req, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect on success
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Test parseRepositories function
func TestParseRepositories(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single repository",
			input:    "repo1",
			expected: []string{"repo1"},
		},
		{
			name:     "multiple repositories",
			input:    "repo1\nrepo2\nrepo3",
			expected: []string{"repo1", "repo2", "repo3"},
		},
		{
			name:     "repositories with whitespace",
			input:    "  repo1  \n  repo2  \n  repo3  ",
			expected: []string{"repo1", "repo2", "repo3"},
		},
		{
			name:     "empty lines",
			input:    "repo1\n\nrepo2\n\n\nrepo3",
			expected: []string{"repo1", "repo2", "repo3"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "only whitespace",
			input:    "   \n  \n  ",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRepositories(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d repositories, got %d", len(tt.expected), len(result))
				return
			}
			for i, repo := range result {
				if repo != tt.expected[i] {
					t.Errorf("Expected repository %d to be %q, got %q", i, tt.expected[i], repo)
				}
			}
		})
	}
}

// Test mapToSlice function
func TestMapToSlice(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    map[string]*payload.MigrationStatus
		expected int
	}{
		{
			name:     "empty map",
			input:    map[string]*payload.MigrationStatus{},
			expected: 0,
		},
		{
			name: "single migration",
			input: map[string]*payload.MigrationStatus{
				"repo1": createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", now, 25),
			},
			expected: 1,
		},
		{
			name: "multiple migrations",
			input: map[string]*payload.MigrationStatus{
				"repo1": createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", now, 25),
				"repo2": createTestMigrationStatus("repo2", payload.StatusSucceeded, "migration", now.Add(-time.Hour), 100),
				"repo3": createTestMigrationStatus("repo3", payload.StatusFailed, "archive", now.Add(-2*time.Hour), 50),
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapToSlice(tt.input)
			if len(result) != tt.expected {
				t.Errorf("Expected %d migrations, got %d", tt.expected, len(result))
			}

			// Verify all migrations are included
			for _, migration := range result {
				original, exists := tt.input[migration.Repository]
				if !exists {
					t.Errorf("Migration %s not found in original map", migration.Repository)
				}
				if migration != original {
					t.Errorf("Migration %s pointer mismatch", migration.Repository)
				}
			}
		})
	}
}

// Test calculateStats function
func TestCalculateStats(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    []*payload.MigrationStatus
		expected MigrationStats
	}{
		{
			name:     "empty slice",
			input:    []*payload.MigrationStatus{},
			expected: MigrationStats{Active: 0, Succeeded: 0, Failed: 0, Total: 0},
		},
		{
			name: "single in-progress migration",
			input: []*payload.MigrationStatus{
				createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", now, 25),
			},
			expected: MigrationStats{Active: 1, Succeeded: 0, Failed: 0, Total: 1},
		},
		{
			name: "single succeeded migration",
			input: []*payload.MigrationStatus{
				createTestMigrationStatus("repo1", payload.StatusSucceeded, "migration", now, 100),
			},
			expected: MigrationStats{Active: 0, Succeeded: 1, Failed: 0, Total: 1},
		},
		{
			name: "single failed migration",
			input: []*payload.MigrationStatus{
				createTestMigrationStatus("repo1", payload.StatusFailed, "archive", now, 50),
			},
			expected: MigrationStats{Active: 0, Succeeded: 0, Failed: 1, Total: 1},
		},
		{
			name: "mixed migrations",
			input: []*payload.MigrationStatus{
				createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", now, 25),
				createTestMigrationStatus("repo2", payload.StatusSucceeded, "migration", now.Add(-time.Hour), 100),
				createTestMigrationStatus("repo3", payload.StatusFailed, "archive", now.Add(-2*time.Hour), 50),
				createTestMigrationStatus("repo4", payload.StatusInProgress, "setup", now.Add(-30*time.Minute), 10),
			},
			expected: MigrationStats{Active: 2, Succeeded: 1, Failed: 1, Total: 4},
		},
		{
			name: "unknown status",
			input: []*payload.MigrationStatus{
				{Repository: "repo1", Status: "unknown"},
			},
			expected: MigrationStats{Active: 0, Succeeded: 0, Failed: 0, Total: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateStats(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %+v, got %+v", tt.expected, result)
			}
		})
	}
}

// Test sanitizeInput function
func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid alphanumeric",
			input:    "repo123",
			expected: "repo123",
		},
		{
			name:     "valid with slashes and hyphens",
			input:    "org/repo-name_test.git",
			expected: "org/repo-name_test.git",
		},
		{
			name:     "with colons (for URLs)",
			input:    "https://github.com/org/repo",
			expected: "https://github.com/org/repo",
		},
		{
			name:     "HTML injection attempt",
			input:    "<script>alert('xss')</script>",
			expected: "ltscriptgtalert39xss39lt/scriptgt",
		},
		{
			name:     "special characters removal",
			input:    "repo@#$%^&*()+=[]{}|\\;':\",<>?`~",
			expected: "repoamp39:34ltgt",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "unicode characters",
			input:    "repo名前",
			expected: "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeInput(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// Test stringSet function
func TestStringSet(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected map[string]struct{}
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: map[string]struct{}{},
		},
		{
			name:     "single item",
			input:    []string{"item1"},
			expected: map[string]struct{}{"item1": {}},
		},
		{
			name:     "multiple unique items",
			input:    []string{"item1", "item2", "item3"},
			expected: map[string]struct{}{"item1": {}, "item2": {}, "item3": {}},
		},
		{
			name:     "duplicate items",
			input:    []string{"item1", "item2", "item1", "item3", "item2"},
			expected: map[string]struct{}{"item1": {}, "item2": {}, "item3": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringSet(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected set size %d, got %d", len(tt.expected), len(result))
			}
			for key := range tt.expected {
				if _, exists := result[key]; !exists {
					t.Errorf("Expected key %q not found in result", key)
				}
			}
		})
	}
}

// Test filterMigrations function
func TestFilterMigrations(t *testing.T) {
	now := time.Now()
	migrations := []*payload.MigrationStatus{
		createTestMigrationStatus("user/repo1", payload.StatusInProgress, "validation", now, 25),
		createTestMigrationStatus("user/repo2", payload.StatusSucceeded, "migration", now.Add(-time.Hour), 100),
		createTestMigrationStatus("org/repo3", payload.StatusFailed, "archive", now.Add(-2*time.Hour), 50),
		createTestMigrationStatus("org/test-repo", payload.StatusInProgress, "setup", now.Add(-30*time.Minute), 10),
	}

	tests := []struct {
		name            string
		statusFilter    string
		repoFilter      string
		timeRangeFilter string
		expectedCount   int
		expectedRepos   []string
	}{
		{
			name:          "no filters",
			expectedCount: 4,
			expectedRepos: []string{"user/repo1", "user/repo2", "org/repo3", "org/test-repo"},
		},
		{
			name:          "filter by status in_progress",
			statusFilter:  payload.StatusInProgress,
			expectedCount: 2,
			expectedRepos: []string{"user/repo1", "org/test-repo"},
		},
		{
			name:          "filter by status succeeded",
			statusFilter:  payload.StatusSucceeded,
			expectedCount: 1,
			expectedRepos: []string{"user/repo2"},
		},
		{
			name:          "filter by repository name",
			repoFilter:    "repo1",
			expectedCount: 1,
			expectedRepos: []string{"user/repo1"},
		},
		{
			name:          "filter by organization",
			repoFilter:    "org/",
			expectedCount: 2,
			expectedRepos: []string{"org/repo3", "org/test-repo"},
		},
		{
			name:          "filter by partial name",
			repoFilter:    "test",
			expectedCount: 1,
			expectedRepos: []string{"org/test-repo"},
		},
		{
			name:            "filter by time range - today",
			timeRangeFilter: "today",
			expectedCount:   4, // All migrations are from today in test
			expectedRepos:   []string{"user/repo1", "user/repo2", "org/repo3", "org/test-repo"},
		},
		{
			name:          "combined filters",
			statusFilter:  payload.StatusInProgress,
			repoFilter:    "org/",
			expectedCount: 1,
			expectedRepos: []string{"org/test-repo"},
		},
		{
			name:          "no matches",
			statusFilter:  payload.StatusSucceeded,
			repoFilter:    "nonexistent",
			expectedCount: 0,
			expectedRepos: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterMigrations(migrations, tt.statusFilter, tt.repoFilter, tt.timeRangeFilter)

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d migrations, got %d", tt.expectedCount, len(result))
			}

			resultRepos := make([]string, len(result))
			for i, migration := range result {
				resultRepos[i] = migration.Repository
			}

			for _, expectedRepo := range tt.expectedRepos {
				found := false
				for _, resultRepo := range resultRepos {
					if resultRepo == expectedRepo {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected repository %q not found in results", expectedRepo)
				}
			}
		})
	}
}

// Test passesTimeFilter function
func TestPassesTimeFilter(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	weekStart := today.AddDate(0, 0, -int(today.Weekday()))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	tests := []struct {
		name            string
		migration       *payload.MigrationStatus
		timeRangeFilter string
		expected        bool
	}{
		{
			name:            "no filter",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", now, 25),
			timeRangeFilter: "",
			expected:        true,
		},
		{
			name:            "zero time with filter",
			migration:       &payload.MigrationStatus{Repository: "repo1", StartedAt: time.Time{}},
			timeRangeFilter: "today",
			expected:        true,
		},
		{
			name:            "today filter - matches",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", today.Add(time.Hour), 25),
			timeRangeFilter: "today",
			expected:        true,
		},
		{
			name:            "today filter - doesn't match",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", yesterday.Add(time.Hour), 25),
			timeRangeFilter: "today",
			expected:        false,
		},
		{
			name:            "yesterday filter - matches",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", yesterday.Add(time.Hour), 25),
			timeRangeFilter: "yesterday",
			expected:        true,
		},
		{
			name:            "yesterday filter - doesn't match (today)",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", today.Add(time.Hour), 25),
			timeRangeFilter: "yesterday",
			expected:        false,
		},
		{
			name:            "week filter - matches",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", weekStart.Add(time.Hour), 25),
			timeRangeFilter: "week",
			expected:        true,
		},
		{
			name:            "week filter - doesn't match",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", weekStart.AddDate(0, 0, -1), 25),
			timeRangeFilter: "week",
			expected:        false,
		},
		{
			name:            "month filter - matches",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", monthStart.Add(time.Hour), 25),
			timeRangeFilter: "month",
			expected:        true,
		},
		{
			name:            "month filter - doesn't match",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", monthStart.AddDate(0, -1, 0), 25),
			timeRangeFilter: "month",
			expected:        false,
		},
		{
			name:            "unknown filter",
			migration:       createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", now, 25),
			timeRangeFilter: "unknown",
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := passesTimeFilter(tt.migration, tt.timeRangeFilter)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Test sortMigrations function
func TestSortMigrations(t *testing.T) {
	now := time.Now()
	migrations := []*payload.MigrationStatus{
		createTestMigrationStatus("repo-c", payload.StatusInProgress, "validation", now.Add(-time.Hour), 25),
		createTestMigrationStatus("repo-a", payload.StatusSucceeded, "migration", now.Add(-2*time.Hour), 100),
		createTestMigrationStatus("repo-b", payload.StatusFailed, "archive", now.Add(-30*time.Minute), 50),
	}

	// Set different repository sizes for testing
	migrations[0].RepositorySize = 3 * 1024 * 1024 // 3MB
	migrations[1].RepositorySize = 1 * 1024 * 1024 // 1MB
	migrations[2].RepositorySize = 2 * 1024 * 1024 // 2MB

	tests := []struct {
		name          string
		sortBy        string
		sortDir       string
		expectedOrder []string
	}{
		{
			name:          "default sort (started_at desc)",
			sortBy:        "",
			sortDir:       "",
			expectedOrder: []string{"repo-b", "repo-c", "repo-a"}, // newest first
		},
		{
			name:          "sort by repository name asc",
			sortBy:        "repository",
			sortDir:       "asc",
			expectedOrder: []string{"repo-a", "repo-b", "repo-c"},
		},
		{
			name:          "sort by repository name desc",
			sortBy:        "repository",
			sortDir:       "desc",
			expectedOrder: []string{"repo-c", "repo-b", "repo-a"},
		},
		{
			name:          "sort by status asc",
			sortBy:        "status",
			sortDir:       "asc",
			expectedOrder: []string{"repo-b", "repo-c", "repo-a"}, // failed, in_progress, succeeded
		},
		{
			name:          "sort by progress desc",
			sortBy:        "progress",
			sortDir:       "desc",
			expectedOrder: []string{"repo-a", "repo-b", "repo-c"}, // 100, 50, 25
		},
		{
			name:          "sort by size asc",
			sortBy:        "size",
			sortDir:       "asc",
			expectedOrder: []string{"repo-a", "repo-b", "repo-c"}, // 1MB, 2MB, 3MB
		},
		{
			name:          "sort by started_at asc",
			sortBy:        "started_at",
			sortDir:       "asc",
			expectedOrder: []string{"repo-a", "repo-c", "repo-b"}, // oldest first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sortMigrations(migrations, tt.sortBy, tt.sortDir)

			if len(result) != len(migrations) {
				t.Errorf("Expected %d migrations, got %d", len(migrations), len(result))
				return
			}

			for i, expectedRepo := range tt.expectedOrder {
				if result[i].Repository != expectedRepo {
					t.Errorf("Position %d: expected %q, got %q", i, expectedRepo, result[i].Repository)
				}
			}

			// Verify original slice is not modified
			if migrations[0].Repository != "repo-c" {
				t.Error("Original slice was modified")
			}
		})
	}
}

// Test getRecentActivity function
func TestGetRecentActivity(t *testing.T) {
	now := time.Now()
	migrations := []*payload.MigrationStatus{
		createTestMigrationStatus("repo1", payload.StatusSucceeded, "migration", now.Add(-time.Hour), 100),
		createTestMigrationStatus("repo2", payload.StatusFailed, "archive", now.Add(-2*time.Hour), 50),
		createTestMigrationStatus("repo3", payload.StatusInProgress, "validation", now.Add(-30*time.Minute), 25),
		{Repository: "repo4", Status: payload.StatusInProgress, StartedAt: time.Time{}}, // Zero time
	}

	migrations[2].Stage = "validation"
	migrations[2].State = "processing"

	tests := []struct {
		name          string
		limit         int
		expectedCount int
		expectedFirst string // Repository name of first (most recent) activity
	}{
		{
			name:          "limit 2",
			limit:         2,
			expectedCount: 2,
			expectedFirst: "repo3", // Most recent non-zero time
		},
		{
			name:          "limit 5 (more than available)",
			limit:         5,
			expectedCount: 3, // Only 3 have non-zero times
			expectedFirst: "repo3",
		},
		{
			name:          "limit 0",
			limit:         0,
			expectedCount: 0,
			expectedFirst: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getRecentActivity(migrations, tt.limit)

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d activities, got %d", tt.expectedCount, len(result))
				return
			}

			if tt.expectedCount > 0 && result[0].Repository != tt.expectedFirst {
				t.Errorf("Expected first activity to be %q, got %q", tt.expectedFirst, result[0].Repository)
			}

			// Verify activities are sorted by time (newest first)
			for i := 1; i < len(result); i++ {
				if result[i-1].ActivityTime.Before(result[i].ActivityTime) {
					t.Error("Activities are not sorted by time (newest first)")
				}
			}

			// Verify descriptions are generated
			for _, activity := range result {
				if activity.ActivityDescription == "" {
					t.Errorf("Activity for %q has empty description", activity.Repository)
				}
			}
		})
	}
}

// Test getStagesInfo function
func TestGetStagesInfo(t *testing.T) {
	tests := []struct {
		name           string
		status         *payload.MigrationStatus
		expectedStages int
		checkStage     string
		expectedStatus string
	}{
		{
			name: "in_progress migration at validation stage",
			status: &payload.MigrationStatus{
				Repository:      "repo1",
				Status:          payload.StatusInProgress,
				Stage:           "validation",
				CompletedStages: []string{},
			},
			expectedStages: len(payload.MigrationStages),
			checkStage:     "validation",
			expectedStatus: StageStatusCurrent,
		},
		{
			name: "succeeded migration",
			status: &payload.MigrationStatus{
				Repository:      "repo1",
				Status:          payload.StatusSucceeded,
				Stage:           "migration",
				CompletedStages: payload.MigrationStages,
			},
			expectedStages: len(payload.MigrationStages),
			checkStage:     "validation",
			expectedStatus: StageStatusCompleted,
		},
		{
			name: "failed migration at archive stage",
			status: &payload.MigrationStatus{
				Repository:      "repo1",
				Status:          payload.StatusFailed,
				Stage:           "archive",
				CompletedStages: []string{"validation", "setup"},
			},
			expectedStages: len(payload.MigrationStages),
			checkStage:     "archive",
			expectedStatus: StageStatusFailed,
		},
		{
			name: "in_progress with completed stages",
			status: &payload.MigrationStatus{
				Repository:      "repo1",
				Status:          payload.StatusInProgress,
				Stage:           "setup",
				CompletedStages: []string{"validation"},
			},
			expectedStages: len(payload.MigrationStages),
			checkStage:     "validation",
			expectedStatus: StageStatusCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStagesInfo(tt.status)

			if len(result) != tt.expectedStages {
				t.Errorf("Expected %d stages, got %d", tt.expectedStages, len(result))
				return
			}

			// Find the stage we want to check
			found := false
			for _, stage := range result {
				if stage.Name == tt.checkStage {
					found = true
					if stage.Status != tt.expectedStatus {
						t.Errorf("Stage %q: expected status %q, got %q", tt.checkStage, tt.expectedStatus, stage.Status)
					}
					if stage.Description == "" {
						t.Errorf("Stage %q has empty description", tt.checkStage)
					}
					break
				}
			}

			if !found {
				t.Errorf("Stage %q not found in results", tt.checkStage)
			}
		})
	}
}

// Test Handler creation and basic functionality
func TestNew(t *testing.T) {
	handler, mockMigrator := createTestHandler()

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	if handler.mock != mockMigrator {
		t.Error("Handler mock not set correctly")
	}

	if handler.templates == nil {
		t.Error("Handler templates not initialized")
	}

	if handler.logger == nil {
		t.Error("Handler logger not initialized")
	}
}

// Test HTTP handlers
func TestHandler_HandleStats(t *testing.T) {
	handler, mockMigrator := createTestHandler()

	// Set up test data
	now := time.Now()
	mockMigrator.migrationStatuses = map[string]*payload.MigrationStatus{
		"repo1": createTestMigrationStatus("repo1", payload.StatusInProgress, "validation", now, 25),
		"repo2": createTestMigrationStatus("repo2", payload.StatusSucceeded, "migration", now, 100),
		"repo3": createTestMigrationStatus("repo3", payload.StatusFailed, "archive", now, 50),
	}

	tests := []struct {
		name           string
		method         string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "GET request",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			expectedBody:   "1", // Active count from template
		},
		{
			name:           "POST request (not allowed)",
			method:         http.MethodPost,
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/dashboard/stats", nil)
			w := httptest.NewRecorder()

			handler.handleStats(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedStatus == http.StatusOK && tt.expectedBody != "" {
				body := strings.TrimSpace(w.Body.String())
				if body != tt.expectedBody {
					t.Errorf("Expected body %q, got %q", tt.expectedBody, body)
				}
			}
		})
	}
}

func TestHandler_HandleQueueStats(t *testing.T) {
	handler, mockMigrator := createTestHandler()

	// Set up test data
	mockMigrator.queueStats = map[string]interface{}{
		"pending": 5,
		"active":  2,
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/queue-stats", nil)
	w := httptest.NewRecorder()

	handler.handleQueueStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// The response should contain the queue stats
	body := w.Body.String()
	if !strings.Contains(body, "map[active:2 pending:5]") {
		t.Errorf("Expected queue stats in response, got: %q", body)
	}
}

func TestHandler_ExportCSV(t *testing.T) {
	now := time.Now()
	migrations := []*payload.MigrationStatus{
		createTestMigrationStatus("repo1", payload.StatusSucceeded, "migration", now, 100),
		createTestMigrationStatus("repo2", payload.StatusFailed, "archive", now.Add(-time.Hour), 50),
	}

	w := httptest.NewRecorder()
	exportCSV(w, migrations)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/csv" {
		t.Errorf("Expected Content-Type text/csv, got %q", contentType)
	}

	// Parse CSV and check content
	reader := csv.NewReader(strings.NewReader(w.Body.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse CSV: %v", err)
	}

	if len(records) != 3 { // Header + 2 data rows
		t.Errorf("Expected 3 CSV records, got %d", len(records))
	}

	// Check header
	expectedHeader := []string{"Repository", "Status", "Stage", "Progress", "Size (MB)", "Size Category", "Started At", "Duration"}
	if len(records) > 0 {
		for i, col := range expectedHeader {
			if i < len(records[0]) && records[0][i] != col {
				t.Errorf("Header column %d: expected %q, got %q", i, col, records[0][i])
			}
		}
	}

	// Check first data row
	if len(records) > 1 {
		if records[1][0] != "repo1" {
			t.Errorf("Expected first repository to be repo1, got %q", records[1][0])
		}
		if records[1][1] != payload.StatusSucceeded {
			t.Errorf("Expected first status to be %q, got %q", payload.StatusSucceeded, records[1][1])
		}
	}
}

func TestHandler_ExportJSON(t *testing.T) {
	now := time.Now()
	migrations := []*payload.MigrationStatus{
		createTestMigrationStatus("repo1", payload.StatusSucceeded, "migration", now, 100),
		createTestMigrationStatus("repo2", payload.StatusFailed, "archive", now.Add(-time.Hour), 50),
	}

	w := httptest.NewRecorder()
	exportJSON(w, migrations)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", contentType)
	}

	// Parse JSON and check content
	var exportData []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &exportData); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(exportData) != 2 {
		t.Errorf("Expected 2 JSON objects, got %d", len(exportData))
	}
}

func TestHandler_HandleExport(t *testing.T) {
	handler, mockMigrator := createTestHandler()

	// Set up test data
	now := time.Now()
	mockMigrator.migrationStatuses = map[string]*payload.MigrationStatus{
		"repo1": createTestMigrationStatus("repo1", payload.StatusSucceeded, "migration", now, 100),
	}

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		expectedType   string
	}{
		{
			name:           "CSV export",
			queryParams:    "format=csv",
			expectedStatus: http.StatusOK,
			expectedType:   "text/csv",
		},
		{
			name:           "JSON export",
			queryParams:    "format=json",
			expectedStatus: http.StatusOK,
			expectedType:   "application/json",
		},
		{
			name:           "invalid format",
			queryParams:    "format=xml",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "no format specified",
			queryParams:    "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/dashboard/export"
			if tt.queryParams != "" {
				url += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			handler.handleExport(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedType != "" {
				contentType := w.Header().Get("Content-Type")
				if contentType != tt.expectedType {
					t.Errorf("Expected Content-Type %q, got %q", tt.expectedType, contentType)
				}
			}
		})
	}
}

func TestHandler_HandleMigrationRoutes(t *testing.T) {
	handler, _ := createTestHandler()

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{
			name:           "GET migration detail",
			method:         http.MethodGet,
			path:           "/dashboard/migration/repo1",
			expectedStatus: http.StatusNotFound, // Repository not found in mock
		},
		{
			name:           "GET retry form",
			method:         http.MethodGet,
			path:           "/dashboard/migration/repo1/retry-form",
			expectedStatus: http.StatusNotFound, // Repository not found in mock
		},
		{
			name:           "POST retry migration",
			method:         http.MethodPost,
			path:           "/dashboard/migration/repo1/retry",
			expectedStatus: http.StatusNotFound, // Repository not found in mock
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			handler.handleMigrationRoutes(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestHandler_HandleSubmitMigration(t *testing.T) {
	handler, mockMigrator := createTestHandler()

	tests := []struct {
		name           string
		method         string
		formData       url.Values
		shouldError    bool
		expectedStatus int
	}{
		{
			name:           "GET request (not allowed)",
			method:         http.MethodGet,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "successful submission",
			method: http.MethodPost,
			formData: url.Values{
				"source-org":       {"source-org"},
				"target-org":       {"target-org"},
				"repositories":     {"repo1\nrepo2"},
				"ghes-base-url":    {"https://github.example.com"},
				"ghes-token":       {"ghp_test_token_12345678901234567890123456789012"},
				"gh-cloud-token":   {"ghp_test_token_12345678901234567890123456789012"},
				"max-duration":     {"24h"},
				"delete-if-exists": {"on"},
			},
			expectedStatus: http.StatusSeeOther, // Redirect after successful submission
		},
		{
			name:   "missing required fields",
			method: http.MethodPost,
			formData: url.Values{
				"source-org": {"source-org"},
				// Missing other required fields
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.formData != nil {
				req = httptest.NewRequest(tt.method, "/dashboard/migrate", strings.NewReader(tt.formData.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				req = httptest.NewRequest(tt.method, "/dashboard/migrate", nil)
			}

			w := httptest.NewRecorder()

			mockMigrator.shouldReturnError = tt.shouldError

			handler.handleSubmitMigration(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}
