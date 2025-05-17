// Package errors provides error classification and handling for the migration tool.
// It defines error categories and provides functions to classify and handle errors
// according to their type and severity.
package errors

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
)

// ErrorCategory represents the category of an error for appropriate handling
type ErrorCategory string

const (
	// CategoryTransient represents errors that are temporary and should be retried
	CategoryTransient ErrorCategory = "TRANSIENT"

	// CategoryPermanent represents errors that are permanent and should not be retried
	CategoryPermanent ErrorCategory = "PERMANENT"

	// CategoryRateLimit represents errors due to rate limiting (special case of transient)
	CategoryRateLimit ErrorCategory = "RATE_LIMIT"

	// CategoryAuthentication represents authentication errors (special case of permanent)
	CategoryAuthentication ErrorCategory = "AUTHENTICATION"

	// CategoryAuthorization represents authorization/permission errors (special case of permanent)
	CategoryAuthorization ErrorCategory = "AUTHORIZATION"

	// CategoryResourceNotFound represents errors when a resource doesn't exist
	CategoryResourceNotFound ErrorCategory = "RESOURCE_NOT_FOUND"

	// CategoryResourceConflict represents errors for resource conflicts (like already exists)
	CategoryResourceConflict ErrorCategory = "RESOURCE_CONFLICT"

	// CategoryValidation represents validation errors in input data
	CategoryValidation ErrorCategory = "VALIDATION"

	// CategoryMigrationCanceled represents errors due to migration cancellation
	CategoryMigrationCanceled ErrorCategory = "MIGRATION_CANCELED"

	// CategoryInternalError represents unexpected internal errors
	CategoryInternalError ErrorCategory = "INTERNAL_ERROR"

	// CategoryUnknown represents errors that couldn't be classified
	CategoryUnknown ErrorCategory = "UNKNOWN"
)

// ClassifiedError wraps an error with its classification
type ClassifiedError struct {
	Err      error         // The original error
	Category ErrorCategory // The error category
}

// Error implements the error interface
func (ce *ClassifiedError) Error() string {
	if ce.Err == nil {
		return fmt.Sprintf("classified error [%s]: <nil>", ce.Category)
	}
	return fmt.Sprintf("classified error [%s]: %s", ce.Category, ce.Err.Error())
}

// Unwrap returns the underlying error
func (ce *ClassifiedError) Unwrap() error {
	return ce.Err
}

// Is reports whether the classified error matches the target
func (ce *ClassifiedError) Is(target error) bool {
	var targetCE *ClassifiedError
	if errors.As(target, &targetCE) {
		return ce.Category == targetCE.Category && errors.Is(ce.Err, targetCE.Err)
	}
	return errors.Is(ce.Err, target)
}

// NewClassifiedError creates a new classified error
func NewClassifiedError(err error, category ErrorCategory) *ClassifiedError {
	return &ClassifiedError{
		Err:      err,
		Category: category,
	}
}

// Classify analyzes an error and returns its classification
func Classify(err error) ErrorCategory {
	if err == nil {
		return CategoryUnknown
	}

	// Check if already classified
	var ce *ClassifiedError
	if errors.As(err, &ce) {
		return ce.Category
	}

	// Check for HTTP errors
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Handle URL errors, which often wrap network errors
		return classifyURLError(urlErr)
	}

	// Check for HTTP response status code
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return classifyHTTPStatusCode(httpErr.StatusCode)
	}

	// Check for common network errors
	if isNetworkError(err) {
		return CategoryTransient
	}

	// Check for specific error strings and patterns
	errMsg := strings.ToLower(err.Error())

	// Authentication/Authorization
	if strings.Contains(errMsg, "unauthorized") ||
		strings.Contains(errMsg, "authentication failed") ||
		strings.Contains(errMsg, "bad credentials") ||
		strings.Contains(errMsg, "token expired") {
		return CategoryAuthentication
	}

	if strings.Contains(errMsg, "permission denied") ||
		strings.Contains(errMsg, "forbidden") ||
		strings.Contains(errMsg, "not allowed") {
		return CategoryAuthorization
	}

	// Rate limit errors
	if strings.Contains(errMsg, "rate limit") ||
		strings.Contains(errMsg, "too many requests") {
		return CategoryRateLimit
	}

	// Resource errors
	if strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "doesn't exist") ||
		strings.Contains(errMsg, "does not exist") {
		return CategoryResourceNotFound
	}

	if strings.Contains(errMsg, "already exists") ||
		strings.Contains(errMsg, "conflict") ||
		strings.Contains(errMsg, "duplicate") {
		return CategoryResourceConflict
	}

	// Validation errors
	if strings.Contains(errMsg, "invalid") ||
		strings.Contains(errMsg, "validation failed") ||
		strings.Contains(errMsg, "malformed") {
		return CategoryValidation
	}

	// Migration canceled
	if strings.Contains(errMsg, "migration canceled") ||
		strings.Contains(errMsg, "operation canceled") {
		return CategoryMigrationCanceled
	}

	// Default to unknown
	return CategoryUnknown
}

// isNetworkError checks if an error is a common network error
func isNetworkError(err error) bool {
	if err == io.EOF {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Common syscall errors that are network-related
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNREFUSED,
			syscall.ECONNRESET,
			syscall.ECONNABORTED,
			syscall.ENETUNREACH,
			syscall.ENETRESET,
			syscall.ETIMEDOUT:
			return true
		}
	}

	return false
}

// classifyURLError analyzes a URL error to determine its category
func classifyURLError(urlErr *url.Error) ErrorCategory {
	// Timeout errors are transient
	if urlErr.Timeout() {
		return CategoryTransient
	}

	// Check the wrapped error for network issues
	if isNetworkError(urlErr.Err) {
		return CategoryTransient
	}

	// Default to unknown for other URL errors
	return CategoryUnknown
}

// HTTPStatusError represents an error from an HTTP status code
type HTTPStatusError struct {
	StatusCode int
	URL        string
	Method     string
}

// Error implements the error interface
func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %s request to %s failed with status code %d", e.Method, e.URL, e.StatusCode)
}

// NewHTTPStatusError creates a new HTTP status error
func NewHTTPStatusError(statusCode int, url, method string) *HTTPStatusError {
	return &HTTPStatusError{
		StatusCode: statusCode,
		URL:        url,
		Method:     method,
	}
}

// classifyHTTPStatusCode determines the error category based on an HTTP status code
func classifyHTTPStatusCode(statusCode int) ErrorCategory {
	switch {
	case statusCode >= 200 && statusCode < 300:
		// 2xx is not an error
		return CategoryUnknown
	case statusCode == http.StatusUnauthorized:
		return CategoryAuthentication
	case statusCode == http.StatusForbidden:
		return CategoryAuthorization
	case statusCode == http.StatusNotFound:
		return CategoryResourceNotFound
	case statusCode == http.StatusConflict:
		return CategoryResourceConflict
	case statusCode == http.StatusTooManyRequests:
		return CategoryRateLimit
	case statusCode >= 400 && statusCode < 500:
		// Other 4xx errors are generally client errors and permanent
		return CategoryPermanent
	case statusCode >= 500:
		// 5xx errors are server errors and generally transient
		return CategoryTransient
	default:
		return CategoryUnknown
	}
}

// IsTransient checks if an error is transient and should be retried
func IsTransient(err error) bool {
	category := Classify(err)
	return category == CategoryTransient || category == CategoryRateLimit
}

// IsPermanent checks if an error is permanent and should not be retried
func IsPermanent(err error) bool {
	category := Classify(err)
	return category == CategoryPermanent ||
		category == CategoryAuthentication ||
		category == CategoryAuthorization ||
		category == CategoryResourceNotFound ||
		category == CategoryResourceConflict ||
		category == CategoryValidation
}

// WrapWithCategory wraps an error with a specific category
func WrapWithCategory(err error, category ErrorCategory, msg string) error {
	if err == nil {
		return nil
	}
	wrappedErr := fmt.Errorf("%s: %w", msg, err)
	return NewClassifiedError(wrappedErr, category)
}
