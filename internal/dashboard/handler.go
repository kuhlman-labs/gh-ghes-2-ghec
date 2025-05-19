// Package dashboard implements a web dashboard for viewing migration status.
// It provides handlers for serving dashboard pages and handling dashboard-related requests.
package dashboard

import (
	"context"
	"embed"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// New creates a new dashboard handler
func New(m *migrator.Migrator) (*Handler, error) {
	// Create template functions
	funcMap := template.FuncMap{
		"ToLower": strings.ToLower,
		"Title":   cases.Title(language.English).String,
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
	}

	// Parse templates
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Handler{
		templates: tmpl,
		migrator:  m,
	}, nil
}

// RegisterHandlers registers the dashboard handlers with the provided mux
func (h *Handler) RegisterHandlers(mux *http.ServeMux) {
	// Dashboard overview
	mux.HandleFunc("/dashboard", h.handleOverview)
	mux.HandleFunc("/dashboard/refresh", h.handleRefresh)
	mux.HandleFunc("/dashboard/stats", h.handleStats)
	mux.HandleFunc("/dashboard/queue-stats", h.handleQueueStats)

	// Migration detail and retry - use a single handler for the path
	mux.HandleFunc("/dashboard/migration/", h.handleMigrationRoutes)

	// New migration form
	mux.HandleFunc("/dashboard/new", h.handleNewMigration)
	mux.HandleFunc("/dashboard/migrate", h.handleSubmitMigration)

	// History page
	mux.HandleFunc("/dashboard/history", h.handleHistory)

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
	logging.Get().Info("Static files directory", "path", staticDir)

	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
}

// handleOverview handles the dashboard overview page
func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
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

	// Get all migration statuses and convert from map to slice
	migrationsMap := h.migrator.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)

	// Filter to show only active migrations in the overview
	var activeMigrations []*payload.MigrationStatus
	for _, migration := range allMigrations {
		if migration.Status == payload.StatusInProgress {
			activeMigrations = append(activeMigrations, migration)
		}
	}

	// Calculate stats on all migrations
	overallStats := calculateStats(allMigrations)

	// Get queue statistics
	queueStats := h.migrator.GetQueueStats()

	// Apply pagination if pageSize > 0
	displayMigrations := activeMigrations
	if pageSize > 0 && len(displayMigrations) > pageSize {
		displayMigrations = displayMigrations[:pageSize]
	}

	// Create template data
	data := TemplateData{
		Title:       "Overview",
		Active:      "overview",
		PageName:    "overview", // Note: Use lowercase to match the condition in base.html
		CurrentYear: time.Now().Year(),
		Migrations:  displayMigrations,
		Stats:       overallStats,
		QueueStats:  queueStats,
		PageSize:    pageSize,
	}

	// Render the base template which will include the overview_content
	if err := h.templates.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, fmt.Sprintf("Error rendering template: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleRefresh handles the AJAX refresh for the dashboard overview
func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
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

	// Get all migration statuses and convert from map to slice
	migrationsMap := h.migrator.GetAllMigrationStatuses()
	allMigrations := mapToSlice(migrationsMap)

	// Filter to show only active migrations in the overview
	var activeMigrations []*payload.MigrationStatus
	for _, migration := range allMigrations {
		if migration.Status == payload.StatusInProgress {
			activeMigrations = append(activeMigrations, migration)
		}
	}

	// Calculate stats on all migrations
	overallStats := calculateStats(allMigrations)

	// Apply pagination if pageSize > 0
	displayMigrations := activeMigrations
	if pageSize > 0 && len(displayMigrations) > pageSize {
		displayMigrations = displayMigrations[:pageSize]
	}

	// Render only the table part
	data := TemplateData{
		Migrations: displayMigrations,
		Stats:      overallStats,
		PageSize:   pageSize,
	}

	if err := h.templates.ExecuteTemplate(w, "migrations_table", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
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
		logging.Get().Warn("Error fetching archived attempts",
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
		if err := h.templates.ExecuteTemplate(w, "migration_detail_content.html", data); err != nil {
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
		if err := h.templates.ExecuteTemplate(w, "base.html", data); err != nil {
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

	if err := h.templates.ExecuteTemplate(w, "base.html", data); err != nil {
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

	// Apply pagination if pageSize > 0
	displayMigrations := filteredMigrations
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
	}

	if err := h.templates.ExecuteTemplate(w, "base.html", data); err != nil {
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

	// Create migration request
	migrationReq := &payload.MigrationRequest{
		SourceOrg:    sourceOrg,
		TargetOrg:    targetOrg,
		GHESBaseURL:  ghesBaseURL,
		GHESToken:    ghesToken,
		GHCloudToken: ghCloudToken,
		Repositories: repositories,
		MaxDuration:  maxDuration,
		UseGHOS:      useGHOS,
	}

	// Validate the request
	if err := migrationReq.Validate(); err != nil {
		data := TemplateData{
			Title:       "New Migration",
			Active:      "new",
			CurrentYear: time.Now().Year(),
			Error:       "Validation error: " + html.EscapeString(err.Error()),
		}
		if err := h.templates.ExecuteTemplate(w, "base.html", data); err != nil {
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
		if err := h.templates.ExecuteTemplate(w, "base.html", data); err != nil {
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

	// Ensure max_migration_threads is set, default to 10 if missing
	if _, exists := queueStats["max_migration_threads"]; !exists {
		queueStats["max_migration_threads"] = 10
	}

	// Debug log the queue stats
	logging.Get().Info("Queue stats returned from migrator",
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
		logging.Get().Error("Failed to render retry form template",
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
		logging.Get().Error("Failed to initiate migration retry",
			"repository", repoPath,
			"error", err.Error())
	} else {
		logging.Get().Info("Migration retry initiated successfully",
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
		logging.Get().Warn("Error getting archived attempts",
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
