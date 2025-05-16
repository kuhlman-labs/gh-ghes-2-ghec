// Package tracing provides utilities for distributed tracing using OpenTelemetry.
// It includes initialization, span creation, and context propagation functionality.
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// Global tracer provider
var (
	// tracerProvider is the global tracer provider
	tracerProvider *sdktrace.TracerProvider
	// tracer is the global tracer instance
	tracer trace.Tracer
	// enabled indicates if tracing is enabled
	enabled bool
	// serviceName is the name of the service for traces
	serviceName = "gh-ghes-2-ghec"
	// meterProvider is the global meter provider for metrics
	meterProvider *metric.MeterProvider
)

// Config holds configuration for the tracing system.
type Config struct {
	// Enabled indicates if tracing should be enabled
	Enabled bool `mapstructure:"enabled"`
	// Endpoint is the OTLP endpoint to send traces to (e.g., "localhost:4317")
	Endpoint string `mapstructure:"endpoint"`
	// ServiceName overrides the default service name
	ServiceName string `mapstructure:"service_name"`
	// SampleRate is the fraction of traces to sample (0.0 to 1.0)
	SampleRate float64 `mapstructure:"sample_rate"`
	// PrometheusMetrics enables exporting OpenTelemetry metrics to Prometheus
	PrometheusMetrics bool `mapstructure:"prometheus_metrics"`
}

// Init initializes the tracing system with the provided configuration.
// It sets up the global tracer provider and configures exporters.
func Init(cfg Config) error {
	if !cfg.Enabled {
		enabled = false
		return nil
	}

	// Use the specified service name, or fall back to default
	if cfg.ServiceName != "" {
		serviceName = cfg.ServiceName
	}

	// Create a new resource with service information
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("0.1.0"), // TODO: Use actual version from the app
			attribute.String("environment", "production"),
		),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOS(),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	// Configure the OTLP exporter
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(), // For development; use TLS in production
	}

	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Configure the sample rate
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	// Create a new tracer provider with better processor options
	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			// Configure the maximum batch size
			sdktrace.WithMaxExportBatchSize(512),
			// Configure the maximum queue size
			sdktrace.WithMaxQueueSize(2048),
			// Configure the batch timeout
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set the global tracer provider and propagator
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create the tracer
	tracer = otel.Tracer(serviceName)
	enabled = true

	// Initialize OpenTelemetry metrics with Prometheus if enabled
	if cfg.PrometheusMetrics {
		promExporter, err := prometheus.New()
		if err != nil {
			logging.Get().Warn("Failed to create Prometheus exporter, metrics will not be available", "error", err)
		} else {
			meterProvider = metric.NewMeterProvider(
				metric.WithResource(res),
				metric.WithReader(promExporter),
			)
			otel.SetMeterProvider(meterProvider)
			logging.Get().Info("OpenTelemetry metrics with Prometheus exporter initialized")
		}
	}

	logging.Get().Info("Distributed tracing initialized",
		"endpoint", cfg.Endpoint,
		"service_name", serviceName,
		"sample_rate", cfg.SampleRate)

	return nil
}

// Shutdown gracefully shuts down the tracer provider.
// It should be called when the application is shutting down.
func Shutdown(ctx context.Context) error {
	if tracerProvider == nil {
		return nil
	}

	// Shutdown the tracer provider
	err := tracerProvider.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}

	// Shutdown the meter provider if it exists
	if meterProvider != nil {
		if err := meterProvider.Shutdown(ctx); err != nil {
			logging.Get().Error("Failed to shutdown meter provider", "error", err)
		}
	}

	return nil
}

// StartSpan starts a new span with the given name and returns the context with the span.
// It uses the correlation ID from the context as the trace ID if present.
// If parent is non-nil, the span will be a child of the parent span.
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if !enabled || tracer == nil {
		// Return a no-op span if tracing is not enabled
		return ctx, trace.SpanFromContext(ctx)
	}

	// Get correlation ID from context
	correlationID := logging.GetCorrelationID(ctx)
	if correlationID != "" {
		// Add correlation ID as an attribute
		opts = append(opts, trace.WithAttributes(
			attribute.String("correlation_id", correlationID),
		))
	}

	return tracer.Start(ctx, name, opts...)
}

// AddAttribute adds an attribute to the current span in the context.
func AddAttribute(ctx context.Context, key string, value interface{}) {
	if !enabled {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	// Convert the value to an attribute based on type
	var attr attribute.KeyValue
	switch v := value.(type) {
	case string:
		attr = attribute.String(key, v)
	case int:
		attr = attribute.Int(key, v)
	case int64:
		attr = attribute.Int64(key, v)
	case float64:
		attr = attribute.Float64(key, v)
	case bool:
		attr = attribute.Bool(key, v)
	default:
		// For other types, convert to string
		attr = attribute.String(key, fmt.Sprintf("%v", v))
	}

	span.SetAttributes(attr)
}

// AddAttributes adds multiple attributes to the current span in the context.
func AddAttributes(ctx context.Context, attrs map[string]interface{}) {
	if !enabled {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	for key, value := range attrs {
		AddAttribute(ctx, key, value)
	}
}

// RecordError records an error on the current span.
func RecordError(ctx context.Context, err error) {
	if !enabled || err == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	span.RecordError(err)
}

// TraceHTTP wraps an HTTP handler to create spans for HTTP requests.
func TraceHTTP(handler http.Handler, operation string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !enabled {
			handler.ServeHTTP(w, r)
			return
		}

		// Start a new span for the HTTP request
		ctx, span := StartSpan(r.Context(), operation)
		defer span.End()

		// Add HTTP-specific attributes
		span.SetAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.String()),
			attribute.String("http.user_agent", r.UserAgent()),
			attribute.String("http.flavor", r.Proto),
			attribute.String("http.host", r.Host),
			attribute.String("http.scheme", getScheme(r)),
			attribute.String("http.target", r.URL.Path),
		)

		// Extract any correlation ID from headers
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID != "" {
			// Add to span
			span.SetAttributes(attribute.String("correlation_id", correlationID))
			// Add to context
			ctx = context.WithValue(ctx, logging.KeyCorrelationID, correlationID)
		} else {
			// Generate a new correlation ID
			correlationID = logging.GetCorrelationID(ctx)
			// Add to response headers
			w.Header().Set("X-Correlation-ID", correlationID)
		}

		// Use a wrapped response writer to capture the status code
		wrappedWriter := newResponseWriter(w)

		// Call the next handler with the updated context
		handler.ServeHTTP(wrappedWriter, r.WithContext(ctx))

		// Record the HTTP status code
		statusCode := wrappedWriter.statusCode
		span.SetAttributes(attribute.Int("http.status_code", statusCode))

		// Set appropriate span status based on HTTP status code
		if statusCode >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}
	})
}

// getScheme returns the scheme (http/https) for the request
func getScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if scheme := r.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "http"
}

// ResponseWriter is a wrapper around http.ResponseWriter that captures the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// NewResponseWriter creates a new response writer.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

// WriteHeader captures the status code and passes it to the wrapped ResponseWriter.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
