package github

import (
	"context"
	"fmt"
	"time"

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

		// For 404 errors, keep the "repository not found" message for compatibility
		if respStatus == 404 {
			return a.classifyGitHubError(err)
		}

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
