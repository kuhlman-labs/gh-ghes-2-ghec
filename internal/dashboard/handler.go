// Package dashboard implements a web dashboard for viewing migration status.
// It provides handlers for serving dashboard pages and handling dashboard-related requests.
package dashboard

import (
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed templates/*.html
var templateFS embed.FS

// Handler represents the dashboard HTTP handler
type Handler struct {
	templates *template.Template
	migrator  *migrator.Migrator
	logger    *slog.Logger
}

// TemplateData represents the data passed to the templates
type TemplateData struct {
	Title            string
	Active           string
	PageName         string
	CurrentYear      int
	Migrations       []*payload.MigrationStatus
	Migration        *payload.MigrationStatus
	ArchivedAttempts []*payload.MigrationStatus // Added for displaying historical attempts
	Stats            MigrationStats
	QueueStats       map[string]interface{} // Added for queue statistics
	Stages           []StageInfo
	Error            string
	Success          string
	PageSize         int
	SearchQuery      string
	AttemptCount     int
	// New fields for filtering and sorting
	StatusFilter    string
	RepoFilter      string
	TimeRangeFilter string
	SortBy          string
	SortDir         string
	RecentActivity  []ActivityEvent
	LastUpdate      time.Time // Added for last update timestamp
}

// MigrationStats represents statistics about migrations
type MigrationStats struct {
	Active    int
	Succeeded int
	Failed    int
	Total     int
}

// StageInfo represents information about a migration stage
type StageInfo struct {
	Name        string
	Description string
	Status      string // "completed", "current", "pending", "failed", "skipped"
}

// Stage status constants for StageInfo.Status
const (
	StageStatusCompleted = "completed"
	StageStatusCurrent   = "current"
	StageStatusPending   = "pending"
	StageStatusFailed    = "failed"
	StageStatusSkipped   = "skipped"
)

// stageDescriptions maps internal stage names to user-friendly descriptions.
var stageDescriptions = map[string]string{
	"validation": "Repository validation",
	"setup":      "Migration setup and source creation",
	"archive":    "Archive generation and export",
	"storage":    "Storage upload (e.g., GitHub Owned Storage)",
	"migration":  "Repository migration to target",
	// "completion" was in the old hardcoded list, but not in payload.MigrationStages.
	// If there are other stages from payload.MigrationStages, they can be added here.
}

// Status constants
const (
	StatusRunning = "in_progress" // Same as payload.StatusInProgress
)

// ActivityEvent represents a timeline entry for migration activity
type ActivityEvent struct {
	Repository          string
	Status              string
	ActivityTime        time.Time
	ActivityDescription string
}

// New creates a new dashboard handler
func New(m *migrator.Migrator) (*Handler, error) {
	// Create template functions
	funcMap := template.FuncMap{
		"ToLower": strings.ToLower,
		"Title": func(s string) string {
			if s == "" {
				return ""
			}
			return cases.Title(language.English).String(s)
		},
		"FormatTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Format("15:04:05")
		},
		"FormatDateTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"FormatDuration": func(d time.Duration) string {
			if d == 0 {
				return "-"
			}
			hours := int(d.Hours())
			minutes := int(d.Minutes()) % 60
			seconds := int(d.Seconds()) % 60

			return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
		},
		"percentage": func(count, total int) string {
			if total == 0 {
				return "0.0"
			}
			return fmt.Sprintf("%.1f", float64(count)/float64(total)*100)
		},
		"divFloat": func(value int64, divisor float64) float64 {
			return float64(value) / divisor
		},
	}

	// Parse templates
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Handler{
		templates: tmpl,
		migrator:  m,
		logger:    logging.Get(),
	}, nil
}

// RegisterHandlers registers the dashboard handlers with the provided mux
func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	// Dashboard overview
	mux.HandleFunc("/dashboard", h.handleOverview)
	mux.HandleFunc("/dashboard/refresh", h.handleRefresh)
	mux.HandleFunc("/dashboard/stats", h.handleStats)
	mux.HandleFunc("/dashboard/queue-stats", h.handleQueueStats)

	// New export endpoints
	mux.HandleFunc("/dashboard/export", h.handleExport)

	// Migration detail and retry - use a single handler for the path
	mux.HandleFunc("/dashboard/migration/", h.handleMigrationRoutes)

	// New migration form
	mux.HandleFunc("/dashboard/new", h.handleNewMigration)
	mux.HandleFunc("/dashboard/migrate", h.handleSubmitMigration)

	// History page and export
	mux.HandleFunc("/dashboard/history", h.handleHistory)
	mux.HandleFunc("/dashboard/history/export", h.handleHistoryExport)

	// Serve static files with relative path detection only
	staticDir := "static"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		// If the default relative path doesn't work, try other relative paths
		workDir, err := os.Getwd()
		if err != nil {
			workDir = "."
		}

		possiblePaths := []string{
			filepath.Join(workDir, "static"), // Relative to working directory
			"../static",                      // One directory up
			"../../static",                   // Two directories up
		}

		for _, path := range possiblePaths {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				staticDir = path
				break
			}
		}
	}

	// Log the static directory path we're using
	h.logger.Info("Static files directory", "path", staticDir)

	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
}

// handleOverview handles the dashboard overview page
func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get migrations from the migrator
	migrationsMap := h.migrator.GetAllMigrationStatuses()

	// Parse query parameters
	pageSizeStr := r.URL.Query().Get("page-size")
	statusFilter := r.URL.Query().Get("filter-status")
	repoFilter := r.URL.Query().Get("filter-repo")
	timeRangeFilter := r.URL.Query().Get("filter-timerange")
	sortBy := r.URL.Query().Get("sort-by")
	sortDir := r.URL.Query().Get("sort-dir")

	// Set default page size
	pageSize := 20
	if pageSizeStr != "" {
		var err error
		pageSize, err = strconv.Atoi(pageSizeStr)
		if err != nil {
			pageSize = 20
		}
	}

	// Get queued repositories (waiting for worker)
	queuedRepos := h.migrator.GetQueuedRepositories()
	queuedReposSet := stringSet(queuedRepos)

	// Convert map to slice
	migrationsSlice := mapToSlice(migrationsMap)

	// For each migration, if in_progress and (stage is empty or unknown) and in queuedReposSet, override stage/state
	for _, m := range migrationsSlice {
		if m.Status == "in_progress" && (m.Stage == "" || m.Stage == "unknown") {
			if _, isQueued := queuedReposSet[m.Repository]; isQueued {
				m.Stage = "queued"
				m.State = "waiting for worker"
			}
		}
	}

	// Apply filters
	filteredMigrations := filterMigrations(migrationsSlice, statusFilter, repoFilter, timeRangeFilter)

	// Apply sorting
	sortedMigrations := sortMigrations(filteredMigrations, sortBy, sortDir)

	// Calculate migration statistics
	stats := calculateStats(migrationsSlice)

	// Get queue statistics
	queueStats := h.migrator.GetQueueStats()

	// Get recent activity events
	recentActivity := getRecentActivity(migrationsSlice, 10)

	data := TemplateData{
		Title:           "Dashboard",
		Active:          "dashboard",
		PageName:        "overview",
		CurrentYear:     time.Now().Year(),
		Migrations:      sortedMigrations,
		Stats:           stats,
		QueueStats:      queueStats,
		PageSize:        pageSize,
		StatusFilter:    statusFilter,
		RepoFilter:      repoFilter,
		TimeRangeFilter: timeRangeFilter,
		SortBy:          sortBy,
		SortDir:         sortDir,
		RecentActivity:  recentActivity,
		LastUpdate:      time.Now(),
	}

	err := h.templates.ExecuteTemplate(w, "base", data)
	if err != nil {
		h.logger.Error("failed to execute template", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// handleRefresh handles refreshing the migrations table
func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// Get migrations from the migrator
	migrationsMap := h.migrator.GetAllMigrationStatuses()

	// Parse query parameters
	pageSizeStr := r.URL.Query().Get("page-size")
	statusFilter := r.URL.Query().Get("filter-status")
	repoFilter := r.URL.Query().Get("filter-repo")
	timeRangeFilter := r.URL.Query().Get("filter-timerange")
	sortBy := r.URL.Query().Get("sort-by")
	sortDir := r.URL.Query().Get("sort-dir")

	// Set default page size
	pageSize := 20
	if pageSizeStr != "" {
		var err error
		pageSize, err = strconv.Atoi(pageSizeStr)
		if err != nil {
			pageSize = 20
		}
	}

	// Get queued repositories (waiting for worker)
	queuedRepos := h.migrator.GetQueuedRepositories()
	queuedReposSet := stringSet(queuedRepos)

	// Convert map to slice
	migrationsSlice := mapToSlice(migrationsMap)

	// For each migration, if in_progress and (stage is empty or unknown) and in queuedReposSet, override stage/state
	for _, m := range migrationsSlice {
		if m.Status == "in_progress" && (m.Stage == "" || m.Stage == "unknown") {
			if _, isQueued := queuedReposSet[m.Repository]; isQueued {
				m.Stage = "queued"
				m.State = "waiting for worker"
			}
		}
	}

	// Apply filters
	filteredMigrations := filterMigrations(migrationsSlice, statusFilter, repoFilter, timeRangeFilter)

	// Apply sorting
	sortedMigrations := sortMigrations(filteredMigrations, sortBy, sortDir)

	// Calculate migration statistics (needed for templates)
	stats := calculateStats(migrationsSlice)

	data := TemplateData{
		Migrations:      sortedMigrations,
		Stats:           stats,
		PageSize:        pageSize,
		StatusFilter:    statusFilter,
		RepoFilter:      repoFilter,
		TimeRangeFilter: timeRangeFilter,
		SortBy:          sortBy,
		SortDir:         sortDir,
		LastUpdate:      time.Now(),
	}

	err := h.templates.ExecuteTemplate(w, "migrations_table", data)
	if err != nil {
		h.logger.Error("failed to execute template", "error", err)
		http.Error(w, "Failed to render migrations table", http.StatusInternalServerError)
		return
	}
}

// handleExport handles exporting migrations data
func (h *Handler) handleExport(w http.ResponseWriter, r *http.Request) {
	// Get migrations from the migrator
	migrationsMap := h.migrator.GetAllMigrationStatuses()

	// Parse query parameters
	statusFilter := r.URL.Query().Get("filter-status")
	repoFilter := r.URL.Query().Get("filter-repo")
	timeRangeFilter := r.URL.Query().Get("filter-timerange")
	sortBy := r.URL.Query().Get("sort-by")
	sortDir := r.URL.Query().Get("sort-dir")
	format := r.URL.Query().Get("format")

	// Convert map to slice
	migrationsSlice := mapToSlice(migrationsMap)

	// Apply filters
	filteredMigrations := filterMigrations(migrationsSlice, statusFilter, repoFilter, timeRangeFilter)

	// Apply sorting
	sortedMigrations := sortMigrations(filteredMigrations, sortBy, sortDir)

	// Log export
	h.logger.Info("Exporting migrations data",
		"format", format,
		"count", len(sortedMigrations),
		"filters", map[string]string{
			"status":    statusFilter,
			"repo":      repoFilter,
			"timeRange": timeRangeFilter,
		})

	// Export based on format
	switch format {
	case "csv":
		exportCSV(w, sortedMigrations)
	case "json":
		exportJSON(w, sortedMigrations)
	default:
		http.Error(w, "Invalid export format", http.StatusBadRequest)
	}
}

// exportCSV exports migrations data as CSV
func exportCSV(w http.ResponseWriter, migrations []*payload.MigrationStatus) {
	// Set headers for file download
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=migrations.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	header := []string{
		"Repository",
		"Status",
		"Stage",
		"Progress",
		"Size (MB)",
		"Size Category",
		"Started At",
		"Duration",
	}
	if err := writer.Write(header); err != nil {
		http.Error(w, "Failed to write CSV header", http.StatusInternalServerError)
		return
	}

	// Write data
	for _, m := range migrations {
		stage := "-"
		if m.Stage != "" {
			stage = fmt.Sprintf("%s: %s", m.Stage, m.State)
		}

		size := "-"
		if m.RepositorySize > 0 {
			size = fmt.Sprintf("%.1f", float64(m.RepositorySize)/1048576)
		}

		startedAt := "-"
		if !m.StartedAt.IsZero() {
			startedAt = m.StartedAt.Format("2006-01-02 15:04:05")
		}

		duration := "-"
		if m.Duration > 0 {
			duration = m.Duration.String()
		}

		row := []string{
			m.Repository,
			m.Status,
			stage,
			strconv.Itoa(m.Progress),
			size,
			string(m.SizeCategory),
			startedAt,
			duration,
		}
		if err := writer.Write(row); err != nil {
			http.Error(w, "Failed to write CSV row", http.StatusInternalServerError)
			return
		}
	}
}

// exportJSON exports migrations data as JSON
func exportJSON(w http.ResponseWriter, migrations []*payload.MigrationStatus) {
	// Set headers for file download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=migrations.json")

	// Create a simplified version for export
	type ExportMigration struct {
		Repository     string    `json:"repository"`
		Status         string    `json:"status"`
		Stage          string    `json:"stage,omitempty"`
		State          string    `json:"state,omitempty"`
		Progress       int       `json:"progress"`
		RepositorySize int64     `json:"repository_size_bytes,omitempty"`
		SizeCategory   string    `json:"size_category,omitempty"`
		StartedAt      time.Time `json:"started_at"`
		Duration       string    `json:"duration"`
	}

	exportData := make([]ExportMigration, 0, len(migrations))
	for _, m := range migrations {
		export := ExportMigration{
			Repository:     m.Repository,
			Status:         m.Status,
			Stage:          m.Stage,
			State:          m.State,
			Progress:       m.Progress,
			RepositorySize: m.RepositorySize,
			SizeCategory:   string(m.SizeCategory),
			StartedAt:      m.StartedAt,
			Duration:       m.Duration.String(),
		}
		exportData = append(exportData, export)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(exportData); err != nil {
		http.Error(w, "Failed to encode JSON data", http.StatusInternalServerError)
		return
	}
}

// filterMigrations applies filters to the migrations list
func filterMigrations(migrations []*payload.MigrationStatus, statusFilter, repoFilter, timeRangeFilter string) []*payload.MigrationStatus {
	result := make([]*payload.MigrationStatus, 0, len(migrations))

	for _, m := range migrations {
		// Apply status filter
		if statusFilter != "" && !strings.EqualFold(m.Status, statusFilter) {
			continue
		}

		// Apply repository filter
		if repoFilter != "" && !strings.Contains(strings.ToLower(m.Repository), strings.ToLower(repoFilter)) {
			continue
		}

		// Apply time range filter
		if !passesTimeFilter(m, timeRangeFilter) {
			continue
		}

		result = append(result, m)
	}

	return result
}

// passesTimeFilter checks if a migration passes the time range filter
func passesTimeFilter(m *payload.MigrationStatus, timeRangeFilter string) bool {
	if timeRangeFilter == "" || m.StartedAt.IsZero() {
		return true
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch timeRangeFilter {
	case "today":
		return m.StartedAt.After(today) || m.StartedAt.Equal(today)
	case "yesterday":
		yesterday := today.AddDate(0, 0, -1)
		return m.StartedAt.After(yesterday) && m.StartedAt.Before(today)
	case "week":
		weekStart := today.AddDate(0, 0, -int(today.Weekday()))
		return m.StartedAt.After(weekStart) || m.StartedAt.Equal(weekStart)
	case "month":
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return m.StartedAt.After(monthStart) || m.StartedAt.Equal(monthStart)
	default:
		return true
	}
}

// sortMigrations sorts the migrations based on the given criteria
func sortMigrations(migrations []*payload.MigrationStatus, sortBy, sortDir string) []*payload.MigrationStatus {
	// Make a copy to avoid modifying the original
	result := make([]*payload.MigrationStatus, len(migrations))
	copy(result, migrations)

	// If no sort criteria, default to started_at descending
	if sortBy == "" {
		sortBy = "started_at"
		sortDir = "desc"
	}

	// If no sort direction, default to ascending
	if sortDir == "" {
		sortDir = "asc"
	}

	// Sort by the specified column
	sort.SliceStable(result, func(i, j int) bool {
		// Determine sort order
		ascending := sortDir == "asc"

		// Default comparison result (for ascending)
		var less bool

		switch sortBy {
		case "repository":
			less = result[i].Repository < result[j].Repository
		case "status":
			less = result[i].Status < result[j].Status
		case "stage":
			less = result[i].Stage < result[j].Stage
		case "progress":
			less = result[i].Progress < result[j].Progress
		case "size":
			less = result[i].RepositorySize < result[j].RepositorySize
		case "started_at":
			// Handle zero time values
			if result[i].StartedAt.IsZero() {
				less = false // Zero times are "greater" (come last)
			} else if result[j].StartedAt.IsZero() {
				less = true // Non-zero times are "less" (come first)
			} else {
				less = result[i].StartedAt.Before(result[j].StartedAt)
			}
		case "duration":
			less = result[i].Duration < result[j].Duration
		default:
			// Default to sorting by started_at
			if result[i].StartedAt.IsZero() {
				less = false
			} else if result[j].StartedAt.IsZero() {
				less = true
			} else {
				less = result[i].StartedAt.Before(result[j].StartedAt)
			}
		}

		// Reverse if descending order
		if !ascending {
			less = !less
		}

		return less
	})

	return result
}

// getRecentActivity generates activity events for display in the timeline
func getRecentActivity(migrations []*payload.MigrationStatus, limit int) []ActivityEvent {
	events := make([]ActivityEvent, 0)

	for _, m := range migrations {
		// Skip migrations with zero time
		if m.StartedAt.IsZero() {
			continue
		}

		// Create description based on status
		description := ""
		switch m.Status {
		case "succeeded", "completed":
			description = "Migration completed successfully"
		case "failed", "error":
			description = "Migration failed"
		case "running", "in_progress", "active":
			if m.Stage != "" {
				description = fmt.Sprintf("Currently in %s stage (%s)", m.Stage, m.State)
			} else {
				description = "Migration in progress"
			}
		case "pending":
			description = "Migration pending"
		default:
			description = fmt.Sprintf("Status: %s", m.Status)
		}

		// Add event
		event := ActivityEvent{
			Repository:          m.Repository,
			Status:              m.Status,
			ActivityTime:        m.StartedAt,
			ActivityDescription: description,
		}

		events = append(events, event)
	}

	// Sort by time (newest first)
	sort.Slice(events, func(i, j int) bool {
		return events[i].ActivityTime.After(events[j].ActivityTime)
	})

	// Limit the number of events
	if len(events) > limit {
		events = events[:limit]
	}

	return events
}

// handleMigrationRoutes routes requests to the appropriate handler based on the URL path and method
func (h *Handler) handleMigrationRoutes(w http.ResponseWriter, r *http.Request) {
	// Check if it's a retry request
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/retry") {
		h.handleRetryMigration(w, r)
		return
	}

	// Check if it's a retry form request
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/retry-form") {
		h.handleRetryForm(w, r)
		return
	}

	// Otherwise handle as a detail or refresh request
	h.handleMigrationDetail(w, r)
}

// handleMigrationDetail handles the migration detail page
func (h *Handler) handleMigrationDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/dashboard/migration/")
	path = strings.TrimSuffix(path, "/")

	// Sanitize the path to prevent XSS
	path = sanitizeInput(path)

	parts := strings.Split(path, "/")

	var repoFullName string
	var isRefresh bool

	if len(parts) == 2 { // org/repo
		repoFullName = parts[0] + "/" + parts[1]
	} else if len(parts) == 3 && parts[2] == "refresh" { // org/repo/refresh
		repoFullName = parts[0] + "/" + parts[1]
		isRefresh = true
	} else {
		http.Error(w, "Invalid migration URL. Expected /dashboard/migration/{org}/{repoName} or /dashboard/migration/{org}/{repoName}/refresh", http.StatusBadRequest)
		return
	}

	if repoFullName == "" {
		http.Error(w, "Repository name cannot be empty", http.StatusBadRequest)
		return
	}

	// Get current migration status
	status := h.migrator.GetMigrationStatus(repoFullName)
	if status == nil && !isRefresh { // If not a refresh and status is nil, it's a 404 for the main page load
		http.Error(w, fmt.Sprintf("Migration not found for %s", repoFullName), http.StatusNotFound)
		return
	}
	// If it IS a refresh and status is nil, it means the migration might have been deleted.
	// The template should handle a nil Migration object gracefully.

	// Get archived migration attempts
	archivedAttempts, err := h.migrator.GetArchivedMigrationAttempts(repoFullName)
	if err != nil {
		// Log the error but don't necessarily fail the entire page load, especially if current status is available.
		// The template can show an error message for the archive section.
		h.logger.Warn("Error fetching archived attempts",
			"repository", repoFullName,
			"error", err.Error())
		// Optionally, set an error in TemplateData to display in the template
	}

	// Get the number of retry attempts
	attemptCount := len(archivedAttempts)

	// Create stages information (only if current status exists)
	var stages []StageInfo
	if status != nil {
		stages = getStagesInfo(status)
	}

	pageTitle := "Migration Details"
	if status != nil {
		pageTitle = fmt.Sprintf("Migration: %s", status.Repository) // status.Repository is repoFullName
	} else if repoFullName != "" {
		pageTitle = fmt.Sprintf("Migration: %s", repoFullName)
	}

	data := TemplateData{
		Title:            pageTitle,
		Active:           "overview", // Or a specific active state if details page has its own nav item
		PageName:         "migration_detail",
		CurrentYear:      time.Now().Year(),
		Migration:        status, // Can be nil if it was a refresh and migration got deleted
		ArchivedAttempts: archivedAttempts,
		AttemptCount:     attemptCount,
		Stages:           stages,
	}

	if isRefresh {
		// For AJAX refresh, render only the detail content part
		// The template "migration_detail_content.html" should be designed to handle a potentially nil data.Migration
		if status == nil {
			// If the migration is gone on refresh, we might want to return an empty div or a specific message.
			// For now, let the template handle nil; it might render nothing or an error.
			w.Header().Set("HX-Reswap", "outerHTML")           // Ensure HTMX replaces the target
			w.Header().Set("HX-Retarget", "#migration-detail") // Target the main detail container
			// Consider sending a specific empty response or a "not found" partial.
			// For now, we send the template which will render based on nil status.
		}
		if err := h.templates.ExecuteTemplate(w, "migration_detail_content", data); err != nil {
			http.Error(w, fmt.Sprintf("Failed to render migration detail content template: %v", err), http.StatusInternalServerError)
		}
	} else {
		// For full page load, render the entire page
		if status == nil {
			// This case should have been caught earlier for non-refresh yielding a 404.
			// However, as a safeguard:
			http.Error(w, fmt.Sprintf("Migration not found for %s for full page load", repoFullName), http.StatusNotFound)
			return
		}
		if err := h.templates.ExecuteTemplate(w, "base", data); err != nil {
			http.Error(w, fmt.Sprintf("Failed to render base template for migration detail: %v", err), http.StatusInternalServerError)
		}
	}
}

// handleNewMigration handles the new migration form page
func (h *Handler) handleNewMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := TemplateData{
		Title:       "New Migration",
		Active:      "new",
		PageName:    "new_migration",
		CurrentYear: time.Now().Year(),
	}

	if err := h.templates.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

// handleHistory handles the migration history page
func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse page size from query parameters, default to 20
	pageSizeStr := r.URL.Query().Get("page-size")
	pageSize := 20 // Default
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil {
			pageSize = ps
		}
	}

	// Get and sanitize search query parameter
	searchQuery := sanitizeInput(r.URL.Query().Get("search"))

	// Get sort parameters
	sortBy := r.URL.Query().Get("sort-by")
	sortDir := r.URL.Query().Get("sort-dir")

	// Get all migration statuses and convert from map to slice
	migrationsMap := h.migrator.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)

	// Filter to show only completed (succeeded or failed) migrations in the history
	var completedMigrations []*payload.MigrationStatus
	for _, migration := range allMigrations {
		if migration.Status == payload.StatusSucceeded || migration.Status == payload.StatusFailed {
			completedMigrations = append(completedMigrations, migration)
		}
	}

	// Apply search filter if a search query is provided
	var filteredMigrations []*payload.MigrationStatus
	if searchQuery != "" {
		searchQuery = strings.ToLower(searchQuery)
		for _, migration := range completedMigrations {
			// Case-insensitive search of repository name
			if strings.Contains(strings.ToLower(migration.Repository), searchQuery) {
				filteredMigrations = append(filteredMigrations, migration)
			}
		}
	} else {
		filteredMigrations = completedMigrations
	}

	// Apply sorting (before pagination)
	sortedMigrations := sortMigrations(filteredMigrations, sortBy, sortDir)

	// Apply pagination if pageSize > 0
	displayMigrations := sortedMigrations
	if pageSize > 0 && len(displayMigrations) > pageSize {
		displayMigrations = displayMigrations[:pageSize]
	}

	data := TemplateData{
		Title:       "Migration History",
		Active:      "history",
		PageName:    "history",
		CurrentYear: time.Now().Year(),
		Migrations:  displayMigrations,
		PageSize:    pageSize,
		SearchQuery: searchQuery,
		SortBy:      sortBy,
		SortDir:     sortDir,
	}

	if err := h.templates.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "Failed to render template for history", http.StatusInternalServerError)
	}
}

// handleSubmitMigration handles the migration form submission
func (h *Handler) handleSubmitMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Extract and sanitize form fields
	sourceOrg := sanitizeInput(r.FormValue("source_org"))
	targetOrg := sanitizeInput(r.FormValue("target_org"))
	ghesBaseURL := sanitizeInput(r.FormValue("ghes_base_url"))
	ghesToken := r.FormValue("ghes_token") // Tokens don't need sanitization for XSS
	ghCloudToken := r.FormValue("gh_cloud_token")
	maxDuration := sanitizeInput(r.FormValue("max_duration"))
	useGHOS := r.FormValue("use_ghos") == "true"

	// Parse repositories (one per line)
	repoText := r.FormValue("repositories")
	repositories := parseRepositories(repoText)

	// Sanitize repository names
	for i, repo := range repositories {
		repositories[i] = sanitizeInput(repo)
	}

	// Process scheduling parameters
	var scheduledTime *time.Time
	scheduledTimeStr := r.FormValue("scheduled_time")
	if scheduledTimeStr != "" {
		t, err := time.Parse("2006-01-02T15:04", scheduledTimeStr)
		if err == nil {
			scheduledTime = &t
		} else {
			h.logger.Error("Failed to parse scheduled time", "error", err, "input", scheduledTimeStr)
		}
	}

	// Get time zone
	scheduledTimeZone := sanitizeInput(r.FormValue("scheduled_time_zone"))

	// Get day restrictions
	scheduledDaysOnly := r.Form["scheduled_days_only"]
	for i, day := range scheduledDaysOnly {
		scheduledDaysOnly[i] = sanitizeInput(day)
	}

	// Get time window
	scheduledTimeStart := sanitizeInput(r.FormValue("scheduled_time_start"))
	scheduledTimeEnd := sanitizeInput(r.FormValue("scheduled_time_end"))

	// Create migration request
	migrationReq := &payload.MigrationRequest{
		SourceOrg:          sourceOrg,
		TargetOrg:          targetOrg,
		GHESBaseURL:        ghesBaseURL,
		GHESToken:          ghesToken,
		GHCloudToken:       ghCloudToken,
		Repositories:       repositories,
		MaxDuration:        maxDuration,
		UseGHOS:            useGHOS,
		ScheduledTime:      scheduledTime,
		ScheduledTimeZone:  scheduledTimeZone,
		ScheduledDaysOnly:  scheduledDaysOnly,
		ScheduledTimeStart: scheduledTimeStart,
		ScheduledTimeEnd:   scheduledTimeEnd,
	}

	// Validate the request
	if err := migrationReq.Validate(); err != nil {
		data := TemplateData{
			Title:       "New Migration",
			Active:      "new",
			CurrentYear: time.Now().Year(),
			Error:       "Validation error: " + html.EscapeString(err.Error()),
		}
		if err := h.templates.ExecuteTemplate(w, "base", data); err != nil {
			http.Error(w, "Failed to render template", http.StatusInternalServerError)
		}
		return
	}

	// Create a context and cancel function for the migration
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	// Start the migration
	err := h.migrator.StartMigration(ctx, migrationReq, cancel)
	if err != nil {
		data := TemplateData{
			Title:       "New Migration",
			Active:      "new",
			CurrentYear: time.Now().Year(),
			Error:       "Failed to start migration: " + html.EscapeString(err.Error()),
		}
		if err := h.templates.ExecuteTemplate(w, "base", data); err != nil {
			http.Error(w, "Failed to render template", http.StatusInternalServerError)
		}
		return
	}

	// Redirect to dashboard with success message
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// parseRepositories splits a multi-line string into a slice of repository names
func parseRepositories(repoText string) []string {
	var repos []string
	lines := strings.Split(repoText, "\n")

	for _, line := range lines {
		// Trim whitespace
		repo := strings.TrimSpace(line)
		if repo != "" {
			// No sanitization here, as we sanitize in the calling function
			repos = append(repos, repo)
		}
	}

	return repos
}

// mapToSlice converts a map of migration statuses to a slice
func mapToSlice(migrationsMap map[string]*payload.MigrationStatus) []*payload.MigrationStatus {
	migrations := make([]*payload.MigrationStatus, 0, len(migrationsMap))
	for _, status := range migrationsMap {
		migrations = append(migrations, status)
	}
	return migrations
}

// calculateStats calculates statistics about migrations
func calculateStats(migrations []*payload.MigrationStatus) MigrationStats {
	var stats MigrationStats
	stats.Total = len(migrations)

	for _, m := range migrations {
		switch m.Status {
		case payload.StatusInProgress: // in_progress
			stats.Active++
		case payload.StatusSucceeded:
			stats.Succeeded++
		case payload.StatusFailed:
			stats.Failed++
		}
	}

	return stats
}

// getStagesInfo creates information about migration stages based on the overall migration status.
func getStagesInfo(status *payload.MigrationStatus) []StageInfo {
	var newStages []StageInfo
	allDefinedStages := payload.MigrationStages // Use the canonical list of stages from payload package

	isStageCompleted := func(stageName string, completedStages []string) bool {
		for _, s := range completedStages {
			if s == stageName {
				return true
			}
		}
		return false
	}

	// Find the index of the current/active/failed stage in the canonical list
	activeStageIndex := -1
	if status.Stage != "" {
		for i, s := range allDefinedStages {
			if s == status.Stage {
				activeStageIndex = i
				break
			}
		}
	}

	for i, stageName := range allDefinedStages {
		description, ok := stageDescriptions[stageName]
		if !ok {
			// Fallback for stages not in our description map (should be kept up-to-date)
			description = cases.Title(language.English).String(strings.ReplaceAll(stageName, "_", " "))
		}

		stageInfo := StageInfo{
			Name:        stageName,
			Description: description,
		}

		switch status.Status {
		case payload.StatusFailed:
			if activeStageIndex != -1 { // A specific stage is identified as the point of failure
				if i < activeStageIndex { // Stages before the one that failed
					if isStageCompleted(stageName, status.CompletedStages) {
						stageInfo.Status = StageStatusCompleted
					} else {
						// If it's before the failed stage but not in CompletedStages, it was likely skipped or an earlier, unrecorded issue.
						stageInfo.Status = StageStatusSkipped
					}
				} else if i == activeStageIndex { // The stage that failed
					stageInfo.Status = StageStatusFailed
				} else { // Stages after the one that failed
					stageInfo.Status = StageStatusSkipped
				}
			} else {
				// Generic failure, unsure which stage failed or it's not in the defined list.
				// Mark all as skipped as a precaution, or based on CompletedStages if available.
				if isStageCompleted(stageName, status.CompletedStages) {
					stageInfo.Status = StageStatusCompleted
				} else {
					stageInfo.Status = StageStatusSkipped
				}
			}
		case payload.StatusSucceeded:
			// If overall status is Succeeded, all defined stages are considered completed.
			// payload.MigrationStatus.CompletedStages should ideally list all of them.
			stageInfo.Status = StageStatusCompleted
		case payload.StatusInProgress:
			if activeStageIndex != -1 { // A specific stage is currently active
				if i < activeStageIndex { // Stages before the current one
					if isStageCompleted(stageName, status.CompletedStages) {
						stageInfo.Status = StageStatusCompleted
					} else {
						// This implies a stage prior to current wasn't marked completed.
						// Could be an issue in how CompletedStages is populated, or it was genuinely not run yet.
						stageInfo.Status = StageStatusPending
					}
				} else if i == activeStageIndex { // The current stage
					stageInfo.Status = StageStatusCurrent
				} else { // Stages after the current one
					stageInfo.Status = StageStatusPending
				}
			} else {
				// InProgress but no specific current stage known or it's not in allDefinedStages.
				// Mark based on CompletedStages, otherwise pending.
				if isStageCompleted(stageName, status.CompletedStages) {
					stageInfo.Status = StageStatusCompleted
				} else {
					stageInfo.Status = StageStatusPending
				}
			}
		default:
			// Unknown overall status, default to pending for safety.
			stageInfo.Status = StageStatusPending
		}
		newStages = append(newStages, stageInfo)
	}
	return newStages
}

// handleStats returns just the migration statistics for HTMX updates
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all migration statuses and calculate stats
	migrationsMap := h.migrator.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)
	stats := calculateStats(allMigrations)

	// Create a template with just the stats HTML
	statsTemplate := `
		<div class="stat-card">
			<h3>Active</h3>
			<span class="stat-value">{{ .Active }}</span>
		</div>
		<div class="stat-card">
			<h3>Succeeded</h3>
			<span class="stat-value">{{ .Succeeded }}</span>
		</div>
		<div class="stat-card">
			<h3>Failed</h3>
			<span class="stat-value">{{ .Failed }}</span>
		</div>
		<div class="stat-card">
			<h3>Total</h3>
			<span class="stat-value">{{ .Total }}</span>
		</div>
	`

	// Parse and execute the template using html/template which auto-escapes
	tmpl, err := template.New("stats").Parse(statsTemplate)
	if err != nil {
		http.Error(w, "Failed to parse stats template", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, stats); err != nil {
		http.Error(w, "Failed to render stats", http.StatusInternalServerError)
	}
}

// handleQueueStats returns just the queue statistics for HTMX updates
func (h *Handler) handleQueueStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get queue statistics
	queueStats := h.migrator.GetQueueStats()

	// Count active migrations in the 'archive' stage
	migrationsMap := h.migrator.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)
	activeArchives := 0
	for _, m := range allMigrations {
		if m.Status == "in_progress" && m.Stage == "archive" {
			activeArchives++
		}
	}
	queueStats["active_archive_generations"] = activeArchives

	// Ensure max_migration_threads is set, default to 10 if missing
	if _, exists := queueStats["max_migration_threads"]; !exists {
		queueStats["max_migration_threads"] = 10
	}

	// Debug log the queue stats
	h.logger.Info("Queue stats returned from migrator",
		"queue_size", queueStats["queue_size"],
		"max_queue_size", queueStats["max_queue_size"],
		"active_archive_generations", queueStats["active_archive_generations"],
		"max_archive_generations", queueStats["max_archive_generations"],
		"active_migrations", queueStats["active_migrations"],
		"max_migration_threads", queueStats["max_migration_threads"])

	// Create a template with just the queue stats HTML
	queueStatsTemplate := `
		<div class="stat-card">
			<h3>Queue Size</h3>
			<span class="stat-value">{{ .queue_size }}</span>
			<span class="stat-label">/ {{ .max_queue_size }}</span>
		</div>
		<div class="stat-card">
			<h3>Active Archives</h3>
			<span class="stat-value">{{ .active_archive_generations }}</span>
			<span class="stat-label">/ {{ .max_archive_generations }}</span>
		</div>
		<div class="stat-card">
			<h3>Active Migrations</h3>
			<span class="stat-value">{{ .active_migrations }}</span>
			<span class="stat-label">/ {{ .max_migration_threads }}</span>
		</div>
	`

	// Parse and execute the template using html/template which auto-escapes
	tmpl, err := template.New("queue_stats").Parse(queueStatsTemplate)
	if err != nil {
		http.Error(w, "Failed to parse queue stats template", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, queueStats); err != nil {
		http.Error(w, "Failed to render queue stats", http.StatusInternalServerError)
	}
}

// handleRetryForm serves the retry form for a failed migration
func (h *Handler) handleRetryForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract repository name from URL path
	path := r.URL.Path
	repoPath := strings.TrimPrefix(path, "/dashboard/migration/")
	repoPath = strings.TrimSuffix(repoPath, "/retry-form")

	// Sanitize the repository path
	repoPath = sanitizeInput(repoPath)

	// Check if we have a valid repository path
	if repoPath == "" || !strings.Contains(repoPath, "/") {
		http.Error(w, "Invalid repository path", http.StatusBadRequest)
		return
	}

	// Get the current migration status to populate the form with saved values
	status := h.migrator.GetMigrationStatus(repoPath)

	// Create a template data struct to pass to the template
	templateData := struct {
		Repository  string
		TargetOrg   string
		GHESBaseURL string
		UseGHOS     bool
	}{
		Repository: repoPath,
	}

	// If we have a status, populate the template data with saved values
	if status != nil {
		templateData.TargetOrg = status.TargetOrg
		templateData.GHESBaseURL = status.GHESBaseURL
		templateData.UseGHOS = status.UseGHOS
	}

	// Use the templates to render the retry form
	if err := h.templates.ExecuteTemplate(w, "retry_form", templateData); err != nil {
		h.logger.Error("Failed to render retry form template",
			"repository", repoPath,
			"error", err.Error())
		http.Error(w, "Failed to render retry form", http.StatusInternalServerError)
	}
}

// handleRetryMigration handles the retry button click on the migration detail page
func (h *Handler) handleRetryMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract repository name from URL path
	path := r.URL.Path
	if !strings.HasSuffix(path, "/retry") {
		return // Not a retry request, let other handlers process it
	}

	// Format: /dashboard/migration/org/repo/retry
	// Remove the /dashboard/migration/ prefix and the /retry suffix
	repoPath := strings.TrimPrefix(path, "/dashboard/migration/")
	repoPath = strings.TrimSuffix(repoPath, "/retry")

	// Sanitize the repository path
	repoPath = sanitizeInput(repoPath)

	// Check if we have a valid repository path
	if repoPath == "" || !strings.Contains(repoPath, "/") {
		http.Error(w, "Invalid repository path", http.StatusBadRequest)
		return
	}

	// Parse form data to get the tokens and GHES URL
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse form data: %v", err), http.StatusBadRequest)
		return
	}

	// Extract and sanitize the required form fields
	ghesBaseURL := sanitizeInput(r.FormValue("ghes_base_url"))
	ghesToken := r.FormValue("ghes_token") // No need to sanitize tokens as they're not displayed
	ghCloudToken := r.FormValue("gh_cloud_token")
	useGHOS := r.FormValue("use_ghos") == "true"

	// Validate the required fields
	if ghesBaseURL == "" || ghesToken == "" || ghCloudToken == "" {
		http.Error(w, "Missing required field: GHES URL, GHES token, and GitHub Cloud token are required", http.StatusBadRequest)
		return
	}

	// Validate the repository path format
	parts := strings.Split(repoPath, "/")
	if len(parts) != 2 {
		http.Error(w, "Invalid repository path format", http.StatusBadRequest)
		return
	}

	// Extract and sanitize target org - either from form or default to source org
	targetOrg := sanitizeInput(r.FormValue("target_org"))
	if targetOrg == "" {
		// If not provided, default to source org
		targetOrg = sanitizeInput(parts[0]) // parts[0] is the source org name
	}

	// Note: We'll update the existing migration record with the provided credentials
	// This makes them available for the RetryMigration operation
	currentStatus := h.migrator.GetMigrationStatus(repoPath)
	if currentStatus != nil {
		// Store the non-sensitive values for future retries
		currentStatus.TargetOrg = targetOrg
		currentStatus.GHESBaseURL = ghesBaseURL
		currentStatus.UseGHOS = useGHOS
	}

	// Create a detached background context with no timeout
	// This ensures that the entire migration process has unlimited time to complete
	bgCtx := context.Background()

	// Add correlation ID for tracking
	bgCtx = logging.ContextWithCorrelationID(bgCtx)

	// Call the dedicated RetryMigration method instead of StartMigration
	// Pass the tokens, URL, and targetOrg from the form
	var migrationErr error
	if err := h.migrator.RetryMigration(bgCtx, repoPath, ghesToken, ghCloudToken, ghesBaseURL, targetOrg); err != nil {
		migrationErr = err
		// Log the error using the structured logger
		h.logger.Error("Failed to initiate migration retry",
			"repository", repoPath,
			"error", err.Error())
	} else {
		h.logger.Info("Migration retry initiated successfully",
			"repository", repoPath)
	}

	// Since RetryMigration already starts the actual work in a background goroutine,
	// we don't need to wait for it to start

	// Load the updated migration status
	status := h.migrator.GetMigrationStatus(repoPath)
	if status == nil {
		http.Error(w, "Failed to load migration status after retry", http.StatusInternalServerError)
		return
	}

	// Add error message if migration failed to start
	var errorMessage string
	if migrationErr != nil {
		errorMessage = html.EscapeString(fmt.Sprintf("Warning: Migration may have issues: %v", migrationErr))
	}

	// Get archived migration attempts
	var archivedAttempts []*payload.MigrationStatus
	var attemptCount int
	var templateErr error
	archivedAttempts, templateErr = h.migrator.GetArchivedMigrationAttempts(repoPath)
	if templateErr != nil {
		// Log error but continue - archived attempts are non-critical
		h.logger.Warn("Error getting archived attempts",
			"repository", repoPath,
			"error", templateErr.Error())
	}
	attemptCount = len(archivedAttempts)

	// Add HTMX headers to close the modal and update the content
	w.Header().Set("HX-Trigger", "closeModal")

	// Prepare the template data
	templateData := TemplateData{
		Title:            "Migration Detail",
		Active:           "migrations",
		PageName:         "migration_detail",
		CurrentYear:      time.Now().Year(),
		Migration:        status,
		ArchivedAttempts: archivedAttempts,
		AttemptCount:     attemptCount,
		Stages:           getStagesInfo(status),
		Success:          "Migration retry initiated successfully",
		Error:            errorMessage,
	}

	// Render the migration detail template
	err := h.templates.ExecuteTemplate(w, "migration_detail_content", templateData)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

// sanitizeInput sanitizes user input to prevent XSS attacks
func sanitizeInput(input string) string {
	// HTML escape the input to prevent XSS
	escaped := html.EscapeString(input)

	// Only allow alphanumeric characters, slashes, hyphens, underscores, periods, and colons
	// This is suitable for repository paths, URLs, and organization names
	re := regexp.MustCompile(`[^a-zA-Z0-9/\-_.:]`)
	sanitized := re.ReplaceAllString(escaped, "")

	return sanitized
}

// Helper to build a set from a slice
func stringSet(slice []string) map[string]struct{} {
	set := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		set[s] = struct{}{}
	}
	return set
}

// handleHistoryExport handles exporting migration history in various formats
func (h *Handler) handleHistoryExport(w http.ResponseWriter, r *http.Request) {
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

	// Parse page size from query parameters
	pageSizeStr := r.URL.Query().Get("page-size")
	pageSize := 0 // Default to all for exports
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil {
			pageSize = ps
		}
	}

	// Get search query parameter
	searchQuery := sanitizeInput(r.URL.Query().Get("search"))

	// Get sort parameters
	sortBy := r.URL.Query().Get("sort-by")
	sortDir := r.URL.Query().Get("sort-dir")

	// Get all migration statuses and convert from map to slice
	migrationsMap := h.migrator.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)

	// Filter to show only completed (succeeded or failed) migrations in the history
	var completedMigrations []*payload.MigrationStatus
	for _, migration := range allMigrations {
		if migration.Status == payload.StatusSucceeded || migration.Status == payload.StatusFailed {
			completedMigrations = append(completedMigrations, migration)
		}
	}

	// Apply search filter if a search query is provided
	var filteredMigrations []*payload.MigrationStatus
	if searchQuery != "" {
		searchQuery = strings.ToLower(searchQuery)
		for _, migration := range completedMigrations {
			// Case-insensitive search of repository name
			if strings.Contains(strings.ToLower(migration.Repository), searchQuery) {
				filteredMigrations = append(filteredMigrations, migration)
			}
		}
	} else {
		filteredMigrations = completedMigrations
	}

	// Apply sorting
	sortedMigrations := sortMigrations(filteredMigrations, sortBy, sortDir)

	// Apply pagination if pageSize > 0
	displayMigrations := sortedMigrations
	if pageSize > 0 && len(displayMigrations) > pageSize {
		displayMigrations = displayMigrations[:pageSize]
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("migration-history-%s.%s", timestamp, format)

	// Set content disposition header to trigger download
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Export in the requested format
	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		csvWriter := csv.NewWriter(w)

		// Write header
		if err := csvWriter.Write([]string{
			"Repository", "Status", "Stage", "State", "Progress", "Started At", "Duration", "Error",
		}); err != nil {
			http.Error(w, "Failed to write CSV header", http.StatusInternalServerError)
			return
		}

		// Write data rows
		for _, m := range displayMigrations {
			if err := csvWriter.Write([]string{
				m.Repository,
				string(m.Status),
				string(m.Stage),
				string(m.State),
				fmt.Sprintf("%d", m.Progress),
				m.StartedAt.Format(time.RFC3339),
				m.Duration.String(),
				m.Error,
			}); err != nil {
				http.Error(w, "Failed to write CSV row", http.StatusInternalServerError)
				return
			}
		}
		csvWriter.Flush()

	case "json":
		w.Header().Set("Content-Type", "application/json")
		// Create a simplified struct for export
		type ExportMigration struct {
			Repository string    `json:"repository"`
			Status     string    `json:"status"`
			Stage      string    `json:"stage"`
			State      string    `json:"state"`
			Progress   int       `json:"progress"`
			StartedAt  time.Time `json:"started_at"`
			Duration   string    `json:"duration"`
			Error      string    `json:"error,omitempty"`
		}

		exportData := make([]ExportMigration, 0, len(displayMigrations))
		for _, m := range displayMigrations {
			exportData = append(exportData, ExportMigration{
				Repository: m.Repository,
				Status:     string(m.Status),
				Stage:      string(m.Stage),
				State:      string(m.State),
				Progress:   m.Progress,
				StartedAt:  m.StartedAt,
				Duration:   m.Duration.String(),
				Error:      m.Error,
			})
		}

		// Encode as JSON
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(exportData); err != nil {
			http.Error(w, "Failed to generate JSON export", http.StatusInternalServerError)
			return
		}
	}
}
