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
func calculateProgressData(stage, state string, existing *payload.MigrationStatus, req *payload.MigrationRequest) progressData {
	// Determine if GHOS is enabled from existing status or migration request
	useGHOS := true // Default to true for backward compatibility
	if existing != nil {
		useGHOS = existing.UseGHOS
	} else if req != nil {
		useGHOS = req.UseGHOS
	}

	// Define weights for each stage (percentages) - dynamic based on GHOS usage
	var stageWeights map[string]int
	if useGHOS {
		// GHOS enabled: include storage stage
		stageWeights = map[string]int{
			"validation": 10,
			"setup":      10,
			"archive":    25,
			"storage":    15,
			"migration":  40,
			"queue":      0, // Queue stage doesn't add progress, just preserves current state
		}
	} else {
		// GHOS disabled: redistribute storage weight to other stages
		stageWeights = map[string]int{
			"validation": 10,
			"setup":      10,
			"archive":    30, // +5 from storage
			"storage":    0,  // Not used when GHOS is disabled
			"migration":  50, // +10 from storage
			"queue":      0,  // Queue stage doesn't add progress, just preserves current state
		}
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

		if stage == "queue" {
			// Queue stage - preserve existing progress and determine what to show based on state
			if existing != nil {
				// Determine if GHOS is enabled for queue progress calculation
				queueUseGHOS := existing.UseGHOS

				// Determine progress based on queue state
				switch state {
				case "waiting_archive_worker":
					// After validation but before archive - preserve validation progress
					result.progress = 10 // Validation completed (10%)
					result.stageProgress = 0
					result.currentStageIndex = 2 // Next would be archive

					// Set completed stages to include validation
					result.completedStages = []string{"validation"}
				case "waiting_migration_worker":
					// After archive but before migration - progress depends on GHOS usage
					if queueUseGHOS {
						// GHOS enabled: Validation (10%) + Setup (10%) + Archive (25%) = 45%
						result.progress = 45
						result.completedStages = []string{"validation", "setup", "archive"}
						result.currentStageIndex = 5 // Next would be migration (after storage)
					} else {
						// GHOS disabled: Validation (10%) + Setup (10%) + Archive (30%) = 50%
						result.progress = 50
						result.completedStages = []string{"validation", "setup", "archive"}
						result.currentStageIndex = 4 // Next would be migration (storage skipped)
					}
					result.stageProgress = 0
				default:
					// Generic queue state - preserve existing progress and completed stages
					result.progress = existing.Progress
					result.stageProgress = 0
					result.currentStageIndex = existing.CurrentStageIndex
					result.completedStages = make([]string, len(existing.CompletedStages))
					copy(result.completedStages, existing.CompletedStages)
				}

				return result
			}
			// No existing status, treat as beginning
			return result
		}

		// Init stage - set to 0
		return result
	}

	// Set current stage index (1-based for better UX)
	result.currentStageIndex = currentStageIndex + 1

	// Adjust stage index for non-GHOS migrations when storage stage is skipped
	if !useGHOS && currentStageIndex >= 3 { // 3 is the index of "storage" in MigrationStages
		// If we're at or past the storage stage in a non-GHOS migration,
		// decrement the stage index since storage is skipped
		result.currentStageIndex = currentStageIndex // Don't add 1 since we're skipping storage
	}

	// Calculate total progress from completed stages
	cumulativeProgress := 0

	// Mark previous stages as completed
	for i, s := range payload.MigrationStages {
		if i < currentStageIndex {
			// Skip storage stage if GHOS is disabled
			if s == "storage" && !useGHOS {
				continue
			}

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
		case "estimating_size":
			return 50
		case "size_estimated":
			return 60
		case "checking_target":
			return 75
		case "target_exists":
			return 85
		case "target_cleaned":
			return 95
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
		case "preparing":
			return 5
		case "generating":
			return 10
		case "waiting":
			return 30
		case "exporting":
			return 50
		case "exported":
			return 80
		case "retrieving_url":
			return 90
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
		case "pre_migration_validation":
			return 15
		case "uploading_to_ghos":
			return 25
		case "ghos_upload_complete":
			return 35
		case "preparing_archive":
			return 20
		case "validating":
			return 18
		case "created":
			return 40
		case "waiting":
			return 50
		case "QUEUED":
			return 60
		case "PENDING":
			return 65
		case "IN_PROGRESS":
			return 80
		case "SUCCEEDED":
			return 100
		case "completed":
			return 100
		default:
			return 50
		}
	case "queue":
		// Queue stage - always 0% stage progress since we're just waiting
		return 0
	default:
		return 0
	}
}
