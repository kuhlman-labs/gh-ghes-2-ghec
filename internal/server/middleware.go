// Package server provides HTTP server functionality for the migration API,
// including request handlers, middleware, and server configuration.
// It implements a RESTful API for initiating and monitoring repository migrations.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/sanitization"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/validation"
)

// Define a custom type for context keys to avoid collisions
type contextKey string

// Define constants for context keys
const (
	requestIDKey contextKey = "request_id"
)

// Middleware struct for holding middleware functions and their dependencies.
// It provides various HTTP middleware functions for security, logging, and rate limiting.
type Middleware struct {
	logger *slog.Logger
}

// NewMiddleware creates a new middleware instance with the provided dependencies.
// It initializes the middleware with a logger.
func NewMiddleware() *Middleware {
	return &Middleware{
		logger: logging.Get(),
	}
}

// LogRequest logs details about each HTTP request and response.
// It adds a request ID to the context, records timing information,
// and logs details such as method, path, remote address, and user agent.
func (m *Middleware) LogRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add request ID to context
		requestID := fmt.Sprintf("%d", time.Now().UnixNano())
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)

		// Log request
		start := time.Now()
		m.logger.Debug("Incoming request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		// Call next handler
		next.ServeHTTP(w, r.WithContext(ctx))

		// Log response time
		m.logger.Debug("Request completed",
			"request_id", requestID,
			"duration_ms", time.Since(start).Milliseconds(),
			"path", r.URL.Path,
		)
	})
}

// SecurityHeaders adds standard security headers to HTTP responses.
// These headers help protect against XSS, clickjacking, MIME sniffing,
// and other common web vulnerabilities.
func (m *Middleware) SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next.ServeHTTP(w, r)
	})
}

// JSONOnly ensures that HTTP requests that modify data (POST, PUT, PATCH)
// have the correct Content-Type set to application/json.
// It returns a 415 Unsupported Media Type status if the content type is incorrect.
func (m *Middleware) JSONOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate POST requests
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			contentType := r.Header.Get("Content-Type")
			if !strings.Contains(contentType, "application/json") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnsupportedMediaType)
				n, err := fmt.Fprintf(w, `{"error": "Content-Type must be application/json"}`)
				if err != nil {
					m.logger.Warn("Failed to write response", "error", err)
				} else if n == 0 {
					m.logger.Warn("Zero bytes written in response")
				}
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// RequestSizeLimit limits the size of request bodies to prevent resource exhaustion attacks.
// It applies limits only to HTTP methods that typically contain request bodies.
// The maximum size is defined in the validation package constants.
func (m *Middleware) RequestSizeLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			// Check Content-Length header first for a quick rejection of obviously large requests
			if r.ContentLength > validation.MaxRequestBodySizeBytes {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				n, err := w.Write([]byte(`{"error": "Request body too large"}`))
				if err != nil {
					m.logger.Warn("Failed to write response", "error", err)
				} else if n == 0 {
					m.logger.Warn("Zero bytes written in response")
				}
				return
			}

			// Set the max bytes reader for streaming requests or when Content-Length is not reliable
			r.Body = http.MaxBytesReader(w, r.Body, validation.MaxRequestBodySizeBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// SanitizeInput sanitizes various inputs in HTTP requests to prevent injection attacks.
// It sanitizes URL parameters, headers, and JSON request bodies.
func (m *Middleware) SanitizeInput(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sanitize query parameters
		if r.URL.RawQuery != "" {
			sanitizedQuery := sanitizeQueryParams(r.URL.Query())
			r.URL.RawQuery = sanitizedQuery.Encode()
		}

		// Sanitize headers
		sanitizeHeaders(r.Header)

		// Sanitize body for POST/PUT/PATCH requests
		if (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) &&
			strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			var err error
			r.Body, err = sanitizeJSONBody(r.Body)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				n, err := fmt.Fprintf(w, `{"error": "Invalid request body format"}`)
				if err != nil {
					m.logger.Warn("Failed to write response", "error", err)
				} else if n == 0 {
					m.logger.Warn("Zero bytes written in response")
				}
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// sanitizeQueryParams sanitizes all query parameters to prevent injection attacks.
func sanitizeQueryParams(query url.Values) url.Values {
	sanitized := url.Values{}
	for key, values := range query {
		sanitizedKey := sanitization.SanitizeGenericInput(key)
		for _, value := range values {
			// Apply both sanitization functions for stronger protection
			sanitizedValue := sanitization.SanitizeGenericInput(value)
			sanitizedValue = sanitization.SanitizeHTML(sanitizedValue)
			sanitized.Add(sanitizedKey, sanitizedValue)
		}
	}
	return sanitized
}

// sanitizeHeaders sanitizes header values to prevent header injection attacks.
func sanitizeHeaders(header http.Header) {
	for key, values := range header {
		for i, value := range values {
			header[key][i] = sanitization.SanitizeHeader(value)
		}
	}
}

// sanitizeJSONBody sanitizes a JSON request body.
// It reads the body, parses it as JSON, sanitizes fields, and returns a new body reader.
func sanitizeJSONBody(body io.ReadCloser) (io.ReadCloser, error) {
	// Read the body
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	// Close the original body and check for errors
	if err := body.Close(); err != nil {
		return nil, fmt.Errorf("failed to close request body: %w", err)
	}

	// Parse as JSON
	var data map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return nil, err
	}

	// Sanitize the data recursively
	sanitizedData := sanitizeJSONObject(data)

	// Marshal back to JSON
	sanitizedBytes, err := json.Marshal(sanitizedData)
	if err != nil {
		return nil, err
	}

	// Return a new body reader
	return io.NopCloser(bytes.NewReader(sanitizedBytes)), nil
}

// sanitizeJSONObject recursively sanitizes a JSON object's keys and string values.
func sanitizeJSONObject(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range data {
		sanitizedKey := sanitization.SanitizeJSONKey(key)

		switch v := value.(type) {
		case string:
			// Apply both HTML and generic sanitization for better protection
			sanitized := sanitization.SanitizeGenericInput(v)
			sanitized = sanitization.SanitizeHTML(sanitized)
			result[sanitizedKey] = sanitized
		case []interface{}:
			result[sanitizedKey] = sanitizeJSONArray(v)
		case map[string]interface{}:
			result[sanitizedKey] = sanitizeJSONObject(v)
		default:
			// For other types (numbers, booleans, null), keep as is
			result[sanitizedKey] = v
		}
	}
	return result
}

// sanitizeJSONArray sanitizes all elements in a JSON array.
func sanitizeJSONArray(data []interface{}) []interface{} {
	result := make([]interface{}, len(data))
	for i, value := range data {
		switch v := value.(type) {
		case string:
			// Apply both HTML and generic sanitization for better protection
			sanitized := sanitization.SanitizeGenericInput(v)
			sanitized = sanitization.SanitizeHTML(sanitized)
			result[i] = sanitized
		case []interface{}:
			result[i] = sanitizeJSONArray(v)
		case map[string]interface{}:
			result[i] = sanitizeJSONObject(v)
		default:
			// For other types (numbers, booleans, null), keep as is
			result[i] = v
		}
	}
	return result
}

// RateLimiter implements a basic rate limiter per IP address to prevent abuse.
// It limits each IP to a configurable number of requests per minute.
// When the rate limit is exceeded, it returns a 429 Too Many Requests status.
func (m *Middleware) RateLimiter(requestsPerMinute int) func(http.Handler) http.Handler {
	// Map to track requests by IP
	requests := make(map[string][]time.Time)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get client IP
			ip := r.RemoteAddr
			now := time.Now()

			// Clean up old requests (older than 1 minute)
			var recent []time.Time
			for _, t := range requests[ip] {
				if now.Sub(t) < time.Minute {
					recent = append(recent, t)
				}
			}
			requests[ip] = recent

			// Check if rate limit exceeded
			if len(requests[ip]) >= requestsPerMinute {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				n, err := fmt.Fprintf(w, `{"error": "Rate limit exceeded. Try again in 1 minute."}`)
				if err != nil {
					m.logger.Warn("Failed to write response", "error", err)
				} else if n == 0 {
					m.logger.Warn("Zero bytes written in response")
				}
				m.logger.Warn("Rate limit exceeded",
					"ip", ip,
					"path", r.URL.Path,
				)
				return
			}

			// Add current request to the tracking
			requests[ip] = append(requests[ip], now)

			// Serve the request
			next.ServeHTTP(w, r)
		})
	}
}

// CombineMiddleware combines multiple middleware functions into one.
// This allows multiple middleware functions to be applied to a handler
// in a clean and readable way.
func CombineMiddleware(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for _, middleware := range middlewares {
		h = middleware(h)
	}
	return h
}
