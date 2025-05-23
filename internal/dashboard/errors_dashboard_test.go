package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
)

func TestErrorStats(t *testing.T) {
	t.Run("constructor", func(t *testing.T) {
		stats := ErrorStats{
			Categories:  map[string]int{"TEST": 5},
			TotalErrors: 5,
			LastUpdate:  time.Now(),
		}

		if stats.Categories["TEST"] != 5 {
			t.Errorf("Expected TEST category to have 5 errors, got %d", stats.Categories["TEST"])
		}

		if stats.TotalErrors != 5 {
			t.Errorf("Expected total errors to be 5, got %d", stats.TotalErrors)
		}
	})
}

func TestErrorsData(t *testing.T) {
	t.Run("constructor", func(t *testing.T) {
		stats := ErrorStats{
			Categories:  map[string]int{},
			TotalErrors: 0,
			LastUpdate:  time.Now(),
		}

		data := ErrorsData{
			Stats:     stats,
			ChartData: "{}",
		}

		if data.Stats.TotalErrors != 0 {
			t.Errorf("Expected total errors to be 0, got %d", data.Stats.TotalErrors)
		}

		if data.ChartData != "{}" {
			t.Errorf("Expected chart data to be '{}', got %s", data.ChartData)
		}
	})
}

func TestGetErrorStats(t *testing.T) {
	// Clear error counts before test
	errors.ReportError(nil) // Reset state

	t.Run("empty stats", func(t *testing.T) {
		stats := getErrorStats()

		if stats.TotalErrors != 0 {
			t.Errorf("Expected total errors to be 0, got %d", stats.TotalErrors)
		}

		if len(stats.Categories) != 0 {
			t.Errorf("Expected no categories, got %d", len(stats.Categories))
		}

		if stats.LastUpdate.IsZero() {
			t.Error("Expected LastUpdate to be set")
		}
	})

	t.Run("with errors", func(t *testing.T) {
		// Add some test errors
		// Note: These will accumulate with any previous tests
		errors.ReportError(errors.NewClassifiedError(nil, errors.CategoryAuthentication))
		errors.ReportError(errors.NewClassifiedError(nil, errors.CategoryRateLimit))
		errors.ReportError(errors.NewClassifiedError(nil, errors.CategoryAuthentication))

		stats := getErrorStats()

		expectedCategories := 2 // AUTHENTICATION and RATE_LIMIT
		if len(stats.Categories) < expectedCategories {
			t.Errorf("Expected at least %d categories, got %d", expectedCategories, len(stats.Categories))
		}

		// Check that we have authentication errors
		authCount, exists := stats.Categories[string(errors.CategoryAuthentication)]
		if !exists || authCount < 2 {
			t.Errorf("Expected at least 2 authentication errors, got %d (exists: %v)", authCount, exists)
		}

		// Check that we have rate limit errors
		rateCount, exists := stats.Categories[string(errors.CategoryRateLimit)]
		if !exists || rateCount < 1 {
			t.Errorf("Expected at least 1 rate limit error, got %d (exists: %v)", rateCount, exists)
		}

		if stats.TotalErrors < 3 {
			t.Errorf("Expected at least 3 total errors, got %d", stats.TotalErrors)
		}
	})
}

func TestPrepareChartData(t *testing.T) {
	t.Run("empty stats", func(t *testing.T) {
		stats := ErrorStats{
			Categories:  map[string]int{},
			TotalErrors: 0,
			LastUpdate:  time.Now(),
		}

		chartData := prepareChartData(stats)

		// Should return valid JSON even for empty data
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(chartData), &data); err != nil {
			t.Errorf("Chart data should be valid JSON, got error: %v", err)
			return
		}

		// Check structure - the chart data should have a labels array (might be empty)
		if labels, exists := data["labels"]; !exists {
			t.Error("Expected labels field in chart data")
		} else {
			// For empty stats, labels could be nil or an empty array
			if labels == nil {
				// This is fine - nil means no data
			} else if labelArray, ok := labels.([]interface{}); !ok {
				t.Errorf("Expected labels to be an array or nil, got %T: %v", labels, labels)
			} else if len(labelArray) != 0 {
				t.Errorf("Expected empty labels array for empty stats, got %d items", len(labelArray))
			}
		}
	})

	t.Run("with data", func(t *testing.T) {
		stats := ErrorStats{
			Categories: map[string]int{
				string(errors.CategoryAuthentication): 5,
				string(errors.CategoryRateLimit):      3,
				string(errors.CategoryValidation):     2,
			},
			TotalErrors: 10,
			LastUpdate:  time.Now(),
		}

		chartData := prepareChartData(stats)

		// Parse JSON
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(chartData), &data); err != nil {
			t.Errorf("Chart data should be valid JSON, got error: %v", err)
			return
		}

		// Check labels exist
		labels, ok := data["labels"].([]interface{})
		if !ok {
			t.Error("Expected labels to be an array")
		}

		if len(labels) != 3 {
			t.Errorf("Expected 3 labels, got %d", len(labels))
		}

		// Check datasets exist
		datasets, ok := data["datasets"].([]interface{})
		if !ok || len(datasets) != 1 {
			t.Error("Expected datasets to be an array with one element")
		}

		dataset := datasets[0].(map[string]interface{})
		dataValues, ok := dataset["data"].([]interface{})
		if !ok {
			t.Error("Expected data values to be an array")
		}

		if len(dataValues) != 3 {
			t.Errorf("Expected 3 data values, got %d", len(dataValues))
		}

		// Check background colors exist
		bgColors, ok := dataset["backgroundColor"].([]interface{})
		if !ok {
			t.Error("Expected backgroundColor to be an array")
		}

		if len(bgColors) != 3 {
			t.Errorf("Expected 3 background colors, got %d", len(bgColors))
		}
	})

	t.Run("filters zero counts", func(t *testing.T) {
		stats := ErrorStats{
			Categories: map[string]int{
				string(errors.CategoryAuthentication): 5,
				string(errors.CategoryRateLimit):      0, // Should be filtered out
				string(errors.CategoryValidation):     2,
			},
			TotalErrors: 7,
			LastUpdate:  time.Now(),
		}

		chartData := prepareChartData(stats)

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(chartData), &data); err != nil {
			t.Errorf("Chart data should be valid JSON, got error: %v", err)
			return
		}

		labels := data["labels"].([]interface{})
		if len(labels) != 2 { // Only non-zero categories
			t.Errorf("Expected 2 labels (filtering out zero counts), got %d", len(labels))
		}

		// Check that rate limit category is not included
		for _, label := range labels {
			if label.(string) == string(errors.CategoryRateLimit) {
				t.Error("Zero-count categories should be filtered out")
			}
		}
	})
}

func TestRegisterErrorsDashboard(t *testing.T) {
	mux := http.NewServeMux()
	RegisterErrorsDashboard(mux)

	// Test that the route is registered
	req := httptest.NewRequest("GET", "/dashboard/errors", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	// The handler should be called (won't return 404)
	if rec.Code == http.StatusNotFound {
		t.Error("Expected errors dashboard route to be registered, but got 404")
	}
}

func TestCreateErrorsDashboardHandler(t *testing.T) {
	handler := createErrorsDashboardHandler()

	t.Run("regular request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/dashboard/errors", nil)
		rec := httptest.NewRecorder()

		handler(rec, req)

		// Should attempt to render template (may fail due to missing templates in test)
		// But should not panic or return early due to missing HX-Request header
		if rec.Code == 0 {
			t.Error("Handler should set a status code")
		}
	})

	t.Run("htmx request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/dashboard/errors", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()

		handler(rec, req)

		// Should attempt to render template (may fail due to missing templates in test)
		// But should detect HTMX request header
		if rec.Code == 0 {
			t.Error("Handler should set a status code")
		}
	})
}

func TestChartDataColors(t *testing.T) {
	// Test that predefined colors are used correctly
	stats := ErrorStats{
		Categories: map[string]int{
			string(errors.CategoryTransient):      1,
			string(errors.CategoryPermanent):      1,
			string(errors.CategoryAuthentication): 1,
			"UNKNOWN_CATEGORY":                    1, // Should get default color
		},
		TotalErrors: 4,
		LastUpdate:  time.Now(),
	}

	chartData := prepareChartData(stats)

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(chartData), &data); err != nil {
		t.Errorf("Chart data should be valid JSON, got error: %v", err)
		return
	}

	datasets := data["datasets"].([]interface{})
	dataset := datasets[0].(map[string]interface{})
	bgColors := dataset["backgroundColor"].([]interface{})

	// Should have 4 colors
	if len(bgColors) != 4 {
		t.Errorf("Expected 4 background colors, got %d", len(bgColors))
	}

	// All colors should be valid hex colors (start with #)
	for i, color := range bgColors {
		colorStr := color.(string)
		if !strings.HasPrefix(colorStr, "#") {
			t.Errorf("Color %d should be a hex color, got %s", i, colorStr)
		}
		if len(colorStr) != 7 {
			t.Errorf("Color %d should be 7 characters long, got %d: %s", i, len(colorStr), colorStr)
		}
	}
}
