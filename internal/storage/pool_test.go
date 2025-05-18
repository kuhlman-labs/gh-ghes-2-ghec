package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestConnectionPooling(t *testing.T) {
	// Create a temporary directory for the test database
	tempDir, err := os.MkdirTemp("", "pooltest")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: failed to remove temp directory: %v", err)
		}
	}()

	dbPath := filepath.Join(tempDir, "test.db")

	// Open a test database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Warning: failed to close database: %v", err)
		}
	}()

	// Test SQLite pool configuration
	t.Run("SQLitePoolConfig", func(t *testing.T) {
		cfg := GetSQLitePoolConfig()
		if cfg.MaxOpenConns != 1 {
			t.Errorf("Expected MaxOpenConns to be 1 for SQLite, got %d", cfg.MaxOpenConns)
		}
		if cfg.MaxIdleConns != 1 {
			t.Errorf("Expected MaxIdleConns to be 1 for SQLite, got %d", cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetime != 5*time.Minute {
			t.Errorf("Expected ConnMaxLifetime to be 5 minutes for SQLite, got %v", cfg.ConnMaxLifetime)
		}
	})

	// Test default pool configuration
	t.Run("DefaultPoolConfig", func(t *testing.T) {
		cfg := DefaultPoolConfig()
		if cfg.MaxOpenConns <= 0 {
			t.Errorf("Expected MaxOpenConns to be positive, got %d", cfg.MaxOpenConns)
		}
		if cfg.MaxIdleConns <= 0 {
			t.Errorf("Expected MaxIdleConns to be positive, got %d", cfg.MaxIdleConns)
		}
		if cfg.ConnMaxLifetime <= 0 {
			t.Errorf("Expected ConnMaxLifetime to be positive, got %v", cfg.ConnMaxLifetime)
		}
	})

	// Test applying configuration to pool
	t.Run("ConfigureConnectionPool", func(t *testing.T) {
		cfg := &PoolConfig{
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: 10 * time.Minute,
			ConnMaxIdleTime: 5 * time.Minute,
		}

		ConfigureConnectionPool(db, cfg)

		stats := db.Stats()
		if stats.MaxOpenConnections != 10 {
			t.Errorf("Expected MaxOpenConnections to be 10, got %d", stats.MaxOpenConnections)
		}
	})

	// Test connection pool stats
	t.Run("GetConnectionStats", func(t *testing.T) {
		stats := GetConnectionStats(db)
		if stats.OpenConnections < 0 {
			t.Errorf("Expected OpenConnections to be non-negative, got %d", stats.OpenConnections)
		}
	})

	// Test pool metrics collector with context cancellation
	t.Run("StartPoolMetricsCollector", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Start the collector with a very short interval for testing
		StartPoolMetricsCollector(ctx, db, "sqlite_test", 10*time.Millisecond)

		// Allow one collection cycle
		time.Sleep(20 * time.Millisecond)

		// Cancel the context to stop the collector
		cancel()

		// Wait for goroutine to exit
		time.Sleep(20 * time.Millisecond)

		// If we get here without deadlocks or panics, the test passes
	})
}
