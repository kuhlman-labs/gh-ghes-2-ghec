// Package storage provides data persistence for migration state information.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
)

// SchemaVersion represents the current database schema version
const SchemaVersion = 2

// Migration represents a database schema migration
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// migrations holds all database schema migrations in order
var migrations = []Migration{
	{
		Version:     1,
		Description: "Initial schema",
		SQL:         "", // Empty as version 1 is created by the initial table creation
	},
	{
		Version:     2,
		Description: "Add performance indexes",
		SQL: `
-- Add index for updated_at on migration_status
CREATE INDEX IF NOT EXISTS idx_{table_prefix}migration_status_updated_at ON {table_prefix}migration_status(updated_at);

-- Add index for status on migration_status
CREATE INDEX IF NOT EXISTS idx_{table_prefix}migration_status_status ON {table_prefix}migration_status(status);

-- Add index for status on migration_history
CREATE INDEX IF NOT EXISTS idx_{table_prefix}migration_history_status ON {table_prefix}migration_history(status);

-- Add index for updated_at on migration_history
CREATE INDEX IF NOT EXISTS idx_{table_prefix}migration_history_updated_at ON {table_prefix}migration_history(updated_at);

-- Add compound index for repository and updated_at on migration_history
CREATE INDEX IF NOT EXISTS idx_{table_prefix}migration_history_repository_date ON {table_prefix}migration_history(repository, updated_at);
`,
	},
}

// MigrationManager handles database schema migrations
type MigrationManager struct {
	db          *sql.DB
	dbType      string
	tablePrefix string
	logger      *slog.Logger
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(db *sql.DB, dbType, tablePrefix string) *MigrationManager {
	return &MigrationManager{
		db:          db,
		dbType:      dbType,
		tablePrefix: tablePrefix,
		logger:      logging.Get(),
	}
}

// Initialize sets up the schema versioning table if it doesn't exist
func (m *MigrationManager) Initialize(ctx context.Context) error {
	schemaTable := fmt.Sprintf("%sschema_version", m.tablePrefix)

	// Create schema version table if it doesn't exist
	var createTableSQL string

	switch m.dbType {
	case "sqlite":
		createTableSQL = `
		CREATE TABLE IF NOT EXISTS [` + schemaTable + `] (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL,
			success BOOLEAN NOT NULL
		)`
	case "postgres":
		createTableSQL = `
		CREATE TABLE IF NOT EXISTS "` + schemaTable + `" (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL,
			success BOOLEAN NOT NULL
		)`
	case "mysql":
		createTableSQL = `
		CREATE TABLE IF NOT EXISTS ` + "`" + schemaTable + "`" + ` (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL,
			success BOOLEAN NOT NULL
		)`
	default:
		return fmt.Errorf("unsupported database type: %s", m.dbType)
	}

	_, err := m.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create schema version table: %w", err)
	}

	return nil
}

// GetCurrentVersion gets the current schema version from the database
func (m *MigrationManager) GetCurrentVersion(ctx context.Context) (int, error) {
	schemaTable := fmt.Sprintf("%sschema_version", m.tablePrefix)

	var version int
	var query string

	// Use database-specific ways to handle table identifiers safely
	switch m.dbType {
	case "sqlite":
		query = "SELECT COALESCE(MAX(version), 0) FROM [" + schemaTable + "] WHERE success = true"
	case "postgres":
		query = "SELECT COALESCE(MAX(version), 0) FROM \"" + schemaTable + "\" WHERE success = true"
	case "mysql":
		query = "SELECT COALESCE(MAX(version), 0) FROM `" + schemaTable + "` WHERE success = true"
	default:
		return 0, fmt.Errorf("unsupported database type: %s", m.dbType)
	}

	err := m.db.QueryRowContext(ctx, query).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("failed to get current schema version: %w", err)
	}

	return version, nil
}

// MigrateToLatest runs all migrations required to reach the latest schema version
func (m *MigrationManager) MigrateToLatest(ctx context.Context) error {
	currentVersion, err := m.GetCurrentVersion(ctx)
	if err != nil {
		return err
	}

	m.logger.Info("Current database schema version", "version", currentVersion, "latest", SchemaVersion)

	if currentVersion == SchemaVersion {
		m.logger.Info("Database schema is up to date", "version", currentVersion)
		return nil
	}

	// Keep track of successfully applied migrations to return partial success
	var appliedCount int
	var lastError error

	// Run migrations that haven't been applied yet
	for _, migration := range migrations {
		if migration.Version <= currentVersion {
			continue
		}

		m.logger.Info("Applying database migration",
			"version", migration.Version,
			"description", migration.Description,
		)

		if err := m.applyMigration(ctx, migration); err != nil {
			lastError = fmt.Errorf("failed to apply migration %d (%s): %w",
				migration.Version, migration.Description, err)

			m.logger.Error("Migration failed",
				"version", migration.Version,
				"description", migration.Description,
				"error", err)

			// Check if context is canceled before continuing
			select {
			case <-ctx.Done():
				return fmt.Errorf("migrations interrupted after applying %d of %d: %w",
					appliedCount, len(migrations)-currentVersion, lastError)
			default:
				// Continue to next migration if context is still valid
			}

			// If we're in test mode, don't continue on errors
			if ctx.Value("test") != nil {
				return lastError
			}

			// Skip this failed migration but try to continue with others
			continue
		}

		appliedCount++
	}

	if lastError != nil {
		return fmt.Errorf("completed %d migrations with errors: %w", appliedCount, lastError)
	}

	m.logger.Info("All migrations applied successfully",
		"current_version", SchemaVersion,
		"migrations_applied", appliedCount)

	return nil
}

// applyMigration applies a single migration and records the result
func (m *MigrationManager) applyMigration(ctx context.Context, migration Migration) error {
	schemaTable := fmt.Sprintf("%sschema_version", m.tablePrefix)

	// Start a transaction
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	// Rollback on failure
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				m.logger.Error("Failed to rollback transaction",
					"error", rbErr,
					"migration_version", migration.Version,
				)
			}
		}
	}()

	// Skip empty SQL for initial schema
	if migration.SQL != "" {
		// Replace table prefix placeholder in SQL
		sql := replacePlaceholders(migration.SQL, m.tablePrefix)

		// For SQLite, we need to execute each statement separately
		statements := strings.Split(sql, ";")
		for _, stmt := range statements {
			// Skip empty statements
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}

			// Apply the migration
			m.logger.Debug("Executing SQL statement",
				"statement", stmt,
				"version", migration.Version)

			_, err = tx.ExecContext(ctx, stmt)
			if err != nil {
				return fmt.Errorf("migration failed on SQL statement %q: %w", stmt, err)
			}
		}
	}

	// Prepare params based on database type
	var insertSQL string
	var params []interface{}

	switch m.dbType {
	case "sqlite":
		insertSQL = "INSERT INTO [" + schemaTable + "] (version, description, applied_at, success) VALUES (?, ?, ?, ?)"
		params = []interface{}{migration.Version, migration.Description, time.Now(), true}
	case "postgres":
		insertSQL = "INSERT INTO \"" + schemaTable + "\" (version, description, applied_at, success) VALUES ($1, $2, $3, $4)"
		params = []interface{}{migration.Version, migration.Description, time.Now(), true}
	case "mysql":
		insertSQL = "INSERT INTO `" + schemaTable + "` (version, description, applied_at, success) VALUES (?, ?, ?, ?)"
		params = []interface{}{migration.Version, migration.Description, time.Now(), true}
	default:
		return fmt.Errorf("unsupported database type: %s", m.dbType)
	}

	// Record the migration
	_, err = tx.ExecContext(ctx, insertSQL, params...)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	m.logger.Info("Migration applied successfully",
		"version", migration.Version,
		"description", migration.Description,
	)

	return nil
}

// replacePlaceholders replaces template placeholders in migration SQL
func replacePlaceholders(sql string, tablePrefix string) string {
	// Replace the table_prefix placeholder with the actual table prefix
	return strings.ReplaceAll(sql, "{table_prefix}", tablePrefix)
}
