// Package storage provides data persistence for migration state information.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL driver
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// MySQLStorage implements MigrationStorage using MySQL database.
type MySQLStorage struct {
	db          *sql.DB
	connString  string
	tablePrefix string
	mu          sync.Mutex
	logger      *slog.Logger
}

// NewMySQLStorage creates a new MySQL storage provider.
func NewMySQLStorage(config *StorageConfig) (MigrationStorage, error) {
	if config.ConnectionString == "" {
		return nil, fmt.Errorf("connection string is required for MySQL storage")
	}

	return &MySQLStorage{
		connString:  config.ConnectionString,
		tablePrefix: config.TablePrefix,
		logger:      logging.Get(),
	}, nil
}

// Initialize sets up the MySQL database connection and tables.
func (s *MySQLStorage) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return nil // Already initialized
	}

	var err error
	s.db, err = sql.Open("mysql", s.connString)
	if err != nil {
		return fmt.Errorf("failed to open MySQL database: %w", err)
	}

	// Set connection pool parameters
	s.db.SetMaxOpenConns(10)
	s.db.SetMaxIdleConns(5)
	s.db.SetConnMaxLifetime(time.Minute * 5)

	// Ping the database to verify the connection
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to connect to MySQL database: %w", err)
	}

	// Create migration status table
	tableName := s.getTableName("migration_status")
	query := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		repository VARCHAR(255) PRIMARY KEY,
		status VARCHAR(50) NOT NULL,
		error TEXT,
		updated_at TIMESTAMP NOT NULL,
		stage VARCHAR(50),
		state VARCHAR(50),
		started_at TIMESTAMP NULL,
		duration_seconds INT,
		migration_id VARCHAR(255),
		progress INT,
		stage_progress INT,
		completed_stages JSON,
		total_stages INT,
		current_stage_index INT,
		data JSON
	)`, tableName)

	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to create migration status table: %w", err)
	}

	// Create migration history table
	historyTableName := s.getTableName("migration_history")
	historyQuery := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		id INT AUTO_INCREMENT PRIMARY KEY,
		repository VARCHAR(255) NOT NULL,
		status VARCHAR(50) NOT NULL,
		error TEXT,
		updated_at TIMESTAMP NOT NULL,
		stage VARCHAR(50),
		state VARCHAR(50),
		started_at TIMESTAMP NULL,
		duration_seconds INT,
		migration_id VARCHAR(255),
		progress INT,
		stage_progress INT,
		completed_stages JSON,
		total_stages INT,
		current_stage_index INT,
		data JSON,
		archived_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_repository (repository)
	)`, historyTableName)

	if _, err := s.db.ExecContext(ctx, historyQuery); err != nil {
		return fmt.Errorf("failed to create migration history table: %w", err)
	}

	return nil
}

// Close releases database resources.
func (s *MySQLStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.db = nil
	return err
}

// SaveMigrationStatus saves or updates a migration status.
func (s *MySQLStorage) SaveMigrationStatus(ctx context.Context, status *payload.MigrationStatus) error {
	if status == nil {
		return fmt.Errorf("cannot save nil migration status")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	// Convert completed stages to JSON
	completedStages, err := json.Marshal(status.CompletedStages)
	if err != nil {
		return fmt.Errorf("failed to marshal completed stages: %w", err)
	}

	// Upsert query using MySQL syntax
	quotedTableName := s.getQuotedTableName("migration_status")
	query := `
	INSERT INTO ` + quotedTableName + ` (
		repository, status, error, updated_at, 
		stage, state, started_at, duration_seconds, 
		migration_id, progress, stage_progress, 
		completed_stages, total_stages, current_stage_index
	) VALUES (
		?, ?, ?, ?, 
		?, ?, ?, ?, 
		?, ?, ?, 
		?, ?, ?
	) ON DUPLICATE KEY UPDATE
		status = VALUES(status),
		error = VALUES(error),
		updated_at = VALUES(updated_at),
		stage = VALUES(stage),
		state = VALUES(state),
		started_at = COALESCE(started_at, VALUES(started_at)),
		duration_seconds = VALUES(duration_seconds),
		migration_id = VALUES(migration_id),
		progress = VALUES(progress),
		stage_progress = VALUES(stage_progress),
		completed_stages = VALUES(completed_stages),
		total_stages = VALUES(total_stages),
		current_stage_index = VALUES(current_stage_index)
	`

	// Format time values for MySQL
	startedAtStr := formatTimeOrEmpty(status.StartedAt)
	var startedAt interface{}
	if startedAtStr == "" {
		startedAt = nil
	} else {
		startedAt = startedAtStr
	}

	_, err = s.db.ExecContext(ctx, query,
		status.Repository,
		status.Status,
		status.Error,
		status.UpdatedAt.Format(time.RFC3339),
		status.Stage,
		status.State,
		startedAt,
		int(status.Duration.Seconds()),
		status.MigrationID,
		status.Progress,
		status.StageProgress,
		string(completedStages),
		status.TotalStages,
		status.CurrentStageIndex,
	)

	if err != nil {
		return fmt.Errorf("failed to save migration status: %w", err)
	}

	return nil
}

// GetMigrationStatus retrieves a migration status by repository name.
func (s *MySQLStorage) GetMigrationStatus(ctx context.Context, repoName string) (*payload.MigrationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	quotedTableName := s.getQuotedTableName("migration_status")
	query := "SELECT repository, status, error, updated_at, stage, state, started_at, duration_seconds, migration_id, progress, stage_progress, completed_stages, total_stages, current_stage_index FROM " + quotedTableName + " WHERE repository = ?"

	row := s.db.QueryRowContext(ctx, query, repoName)

	var status payload.MigrationStatus
	var updatedAt, startedAt sql.NullString
	var completedStagesJSON sql.NullString
	var durationSeconds sql.NullInt64

	err := row.Scan(
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
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get migration status: %w", err)
	}

	// Parse time fields
	if updatedAt.Valid && updatedAt.String != "" {
		parsedTime, err := time.Parse(time.RFC3339, updatedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at time: %w", err)
		}
		status.UpdatedAt = parsedTime
	}

	if startedAt.Valid && startedAt.String != "" {
		parsedTime, err := time.Parse(time.RFC3339, startedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse started_at time: %w", err)
		}
		status.StartedAt = parsedTime
	}

	// Set duration from seconds
	if durationSeconds.Valid {
		status.Duration = time.Duration(durationSeconds.Int64) * time.Second
	}

	// Parse completed stages
	if completedStagesJSON.Valid && completedStagesJSON.String != "" {
		var completedStages []string
		if err := json.Unmarshal([]byte(completedStagesJSON.String), &completedStages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal completed stages: %w", err)
		}
		status.CompletedStages = completedStages
	}

	return &status, nil
}

// GetAllMigrationStatuses retrieves all migration statuses.
func (s *MySQLStorage) GetAllMigrationStatuses(ctx context.Context) (map[string]*payload.MigrationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	result := make(map[string]*payload.MigrationStatus)

	quotedTableName := s.getQuotedTableName("migration_status")
	query := "SELECT repository, status, error, updated_at, stage, state, started_at, duration_seconds, migration_id, progress, stage_progress, completed_stages, total_stages, current_stage_index FROM " + quotedTableName

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query migration statuses: %w", err)
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			s.logger.Warn("failed to close rows", "error", err)
		}
	}()

	for rows.Next() {
		var status payload.MigrationStatus
		var updatedAt, startedAt sql.NullString
		var completedStagesJSON sql.NullString
		var durationSeconds sql.NullInt64

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
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan migration status: %w", err)
		}

		// Parse time fields
		if updatedAt.Valid && updatedAt.String != "" {
			parsedTime, err := time.Parse(time.RFC3339, updatedAt.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse updated_at time: %w", err)
			}
			status.UpdatedAt = parsedTime
		}

		if startedAt.Valid && startedAt.String != "" {
			parsedTime, err := time.Parse(time.RFC3339, startedAt.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse started_at time: %w", err)
			}
			status.StartedAt = parsedTime
		}

		// Set duration from seconds
		if durationSeconds.Valid {
			status.Duration = time.Duration(durationSeconds.Int64) * time.Second
		}

		// Parse completed stages
		if completedStagesJSON.Valid && completedStagesJSON.String != "" {
			var completedStages []string
			if err := json.Unmarshal([]byte(completedStagesJSON.String), &completedStages); err != nil {
				return nil, fmt.Errorf("failed to unmarshal completed stages: %w", err)
			}
			status.CompletedStages = completedStages
		}

		result[status.Repository] = &status
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// DeleteMigrationStatus removes a migration status.
func (s *MySQLStorage) DeleteMigrationStatus(ctx context.Context, repoName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	quotedTableName := s.getQuotedTableName("migration_status")
	query := "DELETE FROM " + quotedTableName + " WHERE repository = ?"

	_, err := s.db.ExecContext(ctx, query, repoName)
	if err != nil {
		return fmt.Errorf("failed to delete migration status: %w", err)
	}

	return nil
}

// CheckAndRepairDatabase attempts to check and repair the MySQL database.
// It performs diagnostics and basic maintenance operations.
func (s *MySQLStorage) CheckAndRepairDatabase(ctx context.Context) (string, error) {
	s.logger.Info("Starting MySQL database check and repair operation")

	// Create a buffer to store the report
	var report strings.Builder
	report.WriteString("MySQL Database Check Report\n")
	report.WriteString("==========================\n")
	report.WriteString(fmt.Sprintf("Time: %s\n\n", time.Now().Format(time.RFC3339)))

	// Lock the mutex for the operation
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if database is initialized
	if s.db == nil {
		report.WriteString("✗ Database not initialized\n")
		return report.String(), fmt.Errorf("database not initialized")
	}

	// Test connection with ping
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	report.WriteString("Testing Database Connection\n")
	if err := s.db.PingContext(pingCtx); err != nil {
		report.WriteString(fmt.Sprintf("✗ Database ping failed: %s\n", err))
		return report.String(), err
	}
	report.WriteString("✓ Database connection is working\n\n")

	// Get database version
	var version string
	err := s.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
	if err != nil {
		report.WriteString(fmt.Sprintf("✗ Failed to get database version: %s\n", err))
	} else {
		report.WriteString(fmt.Sprintf("✓ MySQL version: %s\n", version))
	}

	// Check table status
	tableName := s.getTableName("migration_status")
	report.WriteString(fmt.Sprintf("\nChecking table: %s\n", tableName))

	// Check if table exists
	var tableExists int
	err = s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = '%s'", tableName)).Scan(&tableExists)
	if err != nil {
		report.WriteString(fmt.Sprintf("✗ Failed to check if table exists: %s\n", err))
	} else if tableExists == 0 {
		report.WriteString(fmt.Sprintf("✗ Table %s does not exist\n", tableName))
	} else {
		report.WriteString(fmt.Sprintf("✓ Table %s exists\n", tableName))

		// Get record count
		var count int
		err = s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
		if err != nil {
			report.WriteString(fmt.Sprintf("✗ Failed to count records: %s\n", err))
		} else {
			report.WriteString(fmt.Sprintf("✓ Table contains %d records\n", count))
		}

		// Run maintenance operations
		report.WriteString("\nRunning maintenance operations:\n")

		// ANALYZE TABLE
		_, err = s.db.ExecContext(ctx, fmt.Sprintf("ANALYZE TABLE %s", tableName))
		if err != nil {
			report.WriteString(fmt.Sprintf("✗ ANALYZE TABLE failed: %s\n", err))
		} else {
			report.WriteString("✓ ANALYZE TABLE completed\n")
		}

		// OPTIMIZE TABLE
		_, err = s.db.ExecContext(ctx, fmt.Sprintf("OPTIMIZE TABLE %s", tableName))
		if err != nil {
			report.WriteString(fmt.Sprintf("✗ OPTIMIZE TABLE failed: %s\n", err))
		} else {
			report.WriteString("✓ OPTIMIZE TABLE completed\n")
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

// Helper function to get table name with prefix
func (s *MySQLStorage) getTableName(table string) string {
	if s.tablePrefix == "" {
		return table
	}
	return s.tablePrefix + "_" + table
}

// getQuotedTableName returns a safely quoted table name for use in SQL queries.
// This helps prevent SQL injection by properly handling table names.
func (s *MySQLStorage) getQuotedTableName(table string) string {
	tableName := s.getTableName(table)
	return "`" + strings.ReplaceAll(tableName, "`", "``") + "`"
}

// ArchiveMigrationAttempt saves a completed migration attempt to history
func (s *MySQLStorage) ArchiveMigrationAttempt(ctx context.Context, attempt *payload.MigrationStatus) error {
	if attempt == nil {
		return fmt.Errorf("cannot archive nil migration attempt")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("database not initialized")
	}

	// Convert completed stages to JSON
	completedStages, err := json.Marshal(attempt.CompletedStages)
	if err != nil {
		return fmt.Errorf("failed to marshal completed stages: %w", err)
	}

	// Insert query for archiving
	quotedTableName := s.getQuotedTableName("migration_history")
	query := `
	INSERT INTO ` + quotedTableName + ` (
		repository, status, error, updated_at, 
		stage, state, started_at, duration_seconds, 
		migration_id, progress, stage_progress, 
		completed_stages, total_stages, current_stage_index
	) VALUES (
		?, ?, ?, ?, 
		?, ?, ?, ?, 
		?, ?, ?, 
		?, ?, ?
	)`

	// Format time values for MySQL
	startedAtStr := formatTimeOrEmpty(attempt.StartedAt)
	var startedAt interface{}
	if startedAtStr == "" {
		startedAt = nil
	} else {
		startedAt = startedAtStr
	}

	_, err = s.db.ExecContext(ctx, query,
		attempt.Repository,
		attempt.Status,
		attempt.Error,
		attempt.UpdatedAt.Format(time.RFC3339),
		attempt.Stage,
		attempt.State,
		startedAt,
		int(attempt.Duration.Seconds()),
		attempt.MigrationID,
		attempt.Progress,
		attempt.StageProgress,
		string(completedStages),
		attempt.TotalStages,
		attempt.CurrentStageIndex,
	)

	if err != nil {
		return fmt.Errorf("failed to archive migration attempt: %w", err)
	}

	return nil
}

// GetArchivedMigrationAttempts retrieves all historical migration attempts for a repository
func (s *MySQLStorage) GetArchivedMigrationAttempts(ctx context.Context, repoFullName string) ([]*payload.MigrationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	quotedTableName := s.getQuotedTableName("migration_history")
	query := `
		SELECT 
			repository, status, error, updated_at, 
			stage, state, started_at, duration_seconds, 
			migration_id, progress, stage_progress, 
			completed_stages, total_stages, current_stage_index,
			archived_at
		FROM ` + quotedTableName + ` 
		WHERE repository = ?
		ORDER BY archived_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, repoFullName)
	if err != nil {
		return nil, fmt.Errorf("failed to query archived migration attempts: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			s.logger.Warn("failed to close rows", "error", err)
		}
	}()

	var attempts []*payload.MigrationStatus

	for rows.Next() {
		var attempt payload.MigrationStatus
		var updatedAt, startedAt, archivedAt sql.NullString
		var completedStagesJSON sql.NullString
		var durationSeconds sql.NullInt64

		err := rows.Scan(
			&attempt.Repository,
			&attempt.Status,
			&attempt.Error,
			&updatedAt,
			&attempt.Stage,
			&attempt.State,
			&startedAt,
			&durationSeconds,
			&attempt.MigrationID,
			&attempt.Progress,
			&attempt.StageProgress,
			&completedStagesJSON,
			&attempt.TotalStages,
			&attempt.CurrentStageIndex,
			&archivedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan archived migration attempt: %w", err)
		}

		// Parse time fields
		if updatedAt.Valid && updatedAt.String != "" {
			parsedTime, err := time.Parse(time.RFC3339, updatedAt.String)
			if err != nil {
				s.logger.Warn("Failed to parse updated_at time in archived record",
					"repository", attempt.Repository,
					"error", err)
			} else {
				attempt.UpdatedAt = parsedTime
			}
		}

		if startedAt.Valid && startedAt.String != "" {
			parsedTime, err := time.Parse(time.RFC3339, startedAt.String)
			if err != nil {
				s.logger.Warn("Failed to parse started_at time in archived record",
					"repository", attempt.Repository,
					"error", err)
			} else {
				attempt.StartedAt = parsedTime
			}
		}

		// Set duration from seconds
		if durationSeconds.Valid {
			attempt.Duration = time.Duration(durationSeconds.Int64) * time.Second
		}

		// Parse completed stages
		if completedStagesJSON.Valid && completedStagesJSON.String != "" {
			var completedStages []string
			if err := json.Unmarshal([]byte(completedStagesJSON.String), &completedStages); err != nil {
				s.logger.Warn("Failed to unmarshal completed stages in archived record",
					"repository", attempt.Repository,
					"error", err)
			} else {
				attempt.CompletedStages = completedStages
			}
		}

		attempts = append(attempts, &attempt)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating archived migration attempts: %w", err)
	}

	return attempts, nil
}
