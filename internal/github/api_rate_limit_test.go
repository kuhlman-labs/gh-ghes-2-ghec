package github

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
	"github.com/stretchr/testify/assert"
)

func TestGetRateLimitInfoFromResponse(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected *RateLimitInfo
	}{
		{
			name: "Complete headers",
			headers: map[string]string{
				"X-RateLimit-Limit":     "5000",
				"X-RateLimit-Remaining": "4990",
				"X-RateLimit-Reset":     "1609459200", // 2021-01-01 00:00:00 UTC
				"X-RateLimit-Used":      "10",
			},
			expected: &RateLimitInfo{
				Limit:     5000,
				Remaining: 4990,
				Reset:     time.Unix(1609459200, 0),
				Used:      10,
			},
		},
		{
			name: "Missing used header",
			headers: map[string]string{
				"X-RateLimit-Limit":     "5000",
				"X-RateLimit-Remaining": "4990",
				"X-RateLimit-Reset":     "1609459200", // 2021-01-01 00:00:00 UTC
			},
			expected: &RateLimitInfo{
				Limit:     5000,
				Remaining: 4990,
				Reset:     time.Unix(1609459200, 0),
				Used:      0,
			},
		},
		{
			name:     "No headers",
			headers:  map[string]string{},
			expected: &RateLimitInfo{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				Header: make(http.Header),
			}

			// Add headers to the response
			for key, value := range tc.headers {
				resp.Header.Add(key, value)
			}

			result := getRateLimitInfoFromResponse(resp)

			if tc.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, tc.expected.Limit, result.Limit)
			assert.Equal(t, tc.expected.Remaining, result.Remaining)
			assert.Equal(t, tc.expected.Reset.Unix(), result.Reset.Unix())
			assert.Equal(t, tc.expected.Used, result.Used)
		})
	}
}

func TestShouldRetryRateLimitError(t *testing.T) {
	api := setupTestAPI()

	tests := []struct {
		name           string
		statusCode     int
		headers        map[string]string
		errorMsg       string
		expectRetry    bool
		expectDuration time.Duration
	}{
		{
			name:           "No error",
			errorMsg:       "",
			expectRetry:    false,
			expectDuration: 0,
		},
		{
			name:           "Non-rate limit error",
			errorMsg:       "generic error",
			expectRetry:    false,
			expectDuration: 0,
		},
		{
			name:           "Rate limit error message",
			errorMsg:       "API rate limit exceeded",
			expectRetry:    true,
			expectDuration: 60 * time.Second, // Default fallback
		},
		{
			name:       "Rate limit with headers",
			statusCode: 429,
			headers: map[string]string{
				"Retry-After": "30",
			},
			errorMsg:       "API rate limit exceeded",
			expectRetry:    true,
			expectDuration: 30 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errorMsg != "" {
				if tc.statusCode == 429 {
					err = errors.NewHTTPStatusError(429, "https://api.github.com/test", "GET")
				} else {
					err = fmt.Errorf("%s", tc.errorMsg)
				}
			}

			resp := &http.Response{
				StatusCode: tc.statusCode,
				Header:     make(http.Header),
			}

			// Add headers if provided
			for key, value := range tc.headers {
				resp.Header.Add(key, value)
			}

			isRateLimit, duration, _ := api.ShouldRetryRateLimitError(err, resp)

			assert.Equal(t, tc.expectRetry, isRateLimit)

			// For durations, just check if they're within a reasonable range
			if tc.expectDuration > 0 {
				if tc.headers != nil && tc.headers["Retry-After"] != "" {
					assert.Equal(t, tc.expectDuration, duration)
				} else {
					assert.True(t, duration > 0)
				}
			} else {
				assert.Equal(t, time.Duration(0), duration)
			}
		})
	}
}

func TestCheckAndLogRateLimit(t *testing.T) {
	api := setupTestAPI()

	tests := []struct {
		name           string
		info           *RateLimitInfo
		apiType        string
		threshold      float64
		expectCritical bool
	}{
		{
			name:           "Nil info",
			info:           nil,
			apiType:        "GHES",
			threshold:      10.0,
			expectCritical: false,
		},
		{
			name: "Below threshold",
			info: &RateLimitInfo{
				Limit:     100,
				Remaining: 5,
				Reset:     time.Now().Add(5 * time.Minute),
			},
			apiType:        "GHES",
			threshold:      10.0,
			expectCritical: true,
		},
		{
			name: "Above threshold",
			info: &RateLimitInfo{
				Limit:     100,
				Remaining: 50,
				Reset:     time.Now().Add(5 * time.Minute),
			},
			apiType:        "GHEC",
			threshold:      10.0,
			expectCritical: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isCritical := api.checkAndLogRateLimit(tc.info, tc.apiType, tc.threshold)
			assert.Equal(t, tc.expectCritical, isCritical)
		})
	}
}
