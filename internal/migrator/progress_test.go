package migrator

import (
	"testing"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/stretchr/testify/assert"
)

func TestCalculateProgressData(t *testing.T) {
	tests := []struct {
		name                  string
		stage                 string
		state                 string
		existing              *payload.MigrationStatus
		expectedProgress      int
		expectedStageProgress int
		expectedCompleted     []string
		expectedStageIndex    int
	}{
		{
			name:                  "new migration - init stage",
			stage:                 "init",
			state:                 "starting",
			existing:              nil,
			expectedProgress:      0,
			expectedStageProgress: 0,
			expectedCompleted:     []string{},
			expectedStageIndex:    0,
		},
		{
			name:                  "validation stage - checking source",
			stage:                 "validation",
			state:                 "checking_source",
			existing:              nil,
			expectedProgress:      2, // 10% * 25%
			expectedStageProgress: 25,
			expectedCompleted:     []string{},
			expectedStageIndex:    1,
		},
		{
			name:                  "validation stage - checking target",
			stage:                 "validation",
			state:                 "checking_target",
			existing:              nil,
			expectedProgress:      7, // 10% * 75%
			expectedStageProgress: 75,
			expectedCompleted:     []string{},
			expectedStageIndex:    1,
		},
		{
			name:                  "setup stage - creating source",
			stage:                 "setup",
			state:                 "creating_source",
			existing:              nil,
			expectedProgress:      15, // 10% (validation complete) + 10% * 50%
			expectedStageProgress: 50,
			expectedCompleted:     []string{"validation"},
			expectedStageIndex:    2,
		},
		{
			name:                  "archive stage - generating",
			stage:                 "archive",
			state:                 "generating",
			existing:              nil,
			expectedProgress:      22, // 10% + 10% + 25% * 10%
			expectedStageProgress: 10,
			expectedCompleted:     []string{"validation", "setup"},
			expectedStageIndex:    3,
		},
		{
			name:                  "archive stage - exported",
			stage:                 "archive",
			state:                 "exported",
			existing:              nil,
			expectedProgress:      40, // 10% + 10% + 25% * 80%
			expectedStageProgress: 80,
			expectedCompleted:     []string{"validation", "setup"},
			expectedStageIndex:    3,
		},
		{
			name:                  "storage stage - uploading",
			stage:                 "storage",
			state:                 "uploading",
			existing:              nil,
			expectedProgress:      52, // 10% + 10% + 25% + 15% * 50%
			expectedStageProgress: 50,
			expectedCompleted:     []string{"validation", "setup", "archive"},
			expectedStageIndex:    4,
		},
		{
			name:                  "migration stage - in progress",
			stage:                 "migration",
			state:                 "IN_PROGRESS",
			existing:              nil,
			expectedProgress:      88, // 10% + 10% + 25% + 15% + 40% * 70%
			expectedStageProgress: 70,
			expectedCompleted:     []string{"validation", "setup", "archive", "storage"},
			expectedStageIndex:    5,
		},
		{
			name:                  "migration stage - completed",
			stage:                 "migration",
			state:                 "completed",
			existing:              nil,
			expectedProgress:      100,
			expectedStageProgress: 100,
			expectedCompleted:     []string{"validation", "setup", "archive", "storage", "migration"},
			expectedStageIndex:    5,
		},
		{
			name:  "error stage with existing progress",
			stage: "error",
			state: "failed",
			existing: &payload.MigrationStatus{
				Progress:          45,
				StageProgress:     75,
				CompletedStages:   []string{"validation", "setup"},
				CurrentStageIndex: 3,
			},
			expectedProgress:      45,
			expectedStageProgress: 0,
			expectedCompleted:     []string{"validation", "setup"},
			expectedStageIndex:    3,
		},
		{
			name:  "continuing migration with existing progress",
			stage: "migration",
			state: "PENDING",
			existing: &payload.MigrationStatus{
				Progress:          50,
				StageProgress:     30,
				CompletedStages:   []string{"validation", "setup"},
				CurrentStageIndex: 3,
			},
			expectedProgress:      80, // 10% + 10% + 25% + 15% + 40% * 50%
			expectedStageProgress: 50,
			expectedCompleted:     []string{"validation", "setup", "archive", "storage"},
			expectedStageIndex:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateProgressData(tt.stage, tt.state, tt.existing)

			assert.Equal(t, tt.expectedProgress, result.progress, "Progress mismatch")
			assert.Equal(t, tt.expectedStageProgress, result.stageProgress, "Stage progress mismatch")
			assert.Equal(t, tt.expectedCompleted, result.completedStages, "Completed stages mismatch")
			assert.Equal(t, tt.expectedStageIndex, result.currentStageIndex, "Stage index mismatch")
		})
	}
}

func TestCalculateStageProgress(t *testing.T) {
	tests := []struct {
		name     string
		stage    string
		state    string
		expected int
	}{
		// Validation stage tests
		{
			name:     "validation - checking source",
			stage:    "validation",
			state:    "checking_source",
			expected: 25,
		},
		{
			name:     "validation - checking target",
			stage:    "validation",
			state:    "checking_target",
			expected: 75,
		},
		{
			name:     "validation - unknown state",
			stage:    "validation",
			state:    "unknown",
			expected: 50,
		},

		// Setup stage tests
		{
			name:     "setup - creating source",
			stage:    "setup",
			state:    "creating_source",
			expected: 50,
		},
		{
			name:     "setup - unknown state",
			stage:    "setup",
			state:    "unknown",
			expected: 25,
		},

		// Archive stage tests
		{
			name:     "archive - generating",
			stage:    "archive",
			state:    "generating",
			expected: 10,
		},
		{
			name:     "archive - waiting",
			stage:    "archive",
			state:    "waiting",
			expected: 30,
		},
		{
			name:     "archive - exporting",
			stage:    "archive",
			state:    "exporting",
			expected: 50,
		},
		{
			name:     "archive - exported",
			stage:    "archive",
			state:    "exported",
			expected: 80,
		},
		{
			name:     "archive - ready",
			stage:    "archive",
			state:    "ready",
			expected: 100,
		},
		{
			name:     "archive - pending",
			stage:    "archive",
			state:    "pending",
			expected: 40,
		},

		// Storage stage tests
		{
			name:     "storage - uploading",
			stage:    "storage",
			state:    "uploading",
			expected: 50,
		},
		{
			name:     "storage - completed",
			stage:    "storage",
			state:    "completed",
			expected: 100,
		},
		{
			name:     "storage - unknown state",
			stage:    "storage",
			state:    "unknown",
			expected: 25,
		},

		// Migration stage tests
		{
			name:     "migration - starting",
			stage:    "migration",
			state:    "starting",
			expected: 10,
		},
		{
			name:     "migration - created",
			stage:    "migration",
			state:    "created",
			expected: 20,
		},
		{
			name:     "migration - waiting",
			stage:    "migration",
			state:    "waiting",
			expected: 30,
		},
		{
			name:     "migration - QUEUED",
			stage:    "migration",
			state:    "QUEUED",
			expected: 40,
		},
		{
			name:     "migration - PENDING",
			stage:    "migration",
			state:    "PENDING",
			expected: 50,
		},
		{
			name:     "migration - IN_PROGRESS",
			stage:    "migration",
			state:    "IN_PROGRESS",
			expected: 70,
		},
		{
			name:     "migration - SUCCEEDED",
			stage:    "migration",
			state:    "SUCCEEDED",
			expected: 100,
		},
		{
			name:     "migration - completed",
			stage:    "migration",
			state:    "completed",
			expected: 100,
		},
		{
			name:     "migration - unknown state",
			stage:    "migration",
			state:    "unknown",
			expected: 50,
		},

		// Unknown stage test
		{
			name:     "unknown stage",
			stage:    "unknown",
			state:    "any",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateStageProgress(tt.stage, tt.state)
			assert.Equal(t, tt.expected, result, "Stage progress calculation mismatch")
		})
	}
}

func TestProgressDataStruct(t *testing.T) {
	// Test that the progressData struct is properly initialized
	data := progressData{
		progress:          75,
		stageProgress:     50,
		completedStages:   []string{"validation", "setup"},
		currentStageIndex: 3,
	}

	assert.Equal(t, 75, data.progress)
	assert.Equal(t, 50, data.stageProgress)
	assert.Equal(t, []string{"validation", "setup"}, data.completedStages)
	assert.Equal(t, 3, data.currentStageIndex)
}

func TestCalculateProgressDataEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		stage    string
		state    string
		existing *payload.MigrationStatus
		expected progressData
	}{
		{
			name:  "empty stage and state",
			stage: "",
			state: "",
			expected: progressData{
				progress:          0,
				stageProgress:     0,
				completedStages:   []string{},
				currentStageIndex: 0,
			},
		},
		{
			name:  "unknown stage",
			stage: "unknown_stage",
			state: "unknown_state",
			expected: progressData{
				progress:          0,
				stageProgress:     0,
				completedStages:   []string{},
				currentStageIndex: 0,
			},
		},
		{
			name:  "error stage with nil existing",
			stage: "error",
			state: "failed",
			expected: progressData{
				progress:          0,
				stageProgress:     0,
				completedStages:   []string{},
				currentStageIndex: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateProgressData(tt.stage, tt.state, tt.existing)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateProgressDataStageProgression(t *testing.T) {
	// Test that stages are properly added to completed when progressing
	tests := []struct {
		name             string
		stage            string
		state            string
		expectedComplete []string
	}{
		{
			name:             "first stage",
			stage:            "validation",
			state:            "checking_source",
			expectedComplete: []string{},
		},
		{
			name:             "second stage",
			stage:            "setup",
			state:            "creating_source",
			expectedComplete: []string{"validation"},
		},
		{
			name:             "third stage",
			stage:            "archive",
			state:            "generating",
			expectedComplete: []string{"validation", "setup"},
		},
		{
			name:             "fourth stage",
			stage:            "storage",
			state:            "uploading",
			expectedComplete: []string{"validation", "setup", "archive"},
		},
		{
			name:             "final stage",
			stage:            "migration",
			state:            "starting",
			expectedComplete: []string{"validation", "setup", "archive", "storage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateProgressData(tt.stage, tt.state, nil)
			assert.Equal(t, tt.expectedComplete, result.completedStages)
		})
	}
}

func TestCalculateProgressDataPreservesExistingCompleted(t *testing.T) {
	// Test that existing completed stages are preserved
	existing := &payload.MigrationStatus{
		CompletedStages: []string{"validation", "setup"},
		Progress:        25,
	}

	result := calculateProgressData("archive", "generating", existing)

	// Should have original completed stages plus any new ones based on current stage
	assert.Contains(t, result.completedStages, "validation")
	assert.Contains(t, result.completedStages, "setup")
	assert.Equal(t, []string{"validation", "setup"}, result.completedStages)
}

func TestCalculateProgressDataWeightedProgress(t *testing.T) {
	// Test that the weighted progress calculation is correct
	// Stage weights: validation(10), setup(10), archive(25), storage(15), migration(40)

	// Test middle of archive stage (50% through)
	result := calculateProgressData("archive", "exporting", nil)
	// expectedProgress := 10 + 10 + (25 * 50 / 100) // 20 + 12.5 = 32.5 -> 32
	assert.Equal(t, 32, result.progress)

	// Test beginning of migration stage
	result = calculateProgressData("migration", "starting", nil)
	// expectedProgress = 10 + 10 + 25 + 15 + (40 * 10 / 100) // 60 + 4 = 64
	assert.Equal(t, 64, result.progress)
}

func TestCalculateProgressDataMigrationCompleted(t *testing.T) {
	// Test special case when migration is completed
	result := calculateProgressData("migration", "completed", nil)

	assert.Equal(t, 100, result.progress)
	assert.Equal(t, 100, result.stageProgress)
	assert.Contains(t, result.completedStages, "migration")
	assert.Equal(t, 5, result.currentStageIndex)

	// Test SUCCEEDED state too
	result = calculateProgressData("migration", "SUCCEEDED", nil)
	assert.Equal(t, 100, result.stageProgress)
}
