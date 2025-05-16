package errors

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorCategory
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: CategoryUnknown,
		},
		{
			name:     "already classified error",
			err:      NewClassifiedError(fmt.Errorf("test error"), CategoryAuthentication),
			expected: CategoryAuthentication,
		},
		{
			name:     "http status error - unauthorized",
			err:      NewHTTPStatusError(http.StatusUnauthorized, "https://api.github.com", "GET"),
			expected: CategoryAuthentication,
		},
		{
			name:     "http status error - forbidden",
			err:      NewHTTPStatusError(http.StatusForbidden, "https://api.github.com", "GET"),
			expected: CategoryAuthorization,
		},
		{
			name:     "http status error - not found",
			err:      NewHTTPStatusError(http.StatusNotFound, "https://api.github.com", "GET"),
			expected: CategoryResourceNotFound,
		},
		{
			name:     "http status error - conflict",
			err:      NewHTTPStatusError(http.StatusConflict, "https://api.github.com", "GET"),
			expected: CategoryResourceConflict,
		},
		{
			name:     "http status error - rate limit",
			err:      NewHTTPStatusError(http.StatusTooManyRequests, "https://api.github.com", "GET"),
			expected: CategoryRateLimit,
		},
		{
			name:     "http status error - server error",
			err:      NewHTTPStatusError(http.StatusInternalServerError, "https://api.github.com", "GET"),
			expected: CategoryTransient,
		},
		{
			name:     "http status error - bad request",
			err:      NewHTTPStatusError(http.StatusBadRequest, "https://api.github.com", "GET"),
			expected: CategoryPermanent,
		},
		{
			name:     "url error with timeout",
			err:      &url.Error{Op: "Get", URL: "https://api.github.com", Err: fmt.Errorf("i/o timeout")},
			expected: CategoryUnknown, // Without timeout detection, it will be unknown
		},
		{
			name:     "authentication error string",
			err:      fmt.Errorf("authentication failed for user"),
			expected: CategoryAuthentication,
		},
		{
			name:     "token expired error string",
			err:      fmt.Errorf("token expired or invalid"),
			expected: CategoryAuthentication,
		},
		{
			name:     "permission denied error string",
			err:      fmt.Errorf("permission denied to access resource"),
			expected: CategoryAuthorization,
		},
		{
			name:     "rate limit error string",
			err:      fmt.Errorf("rate limit exceeded, try again later"),
			expected: CategoryRateLimit,
		},
		{
			name:     "resource not found error string",
			err:      fmt.Errorf("repository not found"),
			expected: CategoryResourceNotFound,
		},
		{
			name:     "resource conflict error string",
			err:      fmt.Errorf("repository already exists"),
			expected: CategoryResourceConflict,
		},
		{
			name:     "validation error string",
			err:      fmt.Errorf("invalid input parameter"),
			expected: CategoryValidation,
		},
		{
			name:     "migration canceled error string",
			err:      fmt.Errorf("migration canceled by user"),
			expected: CategoryMigrationCanceled,
		},
		{
			name:     "unknown error",
			err:      fmt.Errorf("some random error"),
			expected: CategoryUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Classify(tc.err)
			if result != tc.expected {
				t.Errorf("Expected category %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestClassifiedError(t *testing.T) {
	originalErr := fmt.Errorf("original error")
	classifiedErr := NewClassifiedError(originalErr, CategoryTransient)

	// Test Error method
	expected := "classified error [TRANSIENT]: original error"
	if classifiedErr.Error() != expected {
		t.Errorf("Expected error message '%s', got '%s'", expected, classifiedErr.Error())
	}

	// Test Unwrap method
	if classifiedErr.Unwrap() != originalErr {
		t.Errorf("Unwrap did not return the original error")
	}

	// Test Is method
	if !classifiedErr.Is(originalErr) {
		t.Errorf("Is method should return true for the original error")
	}

	// Test with nil error
	nilErr := NewClassifiedError(nil, CategoryUnknown)
	if nilErr.Error() != "classified error [UNKNOWN]: <nil>" {
		t.Errorf("Unexpected error message for nil error: %s", nilErr.Error())
	}
}

func TestHTTPStatusError(t *testing.T) {
	httpErr := NewHTTPStatusError(404, "https://api.github.com/repos/test", "GET")
	expected := "HTTP GET request to https://api.github.com/repos/test failed with status code 404"

	if httpErr.Error() != expected {
		t.Errorf("Expected error message '%s', got '%s'", expected, httpErr.Error())
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "transient error",
			err:      NewClassifiedError(fmt.Errorf("temporary error"), CategoryTransient),
			expected: true,
		},
		{
			name:     "rate limit error",
			err:      NewClassifiedError(fmt.Errorf("rate limit exceeded"), CategoryRateLimit),
			expected: true,
		},
		{
			name:     "authentication error",
			err:      NewClassifiedError(fmt.Errorf("unauthorized"), CategoryAuthentication),
			expected: false,
		},
		{
			name:     "permanent error",
			err:      NewClassifiedError(fmt.Errorf("bad request"), CategoryPermanent),
			expected: false,
		},
		{
			name:     "unknown error",
			err:      fmt.Errorf("some random error"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsTransient(tc.err)
			if result != tc.expected {
				t.Errorf("Expected IsTransient to return %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestIsPermanent(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "permanent error",
			err:      NewClassifiedError(fmt.Errorf("bad request"), CategoryPermanent),
			expected: true,
		},
		{
			name:     "authentication error",
			err:      NewClassifiedError(fmt.Errorf("unauthorized"), CategoryAuthentication),
			expected: true,
		},
		{
			name:     "authorization error",
			err:      NewClassifiedError(fmt.Errorf("forbidden"), CategoryAuthorization),
			expected: true,
		},
		{
			name:     "resource not found error",
			err:      NewClassifiedError(fmt.Errorf("not found"), CategoryResourceNotFound),
			expected: true,
		},
		{
			name:     "transient error",
			err:      NewClassifiedError(fmt.Errorf("temporary error"), CategoryTransient),
			expected: false,
		},
		{
			name:     "rate limit error",
			err:      NewClassifiedError(fmt.Errorf("rate limit exceeded"), CategoryRateLimit),
			expected: false,
		},
		{
			name:     "unknown error",
			err:      fmt.Errorf("some random error"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsPermanent(tc.err)
			if result != tc.expected {
				t.Errorf("Expected IsPermanent to return %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestWrapWithCategory(t *testing.T) {
	// Test with nil error
	if WrapWithCategory(nil, CategoryTransient, "wrapped") != nil {
		t.Errorf("Expected nil when wrapping nil error")
	}

	// Test with an error
	originalErr := fmt.Errorf("original error")
	wrappedErr := WrapWithCategory(originalErr, CategoryTransient, "wrapped message")

	var classifiedErr *ClassifiedError
	if !errors.As(wrappedErr, &classifiedErr) {
		t.Errorf("Expected a ClassifiedError, got %T", wrappedErr)
	}

	if classifiedErr.Category != CategoryTransient {
		t.Errorf("Expected category %s, got %s", CategoryTransient, classifiedErr.Category)
	}

	// Ensure the original error is preserved in the chain
	if !errors.Is(wrappedErr, originalErr) {
		t.Errorf("Original error should be in the error chain")
	}
}
