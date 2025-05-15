package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// HealthStatus represents the health status of the storage
type HealthStatus struct {
	Healthy      bool          `json:"healthy"`
	Message      string        `json:"message"`
	LastChecked  time.Time     `json:"last_checked"`
	ResponseTime time.Duration `json:"response_time"`
	Errors       []string      `json:"errors,omitempty"`
}

// HealthChecker is an interface for checking storage health
type HealthChecker interface {
	// CheckHealth returns the health status of the storage
	CheckHealth(ctx context.Context) *HealthStatus

	// GetLastHealthStatus returns the most recent health status
	GetLastHealthStatus() *HealthStatus
}

// DatabaseHealthChecker implements health checking for database storages
type DatabaseHealthChecker struct {
	storage         MigrationStorage
	healthCheckLock sync.Mutex
	lastStatus      *HealthStatus
	checkInterval   time.Duration
}

// NewDatabaseHealthChecker creates a new health checker for the given storage
func NewDatabaseHealthChecker(storage MigrationStorage) *DatabaseHealthChecker {
	return &DatabaseHealthChecker{
		storage:       storage,
		checkInterval: 5 * time.Minute,
		lastStatus: &HealthStatus{
			Healthy:     true,
			Message:     "No health check performed yet",
			LastChecked: time.Now(),
		},
	}
}

// StartPeriodicHealthCheck begins periodic health checking in the background
func (hc *DatabaseHealthChecker) StartPeriodicHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(hc.checkInterval)
	go func() {
		// Perform an initial health check
		hc.CheckHealth(ctx)

		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				hc.CheckHealth(ctx)
			}
		}
	}()
}

// CheckHealth verifies that the storage is working correctly
func (hc *DatabaseHealthChecker) CheckHealth(ctx context.Context) *HealthStatus {
	logger := logging.Get()
	logger.Info("Performing database health check")

	hc.healthCheckLock.Lock()
	defer hc.healthCheckLock.Unlock()

	status := &HealthStatus{
		Healthy:     true,
		LastChecked: time.Now(),
		Errors:      []string{},
	}

	// Create a timeout context for the check
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	start := time.Now()

	// Test writing, reading and retrieving all migrations
	testRepo := fmt.Sprintf("health-check-%d", time.Now().Unix())

	// Create a test migration status
	testStatus := &payload.MigrationStatus{
		Repository:  testRepo,
		Status:      "health_check",
		UpdatedAt:   time.Now(),
		Progress:    100,
		TotalStages: 1,
	}

	// Try to save it
	if err := hc.storage.SaveMigrationStatus(checkCtx, testStatus); err != nil {
		status.Healthy = false
		status.Errors = append(status.Errors, fmt.Sprintf("Write test failed: %v", err))
	}

	// Try to read it back
	if status.Healthy {
		_, err := hc.storage.GetMigrationStatus(checkCtx, testRepo)
		if err != nil {
			status.Healthy = false
			status.Errors = append(status.Errors, fmt.Sprintf("Read test failed: %v", err))
		}
	}

	// Try reading all migrations
	if status.Healthy {
		_, err := hc.storage.GetAllMigrationStatuses(checkCtx)
		if err != nil {
			status.Healthy = false
			status.Errors = append(status.Errors, fmt.Sprintf("GetAll test failed: %v", err))
		}
	}

	// Clean up the test entry
	if status.Healthy {
		if err := hc.storage.DeleteMigrationStatus(checkCtx, testRepo); err != nil {
			status.Healthy = false
			status.Errors = append(status.Errors, fmt.Sprintf("Delete test failed: %v", err))
		}
	}

	// Calculate response time
	status.ResponseTime = time.Since(start)

	// Set appropriate message
	if status.Healthy {
		status.Message = fmt.Sprintf("Database is healthy (response time: %v)", status.ResponseTime)
	} else {
		status.Message = fmt.Sprintf("Database health check failed: %v", status.Errors)
		logger.Error("Database health check failed",
			"errors", status.Errors,
			"response_time", status.ResponseTime,
		)
	}

	// Update the last status
	hc.lastStatus = status

	return status
}

// GetLastHealthStatus returns the most recent health status
func (hc *DatabaseHealthChecker) GetLastHealthStatus() *HealthStatus {
	hc.healthCheckLock.Lock()
	defer hc.healthCheckLock.Unlock()

	return hc.lastStatus
}
