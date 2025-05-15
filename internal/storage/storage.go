// Package storage provides data persistence for migration state information.
// It defines interfaces and implementations for storing and retrieving migration status data.
package storage

import (
	"context"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/config"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// MigrationStorage defines the interface for storing and retrieving migration status.
// This allows for different backend implementations (e.g., in-memory, SQLite, PostgreSQL).
type MigrationStorage interface {
	// Initialize sets up the storage provider, creating any necessary resources.
	// This should be called before any other methods.
	Initialize(ctx context.Context) error

	// Close releases any resources used by the storage provider.
	Close() error

	// SaveMigrationStatus saves the current status of a migration.
	// The status.Repository field should contain the repoFullName (e.g., "org/repo").
	SaveMigrationStatus(ctx context.Context, status *payload.MigrationStatus) error

	// GetMigrationStatus retrieves the current status of a migration by its full name.
	GetMigrationStatus(ctx context.Context, repoFullName string) (*payload.MigrationStatus, error)

	// GetAllMigrationStatuses retrieves all current migration statuses, keyed by repoFullName.
	GetAllMigrationStatuses(ctx context.Context) (map[string]*payload.MigrationStatus, error)

	// DeleteMigrationStatus removes a migration status by its full name.
	// This is typically used for cleanup or if a migration is permanently removed.
	DeleteMigrationStatus(ctx context.Context, repoFullName string) error

	// CheckAndRepairDatabase performs backend-specific checks and repair operations.
	CheckAndRepairDatabase(ctx context.Context) (string, error)

	// ArchiveMigrationAttempt saves a completed (failed or successful) migration attempt to history.
	// The attempt.Repository field should contain the repoFullName.
	ArchiveMigrationAttempt(ctx context.Context, attempt *payload.MigrationStatus) error

	// GetArchivedMigrationAttempts retrieves all historical migration attempts for a repository by its full name.
	GetArchivedMigrationAttempts(ctx context.Context, repoFullName string) ([]*payload.MigrationStatus, error)
}

// NewStorageProvider creates a new storage provider based on the provided configuration.
// It returns a properly initialized storage provider ready for use.
func NewStorageProvider(config *StorageConfig) (MigrationStorage, error) {
	if !config.Enabled {
		return &NoopStorage{}, nil
	}

	switch config.Type {
	case "sqlite":
		return NewSQLiteStorage(config)
	case "mysql":
		return NewMySQLStorage(config)
	case "postgres":
		return NewPostgresStorage(config)
	default:
		// Default to SQLite if type is not recognized
		return NewSQLiteStorage(config)
	}
}

// StorageConfig defines the configuration for a storage provider.
type StorageConfig struct {
	// Enabled indicates whether storage is enabled
	Enabled bool `mapstructure:"enabled"`

	// Type specifies the storage type ("sqlite", "mysql", "postgres")
	Type string `mapstructure:"type"`

	// ConnectionString provides database connection information
	ConnectionString string `mapstructure:"connection_string"`

	// TablePrefix can be used to prefix database table names
	TablePrefix string `mapstructure:"table_prefix"`

	// Timeout specifies the maximum time for database operations (in seconds)
	Timeout int `mapstructure:"timeout"`
}

// NewStorageConfigFromConfig creates a new StorageConfig from the application's config.
// This is a helper function to convert between config types.
func NewStorageConfigFromConfig(cfg *config.StorageConfig) *StorageConfig {
	return &StorageConfig{
		Enabled:          cfg.Enabled,
		Type:             cfg.Type,
		ConnectionString: cfg.ConnectionString,
		TablePrefix:      cfg.TablePrefix,
		Timeout:          cfg.Timeout,
	}
}

// NoopStorage is a placeholder implementation that doesn't persist any data.
// It's used when storage is disabled.
type NoopStorage struct{}

func (n *NoopStorage) Initialize(ctx context.Context) error {
	return nil
}

func (n *NoopStorage) Close() error {
	return nil
}

func (n *NoopStorage) SaveMigrationStatus(ctx context.Context, status *payload.MigrationStatus) error {
	return nil
}

func (n *NoopStorage) GetMigrationStatus(ctx context.Context, repoFullName string) (*payload.MigrationStatus, error) {
	return nil, nil
}

func (n *NoopStorage) GetAllMigrationStatuses(ctx context.Context) (map[string]*payload.MigrationStatus, error) {
	return make(map[string]*payload.MigrationStatus), nil
}

func (n *NoopStorage) DeleteMigrationStatus(ctx context.Context, repoFullName string) error {
	return nil
}

func (n *NoopStorage) CheckAndRepairDatabase(ctx context.Context) (string, error) {
	return "Storage is disabled. No database to check or repair.", nil
}

func (n *NoopStorage) ArchiveMigrationAttempt(ctx context.Context, attempt *payload.MigrationStatus) error {
	return nil
}

func (n *NoopStorage) GetArchivedMigrationAttempts(ctx context.Context, repoFullName string) ([]*payload.MigrationStatus, error) {
	return nil, nil
}
