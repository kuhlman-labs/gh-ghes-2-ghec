// Package server provides HTTP server functionality for the migration API,
// including request handlers, middleware, and server configuration.
// It implements a RESTful API for initiating and monitoring repository migrations.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/validation"
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
		ctx := context.WithValue(r.Context(), "request_id", requestID)

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
				fmt.Fprintf(w, `{"error": "Content-Type must be application/json"}`)
				m.logger.Error("Request error: invalid content type",
					"content_type", contentType,
					"path", r.URL.Path,
				)
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
				w.Write([]byte(`{"error": "Request body too large"}`))
				return
			}

			// Set the max bytes reader for streaming requests or when Content-Length is not reliable
			r.Body = http.MaxBytesReader(w, r.Body, validation.MaxRequestBodySizeBytes)
		}
		next.ServeHTTP(w, r)
	})
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
				fmt.Fprintf(w, `{"error": "Rate limit exceeded. Try again in 1 minute."}`)
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
