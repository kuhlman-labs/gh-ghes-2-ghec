package github

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"log/slog"

	"github.com/google/go-github/v70/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	apierrors "github.com/kuhlman-labs/gh-ghes-2-ghec/internal/errors"
)

func setupTestAPI() *GitHubAPI {
	logger := slog.Default()
	clients := &config.Clients{
		GHESClient:     github.NewClient(nil),
		GHCloudClient:  github.NewClient(nil),
		GHCloudGraphQL: nil,
	}
	return &GitHubAPI{
		clients:               clients,
		logger:                logger,
		retryConfig:           nil,
		ghesCircuitBreaker:    nil,
		ghCloudCircuitBreaker: nil,
	}
}

func TestClassifyGitHubError(t *testing.T) {
	api := setupTestAPI()

	tests := []struct {
		name             string
		err              error
		expectedCategory string
	}{
		{
			name:             "nil error",
			err:              nil,
			expectedCategory: "",
		},
		{
			name:             "generic error",
			err:              fmt.Errorf("something went wrong"),
			expectedCategory: string(apierrors.CategoryUnknown),
		},
		{
			name:             "not found error string",
			err:              fmt.Errorf("repository not found"),
			expectedCategory: string(apierrors.CategoryResourceNotFound),
		},
		{
			name:             "rate limit error string",
			err:              fmt.Errorf("API rate limit exceeded"),
			expectedCategory: string(apierrors.CategoryRateLimit),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := api.classifyGitHubError(tc.err)

			// Nil error should return nil
			if tc.err == nil {
				if result != nil {
					t.Errorf("Expected nil result for nil error, got %v", result)
				}
				return
			}

			// Check the category of the classified error
			var classifiedErr *apierrors.ClassifiedError
			if !errors.As(result, &classifiedErr) {
				t.Fatalf("Expected ClassifiedError, got %T", result)
			}

			if string(classifiedErr.Category) != tc.expectedCategory {
				t.Errorf("Expected category %s, got %s", tc.expectedCategory, classifiedErr.Category)
			}

			// Original error should be preserved
			if !errors.Is(result, tc.err) {
				t.Errorf("Original error should be preserved in the error chain")
			}
		})
	}
}

func TestClassifyHTTPError(t *testing.T) {
	api := setupTestAPI()

	// Create a test request
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/test/test", nil)

	tests := []struct {
		name             string
		err              error
		expectedCategory string
	}{
		{
			name:             "nil error",
			err:              nil,
			expectedCategory: "",
		},
		{
			name:             "http status error 404",
			err:              apierrors.NewHTTPStatusError(404, req.URL.String(), req.Method),
			expectedCategory: string(apierrors.CategoryResourceNotFound),
		},
		{
			name:             "http status error 403",
			err:              apierrors.NewHTTPStatusError(403, req.URL.String(), req.Method),
			expectedCategory: string(apierrors.CategoryAuthorization),
		},
		{
			name:             "http status error 401",
			err:              apierrors.NewHTTPStatusError(401, req.URL.String(), req.Method),
			expectedCategory: string(apierrors.CategoryAuthentication),
		},
		{
			name:             "http status error 429",
			err:              apierrors.NewHTTPStatusError(429, req.URL.String(), req.Method),
			expectedCategory: string(apierrors.CategoryRateLimit),
		},
		{
			name:             "http status error 500",
			err:              apierrors.NewHTTPStatusError(500, req.URL.String(), req.Method),
			expectedCategory: string(apierrors.CategoryTransient),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := api.classifyHTTPError(tc.err, req)

			// Nil error should return nil
			if tc.err == nil {
				if result != nil {
					t.Errorf("Expected nil result for nil error, got %v", result)
				}
				return
			}

			// Check the category of the classified error
			var classifiedErr *apierrors.ClassifiedError
			if !errors.As(result, &classifiedErr) {
				t.Fatalf("Expected ClassifiedError, got %T", result)
			}

			if string(classifiedErr.Category) != tc.expectedCategory {
				t.Errorf("Expected category %s, got %s", tc.expectedCategory, classifiedErr.Category)
			}
		})
	}
}
