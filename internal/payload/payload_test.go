package payload

import (
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/validation"
	"github.com/stretchr/testify/assert"
)

func TestMigrationRequest_Validate(t *testing.T) {
	validRequest := MigrationRequest{
		SourceOrg:    "source-org",
		TargetOrg:    "target-org",
		Repositories: []string{"repo1", "repo2"},
		GHESBaseURL:  "https://github.example.com",
		GHESToken:    "0123456789012345678901234567890123456789",
		GHCloudToken: "0123456789012345678901234567890123456789",
		MaxDuration:  "24h",
		UseGHOS:      true,
	}

	tests := []struct {
		name    string
		request MigrationRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			request: validRequest,
			wantErr: false,
		},
		{
			name: "invalid source org",
			request: MigrationRequest{
				SourceOrg:    "@invalid",
				TargetOrg:    "target-org",
				Repositories: []string{"repo1"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "0123456789012345678901234567890123456789",
				GHCloudToken: "0123456789012345678901234567890123456789",
			},
			wantErr: true,
		},
		{
			name: "invalid target org",
			request: MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "@invalid",
				Repositories: []string{"repo1"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "0123456789012345678901234567890123456789",
				GHCloudToken: "0123456789012345678901234567890123456789",
			},
			wantErr: true,
		},
		{
			name: "same source and target org",
			request: MigrationRequest{
				SourceOrg:    "same-org",
				TargetOrg:    "same-org",
				Repositories: []string{"repo1"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "0123456789012345678901234567890123456789",
				GHCloudToken: "0123456789012345678901234567890123456789",
			},
			wantErr: true,
		},
		{
			name: "invalid repository name",
			request: MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				Repositories: []string{"@invalid"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "0123456789012345678901234567890123456789",
				GHCloudToken: "0123456789012345678901234567890123456789",
			},
			wantErr: true,
		},
		{
			name: "invalid GHES URL",
			request: MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				Repositories: []string{"repo1"},
				GHESBaseURL:  "not-a-url",
				GHESToken:    "0123456789012345678901234567890123456789",
				GHCloudToken: "0123456789012345678901234567890123456789",
			},
			wantErr: true,
		},
		{
			name: "invalid GHES token",
			request: MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				Repositories: []string{"repo1"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "short",
				GHCloudToken: "0123456789012345678901234567890123456789",
			},
			wantErr: true,
		},
		{
			name: "invalid GH Cloud token",
			request: MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				Repositories: []string{"repo1"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "0123456789012345678901234567890123456789",
				GHCloudToken: "short",
			},
			wantErr: true,
		},
		{
			name: "invalid max duration",
			request: MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				Repositories: []string{"repo1"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "0123456789012345678901234567890123456789",
				GHCloudToken: "0123456789012345678901234567890123456789",
				MaxDuration:  "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMigrationRequest_GetMaxDuration(t *testing.T) {
	tests := []struct {
		name     string
		request  MigrationRequest
		expected time.Duration
	}{
		{
			name: "valid duration",
			request: MigrationRequest{
				MaxDuration: "24h",
			},
			expected: 24 * time.Hour,
		},
		{
			name: "empty duration",
			request: MigrationRequest{
				MaxDuration: "",
			},
			expected: time.Duration(validation.DefaultMaxDuration) * time.Hour,
		},
		{
			name: "invalid duration",
			request: MigrationRequest{
				MaxDuration: "invalid",
			},
			expected: time.Duration(validation.DefaultMaxDuration) * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := tt.request.GetMaxDuration()
			assert.Equal(t, tt.expected, duration)
		})
	}
}

func TestMigrationRequest_GetGHESAPIURL(t *testing.T) {
	tests := []struct {
		name     string
		request  MigrationRequest
		expected string
	}{
		{
			name: "URL without trailing slash",
			request: MigrationRequest{
				GHESBaseURL: "https://github.example.com",
			},
			expected: "https://github.example.com/api/v3/",
		},
		{
			name: "URL with trailing slash",
			request: MigrationRequest{
				GHESBaseURL: "https://github.example.com/",
			},
			expected: "https://github.example.com/api/v3/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.request.GetGHESAPIURL()
			assert.Equal(t, tt.expected, url)
		})
	}
}

func TestMigrationRequest_GetGHESGraphQLURL(t *testing.T) {
	tests := []struct {
		name     string
		request  MigrationRequest
		expected string
	}{
		{
			name: "URL without trailing slash",
			request: MigrationRequest{
				GHESBaseURL: "https://github.example.com",
			},
			expected: "https://github.example.com/api/graphql",
		},
		{
			name: "URL with trailing slash",
			request: MigrationRequest{
				GHESBaseURL: "https://github.example.com/",
			},
			expected: "https://github.example.com/api/graphql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.request.GetGHESGraphQLURL()
			assert.Equal(t, tt.expected, url)
		})
	}
}

func TestMigrationStages(t *testing.T) {
	expectedStages := []string{
		"validation",
		"setup",
		"archive",
		"storage",
		"migration",
	}
	assert.Equal(t, expectedStages, MigrationStages)
}

func TestMigrationStatus(t *testing.T) {
	status := MigrationStatus{
		Repository:        "test-repo",
		Status:            StatusInProgress,
		Error:             "",
		UpdatedAt:         time.Now(),
		Stage:             "validation",
		State:             "checking",
		StartedAt:         time.Now().Add(-time.Hour),
		Duration:          time.Hour,
		MigrationID:       "12345",
		Progress:          50,
		StageProgress:     75,
		CompletedStages:   []string{"validation"},
		TotalStages:       4,
		CurrentStageIndex: 2,
	}

	assert.Equal(t, "test-repo", status.Repository)
	assert.Equal(t, StatusInProgress, status.Status)
	assert.Empty(t, status.Error)
	assert.NotZero(t, status.UpdatedAt)
	assert.Equal(t, "validation", status.Stage)
	assert.Equal(t, "checking", status.State)
	assert.NotZero(t, status.StartedAt)
	assert.Equal(t, time.Hour, status.Duration)
	assert.Equal(t, "12345", status.MigrationID)
	assert.Equal(t, 50, status.Progress)
	assert.Equal(t, 75, status.StageProgress)
	assert.Equal(t, []string{"validation"}, status.CompletedStages)
	assert.Equal(t, 4, status.TotalStages)
	assert.Equal(t, 2, status.CurrentStageIndex)
}

func TestGetSizeCategory(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		expected RepositorySizeCategory
	}{
		{
			name:     "Small 0 bytes",
			size:     0,
			expected: SizeSmall,
		},
		{
			name:     "Small 5MB",
			size:     5 * 1024 * 1024,
			expected: SizeSmall,
		},
		{
			name:     "Small boundary",
			size:     10*1024*1024 - 1,
			expected: SizeSmall,
		},
		{
			name:     "Medium at boundary",
			size:     10 * 1024 * 1024,
			expected: SizeMedium,
		},
		{
			name:     "Medium 50MB",
			size:     50 * 1024 * 1024,
			expected: SizeMedium,
		},
		{
			name:     "Medium boundary",
			size:     100*1024*1024 - 1,
			expected: SizeMedium,
		},
		{
			name:     "Large at boundary",
			size:     100 * 1024 * 1024,
			expected: SizeLarge,
		},
		{
			name:     "Large 500MB",
			size:     500 * 1024 * 1024,
			expected: SizeLarge,
		},
		{
			name:     "Large boundary",
			size:     1024*1024*1024 - 1,
			expected: SizeLarge,
		},
		{
			name:     "Extra Large at boundary",
			size:     1024 * 1024 * 1024,
			expected: SizeExtraLarge,
		},
		{
			name:     "Extra Large 2GB",
			size:     2 * 1024 * 1024 * 1024,
			expected: SizeExtraLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSizeCategory(tt.size)
			if result != tt.expected {
				t.Errorf("GetSizeCategory(%d) = %s, want %s", tt.size, result, tt.expected)
			}
		})
	}
}

func TestMigrationRequest_ValidateSchedulingParams(t *testing.T) {
	now := time.Now().Add(time.Hour * 24) // Tomorrow

	tests := []struct {
		name    string
		request *MigrationRequest
		wantErr bool
	}{
		{
			name: "Valid basic request without scheduling",
			request: &MigrationRequest{
				SourceOrg:    "source-org",
				TargetOrg:    "target-org",
				Repositories: []string{"repo1", "repo2"},
				GHESBaseURL:  "https://github.example.com",
				GHESToken:    "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken: "ghp_validtoken1234567890abcdefghijklmno",
			},
			wantErr: false,
		},
		{
			name: "Valid scheduled time",
			request: &MigrationRequest{
				SourceOrg:     "source-org",
				TargetOrg:     "target-org",
				Repositories:  []string{"repo1", "repo2"},
				GHESBaseURL:   "https://github.example.com",
				GHESToken:     "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:  "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime: &now,
			},
			wantErr: false,
		},
		{
			name: "Valid timezone",
			request: &MigrationRequest{
				SourceOrg:         "source-org",
				TargetOrg:         "target-org",
				Repositories:      []string{"repo1", "repo2"},
				GHESBaseURL:       "https://github.example.com",
				GHESToken:         "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:      "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime:     &now,
				ScheduledTimeZone: "America/New_York",
			},
			wantErr: false,
		},
		{
			name: "Invalid timezone",
			request: &MigrationRequest{
				SourceOrg:         "source-org",
				TargetOrg:         "target-org",
				Repositories:      []string{"repo1", "repo2"},
				GHESBaseURL:       "https://github.example.com",
				GHESToken:         "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:      "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime:     &now,
				ScheduledTimeZone: "Invalid/TimeZone",
			},
			wantErr: true,
		},
		{
			name: "Valid days of week",
			request: &MigrationRequest{
				SourceOrg:         "source-org",
				TargetOrg:         "target-org",
				Repositories:      []string{"repo1", "repo2"},
				GHESBaseURL:       "https://github.example.com",
				GHESToken:         "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:      "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime:     &now,
				ScheduledDaysOnly: []string{"Monday", "Wednesday", "Friday"},
			},
			wantErr: false,
		},
		{
			name: "Invalid day of week",
			request: &MigrationRequest{
				SourceOrg:         "source-org",
				TargetOrg:         "target-org",
				Repositories:      []string{"repo1", "repo2"},
				GHESBaseURL:       "https://github.example.com",
				GHESToken:         "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:      "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime:     &now,
				ScheduledDaysOnly: []string{"monday", "Wednesday", "Friday"}, // lowercase not valid
			},
			wantErr: true,
		},
		{
			name: "Valid time window",
			request: &MigrationRequest{
				SourceOrg:          "source-org",
				TargetOrg:          "target-org",
				Repositories:       []string{"repo1", "repo2"},
				GHESBaseURL:        "https://github.example.com",
				GHESToken:          "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:       "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime:      &now,
				ScheduledTimeStart: "22:00",
				ScheduledTimeEnd:   "06:00",
			},
			wantErr: false,
		},
		{
			name: "Invalid time window start",
			request: &MigrationRequest{
				SourceOrg:          "source-org",
				TargetOrg:          "target-org",
				Repositories:       []string{"repo1", "repo2"},
				GHESBaseURL:        "https://github.example.com",
				GHESToken:          "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:       "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime:      &now,
				ScheduledTimeStart: "2200", // Invalid format
				ScheduledTimeEnd:   "06:00",
			},
			wantErr: true,
		},
		{
			name: "Invalid time window end",
			request: &MigrationRequest{
				SourceOrg:          "source-org",
				TargetOrg:          "target-org",
				Repositories:       []string{"repo1", "repo2"},
				GHESBaseURL:        "https://github.example.com",
				GHESToken:          "ghp_validtoken1234567890abcdefghijklmno",
				GHCloudToken:       "ghp_validtoken1234567890abcdefghijklmno",
				ScheduledTime:      &now,
				ScheduledTimeStart: "22:00",
				ScheduledTimeEnd:   "6:00 AM", // Invalid format
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("MigrationRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
