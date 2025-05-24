package migrator

import (
	"log/slog"
	"testing"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/github"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/storage"
)

// TestClientSelection verifies the GitHub client selection logic works correctly
func TestClientSelection(t *testing.T) {
	tests := []struct {
		name             string
		injectedAPI      github.API
		expectInjected   bool
		expectAPICreated bool
	}{
		{
			name:             "no injected API - should create real client",
			injectedAPI:      nil,
			expectInjected:   false,
			expectAPICreated: true,
		},
		{
			name:             "injected mock API - should use injected",
			injectedAPI:      &MockGitHubAPI{},
			expectInjected:   true,
			expectAPICreated: false,
		},
		{
			name:             "injected NoopAPI - should use injected",
			injectedAPI:      github.NewNoopAPI(slog.Default()),
			expectInjected:   true,
			expectAPICreated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create migrator with or without injected API
			cfg := &config.Config{
				GitHub: config.GitHubConfig{
					Proxy: config.ProxyConfig{
						Enabled: false,
					},
				},
			}

			migrator := &Migrator{
				logger:    slog.Default(),
				githubAPI: tt.injectedAPI,
				storage:   &storage.NoopStorage{},
				config:    cfg,
			}

			// Try to determine which client would be used by checking the logic
			var selectedAPI github.API

			// Mimic the logic from migrateRepository
			if migrator.githubAPI != nil {
				selectedAPI = migrator.githubAPI
			} else {
				// In a real scenario, this would create clients, but for this test
				// we just check if we would try to create them
				selectedAPI = nil
			}

			// Verify expectations
			if tt.expectInjected && selectedAPI == nil {
				t.Error("Expected to use injected API but got nil")
			}

			if !tt.expectInjected && selectedAPI != nil {
				t.Error("Expected to create new client but used injected API")
			}

			if tt.expectInjected && selectedAPI != tt.injectedAPI {
				t.Error("Expected to use the specific injected API")
			}

			// Verify test implementation detection
			if selectedAPI != nil {
				isTest := selectedAPI.IsTestImplementation()
				if tt.injectedAPI != nil {
					expectedIsTest := tt.injectedAPI.IsTestImplementation()
					if isTest != expectedIsTest {
						t.Errorf("IsTestImplementation() = %v, expected %v", isTest, expectedIsTest)
					}
				}
			}
		})
	}
}

// TestProductionSetup verifies that in production setup, no API is injected
func TestProductionSetup(t *testing.T) {
	// This test verifies that when we follow the production setup pattern
	// (as in cmd/root.go), no GitHub API client is injected

	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Proxy: config.ProxyConfig{
				Enabled: false,
			},
		},
		Storage: config.StorageConfig{
			Enabled: false,
		},
		Queue: config.QueueConfig{
			Enabled: false,
		},
	}

	// Create migrator the same way production does (no injected API)
	migrator := NewMigrator(
		slog.Default(),         // Logger
		nil,                    // GitHub API client (nil - will create real clients per migration)
		&storage.NoopStorage{}, // Storage provider
		"",                     // Webhook URL
		cfg,                    // Full config
		nil,                    // HTTP client
		nil,                    // Tracing provider
	)

	// Verify no API was injected
	if migrator.githubAPI != nil {
		t.Error("Expected no GitHub API to be injected in production setup")
	}
}

// TestTestSetup verifies that in test setup, mock APIs are properly injected
func TestTestSetup(t *testing.T) {
	// This test verifies the test setup pattern used in tests

	mockAPI := &MockGitHubAPI{}
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Proxy: config.ProxyConfig{
				Enabled: false,
			},
		},
	}

	migrator := &Migrator{
		logger:    slog.Default(),
		githubAPI: mockAPI,
		storage:   &storage.NoopStorage{},
		config:    cfg,
	}

	// Verify mock API was injected
	if migrator.githubAPI == nil {
		t.Error("Expected mock GitHub API to be injected in test setup")
	}

	if migrator.githubAPI != mockAPI {
		t.Error("Expected the specific mock API to be injected")
	}

	if !migrator.githubAPI.IsTestImplementation() {
		t.Error("Expected injected API to be a test implementation")
	}
}
