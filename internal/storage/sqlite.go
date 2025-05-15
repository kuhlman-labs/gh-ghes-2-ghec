// Package storage provides data persistence for migration state information.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// SQLiteStorage implements MigrationStorage using SQLite database.
type SQLiteStorage struct {
	db               *sql.DB
	dbPath           string
	tablePrefix      string
	mu               sync.Mutex
	logger           *slog.Logger
	isInitComplete   bool
	maintenanceTimer *time.Timer
	operationTimeout time.Duration
}

// Constants for database operations
const (
	defaultTimeout      = 60 * time.Second
	maxRetries          = 3
	maintenanceInterval = 30 * time.Minute // Run maintenance every 30 minutes
)

// withRetry executes a database operation with retry logic
func (s *SQLiteStorage) withRetry(ctx context.Context, operation string, fn func(context.Context) error) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		// If the error is a context deadline exceeded, we always log it but don't retry
		// since it's unlikely to succeed with the same timeout
		if err == context.DeadlineExceeded {
			s.logger.Error("Database operation timed out and won't be retried",
				"operation", operation,
				"attempt", i+1,
				"error", err,
			)
			return err
		}

		lastErr = err
		// Add exponential backoff starting with 3 seconds (rather than 1)
		// This gives SQLite more time to recover from locks
		backoff := time.Second * time.Duration(3*(1<<uint(i)))
		s.logger.Warn("Retrying database operation",
			"operation", operation,
			"attempt", i+1,
			"max_attempts", maxRetries,
			"backoff", backoff,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			continue
		}
	}
	return fmt.Errorf("operation %s failed after %d retries: %w", operation, maxRetries, lastErr)
}

// withTransaction executes database operations within a transaction
func (s *SQLiteStorage) withTransaction(ctx context.Context, operation string, fn func(*sql.Tx) error) error {
	// Create a timeout context specifically for the transaction
	txCtx, cancel := context.WithTimeout(ctx, s.operationTimeout)
	defer cancel()

	// Begin transaction
	tx, err := s.db.BeginTx(txCtx, nil)
	if err != nil {
		s.logger.Error("Failed to begin transaction", "operation", operation, "error", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Execute the function within the transaction
	err = fn(tx)

	// Handle the transaction outcome
	if err != nil {
		// Roll back on error
		if rbErr := tx.Rollback(); rbErr != nil {
			s.logger.Error("Failed to rollback transaction",
				"operation", operation,
				"original_error", err,
				"rollback_error", rbErr,
			)
			return fmt.Errorf("failed to rollback transaction after error: %w (original error: %v)", rbErr, err)
		}
		return err
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		s.logger.Error("Failed to commit transaction", "operation", operation, "error", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// NewSQLiteStorage creates a new SQLite storage provider.
// By default, it uses an SQLite database in the current directory.
func NewSQLiteStorage(config *StorageConfig) (MigrationStorage, error) {
	// Default to migrations.db in the current directory if not specified
	dbPath := "migrations.db"

	// If a connection string is provided, use it
	if config.ConnectionString != "" {
		dbPath = config.ConnectionString
	}

	// Set timeout for database operations
	timeout := defaultTimeout
	if config.Timeout > 0 {
		timeout = time.Duration(config.Timeout) * time.Second
	}

	// Ensure the directory exists
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := ensureDir(dir); err != nil {
			return nil, fmt.Errorf("failed to create directory for SQLite database: %w", err)
		}
	}

	return &SQLiteStorage{
		dbPath:           dbPath,
		tablePrefix:      config.TablePrefix,
		logger:           logging.Get(),
		operationTimeout: timeout,
	}, nil
}

// Initialize sets up the SQLite database.
func (s *SQLiteStorage) Initialize(ctx context.Context) error {
	start := time.Now()
	s.logger.Info("Starting SQLite initialization", "dbPath", s.dbPath)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		s.logger.Info("Database already initialized")
		s.isInitComplete = true
		return nil // Already initialized
	}

	var err error
	s.logger.Info("Opening SQLite database", "path", s.dbPath)
	s.db, err = sql.Open("sqlite3", s.dbPath)
	if err != nil {
		s.logger.Error("Failed to open SQLite database", "error", err)
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Verify database connection
	s.logger.Info("Pinging database to verify connection")
	if err := s.db.PingContext(ctx); err != nil {
		s.logger.Error("Failed to ping database", "error", err)
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Set connection pool parameters optimized for SQLite
	s.logger.Info("Setting connection pool parameters")
	s.db.SetMaxOpenConns(1) // SQLite only supports one writer at a time
	s.db.SetMaxIdleConns(1)
	s.db.SetConnMaxLifetime(time.Minute * 5)

	// Set pragmas for better performance and safety
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=30000",      // Wait up to 30 seconds when the database is locked (increased from 10s)
		"PRAGMA cache_size=-8000",        // Use 8MB of memory for cache (increased from 4MB)
		"PRAGMA temp_store=MEMORY",       // Store temporary tables and indices in memory
		"PRAGMA mmap_size=30000000000",   // Use memory-mapped I/O for better performance
		"PRAGMA wal_autocheckpoint=1000", // Checkpoint WAL file after 1000 pages
		"PRAGMA page_size=4096",          // Default page size, explicitly set for clarity
		"PRAGMA locking_mode=EXCLUSIVE",  // Use exclusive locking mode for better performance
	}

	s.logger.Info("Setting SQLite pragmas")
	for _, pragma := range pragmas {
		s.logger.Debug("Executing pragma", "pragma", pragma)
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			s.logger.Error("Failed to set pragma", "pragma", pragma, "error", err)
			return fmt.Errorf("failed to set pragma '%s': %w", pragma, err)
		}
	}

	// Create migration status table
	tableName := s.getTableName("migration_status")
	query := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		repository TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		error TEXT,
		updated_at TEXT NOT NULL,
		stage TEXT,
		state TEXT,
		started_at TEXT,
		duration_seconds INTEGER,
		migration_id TEXT,
		progress INTEGER,
		stage_progress INTEGER,
		completed_stages TEXT,
		total_stages INTEGER,
		current_stage_index INTEGER,
		data TEXT
	)`, tableName)

	s.logger.Info("Creating migration status table if not exists", "table", tableName)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		s.logger.Error("Failed to create migration status table", "error", err)
		return fmt.Errorf("failed to create migration status table: %w", err)
	}

	// Create migration history table
	historyTableName := s.getTableName("migration_history")
	historyQuery := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		repository TEXT NOT NULL,
		status TEXT NOT NULL,
		error TEXT,
		updated_at TEXT NOT NULL,
		stage TEXT,
		state TEXT,
		started_at TEXT,
		duration_seconds INTEGER,
		migration_id TEXT,
		progress INTEGER,
		stage_progress INTEGER,
		completed_stages TEXT,
		total_stages INTEGER,
		current_stage_index INTEGER,
		data TEXT,
		archived_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`, historyTableName)

	s.logger.Info("Creating migration history table if not exists", "table", historyTableName)
	if _, err := s.db.ExecContext(ctx, historyQuery); err != nil {
		s.logger.Error("Failed to create migration history table", "error", err)
		return fmt.Errorf("failed to create migration history table: %w", err)
	}

	// Create index on repository column for faster lookups
	historyIndexQuery := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_repository ON %s(repository)", historyTableName, historyTableName)
	s.logger.Info("Creating index if not exists", "query", historyIndexQuery)
	if _, err := s.db.ExecContext(ctx, historyIndexQuery); err != nil {
		s.logger.Error("Failed to create index on history table", "error", err)
		return fmt.Errorf("failed to create index on history table: %w", err)
	}

	// Create index on repository column
	indexQuery := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_repository ON %s(repository)", tableName, tableName)
	s.logger.Info("Creating index if not exists", "query", indexQuery)
	if _, err := s.db.ExecContext(ctx, indexQuery); err != nil {
		s.logger.Error("Failed to create index", "error", err)
		return fmt.Errorf("failed to create index: %w", err)
	}

	// Analyze to optimize query performance
	analyzeQuery := fmt.Sprintf("ANALYZE %s", tableName)
	s.logger.Info("Running ANALYZE on table")
	if _, err := s.db.ExecContext(ctx, analyzeQuery); err != nil {
		s.logger.Warn("Failed to analyze table", "error", err)
		// Continue anyway as this is not critical
	}

	// Setup periodic maintenance
	s.startMaintenanceRoutine(ctx)

	s.logger.Info("SQLite initialization completed successfully", "duration", time.Since(start))
	s.isInitComplete = true
	return nil
}

// startMaintenanceRoutine sets up a background routine to perform database maintenance
func (s *SQLiteStorage) startMaintenanceRoutine(ctx context.Context) {
	// Cancel any existing timer
	if s.maintenanceTimer != nil {
		s.maintenanceTimer.Stop()
	}

	// Setup a new timer for database maintenance
	s.maintenanceTimer = time.AfterFunc(maintenanceInterval, func() {
		s.logger.Info("Running scheduled database maintenance")

		// Create a new background context for maintenance
		maintCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Run maintenance with retries
		if err := s.withRetry(maintCtx, "DatabaseMaintenance", func(ctx context.Context) error {
			return s.performDatabaseMaintenance(ctx)
		}); err != nil {
			s.logger.Error("Database maintenance failed", "error", err)
		}

		// Schedule the next maintenance run
		s.startMaintenanceRoutine(ctx)
	})
}

// performDatabaseMaintenance executes maintenance operations on the database
func (s *SQLiteStorage) performDatabaseMaintenance(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	// Operations to perform during maintenance
	operations := []struct {
		name  string
		query string
	}{
		{"ANALYZE", "ANALYZE"},
		{"PRAGMA optimize", "PRAGMA optimize"},
		{"PRAGMA wal_checkpoint(RESTART)", "PRAGMA wal_checkpoint(RESTART)"},
	}

	for _, op := range operations {
		s.logger.Info("Performing database maintenance operation", "operation", op.name)
		_, err := s.db.ExecContext(ctx, op.query)
		if err != nil {
			s.logger.Error("Database maintenance operation failed",
				"operation", op.name,
				"error", err,
			)
			return fmt.Errorf("failed to execute %s: %w", op.name, err)
		}
	}

	// Run vacuum and analyze in a transaction with proper handling
	if err := s.performVacuumAnalyze(ctx); err != nil {
		s.logger.Warn("Vacuum and analyze operations failed during maintenance", "error", err)
		// Continue despite failure since this is non-critical
	}

	return nil
}

// performVacuumAnalyze performs VACUUM and ANALYZE operations in a transaction
// This demonstrates the usage of withTransaction method
func (s *SQLiteStorage) performVacuumAnalyze(ctx context.Context) error {
	s.logger.Info("Performing VACUUM and ANALYZE in transaction")

	// Use withTransaction to safely run these operations
	return s.withTransaction(ctx, "VacuumAnalyze", func(tx *sql.Tx) error {
		// Get the table name
		tableName := s.getTableName("migration_status")

		// Execute VACUUM - this will compact the database
		// Note: Some versions of SQLite cannot VACUUM in a transaction
		// So we'll just do an ANALYZE which is transaction-safe
		analyzeQuery := fmt.Sprintf("ANALYZE %s", tableName)

		if _, err := tx.Exec(analyzeQuery); err != nil {
			return fmt.Errorf("failed to analyze table: %w", err)
		}

		// Also run statistics updates for better query planning
		if _, err := tx.Exec("ANALYZE sqlite_master"); err != nil {
			return fmt.Errorf("failed to analyze sqlite_master: %w", err)
		}

		return nil
	})
}

// Close releases database resources.
func (s *SQLiteStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop maintenance timer if running
	if s.maintenanceTimer != nil {
		s.maintenanceTimer.Stop()
		s.maintenanceTimer = nil
	}

	if s.db == nil {
		return nil
	}

	// Try to vacuum the database before closing to optimize storage
	// But do it with a timeout to avoid hanging
	s.logger.Info("Performing VACUUM on SQLite database before closing")

	// Create a context with timeout for vacuum operation
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to perform VACUUM with timeout
	vacuumErr := func() error {
		// Execute VACUUM with timeout
		_, err := s.db.ExecContext(ctx, "VACUUM")
		return err
	}()

	if vacuumErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			s.logger.Warn("VACUUM operation timed out, continuing with close", "timeout", "10s")
		} else {
			s.logger.Warn("Failed to vacuum database", "error", vacuumErr)
		}
		// Continue with close despite VACUUM failure
	}

	// Close the database connection
	err := s.db.Close()
	s.db = nil
	s.isInitComplete = false

	if err != nil {
		s.logger.Error("Error closing database connection", "error", err)
	} else {
		s.logger.Info("Database connection closed successfully")
	}

	return err
}

// SaveMigrationStatus saves or updates a migration status.
func (s *SQLiteStorage) SaveMigrationStatus(ctx context.Context, status *payload.MigrationStatus) error {
	if status == nil {
		return fmt.Errorf("cannot save nil migration status")
	}

	start := time.Now()
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		duration := time.Since(start)
		if duration > time.Second {
			s.logger.Warn("Long database operation",
				"operation", "SaveMigrationStatus",
				"repository", status.Repository,
				"duration", duration,
			)
		}
	}()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	return s.withRetry(ctx, "SaveMigrationStatus", func(ctx context.Context) error {
		// Create a new context with a longer timeout for database operations
		dbCtx, cancel := context.WithTimeout(ctx, s.operationTimeout)
		defer cancel()

		tableName := s.getTableName("migration_status")

		// Convert completed stages to JSON
		completedStages, err := json.Marshal(status.CompletedStages)
		if err != nil {
			return fmt.Errorf("failed to marshal completed stages: %w", err)
		}

		// Upsert query (insert or update)
		query := fmt.Sprintf(`
		INSERT INTO %s (
			repository, status, error, updated_at, 
			stage, state, started_at, duration_seconds, 
			migration_id, progress, stage_progress, 
			completed_stages, total_stages, current_stage_index
		) VALUES (
			?, ?, ?, ?, 
			?, ?, ?, ?, 
			?, ?, ?, 
			?, ?, ?
		) ON CONFLICT(repository) DO UPDATE SET
			status = excluded.status,
			error = excluded.error,
			updated_at = excluded.updated_at,
			stage = excluded.stage,
			state = excluded.state,
			started_at = COALESCE(migration_status.started_at, excluded.started_at),
			duration_seconds = excluded.duration_seconds,
			migration_id = excluded.migration_id,
			progress = excluded.progress,
			stage_progress = excluded.stage_progress,
			completed_stages = excluded.completed_stages,
			total_stages = excluded.total_stages,
			current_stage_index = excluded.current_stage_index
		`, tableName)

		_, err = s.db.ExecContext(dbCtx, query,
			status.Repository,
			status.Status,
			status.Error,
			status.UpdatedAt.Format(time.RFC3339),
			status.Stage,
			status.State,
			formatTimeOrEmpty(status.StartedAt),
			int(status.Duration.Seconds()),
			status.MigrationID,
			status.Progress,
			status.StageProgress,
			string(completedStages),
			status.TotalStages,
			status.CurrentStageIndex,
		)

		if err != nil {
			if err == context.DeadlineExceeded {
				s.logger.Error("Database operation timed out",
					"operation", "SaveMigrationStatus",
					"repository", status.Repository,
					"duration", time.Since(start),
				)
			}
			return fmt.Errorf("failed to save migration status: %w", err)
		}

		return nil
	})
}

// GetMigrationStatus retrieves a migration status by repository name.
func (s *SQLiteStorage) GetMigrationStatus(ctx context.Context, repoName string) (*payload.MigrationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var status *payload.MigrationStatus
	err := s.withRetry(ctx, "GetMigrationStatus", func(ctx context.Context) error {
		// Create a new context with a longer timeout for database operations
		dbCtx, cancel := context.WithTimeout(ctx, s.operationTimeout)
		defer cancel()

		tableName := s.getTableName("migration_status")
		query := fmt.Sprintf("SELECT repository, status, error, updated_at, stage, state, started_at, duration_seconds, migration_id, progress, stage_progress, completed_stages, total_stages, current_stage_index FROM %s WHERE repository = ?", tableName)

		row := s.db.QueryRowContext(dbCtx, query, repoName)

		var migrationStatus payload.MigrationStatus
		var updatedAt, startedAt string
		var completedStagesJSON string
		var durationSeconds int

		err := row.Scan(
			&migrationStatus.Repository,
			&migrationStatus.Status,
			&migrationStatus.Error,
			&updatedAt,
			&migrationStatus.Stage,
			&migrationStatus.State,
			&startedAt,
			&durationSeconds,
			&migrationStatus.MigrationID,
			&migrationStatus.Progress,
			&migrationStatus.StageProgress,
			&completedStagesJSON,
			&migrationStatus.TotalStages,
			&migrationStatus.CurrentStageIndex,
		)

		if err == sql.ErrNoRows {
			return nil
		}

		if err != nil {
			return fmt.Errorf("failed to get migration status: %w", err)
		}

		// Parse time fields
		if updatedAt != "" {
			parsedTime, err := time.Parse(time.RFC3339, updatedAt)
			if err != nil {
				return fmt.Errorf("failed to parse updated_at time: %w", err)
			}
			migrationStatus.UpdatedAt = parsedTime
		}

		if startedAt != "" {
			parsedTime, err := time.Parse(time.RFC3339, startedAt)
			if err != nil {
				return fmt.Errorf("failed to parse started_at time: %w", err)
			}
			migrationStatus.StartedAt = parsedTime
		}

		// Set duration from seconds
		migrationStatus.Duration = time.Duration(durationSeconds) * time.Second

		// Parse completed stages
		if completedStagesJSON != "" {
			var completedStages []string
			if err := json.Unmarshal([]byte(completedStagesJSON), &completedStages); err != nil {
				return fmt.Errorf("failed to unmarshal completed stages: %w", err)
			}
			migrationStatus.CompletedStages = completedStages
		}

		status = &migrationStatus
		return nil
	})

	if err != nil {
		return nil, err
	}

	return status, nil
}

// GetAllMigrationStatuses retrieves all migration statuses.
func (s *SQLiteStorage) GetAllMigrationStatuses(ctx context.Context) (map[string]*payload.MigrationStatus, error) {
	// Initialize results map
	result := make(map[string]*payload.MigrationStatus)

	// Don't try to read during initialization
	if !s.isInitComplete {
		s.logger.Warn("Skipping GetAllMigrationStatuses during initialization", "path", s.dbPath)
		return result, nil
	}

	// Check database file existence
	if _, err := os.Stat(s.dbPath); err != nil {
		s.logger.Error("Database file not found", "path", s.dbPath, "error", err)
		return result, nil
	}

	// Use the main connection with retry logic instead of a separate connection
	err := s.withRetry(ctx, "GetAllMigrationStatuses", func(ctx context.Context) error {
		// Create a context with timeout for this operation
		opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		// Lock the mutex for the operation
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.db == nil {
			return fmt.Errorf("database not initialized")
		}

		tableName := s.getTableName("migration_status")
		query := fmt.Sprintf(`
			SELECT repository, status, error, updated_at, stage, state, started_at, 
				duration_seconds, migration_id, progress, stage_progress, 
				completed_stages, total_stages, current_stage_index, data 
			FROM %s
		`, tableName)

		s.logger.Info("Executing query to get all migration statuses")

		rows, err := s.db.QueryContext(opCtx, query)
		if err != nil {
			s.logger.Error("Failed to query migration statuses",
				"error", err,
				"query", query)
			return err
		}
		defer func() {
			if err := rows.Close(); err != nil {
				s.logger.Error("Failed to close rows", "error", err)
			}
		}()

		// Process rows
		rowCount := 0
		for rows.Next() {
			rowCount++
			var status payload.MigrationStatus
			var updatedAt, startedAt string
			var completedStagesJSON string
			var data sql.NullString
			var durationSeconds int

			err := rows.Scan(
				&status.Repository,
				&status.Status,
				&status.Error,
				&updatedAt,
				&status.Stage,
				&status.State,
				&startedAt,
				&durationSeconds,
				&status.MigrationID,
				&status.Progress,
				&status.StageProgress,
				&completedStagesJSON,
				&status.TotalStages,
				&status.CurrentStageIndex,
				&data,
			)

			if err != nil {
				s.logger.Error("Failed to scan row", "row", rowCount, "error", err)
				continue // Skip this row but continue processing
			}

			// Parse time fields
			if updatedAt != "" {
				parsedTime, err := time.Parse(time.RFC3339, updatedAt)
				if err != nil {
					s.logger.Warn("Failed to parse updated_at time", "repository", status.Repository, "error", err)
				} else {
					status.UpdatedAt = parsedTime
				}
			}

			if startedAt != "" {
				parsedTime, err := time.Parse(time.RFC3339, startedAt)
				if err != nil {
					s.logger.Warn("Failed to parse started_at time", "repository", status.Repository, "error", err)
				} else {
					status.StartedAt = parsedTime
				}
			}

			// Set duration from seconds
			status.Duration = time.Duration(durationSeconds) * time.Second

			// Parse completed stages
			if completedStagesJSON != "" {
				var completedStages []string
				if err := json.Unmarshal([]byte(completedStagesJSON), &completedStages); err != nil {
					s.logger.Warn("Failed to unmarshal completed stages", "repository", status.Repository, "error", err)
				} else {
					status.CompletedStages = completedStages
				}
			}

			// Add to results
			result[status.Repository] = &status
		}

		// Check for row iteration errors
		if err := rows.Err(); err != nil {
			s.logger.Error("Error iterating rows", "error", err)
			return err
		}

		s.logger.Info("GetAllMigrationStatuses completed successfully", "count", len(result))
		return nil
	})

	if err != nil {
		s.logger.Error("GetAllMigrationStatuses failed", "error", err)
		// Return partial results if we have any
		if len(result) > 0 {
			s.logger.Info("Returning partial results despite error", "count", len(result))
		}
	}

	return result, nil
}

// DeleteMigrationStatus removes a migration status.
func (s *SQLiteStorage) DeleteMigrationStatus(ctx context.Context, repoName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	return s.withRetry(ctx, "DeleteMigrationStatus", func(ctx context.Context) error {
		// Create a new context with a longer timeout for database operations
		dbCtx, cancel := context.WithTimeout(ctx, s.operationTimeout)
		defer cancel()

		tableName := s.getTableName("migration_status")
		query := fmt.Sprintf("DELETE FROM %s WHERE repository = ?", tableName)

		_, err := s.db.ExecContext(dbCtx, query, repoName)
		if err != nil {
			return fmt.Errorf("failed to delete migration status: %w", err)
		}

		return nil
	})
}

// Helper functions

// getTableName returns a table name with the configured prefix.
func (s *SQLiteStorage) getTableName(table string) string {
	if s.tablePrefix == "" {
		return table
	}
	return s.tablePrefix + "_" + table
}

// formatTimeOrEmpty formats a time value as RFC3339 or returns an empty string if the time is zero.
func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ensureDir creates a directory if it doesn't exist.
// It creates all necessary parent directories if they don't exist.
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// CheckAndRepairDatabase is a utility function that attempts to check and repair the database.
// This can be called to recover from database lock issues or corruption.
// It returns a detailed report of actions taken and any problems found.
func (s *SQLiteStorage) CheckAndRepairDatabase(ctx context.Context) (string, error) {
	s.logger.Info("Starting database check and repair operation", "database", s.dbPath)

	// Ensure we're not in the middle of an operation
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a buffer to store the report
	var report strings.Builder
	report.WriteString("SQLite Database Check Report\n")
	report.WriteString("==========================\n")
	report.WriteString(fmt.Sprintf("Database: %s\n", s.dbPath))
	report.WriteString(fmt.Sprintf("Time: %s\n\n", time.Now().Format(time.RFC3339)))

	// Check if database file exists
	if _, err := os.Stat(s.dbPath); err != nil {
		report.WriteString(fmt.Sprintf("ERROR: Database file not found: %s\n", err))
		return report.String(), fmt.Errorf("database file not found: %w", err)
	}
	report.WriteString("✓ Database file exists\n")

	// Check for associated WAL and SHM files
	walPath := s.dbPath + "-wal"
	shmPath := s.dbPath + "-shm"

	if walInfo, err := os.Stat(walPath); err == nil {
		report.WriteString(fmt.Sprintf("✓ WAL file exists (%s, %d bytes)\n", walPath, walInfo.Size()))

		// Check if WAL file is very large
		if walInfo.Size() > 50*1024*1024 { // 50MB
			report.WriteString(fmt.Sprintf("! WARNING: WAL file is very large (%d MB)\n", walInfo.Size()/1024/1024))
		}
	} else {
		report.WriteString(fmt.Sprintf("- No WAL file found (%s)\n", walPath))
	}

	if _, err := os.Stat(shmPath); err == nil {
		report.WriteString(fmt.Sprintf("✓ SHM file exists (%s)\n", shmPath))
	} else {
		report.WriteString(fmt.Sprintf("- No SHM file found (%s)\n", shmPath))
	}

	// If the database connection is active, try basic operations
	if s.db != nil {
		report.WriteString("\nTesting Database Connection\n")

		// Try a ping with timeout
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := s.db.PingContext(pingCtx); err != nil {
			report.WriteString(fmt.Sprintf("✗ PING failed: %s\n", err))

			// Check if database is locked
			if err.Error() == "database is locked" {
				report.WriteString("- Database is locked. Attempting recovery...\n")

				// Try to close and reopen the database
				report.WriteString("- Closing current connection\n")
				if err := s.db.Close(); err != nil {
					report.WriteString(fmt.Sprintf("  ✗ Error closing connection: %s\n", err))
				} else {
					report.WriteString("  ✓ Connection closed successfully\n")
				}

				// Create new connection with higher timeout
				dsn := s.dbPath + "?_timeout=60000&_journal=WAL&_sync=NORMAL"
				report.WriteString(fmt.Sprintf("- Opening new connection with DSN: %s\n", dsn))

				newDb, err := sql.Open("sqlite3", dsn)
				if err != nil {
					report.WriteString(fmt.Sprintf("  ✗ Failed to open new connection: %s\n", err))
				} else {
					report.WriteString("  ✓ New connection opened\n")

					// Set aggressive timeout for busy handler
					_, err = newDb.Exec("PRAGMA busy_timeout = 60000")
					if err != nil {
						report.WriteString(fmt.Sprintf("  ✗ Failed to set busy_timeout: %s\n", err))
					} else {
						report.WriteString("  ✓ Set busy_timeout to 60 seconds\n")
					}

					// Try to run integrity check
					report.WriteString("- Running PRAGMA integrity_check...\n")
					row := newDb.QueryRow("PRAGMA integrity_check")
					var result string
					if err := row.Scan(&result); err != nil {
						report.WriteString(fmt.Sprintf("  ✗ Integrity check failed: %s\n", err))
					} else if result == "ok" {
						report.WriteString("  ✓ Integrity check passed\n")
					} else {
						report.WriteString(fmt.Sprintf("  ! Integrity problems found: %s\n", result))
					}

					// Try to reset locks by checkpointing
					report.WriteString("- Running WAL checkpoint...\n")
					_, err = newDb.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
					if err != nil {
						report.WriteString(fmt.Sprintf("  ✗ WAL checkpoint failed: %s\n", err))
					} else {
						report.WriteString("  ✓ WAL checkpoint succeeded\n")
					}

					// Close new connection
					if err := newDb.Close(); err != nil {
						report.WriteString(fmt.Sprintf("  ✗ Error closing new connection: %s\n", err))
					} else {
						report.WriteString("  ✓ New connection closed successfully\n")
					}

					// Reopen original connection
					s.db, err = sql.Open("sqlite3", s.dbPath)
					if err != nil {
						report.WriteString(fmt.Sprintf("✗ Failed to reopen original connection: %s\n", err))
						s.db = nil
					} else {
						report.WriteString("✓ Original connection reopened\n")

						// Re-initialize connection parameters
						s.db.SetMaxOpenConns(1)
						s.db.SetMaxIdleConns(1)
						s.db.SetConnMaxLifetime(time.Minute * 5)

						// Try setting pragmas again
						for _, pragma := range []string{
							"PRAGMA busy_timeout=30000",
							"PRAGMA journal_mode=WAL",
							"PRAGMA synchronous=NORMAL",
						} {
							if _, err := s.db.Exec(pragma); err != nil {
								report.WriteString(fmt.Sprintf("✗ Failed to set %s: %s\n", pragma, err))
							} else {
								report.WriteString(fmt.Sprintf("✓ Successfully set %s\n", pragma))
							}
						}
					}
				}
			}
		} else {
			report.WriteString("✓ PING successful\n")

			// Try a simple table check
			tableName := s.getTableName("migration_status")
			countCtx, countCancel := context.WithTimeout(ctx, 5*time.Second)
			defer countCancel()

			var count int
			err := s.db.QueryRowContext(countCtx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
			if err != nil {
				report.WriteString(fmt.Sprintf("✗ Failed to count records: %s\n", err))
			} else {
				report.WriteString(fmt.Sprintf("✓ Table count successful: %d records\n", count))
			}
		}
	} else {
		report.WriteString("\n✗ No active database connection\n")
	}

	report.WriteString("\nRepair Operations\n")

	// If we have a valid connection after all previous operations
	if s.db != nil {
		// Try a VACUUM operation
		vacuumCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		report.WriteString("- Running VACUUM...\n")
		_, err := s.db.ExecContext(vacuumCtx, "VACUUM")
		if err != nil {
			report.WriteString(fmt.Sprintf("  ✗ VACUUM failed: %s\n", err))
		} else {
			report.WriteString("  ✓ VACUUM completed successfully\n")
		}

		// Try an ANALYZE operation
		report.WriteString("- Running ANALYZE...\n")
		_, err = s.db.ExecContext(vacuumCtx, "ANALYZE")
		if err != nil {
			report.WriteString(fmt.Sprintf("  ✗ ANALYZE failed: %s\n", err))
		} else {
			report.WriteString("  ✓ ANALYZE completed successfully\n")
		}
	}

	report.WriteString("\nSummary\n")
	report.WriteString("-------\n")
	if s.db != nil && s.db.Ping() == nil {
		report.WriteString("✓ Database is operational\n")
	} else {
		report.WriteString("✗ Database is NOT operational\n")
	}

	return report.String(), nil
}

// ArchiveMigrationAttempt saves a completed migration attempt to history
func (s *SQLiteStorage) ArchiveMigrationAttempt(ctx context.Context, attempt *payload.MigrationStatus) error {
	if attempt == nil {
		return fmt.Errorf("cannot archive nil migration attempt")
	}

	start := time.Now()
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		duration := time.Since(start)
		if duration > time.Second {
			s.logger.Warn("Long database operation",
				"operation", "ArchiveMigrationAttempt",
				"repository", attempt.Repository,
				"duration", duration,
			)
		}
	}()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	return s.withRetry(ctx, "ArchiveMigrationAttempt", func(ctx context.Context) error {
		// Create a new context with a longer timeout for database operations
		dbCtx, cancel := context.WithTimeout(ctx, s.operationTimeout)
		defer cancel()

		tableName := s.getTableName("migration_history")

		// Convert completed stages to JSON
		completedStages, err := json.Marshal(attempt.CompletedStages)
		if err != nil {
			return fmt.Errorf("failed to marshal completed stages: %w", err)
		}

		// Insert into history table
		query := fmt.Sprintf(`
		INSERT INTO %s (
			repository, status, error, updated_at, 
			stage, state, started_at, duration_seconds, 
			migration_id, progress, stage_progress, 
			completed_stages, total_stages, current_stage_index
		) VALUES (
			?, ?, ?, ?, 
			?, ?, ?, ?, 
			?, ?, ?, 
			?, ?, ?
		)`, tableName)

		_, err = s.db.ExecContext(dbCtx, query,
			attempt.Repository,
			attempt.Status,
			attempt.Error,
			attempt.UpdatedAt.Format(time.RFC3339),
			attempt.Stage,
			attempt.State,
			formatTimeOrEmpty(attempt.StartedAt),
			int(attempt.Duration.Seconds()),
			attempt.MigrationID,
			attempt.Progress,
			attempt.StageProgress,
			string(completedStages),
			attempt.TotalStages,
			attempt.CurrentStageIndex,
		)

		if err != nil {
			if err == context.DeadlineExceeded {
				s.logger.Error("Database operation timed out",
					"operation", "ArchiveMigrationAttempt",
					"repository", attempt.Repository,
					"duration", time.Since(start),
				)
			}
			return fmt.Errorf("failed to archive migration attempt: %w", err)
		}

		return nil
	})
}

// GetArchivedMigrationAttempts retrieves all historical migration attempts for a repository
func (s *SQLiteStorage) GetArchivedMigrationAttempts(ctx context.Context, repoFullName string) ([]*payload.MigrationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var attempts []*payload.MigrationStatus
	err := s.withRetry(ctx, "GetArchivedMigrationAttempts", func(ctx context.Context) error {
		// Create a new context with a longer timeout for database operations
		dbCtx, cancel := context.WithTimeout(ctx, s.operationTimeout)
		defer cancel()

		tableName := s.getTableName("migration_history")
		query := fmt.Sprintf(`
			SELECT 
				repository, status, error, updated_at, 
				stage, state, started_at, duration_seconds, 
				migration_id, progress, stage_progress, 
				completed_stages, total_stages, current_stage_index,
				archived_at
			FROM %s 
			WHERE repository = ?
			ORDER BY archived_at DESC
		`, tableName)

		rows, err := s.db.QueryContext(dbCtx, query, repoFullName)
		if err != nil {
			return fmt.Errorf("failed to query archived migration attempts: %w", err)
		}
		defer func() {
			if err := rows.Close(); err != nil {
				s.logger.Warn("failed to close rows", "error", err)
			}
		}()

		// Clear any previous results
		attempts = []*payload.MigrationStatus{}

		for rows.Next() {
			var migrationStatus payload.MigrationStatus
			var updatedAt, startedAt, archivedAt string
			var completedStagesJSON string
			var durationSeconds int

			err := rows.Scan(
				&migrationStatus.Repository,
				&migrationStatus.Status,
				&migrationStatus.Error,
				&updatedAt,
				&migrationStatus.Stage,
				&migrationStatus.State,
				&startedAt,
				&durationSeconds,
				&migrationStatus.MigrationID,
				&migrationStatus.Progress,
				&migrationStatus.StageProgress,
				&completedStagesJSON,
				&migrationStatus.TotalStages,
				&migrationStatus.CurrentStageIndex,
				&archivedAt,
			)

			if err != nil {
				return fmt.Errorf("failed to scan archived migration attempt: %w", err)
			}

			// Parse time fields
			if updatedAt != "" {
				parsedTime, err := time.Parse(time.RFC3339, updatedAt)
				if err != nil {
					s.logger.Warn("Failed to parse updated_at time in archived record",
						"repository", migrationStatus.Repository,
						"error", err)
				} else {
					migrationStatus.UpdatedAt = parsedTime
				}
			}

			if startedAt != "" {
				parsedTime, err := time.Parse(time.RFC3339, startedAt)
				if err != nil {
					s.logger.Warn("Failed to parse started_at time in archived record",
						"repository", migrationStatus.Repository,
						"error", err)
				} else {
					migrationStatus.StartedAt = parsedTime
				}
			}

			// Set duration from seconds
			migrationStatus.Duration = time.Duration(durationSeconds) * time.Second

			// Parse completed stages
			if completedStagesJSON != "" {
				var completedStages []string
				if err := json.Unmarshal([]byte(completedStagesJSON), &completedStages); err != nil {
					s.logger.Warn("Failed to unmarshal completed stages in archived record",
						"repository", migrationStatus.Repository,
						"error", err)
				} else {
					migrationStatus.CompletedStages = completedStages
				}
			}

			attempts = append(attempts, &migrationStatus)
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("error iterating archived migration attempts: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return attempts, nil
}
