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
			clients, err := NewClients(tt.ghesToken, tt.ghCloudToken)
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
			clients, err := NewClients("ghes-token", "ghcloud-token")
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
