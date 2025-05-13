// Package config provides configuration management and GitHub API client initialization
// for the migration tool. It handles reading, writing, and validating configuration
// as well as creating authenticated GitHub clients.
package config

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/go-github/v70/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Clients holds all GitHub API clients used in the application.
// It provides access to both GitHub Enterprise Server and GitHub Cloud APIs
// through REST and GraphQL interfaces.
type Clients struct {
	// GHESClient is the REST API client for GitHub Enterprise Server.
	GHESClient *github.Client
	// GHCloudClient is the REST API client for GitHub Enterprise Cloud.
	GHCloudClient *github.Client
	// GHCloudGraphQL is the GraphQL API client for GitHub Enterprise Cloud.
	GHCloudGraphQL *githubv4.Client
}

// NewClients creates new GitHub API clients with the provided authentication tokens.
// It initializes REST and GraphQL clients for both GitHub Enterprise Server and Cloud.
//
// Parameters:
//   - ghesToken: The personal access token for GitHub Enterprise Server.
//   - ghCloudToken: The personal access token for GitHub Enterprise Cloud.
//
// Returns:
//   - *Clients: The initialized clients structure.
//   - error: An error if client initialization fails.
func NewClients(ghesToken, ghCloudToken string) (*Clients, error) {
	// Create GHES client
	ghesTransport := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: ghesToken},
	))
	ghesClient := github.NewClient(ghesTransport)

	// Create GHEC client
	ghCloudTransport := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: ghCloudToken},
	))
	ghCloudClient := github.NewClient(ghCloudTransport)
	ghCloudGraphQL := githubv4.NewClient(ghCloudTransport)

	return &Clients{
		GHESClient:     ghesClient,
		GHCloudClient:  ghCloudClient,
		GHCloudGraphQL: ghCloudGraphQL,
	}, nil
}

// UpdateGHESBaseURL updates the GHES client's base URL for API requests.
// This is necessary since GitHub Enterprise Server instances have custom domains.
//
// Parameters:
//   - baseURL: The base URL of the GitHub Enterprise Server API.
//
// Returns:
//   - error: An error if the URL is invalid or cannot be parsed.
func (c *Clients) UpdateGHESBaseURL(baseURL string) error {
	// check if baseURL is empty
	if baseURL == "" {
		return errors.New("base URL is empty")
	}

	// Ensure the URL has a trailing slash as required by go-github
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return err
	}

	// Validate that the URL has a scheme and host
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return errors.New("invalid URL: must include scheme (http/https) and host")
	}

	// Validate that the scheme is either http or https
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return errors.New("invalid URL scheme: must be http or https")
	}

	c.GHESClient.BaseURL = parsedURL
	return nil
}
