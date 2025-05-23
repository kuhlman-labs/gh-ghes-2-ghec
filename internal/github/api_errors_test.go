package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"log/slog"

	"github.com/google/go-github/v70/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/utils"
	"github.com/shurcooL/githubv4"
)

func setupTestErrorsAPI() *GitHubAPI {
	logger := slog.Default()
	clients := &config.Clients{
		GHESClient:     github.NewClient(nil),
		GHCloudClient:  github.NewClient(nil),
		GHCloudGraphQL: githubv4.NewClient(nil),
	}
	return &GitHubAPI{
		clients:     clients,
		logger:      logger,
		retryConfig: utils.DefaultRetryConfig(logger),
		ghesCircuitBreaker: utils.NewCircuitBreaker(
			utils.DefaultCircuitConfig("test-ghes", logger),
		),
		ghCloudCircuitBreaker: utils.NewCircuitBreaker(
			utils.DefaultCircuitConfig("test-ghcloud", logger),
		),
	}
}

func TestClassifyGitHubError_NilError(t *testing.T) {
	api := setupTestErrorsAPI()

	result := api.classifyGitHubError(nil)
	if result != nil {
		t.Errorf("Expected nil for nil input, got %v", result)
	}
}

func TestClassifyGitHubError_GitHubErrorResponse(t *testing.T) {
	api := setupTestErrorsAPI()

	// Create a test HTTP request and response
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/test/test", nil)
	resp := &http.Response{
		StatusCode: 404,
		Request:    req,
	}

	// Create a GitHub ErrorResponse
	githubErr := &github.ErrorResponse{
		Response: resp,
		Message:  "Not Found",
		Errors: []github.Error{
			{Message: "Repository not found"},
		},
	}

	result := api.classifyGitHubError(githubErr)

	// Check that we get a classified error
	var classifiedErr *apierrors.ClassifiedError
	if !errors.As(result, &classifiedErr) {
		t.Fatalf("Expected ClassifiedError, got %T", result)
	}

	// Verify the error is properly categorized
	if classifiedErr.Category != apierrors.CategoryResourceNotFound {
		t.Errorf("Expected ResourceNotFound category, got %s", classifiedErr.Category)
	}

	// Verify original error is preserved in chain
	if !errors.Is(result, githubErr) {
		t.Errorf("Original error should be preserved in error chain")
	}
}

func TestClassifyGitHubError_ConflictDetection(t *testing.T) {
	api := setupTestErrorsAPI()

	tests := []struct {
		name          string
		statusCode    int
		message       string
		errorMessages []string
		expectedCat   apierrors.ErrorCategory
	}{
		{
			name:        "conflict_status_code",
			statusCode:  http.StatusConflict,
			message:     "Conflict occurred",
			expectedCat: apierrors.CategoryResourceConflict,
		},
		{
			name:        "already_exists_in_message",
			statusCode:  400,
			message:     "Repository already exists",
			expectedCat: apierrors.CategoryResourceConflict,
		},
		{
			name:          "already_exists_in_error_details",
			statusCode:    400,
			message:       "Bad Request",
			errorMessages: []string{"Repository already exists"},
			expectedCat:   apierrors.CategoryResourceConflict,
		},
		{
			name:        "normal_404",
			statusCode:  404,
			message:     "Not Found",
			expectedCat: apierrors.CategoryResourceNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create test request and response
			req, _ := http.NewRequest("GET", "https://api.github.com/repos/test/test", nil)
			resp := &http.Response{
				StatusCode: tc.statusCode,
				Request:    req,
			}

			// Create GitHub errors
			var githubErrors []github.Error
			for _, errMsg := range tc.errorMessages {
				githubErrors = append(githubErrors, github.Error{Message: errMsg})
			}

			githubErr := &github.ErrorResponse{
				Response: resp,
				Message:  tc.message,
				Errors:   githubErrors,
			}

			result := api.classifyGitHubError(githubErr)

			var classifiedErr *apierrors.ClassifiedError
			if !errors.As(result, &classifiedErr) {
				t.Fatalf("Expected ClassifiedError, got %T", result)
			}

			if classifiedErr.Category != tc.expectedCat {
				t.Errorf("Expected category %s, got %s", tc.expectedCat, classifiedErr.Category)
			}
		})
	}
}

func TestClassifyGitHubError_StringErrors(t *testing.T) {
	api := setupTestErrorsAPI()

	tests := []struct {
		name        string
		error       string
		expectedCat apierrors.ErrorCategory
	}{
		{
			name:        "already_exists_error",
			error:       "repository already exists",
			expectedCat: apierrors.CategoryResourceConflict,
		},
		{
			name:        "duplicate_error",
			error:       "duplicate repository name",
			expectedCat: apierrors.CategoryResourceConflict,
		},
		{
			name:        "conflict_error",
			error:       "conflict with existing resource",
			expectedCat: apierrors.CategoryResourceConflict,
		},
		{
			name:        "generic_error",
			error:       "something went wrong",
			expectedCat: apierrors.CategoryUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("%s", tc.error)
			result := api.classifyGitHubError(err)

			var classifiedErr *apierrors.ClassifiedError
			if !errors.As(result, &classifiedErr) {
				t.Fatalf("Expected ClassifiedError, got %T", result)
			}

			if classifiedErr.Category != tc.expectedCat {
				t.Errorf("Expected category %s, got %s", tc.expectedCat, classifiedErr.Category)
			}

			// Verify original error is preserved
			if !errors.Is(result, err) {
				t.Errorf("Original error should be preserved in error chain")
			}
		})
	}
}

func TestClassifyHTTPError_NilError(t *testing.T) {
	api := setupTestErrorsAPI()
	req, _ := http.NewRequest("GET", "https://api.github.com/test", nil)

	result := api.classifyHTTPError(nil, req)
	if result != nil {
		t.Errorf("Expected nil for nil input, got %v", result)
	}
}

func TestClassifyHTTPError_HTTPStatusError(t *testing.T) {
	api := setupTestErrorsAPI()
	req, _ := http.NewRequest("GET", "https://api.github.com/test", nil)

	tests := []struct {
		name        string
		statusCode  int
		expectedCat apierrors.ErrorCategory
	}{
		{
			name:        "unauthorized",
			statusCode:  401,
			expectedCat: apierrors.CategoryAuthentication,
		},
		{
			name:        "forbidden",
			statusCode:  403,
			expectedCat: apierrors.CategoryAuthorization,
		},
		{
			name:        "not_found",
			statusCode:  404,
			expectedCat: apierrors.CategoryResourceNotFound,
		},
		{
			name:        "rate_limit",
			statusCode:  429,
			expectedCat: apierrors.CategoryRateLimit,
		},
		{
			name:        "server_error",
			statusCode:  500,
			expectedCat: apierrors.CategoryTransient,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			httpErr := apierrors.NewHTTPStatusError(tc.statusCode, req.URL.String(), req.Method)
			result := api.classifyHTTPError(httpErr, req)

			var classifiedErr *apierrors.ClassifiedError
			if !errors.As(result, &classifiedErr) {
				t.Fatalf("Expected ClassifiedError, got %T", result)
			}

			if classifiedErr.Category != tc.expectedCat {
				t.Errorf("Expected category %s, got %s", tc.expectedCat, classifiedErr.Category)
			}
		})
	}
}

func TestClassifyHTTPError_URLError(t *testing.T) {
	api := setupTestErrorsAPI()
	req, _ := http.NewRequest("GET", "https://api.github.com/test", nil)

	// Create a URL error
	origErr := fmt.Errorf("network unreachable")
	urlErr := &url.Error{
		Op:  "Get",
		URL: "https://api.github.com/test",
		Err: origErr,
	}

	result := api.classifyHTTPError(urlErr, req)

	var classifiedErr *apierrors.ClassifiedError
	if !errors.As(result, &classifiedErr) {
		t.Fatalf("Expected ClassifiedError, got %T", result)
	}

	// URL errors should be classified as network/transient
	expectedCategory := apierrors.Classify(urlErr)
	if classifiedErr.Category != expectedCategory {
		t.Errorf("Expected category %s, got %s", expectedCategory, classifiedErr.Category)
	}

	// Verify original error is preserved
	if !errors.Is(result, urlErr) {
		t.Errorf("Original error should be preserved in error chain")
	}
}

func TestClassifyHTTPError_GenericError(t *testing.T) {
	api := setupTestErrorsAPI()
	req, _ := http.NewRequest("GET", "https://api.github.com/test", nil)

	genericErr := fmt.Errorf("generic error")
	result := api.classifyHTTPError(genericErr, req)

	var classifiedErr *apierrors.ClassifiedError
	if !errors.As(result, &classifiedErr) {
		t.Fatalf("Expected ClassifiedError, got %T", result)
	}

	// Generic errors should be classified using standard classification
	expectedCategory := apierrors.Classify(genericErr)
	if classifiedErr.Category != expectedCategory {
		t.Errorf("Expected category %s, got %s", expectedCategory, classifiedErr.Category)
	}
}

// TestErrorClassificationPreservation tests that error classification preserves original error information
func TestErrorClassificationPreservation(t *testing.T) {
	api := setupTestErrorsAPI()

	originalErr := fmt.Errorf("original error message")
	result := api.classifyGitHubError(originalErr)

	// Verify error can be unwrapped to original
	if !errors.Is(result, originalErr) {
		t.Errorf("Classified error should wrap original error")
	}

	// Verify error message contains relevant information
	if result.Error() == "" {
		t.Errorf("Classified error should have a non-empty message")
	}
}

// TestErrorCaseInsensitivity tests that error detection is case insensitive
func TestErrorCaseInsensitivity(t *testing.T) {
	api := setupTestErrorsAPI()

	caseVariations := []string{
		"Repository ALREADY EXISTS",
		"repository already exists",
		"REPOSITORY ALREADY EXISTS",
		"Repository Already Exists",
	}

	for _, variation := range caseVariations {
		t.Run(fmt.Sprintf("case_%s", variation), func(t *testing.T) {
			err := fmt.Errorf("%s", variation)
			result := api.classifyGitHubError(err)

			var classifiedErr *apierrors.ClassifiedError
			if !errors.As(result, &classifiedErr) {
				t.Fatalf("Expected ClassifiedError, got %T", result)
			}

			if classifiedErr.Category != apierrors.CategoryResourceConflict {
				t.Errorf("Expected ResourceConflict category for '%s', got %s", variation, classifiedErr.Category)
			}
		})
	}
}
