// Package metrics provides metrics collection for the application.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Queue metrics
var (
	// QueueSize tracks the current size of the migration queue
	QueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "migration_queue_size",
		Help: "The current number of jobs in the migration queue",
	})

	// QueuedJobs counts the total number of jobs that have been queued
	QueuedJobs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "migration_queued_jobs_total",
		Help: "The total number of jobs that have been queued",
	})

	// ActiveArchives tracks the number of currently active archive generation jobs
	ActiveArchives = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "migration_active_archives",
		Help: "The current number of active archive generation jobs",
	})

	// ActiveMigrations tracks the number of currently active migration jobs
	ActiveMigrations = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "migration_active_migrations",
		Help: "The current number of active migration jobs",
	})

	// CompletedArchives counts the total number of completed archive generation jobs
	CompletedArchives = promauto.NewCounter(prometheus.CounterOpts{
		Name: "migration_completed_archives_total",
		Help: "The total number of archive generation jobs that have been completed",
	})

	// CompletedMigrations counts the total number of completed migration jobs
	CompletedMigrations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "migration_completed_migrations_total",
		Help: "The total number of migration jobs that have been completed",
	})
)
