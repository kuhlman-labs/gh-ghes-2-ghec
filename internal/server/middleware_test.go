package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMiddleware(t *testing.T) {
	middleware := NewMiddleware()
	assert.NotNil(t, middleware)
	assert.NotNil(t, middleware.logger)
}

func TestLogRequest(t *testing.T) {
	middleware := NewMiddleware()
	var capturedRequestID any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = r.Context().Value(requestIDKey)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	middleware.LogRequest(handler).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, capturedRequestID)
}

func TestSecurityHeaders(t *testing.T) {
	middleware := NewMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	middleware.SecurityHeaders(handler).ServeHTTP(w, req)

	headers := w.Header()
	assert.Equal(t, "default-src 'self'", headers.Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", headers.Get("X-Frame-Options"))
	assert.Equal(t, "1; mode=block", headers.Get("X-XSS-Protection"))
	assert.Equal(t, "max-age=31536000; includeSubDomains", headers.Get("Strict-Transport-Security"))
	assert.Equal(t, "strict-origin-when-cross-origin", headers.Get("Referrer-Policy"))
}

func TestJSONOnly(t *testing.T) {
	middleware := NewMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name           string
		method         string
		contentType    string
		expectedStatus int
	}{
		{
			name:           "GET request",
			method:         "GET",
			contentType:    "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with JSON",
			method:         "POST",
			contentType:    "application/json",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request without JSON",
			method:         "POST",
			contentType:    "text/plain",
			expectedStatus: http.StatusUnsupportedMediaType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			w := httptest.NewRecorder()

			middleware.JSONOnly(handler).ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestRequestSizeLimit(t *testing.T) {
	middleware := NewMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name           string
		method         string
		body           string
		expectedStatus int
	}{
		{
			name:           "GET request",
			method:         "GET",
			body:           "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with small body",
			method:         "POST",
			body:           "small body",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with large body",
			method:         "POST",
			body:           strings.Repeat("a", 2*1024*1024), // 2MB
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			middleware.RequestSizeLimit(handler).ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestRateLimiter(t *testing.T) {
	middleware := NewMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rateLimiter := middleware.RateLimiter(2) // 2 requests per minute

	t.Run("within rate limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			rateLimiter(handler).ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("exceed rate limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.2:12345" // Use a different IP to avoid state leakage
		for i := 0; i < 3; i++ {
			w := httptest.NewRecorder()
			rateLimiter(handler).ServeHTTP(w, req)
			if i < 2 {
				assert.Equal(t, http.StatusOK, w.Code)
			} else {
				assert.Equal(t, http.StatusTooManyRequests, w.Code)
			}
		}
	})
}

func TestCombineMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-1", "test1")
			next.ServeHTTP(w, r)
		})
	}

	middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-2", "test2")
			next.ServeHTTP(w, r)
		})
	}

	combined := CombineMiddleware(handler, middleware1, middleware2)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	combined.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test1", w.Header().Get("X-Test-1"))
	assert.Equal(t, "test2", w.Header().Get("X-Test-2"))
}
