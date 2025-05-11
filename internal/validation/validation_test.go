package validation

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid https URL",
			url:     "https://github.example.com",
			wantErr: false,
		},
		{
			name:    "valid http URL",
			url:     "http://github.example.com",
			wantErr: false,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name:    "invalid scheme",
			url:     "ftp://github.example.com",
			wantErr: true,
		},
		{
			name:    "missing hostname",
			url:     "https://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateGitHubToken(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "valid classic token",
			token:   "0123456789012345678901234567890123456789",
			wantErr: false,
		},
		{
			name:    "valid GitHub App token",
			token:   "ghs_012345678901234567890123456789012345",
			wantErr: false,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
		{
			name:    "too short token",
			token:   "short",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitHubToken(tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateOrganizationName(t *testing.T) {
	tests := []struct {
		name    string
		org     string
		wantErr bool
	}{
		{
			name:    "valid org name",
			org:     "test-org",
			wantErr: false,
		},
		{
			name:    "valid org name with numbers",
			org:     "test123",
			wantErr: false,
		},
		{
			name:    "empty org name",
			org:     "",
			wantErr: true,
		},
		{
			name:    "too long org name",
			org:     "a" + string(make([]byte, MaxOrgNameLength)),
			wantErr: true,
		},
		{
			name:    "invalid characters",
			org:     "test@org",
			wantErr: true,
		},
		{
			name:    "starts with hyphen",
			org:     "-test-org",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOrganizationName(tt.org)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRepositoryName(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		{
			name:    "valid repo name",
			repo:    "test-repo",
			wantErr: false,
		},
		{
			name:    "valid repo name with numbers",
			repo:    "test123",
			wantErr: false,
		},
		{
			name:    "empty repo name",
			repo:    "",
			wantErr: true,
		},
		{
			name:    "too long repo name",
			repo:    "a" + string(make([]byte, MaxRepoNameLength)),
			wantErr: true,
		},
		{
			name:    "invalid characters",
			repo:    "test@repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRepositoryName(tt.repo)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRepositoryList(t *testing.T) {
	tests := []struct {
		name    string
		repos   []string
		wantErr bool
	}{
		{
			name:    "valid repo list",
			repos:   []string{"repo1", "repo2", "repo3"},
			wantErr: false,
		},
		{
			name:    "empty repo list",
			repos:   []string{},
			wantErr: true,
		},
		{
			name:    "too many repos",
			repos:   make([]string, MaxRepositories+1),
			wantErr: true,
		},
		{
			name:    "duplicate repos",
			repos:   []string{"repo1", "Repo1"},
			wantErr: true,
		},
		{
			name:    "invalid repo name",
			repos:   []string{"valid-repo", "invalid@repo"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRepositoryList(tt.repos)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateDuration(t *testing.T) {
	tests := []struct {
		name       string
		duration   string
		wantErr    bool
		checkValue bool
		wantValue  time.Duration
	}{
		{
			name:       "valid duration",
			duration:   "1h",
			wantErr:    false,
			checkValue: true,
			wantValue:  time.Hour,
		},
		{
			name:       "empty duration",
			duration:   "",
			wantErr:    false,
			checkValue: true,
			wantValue:  time.Duration(DefaultMaxDuration) * time.Hour,
		},
		{
			name:     "invalid duration",
			duration: "invalid",
			wantErr:  true,
		},
		{
			name:     "negative duration",
			duration: "-1h",
			wantErr:  true,
		},
		{
			name:     "too long duration",
			duration: "200h",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration, err := ValidateDuration(tt.duration)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkValue {
					assert.Equal(t, tt.wantValue, duration)
				}
			}
		})
	}
}

func TestTestGHESURL(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authorization header
		if r.Header.Get("Authorization") != "token test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Check if it's the meta endpoint
		if r.URL.Path != "/api/v3/meta" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tests := []struct {
		name    string
		baseURL string
		token   string
		wantErr bool
	}{
		{
			name:    "valid connection",
			baseURL: server.URL,
			token:   "test-token",
			wantErr: false,
		},
		{
			name:    "invalid token",
			baseURL: server.URL,
			token:   "wrong-token",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			baseURL: "not-a-url",
			token:   "test-token",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := TestGHESURL(tt.baseURL, tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
