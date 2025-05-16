// Package metrics provides functionality for collecting and exposing application metrics
// using Prometheus. It includes initialization, metric registration, and HTTP handlers.
package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Registry is the main Prometheus registry
	registry = prometheus.NewRegistry()

	// Default metrics namespace
	namespace = "ghghe2ec"

	// Enabled flag to track if metrics are enabled
	enabled = false

	// Migration metrics
	migrationTotal = promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "migrations_total",
			Help:      "Total number of migration operations",
		},
		[]string{"source_org", "target_org", "status"},
	)

	migrationDuration = promauto.With(registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "migration_duration_seconds",
			Help:      "Duration of migration operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 15), // 1s to ~16h
		},
		[]string{"source_org", "target_org", "stage", "status"},
	)

	migrationSize = promauto.With(registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "migration_size_bytes",
			Help:      "Size of migrated repositories in bytes",
			Buckets:   prometheus.ExponentialBuckets(1024*10, 4, 10), // 10KB to ~10GB
		},
		[]string{"source_org", "target_org"},
	)

	// HTTP metrics
	httpRequestsTotal = promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"handler", "method", "status"},
	)

	httpRequestDuration = promauto.With(registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"handler", "method"},
	)

	// GitHub API metrics
	githubAPIRequestsTotal = promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "github_api_requests_total",
			Help:      "Total number of GitHub API requests",
		},
		[]string{"api", "endpoint", "status"},
	)

	githubAPIRequestDuration = promauto.With(registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "github_api_request_duration_seconds",
			Help:      "Duration of GitHub API requests in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"api", "endpoint"},
	)

	// Rate limit remaining metrics
	githubRateLimitRemaining = promauto.With(registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "github_rate_limit_remaining",
			Help:      "Number of GitHub API rate limit calls remaining",
		},
		[]string{"api"},
	)

	// System metrics
	goRoutinesGauge = promauto.With(registry).NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "goroutines",
			Help:      "Number of goroutines",
		},
	)

	memAllocBytes = promauto.With(registry).NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "memory_alloc_bytes",
			Help:      "Number of bytes allocated and not yet freed",
		},
	)

	// Storage metrics
	storageOperationsTotal = promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "storage_operations_total",
			Help:      "Total number of storage operations",
		},
		[]string{"operation", "status"},
	)

	storageOperationDuration = promauto.With(registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "storage_operation_duration_seconds",
			Help:      "Duration of storage operations in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"operation"},
	)
)

// Config holds configuration for the metrics system.
type Config struct {
	// Enabled indicates if metrics should be enabled
	Enabled bool `mapstructure:"enabled"`
	// Port to expose metrics on
	Port int `mapstructure:"port"`
	// Path is the endpoint path for metrics
	Path string `mapstructure:"path"`
	// ServiceName overrides the default service name/namespace
	ServiceName string `mapstructure:"service_name"`
}

// Init initializes the metrics system with the provided configuration.
func Init(cfg Config) error {
	if !cfg.Enabled {
		enabled = false
		return nil
	}

	// Use the specified namespace if provided
	if cfg.ServiceName != "" {
		namespace = cfg.ServiceName
	}

	// Register default Go collectors
	registry.MustRegister(prometheus.NewGoCollector())
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	// Start metrics server on separate port if configured
	if cfg.Port > 0 {
		// Default metrics path
		path := "/metrics"
		if cfg.Path != "" {
			path = cfg.Path
		}

		// Create a new serve mux
		mux := http.NewServeMux()
		mux.Handle(path, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

		// Start metrics server in a goroutine
		go func() {
			addr := fmt.Sprintf(":%d", cfg.Port)
			server := &http.Server{
				Addr:    addr,
				Handler: mux,
			}

			logging.Get().Info("Starting metrics server", "address", addr, "path", path)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logging.Get().Error("Metrics server failed", "error", err)
			}
		}()
	}

	enabled = true
	logging.Get().Info("Metrics collection initialized", "namespace", namespace)
	return nil
}

// Handler returns an HTTP handler for exposing metrics.
// This can be used to expose metrics on the main server.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// InstrumentHandler wraps an HTTP handler to record request metrics.
func InstrumentHandler(handler http.Handler, name string) http.Handler {
	if !enabled {
		return handler
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture the status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Call the original handler
		handler.ServeHTTP(rw, r)

		// Record metrics
		duration := time.Since(start).Seconds()
		httpRequestsTotal.WithLabelValues(name, r.Method, fmt.Sprintf("%d", rw.statusCode)).Inc()
		httpRequestDuration.WithLabelValues(name, r.Method).Observe(duration)
	})
}

// RecordMigrationStart records the start of a migration operation.
func RecordMigrationStart(sourceOrg, targetOrg string) {
	if !enabled {
		return
	}
	migrationTotal.WithLabelValues(sourceOrg, targetOrg, "started").Inc()
}

// RecordMigrationComplete records the completion of a migration operation.
func RecordMigrationComplete(sourceOrg, targetOrg, status string, duration time.Duration, sizeBytes int64) {
	if !enabled {
		return
	}
	migrationTotal.WithLabelValues(sourceOrg, targetOrg, status).Inc()
	migrationDuration.WithLabelValues(sourceOrg, targetOrg, "overall", status).Observe(duration.Seconds())

	if sizeBytes > 0 {
		migrationSize.WithLabelValues(sourceOrg, targetOrg).Observe(float64(sizeBytes))
	}
}

// RecordMigrationStage records metrics for a specific migration stage.
func RecordMigrationStage(sourceOrg, targetOrg, stage, status string, duration time.Duration) {
	if !enabled {
		return
	}
	migrationDuration.WithLabelValues(sourceOrg, targetOrg, stage, status).Observe(duration.Seconds())
}

// RecordGitHubAPIRequest records metrics for a GitHub API request.
func RecordGitHubAPIRequest(api, endpoint, status string, duration time.Duration) {
	if !enabled {
		return
	}
	githubAPIRequestsTotal.WithLabelValues(api, endpoint, status).Inc()
	githubAPIRequestDuration.WithLabelValues(api, endpoint).Observe(duration.Seconds())
}

// SetGitHubRateLimit sets the current GitHub API rate limit.
func SetGitHubRateLimit(api string, remaining int) {
	if !enabled {
		return
	}
	githubRateLimitRemaining.WithLabelValues(api).Set(float64(remaining))
}

// RecordStorageOperation records metrics for a storage operation.
func RecordStorageOperation(operation, status string, duration time.Duration) {
	if !enabled {
		return
	}
	storageOperationsTotal.WithLabelValues(operation, status).Inc()
	storageOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// responseWriter wraps an http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code and passes it to the wrapped ResponseWriter.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
