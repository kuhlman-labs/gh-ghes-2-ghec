package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ErrorStats represents error statistics grouped by category
type ErrorStats struct {
	Categories  map[string]int
	TotalErrors int
	LastUpdate  time.Time
}

// ErrorsData holds data for the errors dashboard template
type ErrorsData struct {
	Stats     ErrorStats
	ChartData string // Changed from template.JS to string
}

// RegisterErrorsDashboard creates and returns an error dashboard handler
func RegisterErrorsDashboard(mux *http.ServeMux) {
	errDashHandler := createErrorsDashboardHandler()
	mux.Handle("/dashboard/errors", errDashHandler)
}

// createErrorsDashboardHandler creates the actual handler
func createErrorsDashboardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get error stats
		stats := getErrorStats()

		// Prepare chart data
		chartData := prepareChartData(stats)

		// Check if this is an HTMX request
		isHtmxRequest := r.Header.Get("HX-Request") == "true"

		// Create a template function map with all required functions
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

		// Create a new template with functions
		tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
		if err != nil {
			http.Error(w, "Failed to parse templates: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if isHtmxRequest {
			// For HTMX requests, only render the errors_content template
			if err := tmpl.ExecuteTemplate(w, "errors_content", map[string]interface{}{
				"Stats":     stats,
				"ChartData": chartData,
			}); err != nil {
				http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
			}
		} else {
			// For full page loads, render the base template with our data
			// Add custom data for the errors dashboard
			customData := map[string]interface{}{
				"Title":       "Error Dashboard",
				"Active":      "errors",
				"PageName":    "errors",
				"CurrentYear": time.Now().Year(),
				"Stats":       stats,
				"ChartData":   chartData,
			}

			if err := tmpl.ExecuteTemplate(w, "base.html", customData); err != nil {
				http.Error(w, "Failed to render error dashboard: "+err.Error(), http.StatusInternalServerError)
			}
		}
	}
}

// getErrorStats retrieves error statistics from the error reporting system
func getErrorStats() ErrorStats {
	// Get counts from error reporting
	counts := errors.GetErrorCounts()

	// Convert to map for template
	categories := make(map[string]int)
	totalErrors := 0

	for category, count := range counts {
		categories[string(category)] = count
		totalErrors += count
	}

	return ErrorStats{
		Categories:  categories,
		TotalErrors: totalErrors,
		LastUpdate:  time.Now(),
	}
}

// prepareChartData creates a Chart.js compatible data structure
func prepareChartData(stats ErrorStats) string {
	// Define colors for each category
	colors := map[string]string{
		string(errors.CategoryTransient):         "#3498db", // Blue
		string(errors.CategoryPermanent):         "#e74c3c", // Red
		string(errors.CategoryRateLimit):         "#f39c12", // Orange
		string(errors.CategoryAuthentication):    "#9b59b6", // Purple
		string(errors.CategoryAuthorization):     "#8e44ad", // Dark Purple
		string(errors.CategoryResourceNotFound):  "#95a5a6", // Gray
		string(errors.CategoryResourceConflict):  "#d35400", // Dark Orange
		string(errors.CategoryValidation):        "#27ae60", // Green
		string(errors.CategoryMigrationCanceled): "#2c3e50", // Dark Blue
		string(errors.CategoryInternalError):     "#c0392b", // Dark Red
		string(errors.CategoryUnknown):           "#7f8c8d", // Dark Gray
	}

	// Sort categories for consistent display
	var categories []string
	for category := range stats.Categories {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	// Build labels and data arrays
	var labels []string
	var data []int
	var backgroundColor []string

	for _, category := range categories {
		count := stats.Categories[category]
		if count > 0 {
			labels = append(labels, category)
			data = append(data, count)

			// Use predefined color or default gray
			color, ok := colors[category]
			if !ok {
				color = "#7f8c8d" // Default gray
			}
			backgroundColor = append(backgroundColor, color)
		}
	}

	// Create chart data structure
	chartDataStruct := struct {
		Labels   []string `json:"labels"`
		Datasets []struct {
			Data            []int    `json:"data"`
			BackgroundColor []string `json:"backgroundColor"`
		} `json:"datasets"`
	}{
		Labels: labels,
		Datasets: []struct {
			Data            []int    `json:"data"`
			BackgroundColor []string `json:"backgroundColor"`
		}{
			{
				Data:            data,
				BackgroundColor: backgroundColor,
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(chartDataStruct)
	if err != nil {
		// Return empty object on error
		return "{}"
	}

	// Return as string
	return string(jsonData)
}
