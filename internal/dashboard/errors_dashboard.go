package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
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

// NewErrorsDashboard returns a handler for the errors dashboard
func NewErrorsDashboard() http.HandlerFunc {
	tmpl := template.Must(template.New("errors").Parse(`
<div class="card">
  <div class="card-header">
    <h5 class="card-title">Error Distribution by Category</h5>
    <h6 class="card-subtitle text-muted">Last updated: {{.Stats.LastUpdate.Format "Jan 02, 15:04:05"}}</h6>
  </div>
  <div class="card-body">
    <div class="row">
      <div class="col-md-8">
        <canvas id="errorChart" width="400" height="300"></canvas>
      </div>
      <div class="col-md-4">
        <table class="table table-sm">
          <thead>
            <tr>
              <th>Category</th>
              <th>Count</th>
              <th>%</th>
            </tr>
          </thead>
          <tbody>
            {{range $category, $count := .Stats.Categories}}
            <tr>
              <td>{{$category}}</td>
              <td>{{$count}}</td>
              <td>{{percentage $count $.Stats.TotalErrors}}%</td>
            </tr>
            {{end}}
          </tbody>
          <tfoot>
            <tr>
              <th>Total</th>
              <th>{{.Stats.TotalErrors}}</th>
              <th>100%</th>
            </tr>
          </tfoot>
        </table>
      </div>
    </div>
  </div>
</div>

<script>
  document.addEventListener('DOMContentLoaded', function() {
    var ctx = document.getElementById('errorChart').getContext('2d');
    // Safely parse the JSON string from server
    var chartData = JSON.parse('{{.ChartData}}');
    var errorChart = new Chart(ctx, {
      type: 'pie',
      data: chartData,
      options: {
        responsive: true,
        plugins: {
          legend: {
            position: 'right',
          },
          tooltip: {
            callbacks: {
              label: function(context) {
                var label = context.label || '';
                var value = context.raw || 0;
                var percentage = (value / {{.Stats.TotalErrors}} * 100).toFixed(1);
                return label + ': ' + value + ' (' + percentage + '%)';
              }
            }
          }
        }
      }
    });
  });
</script>
`))

	// Add a function to calculate percentage
	tmpl.Funcs(template.FuncMap{
		"percentage": func(count, total int) string {
			if total == 0 {
				return "0.0"
			}
			return fmt.Sprintf("%.1f", float64(count)/float64(total)*100)
		},
	})

	return func(w http.ResponseWriter, r *http.Request) {
		// Get error stats
		stats := getErrorStats()

		// Prepare chart data
		chartData := prepareChartData(stats)

		// Render template
		if err := tmpl.Execute(w, ErrorsData{
			Stats:     stats,
			ChartData: chartData,
		}); err != nil {
			http.Error(w, "Failed to render error dashboard", http.StatusInternalServerError)
			return
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
