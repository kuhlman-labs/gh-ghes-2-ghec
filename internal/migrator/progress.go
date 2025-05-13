// Package migrator provides functionality for migrating repositories from
// GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).
package migrator

import (
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// progressData holds calculated progress information
type progressData struct {
	progress          int
	stageProgress     int
	completedStages   []string
	currentStageIndex int
}

// calculateProgressData calculates progress information based on stage and state
func calculateProgressData(stage, state string, existing *payload.MigrationStatus) progressData {
	// Define weights for each stage (percentages)
	stageWeights := map[string]int{
		"validation": 10,
		"setup":      10,
		"archive":    25,
		"storage":    15,
		"migration":  40,
	}

	// Initialize result
	result := progressData{
		progress:          0,
		stageProgress:     0,
		completedStages:   []string{},
		currentStageIndex: 0,
	}

	// If it's a new migration, just set the initial progress
	if existing == nil {
		if stage == "init" {
			return result
		}
	} else {
		// Copy existing completed stages
		result.completedStages = append(result.completedStages, existing.CompletedStages...)
	}

	// Find current stage index
	currentStageIndex := -1
	for i, s := range payload.MigrationStages {
		if s == stage {
			currentStageIndex = i
			break
		}
	}

	// If stage not found in the progression (like "init" or "error")
	if currentStageIndex == -1 {
		if stage == "error" {
			// Error state - keep existing progress if available
			if existing != nil {
				return progressData{
					progress:          existing.Progress,
					stageProgress:     0,
					completedStages:   existing.CompletedStages,
					currentStageIndex: existing.CurrentStageIndex,
				}
			}
			return result
		}
		// Init stage - set to 0
		return result
	}

	// Set current stage index (1-based for better UX)
	result.currentStageIndex = currentStageIndex + 1

	// Calculate total progress from completed stages
	cumulativeProgress := 0

	// Mark previous stages as completed
	for i, s := range payload.MigrationStages {
		if i < currentStageIndex {
			// Add to completed stages if not already included
			found := false
			for _, cs := range result.completedStages {
				if cs == s {
					found = true
					break
				}
			}
			if !found {
				result.completedStages = append(result.completedStages, s)
			}

			// Add weight to cumulative progress
			cumulativeProgress += stageWeights[s]
		}
	}

	// Calculate stage progress based on the state
	stageProgress := calculateStageProgress(stage, state)
	result.stageProgress = stageProgress

	// Add weighted stage progress to total
	currentStageWeight := stageWeights[stage]
	stageContribution := (currentStageWeight * stageProgress) / 100

	// Calculate total progress
	result.progress = cumulativeProgress + stageContribution

	// Special cases
	if stage == "migration" && state == "completed" {
		result.progress = 100
		result.stageProgress = 100

		// Add final stage to completed stages if not there
		found := false
		for _, s := range result.completedStages {
			if s == stage {
				found = true
				break
			}
		}
		if !found {
			result.completedStages = append(result.completedStages, stage)
		}
	}

	return result
}

// calculateStageProgress estimates progress within a stage based on the state
func calculateStageProgress(stage, state string) int {
	switch stage {
	case "validation":
		switch state {
		case "checking_source":
			return 25
		case "checking_target":
			return 75
		default:
			return 50
		}
	case "setup":
		switch state {
		case "creating_source":
			return 50
		default:
			return 25
		}
	case "archive":
		switch state {
		case "generating":
			return 10
		case "waiting":
			return 30
		case "exporting":
			return 50
		case "exported":
			return 80
		case "ready":
			return 100
		default:
			// For archive export states like "pending"
			return 40
		}
	case "storage":
		switch state {
		case "uploading":
			return 50
		case "completed":
			return 100
		default:
			return 25
		}
	case "migration":
		switch state {
		case "starting":
			return 10
		case "created":
			return 20
		case "waiting":
			return 30
		case "QUEUED":
			return 40
		case "PENDING":
			return 50
		case "IN_PROGRESS":
			return 70
		case "SUCCEEDED":
			return 100
		case "completed":
			return 100
		default:
			return 50
		}
	default:
		return 0
	}
}
