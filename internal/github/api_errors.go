package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v70/github"
	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
)

// classifyGitHubError converts GitHub API errors to classified errors.
// This provides a consistent error classification for better handling.
func (a *GitHubAPI) classifyGitHubError(err error) error {
	if err == nil {
		return nil
	}

	// Handle GitHub API errors
	var respErr *github.ErrorResponse
	if errors.As(err, &respErr) {
		// Create HTTP status error with appropriate metadata
		httpErr := apierrors.NewHTTPStatusError(
			respErr.Response.StatusCode,
			respErr.Response.Request.URL.String(),
			respErr.Response.Request.Method,
		)

		// Create a message that includes GitHub error message and preserves raw error
		rawErrorBody := respErr.Message

		// Check specifically for repository conflict errors
		category := apierrors.Classify(httpErr)
		if respErr.Response.StatusCode == http.StatusConflict ||
			strings.Contains(strings.ToLower(respErr.Message), "already exists") {
			category = apierrors.CategoryResourceConflict
			a.logger.Warn("Repository conflict detected",
				"status_code", respErr.Response.StatusCode,
				"message", respErr.Message,
			)
		}

		// Create a detailed error message with all available error information
		var msg strings.Builder
		fmt.Fprintf(&msg, "GitHub API error: %s", respErr.Message)

		if len(respErr.Errors) > 0 {
			errDetails := make([]string, 0, len(respErr.Errors))
			for _, e := range respErr.Errors {
				if e.Message != "" {
					errDetails = append(errDetails, e.Message)
					// Also check each individual error message for conflict indicators
					if strings.Contains(strings.ToLower(e.Message), "already exists") {
						category = apierrors.CategoryResourceConflict
						a.logger.Warn("Repository conflict in error details",
							"error_message", e.Message,
						)
					}
				}
			}
			if len(errDetails) > 0 {
				fmt.Fprintf(&msg, " - %s", strings.Join(errDetails, ", "))
				rawErrorBody += " - " + strings.Join(errDetails, ", ")
			}
		}

		// Include the raw error from GitHub for complete transparency
		fmt.Fprintf(&msg, " (Raw response: %s)", rawErrorBody)

		// Return the error with appropriate classification
		classifiedErr := apierrors.WrapWithCategory(err, category, msg.String())

		// Report the error for metrics and dashboard
		apierrors.ReportError(classifiedErr)

		return classifiedErr
	}

	// Check for GraphQL errors which might contain repository conflict information
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "already exists") ||
		strings.Contains(errMsg, "duplicate") ||
		strings.Contains(errMsg, "conflict") {
		// This is likely a resource conflict error
		a.logger.Warn("Repository conflict detected in error message",
			"message", err.Error(),
		)
		msg := fmt.Sprintf("Repository conflict: %s", err.Error())
		classifiedErr := apierrors.WrapWithCategory(err, apierrors.CategoryResourceConflict, msg)
		apierrors.ReportError(classifiedErr)
		return classifiedErr
	}

	// Handle other GitHub-specific errors
	category := apierrors.Classify(err)
	msg := fmt.Sprintf("GitHub error: %s", err.Error())
	classifiedErr := apierrors.WrapWithCategory(err, category, msg)

	// Report the error for metrics and dashboard
	apierrors.ReportError(classifiedErr)

	return classifiedErr
}

// classifyHTTPError converts HTTP errors to classified errors
func (a *GitHubAPI) classifyHTTPError(err error, req *http.Request) error {
	if err == nil {
		return nil
	}

	// If it's already an HTTPStatusError, just classify it
	var httpErr *apierrors.HTTPStatusError
	if errors.As(err, &httpErr) {
		category := apierrors.Classify(httpErr)
		classifiedErr := apierrors.NewClassifiedError(err, category)

		// Report the error for metrics and dashboard
		apierrors.ReportError(classifiedErr)

		return classifiedErr
	}

	// Handle URL errors (network, timeout, etc.)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		msg := fmt.Sprintf("HTTP %s request to %s failed", req.Method, req.URL.String())
		category := apierrors.Classify(err)
		classifiedErr := apierrors.WrapWithCategory(err, category, msg)

		// Report the error for metrics and dashboard
		apierrors.ReportError(classifiedErr)

		return classifiedErr
	}

	// For other errors, just use the standard classification
	category := apierrors.Classify(err)
	classifiedErr := apierrors.NewClassifiedError(err, category)

	// Report the error for metrics and dashboard
	apierrors.ReportError(classifiedErr)

	return classifiedErr
}
