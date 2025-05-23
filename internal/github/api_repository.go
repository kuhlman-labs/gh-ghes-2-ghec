package github

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-github/v70/github"
	"github.com/shurcooL/githubv4"
)

// ValidateRepository checks if a repository exists in the specified organization on GitHub Enterprise Server.
// It makes a REST API call to the GHES instance and verifies the repository's existence.
// Returns an error if the repository doesn't exist or can't be accessed.
func (a *GitHubAPI) ValidateRepository(ctx context.Context, org, repo string) error {
	startTime := time.Now()
	a.logger.Debug("Validating repository",
		"api", "GHES_REST",
		"method", "Repositories.Get",
		"org", org,
		"repo", repo,
	)

	var respStatus int
	err := a.circuitProtectedGhesOperation(ctx, "validate_repository", func() error {
		_, resp, err := a.clients.GHESClient.Repositories.Get(ctx, org, repo)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Repository validation failed",
			"api", "GHES_REST",
			"method", "Repositories.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
			"error", err,
		)

		// Use the error classification system to properly categorize this error
		classifiedErr := a.classifyGitHubError(err)

		// For all other errors, return the properly classified error
		return classifiedErr
	}

	a.logger.Debug("Repository validation successful",
		"api", "GHES_REST",
		"method", "Repositories.Get",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"org", org,
		"repo", repo,
	)
	return nil
}

// ValidateCloudRepository checks if a repository exists in the specified organization on GitHub Enterprise Cloud.
// It makes a REST API call to the GHEC instance and verifies the repository's existence.
// Returns an error if the repository doesn't exist or can't be accessed.
func (a *GitHubAPI) ValidateCloudRepository(ctx context.Context, org, repo string) error {
	startTime := time.Now()
	a.logger.Debug("Validating cloud repository",
		"api", "GHEC_REST",
		"method", "Repositories.Get",
		"org", org,
		"repo", repo,
	)

	var respStatus int
	err := a.circuitProtectedGhCloudOperation(ctx, "validate_cloud_repository", func() error {
		_, resp, err := a.clients.GHCloudClient.Repositories.Get(ctx, org, repo)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Cloud repository validation failed",
			"api", "GHEC_REST",
			"method", "Repositories.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
			"error", err,
		)

		// Use the error classification system to properly categorize this error
		classifiedErr := a.classifyGitHubError(err)

		// For 404 errors, keep the "repository not found" message for compatibility
		if respStatus == 404 {
			return fmt.Errorf("repository not found: %w", classifiedErr)
		}

		// For all other errors, return the properly classified error
		return classifiedErr
	}

	a.logger.Debug("Cloud repository validation successful",
		"api", "GHEC_REST",
		"method", "Repositories.Get",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"org", org,
		"repo", repo,
	)
	return nil
}

// GetOrganizationID retrieves the organization node ID from GitHub Enterprise Cloud.
// It makes a GraphQL API call to fetch the organization's unique identifier,
// which is needed for migration operations.
// Returns the organization ID as a string and any errors encountered.
func (a *GitHubAPI) GetOrganizationID(ctx context.Context, org string) (string, int64, error) {
	var query struct {
		Organization struct {
			ID         string `graphql:"id"`
			DatabaseID int64  `graphql:"databaseId"`
		} `graphql:"organization(login: $login)"`
	}

	variables := map[string]interface{}{
		"login": githubv4.String(org),
	}

	a.logger.Debug("Querying organization ID",
		"api", "GHEC_GraphQL",
		"method", "Query(organization)",
		"org", org,
	)

	startTime := time.Now()

	err := a.circuitProtectedGhCloudOperation(ctx, "get_organization_id", func() error {
		return a.clients.GHCloudGraphQL.Query(ctx, &query, variables)
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get organization ID",
			"api", "GHEC_GraphQL",
			"method", "Query(organization)",
			"duration_ms", duration.Milliseconds(),
			"error", err,
			"org", org,
		)
		return "", 0, fmt.Errorf("failed to get organization: %w", err)
	}

	// check if the organization ID is empty
	if query.Organization.ID == "" {
		a.logger.Error("Organization ID is empty",
			"api", "GHEC_GraphQL",
			"method", "Query(organization)",
			"duration_ms", duration.Milliseconds(),
			"org", org,
		)
		return "", 0, fmt.Errorf("organization ID is empty")
	}

	a.logger.Debug("Organization ID retrieved",
		"api", "GHEC_GraphQL",
		"method", "Query(organization)",
		"duration_ms", duration.Milliseconds(),
		"org", org,
		"id", query.Organization.ID,
		"databaseId", query.Organization.DatabaseID,
	)
	return query.Organization.ID, query.Organization.DatabaseID, nil
}

// DeleteCloudRepositoryIfExists checks if a repository exists in GitHub Cloud and deletes it if found.
// Returns true if repository was deleted, false if it didn't exist, and error if deletion failed.
func (a *GitHubAPI) DeleteCloudRepositoryIfExists(ctx context.Context, org, repo string) (bool, error) {
	startTime := time.Now()
	a.logger.Debug("Checking if repository exists in target org before deletion",
		"api", "GHEC_REST",
		"method", "Repositories.Get",
		"org", org,
		"repo", repo,
	)

	// First check if the repository exists
	var respStatus int
	var repoExists bool
	err := a.circuitProtectedGhCloudOperation(ctx, "validate_cloud_repository_for_deletion", func() error {
		_, resp, err := a.clients.GHCloudClient.Repositories.Get(ctx, org, repo)
		if resp != nil {
			respStatus = resp.StatusCode
			// Repository exists if status code is 200
			repoExists = respStatus == 200
		}
		// We want to handle 404 specially, it's not really an error in this context
		if respStatus == 404 {
			return nil
		}
		return err
	})

	// If repository doesn't exist (404), return false with no error
	if respStatus == 404 || !repoExists {
		a.logger.Debug("Repository doesn't exist in target org, no deletion needed",
			"api", "GHEC_REST",
			"duration_ms", time.Since(startTime).Milliseconds(),
			"org", org,
			"repo", repo,
		)
		return false, nil
	}

	// If there was any other error, return it
	if err != nil {
		a.logger.Error("Failed to check if repository exists in target org",
			"api", "GHEC_REST",
			"method", "Repositories.Get",
			"duration_ms", time.Since(startTime).Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
			"error", err,
		)
		return false, a.classifyGitHubError(err)
	}

	// Repository exists, proceed with deletion
	a.logger.Info("Deleting existing repository in target org",
		"api", "GHEC_REST",
		"method", "Repositories.Delete",
		"org", org,
		"repo", repo,
	)

	// Reset the status code for the delete operation
	respStatus = 0
	err = a.circuitProtectedGhCloudOperation(ctx, "delete_cloud_repository", func() error {
		resp, err := a.clients.GHCloudClient.Repositories.Delete(ctx, org, repo)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to delete repository in target org",
			"api", "GHEC_REST",
			"method", "Repositories.Delete",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
			"error", err,
		)
		return false, a.classifyGitHubError(err)
	}

	a.logger.Info("Successfully deleted repository in target org",
		"api", "GHEC_REST",
		"method", "Repositories.Delete",
		"duration_ms", duration.Milliseconds(),
		"org", org,
		"repo", repo,
	)
	return true, nil
}

// CheckCloudRepositoryExists checks if a repository exists in GitHub Cloud.
// Unlike ValidateCloudRepository, this function treats 404 responses as a valid response (not an error)
// and returns a simple boolean indicating if the repository exists.
// This prevents 404s from triggering circuit breaker failures when checking for repository existence.
func (a *GitHubAPI) CheckCloudRepositoryExists(ctx context.Context, org, repo string) (bool, error) {
	startTime := time.Now()
	a.logger.Debug("Checking if repository exists in target org",
		"api", "GHEC_REST",
		"method", "Repositories.Get",
		"org", org,
		"repo", repo,
	)

	var respStatus int
	var exists bool
	var nonResourceNotFoundErr error

	// Use non-circuit-protected operation to directly handle the request
	// This ensures 404s don't count against circuit breaker thresholds
	err := a.retryableOperation(ctx, "check_cloud_repository_exists", func() error {
		_, resp, err := a.clients.GHCloudClient.Repositories.Get(ctx, org, repo)
		if resp != nil {
			respStatus = resp.StatusCode

			// Only consider it an error for retryable operation if it's not a 404
			if respStatus == 404 {
				exists = false
				return nil // Not a real error for our purposes
			}

			// Repository exists if status code is 200
			exists = respStatus == 200
		}

		// Keep track of non-404 errors for later reporting
		if err != nil && (resp == nil || resp.StatusCode != 404) {
			nonResourceNotFoundErr = err
		}

		return err
	})

	duration := time.Since(startTime)

	// Handle 404 case (repository doesn't exist)
	if respStatus == 404 || !exists {
		a.logger.Debug("Repository doesn't exist in target org",
			"api", "GHEC_REST",
			"method", "Repositories.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
		)
		return false, nil
	}

	// Handle real errors (not 404s)
	if err != nil || nonResourceNotFoundErr != nil {
		errorToUse := err
		if nonResourceNotFoundErr != nil {
			errorToUse = nonResourceNotFoundErr
		}

		a.logger.Error("Failed to check if repository exists in target org",
			"api", "GHEC_REST",
			"method", "Repositories.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
			"error", errorToUse,
		)
		return false, a.classifyGitHubError(errorToUse)
	}

	a.logger.Debug("Repository exists in target org",
		"api", "GHEC_REST",
		"method", "Repositories.Get",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"org", org,
		"repo", repo,
	)
	return true, nil
}

// GetRepositorySize retrieves the size of a repository in bytes.
// It returns the size in bytes and an error if the repository doesn't exist or can't be accessed.
func (a *GitHubAPI) GetRepositorySize(ctx context.Context, org, repo string) (int64, error) {
	startTime := time.Now()
	a.logger.Debug("Getting repository size",
		"api", "GHES_REST",
		"method", "Repositories.Get",
		"org", org,
		"repo", repo,
	)

	var respStatus int
	var repository *github.Repository

	err := a.circuitProtectedGhesOperation(ctx, "get_repository_size", func() error {
		var resp *github.Response
		var err error
		repository, resp, err = a.clients.GHESClient.Repositories.Get(ctx, org, repo)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("Failed to get repository size",
			"api", "GHES_REST",
			"method", "Repositories.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"repo", repo,
			"error", err,
		)
		return 0, a.classifyGitHubError(err)
	}

	// GitHub API returns size in KB, so we convert to bytes
	sizeInBytes := int64(repository.GetSize()) * 1024

	a.logger.Debug("Repository size retrieved successfully",
		"api", "GHES_REST",
		"method", "Repositories.Get",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"org", org,
		"repo", repo,
		"size_kb", repository.GetSize(),
		"size_bytes", sizeInBytes,
	)
	return sizeInBytes, nil
}

// ValidateGHESOrganization checks if an organization exists and is accessible on GitHub Enterprise Server.
// It makes a REST API call to the GHES instance and verifies the organization's existence and accessibility.
// Returns an error if the organization doesn't exist, can't be accessed, or if there are API issues.
func (a *GitHubAPI) ValidateGHESOrganization(ctx context.Context, org string) error {
	startTime := time.Now()
	a.logger.Debug("Validating GHES organization",
		"api", "GHES_REST",
		"method", "Organizations.Get",
		"org", org,
	)

	var respStatus int
	err := a.circuitProtectedGhesOperation(ctx, "validate_ghes_organization", func() error {
		_, resp, err := a.clients.GHESClient.Organizations.Get(ctx, org)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("GHES organization validation failed",
			"api", "GHES_REST",
			"method", "Organizations.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"error", err,
		)
		return a.classifyGitHubError(err)
	}

	a.logger.Debug("GHES organization validation successful",
		"api", "GHES_REST",
		"method", "Organizations.Get",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"org", org,
	)
	return nil
}

// ValidateGHCloudOrganization checks if an organization exists and is accessible on GitHub Enterprise Cloud.
// It makes a REST API call to the GHEC instance and verifies the organization's existence and accessibility.
// Returns an error if the organization doesn't exist, can't be accessed, or if there are API issues.
func (a *GitHubAPI) ValidateGHCloudOrganization(ctx context.Context, org string) error {
	startTime := time.Now()
	a.logger.Debug("Validating GitHub Cloud organization",
		"api", "GHEC_REST",
		"method", "Organizations.Get",
		"org", org,
	)

	var respStatus int
	err := a.circuitProtectedGhCloudOperation(ctx, "validate_ghcloud_organization", func() error {
		_, resp, err := a.clients.GHCloudClient.Organizations.Get(ctx, org)
		if resp != nil {
			respStatus = resp.StatusCode
		}
		return err
	})

	duration := time.Since(startTime)

	if err != nil {
		a.logger.Error("GitHub Cloud organization validation failed",
			"api", "GHEC_REST",
			"method", "Organizations.Get",
			"duration_ms", duration.Milliseconds(),
			"status_code", respStatus,
			"org", org,
			"error", err,
		)
		return a.classifyGitHubError(err)
	}

	a.logger.Debug("GitHub Cloud organization validation successful",
		"api", "GHEC_REST",
		"method", "Organizations.Get",
		"duration_ms", duration.Milliseconds(),
		"status_code", respStatus,
		"org", org,
	)
	return nil
}
