package version

import (
	"testing"
)

func TestGetVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{
			name:     "default version",
			version:  "dev",
			expected: "dev",
		},
		{
			name:     "custom version",
			version:  "v1.2.3",
			expected: "v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original value
			originalVersion := Version
			defer func() {
				Version = originalVersion
			}()

			Version = tt.version
			result := GetVersion()
			if result != tt.expected {
				t.Errorf("GetVersion() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetBuildTime(t *testing.T) {
	tests := []struct {
		name      string
		buildTime string
		expected  string
	}{
		{
			name:      "default build time",
			buildTime: "unknown",
			expected:  "unknown",
		},
		{
			name:      "custom build time",
			buildTime: "2023-12-01T10:00:00Z",
			expected:  "2023-12-01T10:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original value
			originalBuildTime := BuildTime
			defer func() {
				BuildTime = originalBuildTime
			}()

			BuildTime = tt.buildTime
			result := GetBuildTime()
			if result != tt.expected {
				t.Errorf("GetBuildTime() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetVersionInfo(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		buildTime string
		expected  string
	}{
		{
			name:      "default values",
			version:   "dev",
			buildTime: "unknown",
			expected:  "Version: dev (Built: unknown)",
		},
		{
			name:      "custom values",
			version:   "v1.2.3",
			buildTime: "2023-12-01T10:00:00Z",
			expected:  "Version: v1.2.3 (Built: 2023-12-01T10:00:00Z)",
		},
		{
			name:      "empty values",
			version:   "",
			buildTime: "",
			expected:  "Version:  (Built: )",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original values
			originalVersion := Version
			originalBuildTime := BuildTime
			defer func() {
				Version = originalVersion
				BuildTime = originalBuildTime
			}()

			Version = tt.version
			BuildTime = tt.buildTime
			result := GetVersionInfo()
			if result != tt.expected {
				t.Errorf("GetVersionInfo() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGlobalVariables(t *testing.T) {
	t.Run("default Version value", func(t *testing.T) {
		if Version != "dev" {
			t.Errorf("Version default value = %v, want %v", Version, "dev")
		}
	})

	t.Run("default BuildTime value", func(t *testing.T) {
		if BuildTime != "unknown" {
			t.Errorf("BuildTime default value = %v, want %v", BuildTime, "unknown")
		}
	})
}
