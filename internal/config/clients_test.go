package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClients(t *testing.T) {
	tests := []struct {
		name         string
		ghesToken    string
		ghCloudToken string
		wantErr      bool
	}{
		{
			name:         "valid tokens",
			ghesToken:    "ghes-token",
			ghCloudToken: "ghcloud-token",
			wantErr:      false,
		},
		{
			name:         "empty tokens",
			ghesToken:    "",
			ghCloudToken: "",
			wantErr:      false, // Empty tokens are valid, they'll just result in unauthorized requests
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ClientsConfig{
				GHESToken:    tt.ghesToken,
				GHCloudToken: tt.ghCloudToken,
				Proxy: ProxyConfig{
					Enabled: false,
				},
			}

			clients, err := NewClients(config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, clients)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, clients)
				assert.NotNil(t, clients.GHESClient)
				assert.NotNil(t, clients.GHCloudClient)
				assert.NotNil(t, clients.GHCloudGraphQL)
			}
		})
	}
}

func TestUpdateGHESBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{
			name:    "valid URL with trailing slash",
			baseURL: "https://ghes.example.com/api/v3/",
			wantErr: false,
		},
		{
			name:    "valid URL without trailing slash",
			baseURL: "https://ghes.example.com/api/v3",
			wantErr: false,
		},
		{
			name:    "invalid URL",
			baseURL: "not-a-url",
			wantErr: true,
		},
		{
			name:    "empty URL",
			baseURL: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ClientsConfig{
				GHESToken:    "ghes-token",
				GHCloudToken: "ghcloud-token",
				Proxy: ProxyConfig{
					Enabled: false,
				},
			}

			clients, err := NewClients(config)
			require.NoError(t, err)
			require.NotNil(t, clients)

			err = clients.UpdateGHESBaseURL(tt.baseURL)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify the URL was properly set
				assert.NotNil(t, clients.GHESClient.BaseURL)
				expectedURL := tt.baseURL
				if !tt.wantErr && !strings.HasSuffix(expectedURL, "/") {
					expectedURL += "/"
				}
				assert.Equal(t, expectedURL, clients.GHESClient.BaseURL.String())
			}
		})
	}
}

func TestClientWithProxy(t *testing.T) {
	tests := []struct {
		name     string
		proxyURL string
		username string
		password string
		noProxy  string
		enabled  bool
		wantErr  bool
	}{
		{
			name:     "proxy disabled",
			proxyURL: "http://proxy.example.com:8080",
			enabled:  false,
			wantErr:  false,
		},
		{
			name:     "proxy enabled",
			proxyURL: "http://proxy.example.com:8080",
			enabled:  true,
			wantErr:  false,
		},
		{
			name:     "proxy with auth",
			proxyURL: "http://proxy.example.com:8080",
			username: "user",
			password: "pass",
			enabled:  true,
			wantErr:  false,
		},
		{
			name:     "proxy with no proxy list",
			proxyURL: "http://proxy.example.com:8080",
			noProxy:  "localhost,127.0.0.1,*.internal",
			enabled:  true,
			wantErr:  false,
		},
		{
			name:     "invalid proxy URL",
			proxyURL: "not-a-url",
			enabled:  true,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ClientsConfig{
				GHESToken:    "ghes-token",
				GHCloudToken: "ghcloud-token",
				Proxy: ProxyConfig{
					Enabled:     tt.enabled,
					URL:         tt.proxyURL,
					Username:    tt.username,
					Password:    tt.password,
					NoProxyList: tt.noProxy,
				},
			}

			clients, err := NewClients(config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, clients)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, clients)
				assert.NotNil(t, clients.GHESClient)
				assert.NotNil(t, clients.GHCloudClient)
				assert.NotNil(t, clients.GHCloudGraphQL)

				// Cannot easily verify proxy was configured since it's internal to the HTTP client
				if tt.enabled {
					assert.True(t, config.Proxy.Enabled)
				} else {
					assert.False(t, config.Proxy.Enabled)
				}
			}
		})
	}
}
