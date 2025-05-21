// Package scheduler provides functionality for scheduling repository migrations
// based on time and calendar restrictions.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/migrator"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// Scheduler manages scheduled migrations.
// It periodically checks for scheduled migrations and executes them when their time arrives.
type Scheduler struct {
	migrator    *migrator.Migrator                   // Migrator to execute migrations
	logger      *slog.Logger                         // Logger for the scheduler
	ticker      *time.Ticker                         // Ticker for periodic checks
	done        chan struct{}                        // Channel to signal shutdown
	wg          sync.WaitGroup                       // WaitGroup to track worker goroutines
	mu          sync.RWMutex                         // Mutex to protect schedules map
	schedules   map[string]*payload.MigrationRequest // Map of scheduled migrations keyed by sourceRepoFullName
	cancelFuncs map[string]context.CancelFunc        // Map of cancel functions for migrations in progress
}

// New creates a new Scheduler instance.
func New(migrator *migrator.Migrator, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Scheduler{
		migrator:    migrator,
		logger:      logger,
		done:        make(chan struct{}),
		schedules:   make(map[string]*payload.MigrationRequest),
		cancelFuncs: make(map[string]context.CancelFunc),
	}
}

// Start starts the scheduler.
// The checkInterval parameter specifies how often to check for migrations to run.
func (s *Scheduler) Start(checkInterval time.Duration) {
	s.logger.Info("Starting migration scheduler", "check_interval", checkInterval)
	s.ticker = time.NewTicker(checkInterval)
	s.wg.Add(1)
	go s.run()
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	s.logger.Info("Stopping migration scheduler")
	close(s.done)
	if s.ticker != nil {
		s.ticker.Stop()
	}

	// Cancel any in-progress migrations
	s.mu.Lock()
	for repoName, cancel := range s.cancelFuncs {
		s.logger.Info("Cancelling scheduled migration", "repository", repoName)
		cancel()
	}
	s.mu.Unlock()

	s.wg.Wait()
	s.logger.Info("Migration scheduler stopped")
}

// ScheduleMigration schedules a migration to be executed at the specified time.
func (s *Scheduler) ScheduleMigration(req *payload.MigrationRequest) error {
	if req == nil {
		return nil // Nothing to schedule
	}

	if req.ScheduledTime == nil {
		return nil // No scheduled time specified
	}

	// If a timezone is specified, re-interpret the timestamp in that timezone
	if req.ScheduledTimeZone != "" {
		loc, err := time.LoadLocation(req.ScheduledTimeZone)
		if err == nil {
			// Extract the date/time components from the scheduled time
			year, month, day := req.ScheduledTime.Date()
			hour, min, sec := req.ScheduledTime.Clock()

			// Create a new time in the specified timezone
			correctedTime := time.Date(year, month, day, hour, min, sec, 0, loc)
			req.ScheduledTime = &correctedTime

			s.logger.Info("Reinterpreted scheduled time in specified timezone",
				"original_time", req.ScheduledTime.Format(time.RFC3339),
				"corrected_time", correctedTime.Format(time.RFC3339),
				"timezone", req.ScheduledTimeZone)
		} else {
			s.logger.Warn("Invalid timezone specified, using UTC for scheduling",
				"timezone", req.ScheduledTimeZone,
				"error", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Store each repository as a separate scheduled migration
	for _, repoName := range req.Repositories {
		sourceRepoFullName := req.SourceOrg + "/" + repoName

		// Create a separate request for this repository
		repoReq := &payload.MigrationRequest{
			SourceOrg:          req.SourceOrg,
			TargetOrg:          req.TargetOrg,
			Repositories:       []string{repoName},
			GHESBaseURL:        req.GHESBaseURL,
			GHESToken:          req.GHESToken,
			GHCloudToken:       req.GHCloudToken,
			MaxDuration:        req.MaxDuration,
			UseGHOS:            req.UseGHOS,
			DeleteIfExists:     req.DeleteIfExists,
			ScheduledTime:      req.ScheduledTime,
			ScheduledTimeZone:  req.ScheduledTimeZone,
			ScheduledDaysOnly:  req.ScheduledDaysOnly,
			ScheduledTimeStart: req.ScheduledTimeStart,
			ScheduledTimeEnd:   req.ScheduledTimeEnd,
		}

		s.schedules[sourceRepoFullName] = repoReq
		s.logger.Info("Migration scheduled",
			"repository", sourceRepoFullName,
			"scheduled_time", req.ScheduledTime.Format(time.RFC3339),
			"time_zone", req.ScheduledTimeZone)

		// Update the migration status to show it's scheduled
		// We need to create a minimal status object with just the repository name
		// TODO: Connect this to migrator's status tracking when implemented
		// status := &payload.MigrationStatus{
		//	Repository:    sourceRepoFullName,
		//	Status:        payload.StatusScheduled,
		//	UpdatedAt:     time.Now(),
		//	ScheduledTime: req.ScheduledTime,
		// }
	}

	return nil
}

// run is the main scheduler loop that periodically checks for migrations to execute.
func (s *Scheduler) run() {
	defer s.wg.Done()

	for {
		select {
		case <-s.done:
			return
		case <-s.ticker.C:
			s.checkSchedules()
		}
	}
}

// checkSchedules checks all scheduled migrations to see if any should be executed.
func (s *Scheduler) checkSchedules() {
	now := time.Now()
	s.logger.Debug("Checking scheduled migrations", "current_time", now.Format(time.RFC3339))

	s.mu.Lock()
	defer s.mu.Unlock()

	for sourceRepoFullName, req := range s.schedules {
		// Skip if we don't have a scheduled time
		if req.ScheduledTime == nil {
			continue
		}

		// Get current time and scheduled time, adjusted for timezone if specified
		currentTime := now
		scheduledTime := *req.ScheduledTime

		// If a timezone is specified, convert both times to that timezone for comparison
		if req.ScheduledTimeZone != "" {
			loc, err := time.LoadLocation(req.ScheduledTimeZone)
			if err == nil {
				// Convert the scheduled time to the specified timezone
				scheduledTime = scheduledTime.In(loc)
				// Get the current time in the specified timezone
				currentTime = now.In(loc)

				s.logger.Debug("Adjusted times for timezone comparison",
					"repository", sourceRepoFullName,
					"timezone", req.ScheduledTimeZone,
					"scheduled_time_in_tz", scheduledTime.Format(time.RFC3339),
					"current_time_in_tz", currentTime.Format(time.RFC3339))
			} else {
				s.logger.Warn("Invalid timezone specified, using UTC for comparison",
					"repository", sourceRepoFullName,
					"timezone", req.ScheduledTimeZone,
					"error", err)
			}
		}

		// Check if the scheduled time has passed
		if currentTime.After(scheduledTime) {
			// Additional calendar-based checks
			if !s.isWithinTimeWindow(currentTime, req) || !s.isAllowedDayOfWeek(currentTime, req) {
				s.logger.Debug("Migration not executed due to calendar restrictions",
					"repository", sourceRepoFullName,
					"current_time", currentTime.Format(time.RFC3339),
					"scheduled_time", scheduledTime.Format(time.RFC3339))
				continue
			}

			// Execute the migration
			s.logger.Info("Executing scheduled migration",
				"repository", sourceRepoFullName,
				"scheduled_time", scheduledTime.Format(time.RFC3339),
				"current_time", currentTime.Format(time.RFC3339))

			// Start the migration
			ctx, cancel := context.WithCancel(context.Background())
			s.cancelFuncs[sourceRepoFullName] = cancel

			// Unscheduling this migration (remove from schedules)
			delete(s.schedules, sourceRepoFullName)

			// Execute the migration in a goroutine
			go func(r *payload.MigrationRequest, repoFullName string) {
				defer func() {
					s.mu.Lock()
					delete(s.cancelFuncs, repoFullName)
					s.mu.Unlock()
				}()

				err := s.migrator.StartMigration(ctx, r, cancel)
				if err != nil {
					s.logger.Error("Failed to start scheduled migration",
						"repository", repoFullName,
						"error", err)
				}
			}(req, sourceRepoFullName)
		} else {
			// Log that the scheduled time has not yet arrived
			timeUntil := scheduledTime.Sub(currentTime).String()
			s.logger.Debug("Scheduled migration not yet due",
				"repository", sourceRepoFullName,
				"scheduled_time", scheduledTime.Format(time.RFC3339),
				"current_time", currentTime.Format(time.RFC3339),
				"time_until", timeUntil)
		}
	}
}

// isWithinTimeWindow checks if the current time is within the allowed time window.
func (s *Scheduler) isWithinTimeWindow(now time.Time, req *payload.MigrationRequest) bool {
	// If no time window is specified, allow execution at any time
	if req.ScheduledTimeStart == "" || req.ScheduledTimeEnd == "" {
		return true
	}

	// Parse the time window
	timeStart, err := time.Parse("15:04", req.ScheduledTimeStart)
	if err != nil {
		s.logger.Error("Failed to parse time window start",
			"time_start", req.ScheduledTimeStart,
			"error", err)
		return false
	}

	timeEnd, err := time.Parse("15:04", req.ScheduledTimeEnd)
	if err != nil {
		s.logger.Error("Failed to parse time window end",
			"time_end", req.ScheduledTimeEnd,
			"error", err)
		return false
	}

	// Extract hours and minutes from the current time
	currentHour, currentMinute := now.Hour(), now.Minute()
	// Create time objects with just hours and minutes for comparison
	currentTime := time.Date(0, 1, 1, currentHour, currentMinute, 0, 0, time.UTC)

	// Use the same base date for all time objects to ensure proper comparison
	startTime := time.Date(0, 1, 1, timeStart.Hour(), timeStart.Minute(), 0, 0, time.UTC)
	endTime := time.Date(0, 1, 1, timeEnd.Hour(), timeEnd.Minute(), 0, 0, time.UTC)

	// Handle time windows that span across midnight
	if endTime.Before(startTime) {
		// If end time is before start time, it means the window spans across midnight
		// e.g., 22:00 - 06:00 means from 22:00 today to 06:00 tomorrow
		return currentTime.After(startTime) || currentTime.Before(endTime)
	}

	// Regular time window within the same day
	return currentTime.After(startTime) && currentTime.Before(endTime)
}

// isAllowedDayOfWeek checks if the current day is allowed for execution.
func (s *Scheduler) isAllowedDayOfWeek(now time.Time, req *payload.MigrationRequest) bool {
	// If no day restrictions are specified, allow execution on any day
	if len(req.ScheduledDaysOnly) == 0 {
		return true
	}

	// Get current day of week
	currentDay := now.Weekday().String()

	// Check if the current day is in the allowed days
	for _, day := range req.ScheduledDaysOnly {
		if day == currentDay {
			return true
		}
	}

	return false
}

// GetSchedules returns the current scheduled migrations.
func (s *Scheduler) GetSchedules() map[string]*payload.MigrationRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a deep copy to avoid race conditions
	schedulesCopy := make(map[string]*payload.MigrationRequest, len(s.schedules))
	for k, v := range s.schedules {
		// Create a copy of the request
		reqCopy := *v
		schedulesCopy[k] = &reqCopy
	}

	return schedulesCopy
}

// CancelScheduledMigration cancels a scheduled migration.
func (s *Scheduler) CancelScheduledMigration(sourceRepoFullName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.schedules[sourceRepoFullName]
	if exists {
		delete(s.schedules, sourceRepoFullName)
		s.logger.Info("Scheduled migration cancelled", "repository", sourceRepoFullName)
		return true
	}

	return false
}
