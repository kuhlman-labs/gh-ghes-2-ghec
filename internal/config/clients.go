// Package config provides configuration management and GitHub API client initialization
// for the migration tool. It handles reading, writing, and validating configuration
// as well as creating authenticated GitHub clients.
package config

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v70/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// ProxyConfig contains proxy server configuration for GitHub API requests.
// Enterprise environments often require HTTP traffic to pass through proxy servers
// for security, monitoring, and network policy enforcement.
type ProxyConfig struct {
	// Enabled indicates whether the proxy configuration should be applied.
	Enabled bool
	// URL is the proxy server URL.
	URL string
	// Username for authenticating with the proxy, if required.
	Username string
	// Password for authenticating with the proxy, if required.
	Password string
	// NoProxyList is a comma-separated list of hostnames that should not use the proxy.
	NoProxyList string
}

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
	// Config holds the original client configuration
	Config *ClientsConfig
}

// ClientsConfig holds configuration for creating GitHub API clients.
type ClientsConfig struct {
	// GHESToken is the personal access token for GitHub Enterprise Server.
	GHESToken string
	// GHCloudToken is the personal access token for GitHub Enterprise Cloud.
	GHCloudToken string
	// GHESBaseURL is the base URL for the GitHub Enterprise Server API.
	GHESBaseURL string
	// Proxy contains proxy server configuration.
	Proxy ProxyConfig
}

// NewClients creates new GitHub API clients with the provided authentication tokens.
// It initializes REST and GraphQL clients for both GitHub Enterprise Server and Cloud.
//
// Parameters:
//   - config: The configuration for creating clients
//
// Returns:
//   - *Clients: The initialized clients structure.
//   - error: An error if client initialization fails.
func NewClients(config *ClientsConfig) (*Clients, error) {
	if config == nil {
		return nil, errors.New("client configuration is nil")
	}

	// Create transport with optional proxy support
	ghesTransport, err := createTransportWithProxy(config.GHESToken, &config.Proxy)
	if err != nil {
		return nil, err
	}

	ghCloudTransport, err := createTransportWithProxy(config.GHCloudToken, &config.Proxy)
	if err != nil {
		return nil, err
	}

	// Create API clients
	ghesClient := github.NewClient(ghesTransport)
	ghCloudClient := github.NewClient(ghCloudTransport)
	ghCloudGraphQL := githubv4.NewClient(ghCloudTransport)

	// Create client wrapper
	clients := &Clients{
		GHESClient:     ghesClient,
		GHCloudClient:  ghCloudClient,
		GHCloudGraphQL: ghCloudGraphQL,
		Config:         config,
	}

	// Set GHES base URL if provided
	if config.GHESBaseURL != "" {
		if err := clients.UpdateGHESBaseURL(config.GHESBaseURL); err != nil {
			return nil, err
		}
	}

	return clients, nil
}

// createTransportWithProxy creates an HTTP client with OAuth2 token authentication
// and optional proxy configuration.
func createTransportWithProxy(token string, proxyConfig *ProxyConfig) (*http.Client, error) {
	// Start with the standard OAuth2 token source
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})

	// If proxy is not enabled, create standard client
	if proxyConfig == nil || !proxyConfig.Enabled || proxyConfig.URL == "" {
		return oauth2.NewClient(context.Background(), tokenSource), nil
	}

	// Parse proxy URL
	proxyURL, err := url.Parse(proxyConfig.URL)
	if err != nil {
		return nil, err
	}

	// Add basic auth to proxy URL if credentials are provided
	if proxyConfig.Username != "" {
		if proxyConfig.Password != "" {
			proxyURL.User = url.UserPassword(proxyConfig.Username, proxyConfig.Password)
		} else {
			proxyURL.User = url.User(proxyConfig.Username)
		}
	}

	// Create transport with proxy
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	// Add no-proxy support if specified
	if proxyConfig.NoProxyList != "" {
		noProxyHosts := strings.Split(proxyConfig.NoProxyList, ",")
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			host := req.URL.Hostname()

			// Skip proxy for hosts in the no-proxy list
			for _, noProxyHost := range noProxyHosts {
				if strings.TrimSpace(noProxyHost) == host {
					return nil, nil
				}

				// Support wildcard matching (e.g., *.example.com)
				if strings.HasPrefix(noProxyHost, "*.") &&
					strings.HasSuffix(host, strings.TrimPrefix(noProxyHost, "*")) {
					return nil, nil
				}
			}

			return proxyURL, nil
		}
	}

	// Create custom context that doesn't require background
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: transport,
	})

	// Return OAuth2 client with custom transport
	return oauth2.NewClient(ctx, tokenSource), nil
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

	// Store the updated base URL in the config
	if c.Config != nil {
		c.Config.GHESBaseURL = baseURL
	}

	return nil
}

// BackwardCompatNewClients creates clients using the old API for backward compatibility
func BackwardCompatNewClients(ghesToken, ghCloudToken string) (*Clients, error) {
	config := &ClientsConfig{
		GHESToken:    ghesToken,
		GHCloudToken: ghCloudToken,
		Proxy: ProxyConfig{
			Enabled: false,
		},
	}

	return NewClients(config)
}
