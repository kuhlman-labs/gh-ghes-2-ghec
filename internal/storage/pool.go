// Package storage provides data persistence for migration state information.
package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/metrics"
)

// PoolConfig defines configuration options for database connection pooling
type PoolConfig struct {
	// MaxOpenConns is the maximum number of open connections to the database
	MaxOpenConns int
	// MaxIdleConns is the maximum number of idle connections in the pool
	MaxIdleConns int
	// ConnMaxLifetime is the maximum amount of time a connection may be reused
	ConnMaxLifetime time.Duration
	// ConnMaxIdleTime is the maximum amount of time a connection may be idle
	ConnMaxIdleTime time.Duration
}

// DefaultPoolConfig returns a default database pool configuration
func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 15 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// GetSQLitePoolConfig returns optimized pool settings for SQLite
func GetSQLitePoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    1, // SQLite supports only one writer at a time
		MaxIdleConns:    1,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 1 * time.Minute,
	}
}

// GetMySQLPoolConfig returns optimized pool settings for MySQL
func GetMySQLPoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 15 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// GetPostgresPoolConfig returns optimized pool settings for PostgreSQL
func GetPostgresPoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 15 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// ConfigureConnectionPool configures a database connection pool with the provided settings
func ConfigureConnectionPool(db *sql.DB, config *PoolConfig) {
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)
}

// ConnectionStats represents the statistics of a database connection pool
type ConnectionStats struct {
	OpenConnections   int           `json:"open_connections"`
	InUseConnections  int           `json:"in_use_connections"`
	IdleConnections   int           `json:"idle_connections"`
	WaitCount         int64         `json:"wait_count"`
	WaitDuration      time.Duration `json:"wait_duration"`
	MaxIdleClosed     int64         `json:"max_idle_closed"`
	MaxLifetimeClosed int64         `json:"max_lifetime_closed"`
}

// GetConnectionStats retrieves current connection pool statistics
func GetConnectionStats(db *sql.DB) ConnectionStats {
	stats := db.Stats()
	return ConnectionStats{
		OpenConnections:   stats.OpenConnections,
		InUseConnections:  stats.InUse,
		IdleConnections:   stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration,
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
	}
}

// StartPoolMetricsCollector starts a goroutine that periodically collects and reports database connection metrics
func StartPoolMetricsCollector(ctx context.Context, db *sql.DB, databaseType string, interval time.Duration) {
	logger := logging.Get()
	logger.Info("Starting database connection pool metrics collector",
		"database_type", databaseType,
		"interval", interval,
	)

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Debug("Stopping database pool metrics collector")
				return
			case <-ticker.C:
				stats := GetConnectionStats(db)

				// Expose metrics for the connection pool
				metrics.SetDatabaseConnections(databaseType, "open", float64(stats.OpenConnections))
				metrics.SetDatabaseConnections(databaseType, "in_use", float64(stats.InUseConnections))
				metrics.SetDatabaseConnections(databaseType, "idle", float64(stats.IdleConnections))
				metrics.SetDatabaseWaitCount(databaseType, stats.WaitCount)
				metrics.SetDatabaseWaitDuration(databaseType, stats.WaitDuration.Seconds())

				// Log stats if there are waits or connection churn
				if stats.WaitCount > 0 || stats.MaxIdleClosed > 0 || stats.MaxLifetimeClosed > 0 {
					logger.Debug("Database connection pool statistics",
						"database_type", databaseType,
						"open", stats.OpenConnections,
						"in_use", stats.InUseConnections,
						"idle", stats.IdleConnections,
						"wait_count", stats.WaitCount,
						"wait_duration", stats.WaitDuration,
						"max_idle_closed", stats.MaxIdleClosed,
						"max_lifetime_closed", stats.MaxLifetimeClosed,
					)
				}
			}
		}
	}()
}
