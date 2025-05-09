package config

import (
	"context"
	"net/url"

	"github.com/google/go-github/v70/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Clients holds all GitHub API clients
type Clients struct {
	GHESClient     *github.Client
	GHCloudClient  *github.Client
	GHCloudGraphQL *githubv4.Client
}

// NewClients creates new GitHub API clients
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

// UpdateGHESBaseURL updates the GHES client's base URL
func (c *Clients) UpdateGHESBaseURL(baseURL string) error {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	c.GHESClient.BaseURL = parsedURL
	return nil
}

// GetClients returns the clients instance
func GetClients() *Clients {
	return cfg.Clients
}

// InitClients initializes the GitHub API clients
func InitClients(ghesToken, ghCloudToken string) error {
	clients, err := NewClients(ghesToken, ghCloudToken)
	if err != nil {
		return err
	}
	cfg.Clients = clients
	return nil
}
