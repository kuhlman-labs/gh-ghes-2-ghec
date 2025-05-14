package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestSanitizeInput(t *testing.T) {
	// Initialize logging for middleware
	_ = logging.Init()

	// Create middleware
	middleware := NewMiddleware()

	// Create a simple test handler that reads request data and returns it
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read query params
		queryParams := r.URL.Query()

		// Read body if it exists
		var body map[string]interface{}
		if r.Body != nil {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Failed to read body", http.StatusInternalServerError)
				return
			}

			if len(bodyBytes) > 0 {
				if err := json.Unmarshal(bodyBytes, &body); err != nil {
					http.Error(w, "Failed to parse JSON", http.StatusInternalServerError)
					return
				}
			}
		}

		// Return response with sanitized data
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"query":   queryParams,
			"headers": r.Header,
			"body":    body,
		}); err != nil {
			// In a test handler, just log the error
			log.Printf("Error encoding JSON response: %v", err)
		}
	})

	// Apply the middleware
	sanitizeHandler := middleware.SanitizeInput(testHandler)

	t.Run("sanitizes query parameters", func(t *testing.T) {
		// Create a test request with malicious query params
		req := httptest.NewRequest("GET", "/test?normal=value&script=<script>alert('xss')</script>&long="+strings.Repeat("a", 2000), nil)
		rr := httptest.NewRecorder()

		// Process the request
		sanitizeHandler.ServeHTTP(rr, req)

		// Check response
		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Query should be sanitized
		query := response["query"].(map[string]interface{})
		scriptParam := query["script"].([]interface{})[0]
		assert.Equal(t, "value", query["normal"].([]interface{})[0])
		assert.NotContains(t, scriptParam, "<script>")
		assert.NotContains(t, scriptParam, "alert")
	})

	t.Run("sanitizes request headers", func(t *testing.T) {
		// Create a test request with potentially dangerous headers
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Custom-Header", "normal value")
		req.Header.Set("X-Dangerous", "value\r\nSet-Cookie: malicious=1")
		req.Header.Set("User-Agent", "<script>alert('ua')</script>")

		rr := httptest.NewRecorder()

		// Process the request
		sanitizeHandler.ServeHTTP(rr, req)

		// Check response
		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Headers should be sanitized
		headers := response["headers"].(map[string]interface{})
		assert.NotContains(t, headers["X-Dangerous"], "\r\n")
		assert.NotContains(t, headers["User-Agent"], "<script>")
	})

	t.Run("sanitizes JSON body", func(t *testing.T) {
		// Create a JSON body with potentially dangerous content
		body := map[string]interface{}{
			"normal": "value",
			"script": "<script>alert('xss')</script>",
			"nested": map[string]interface{}{
				"deep": "<img src=x onerror=alert('deep')>",
			},
			"array": []interface{}{
				"safe",
				"<script>bad()</script>",
				map[string]interface{}{
					"innerKey": "javascript:alert('boom')",
				},
			},
		}

		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		// Create request with JSON body
		req := httptest.NewRequest("POST", "/test", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Process the request
		sanitizeHandler.ServeHTTP(rr, req)

		// Check response
		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Body should be sanitized
		body = response["body"].(map[string]interface{})
		assert.Equal(t, "value", body["normal"])
		assert.NotContains(t, body["script"], "<script>")

		// Check nested sanitization
		nested := body["nested"].(map[string]interface{})
		assert.NotContains(t, nested["deep"], "<img")

		// Check array sanitization
		array := body["array"].([]interface{})
		assert.Equal(t, "safe", array[0])
		assert.NotContains(t, array[1], "<script>")

		// Check nested object in array
		arrayObj := array[2].(map[string]interface{})
		assert.NotContains(t, arrayObj["innerKey"], "javascript:")
	})

	t.Run("handles invalid JSON correctly", func(t *testing.T) {
		// Create invalid JSON
		invalidJSON := []byte(`{"key": "value", invalid}`)

		// Create request with invalid JSON
		req := httptest.NewRequest("POST", "/test", bytes.NewReader(invalidJSON))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		// Process the request
		sanitizeHandler.ServeHTTP(rr, req)

		// Should return bad request
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}
