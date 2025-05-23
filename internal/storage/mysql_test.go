package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
)

// Test helper to create a test MySQLStorage with mocked database
func createTestMySQLStorage(db *sql.DB) *MySQLStorage {
	return &MySQLStorage{
		db:          db,
		connString:  "test-connection",
		tablePrefix: "test",
		logger:      slog.Default(),
	}
}

// Test helper to create sample migration status
func createTestMigrationStatus(repo, status, stage string) *payload.MigrationStatus {
	return &payload.MigrationStatus{
		Repository:        repo,
		Status:            status,
		Stage:             stage,
		State:             "processing",
		StartedAt:         time.Now().Add(-time.Hour),
		Duration:          time.Hour,
		UpdatedAt:         time.Now(),
		MigrationID:       "mig-123",
		Progress:          50,
		StageProgress:     25,
		CompletedStages:   []string{"validation", "setup"},
		TotalStages:       4,
		CurrentStageIndex: 2,
		Error:             "",
	}
}

// Custom matcher for time values that allows slight differences
type timeValueMatcher struct {
	expected time.Time
	delta    time.Duration
}

func (t timeValueMatcher) Match(v driver.Value) bool {
	switch val := v.(type) {
	case string:
		parsed, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return false
		}
		diff := parsed.Sub(t.expected)
		if diff < 0 {
			diff = -diff
		}
		return diff <= t.delta
	case time.Time:
		diff := val.Sub(t.expected)
		if diff < 0 {
			diff = -diff
		}
		return diff <= t.delta
	}
	return false
}

func TimeWithDelta(expected time.Time, delta time.Duration) timeValueMatcher {
	return timeValueMatcher{expected: expected, delta: delta}
}

func TestNewMySQLStorage(t *testing.T) {
	tests := []struct {
		name        string
		config      *StorageConfig
		shouldError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &StorageConfig{
				ConnectionString: "user:pass@tcp(localhost:3306)/dbname",
				TablePrefix:      "test",
			},
			shouldError: false,
		},
		{
			name: "config with empty table prefix",
			config: &StorageConfig{
				ConnectionString: "user:pass@tcp(localhost:3306)/dbname",
				TablePrefix:      "",
			},
			shouldError: false,
		},
		{
			name: "empty connection string",
			config: &StorageConfig{
				ConnectionString: "",
				TablePrefix:      "test",
			},
			shouldError: true,
			errorMsg:    "connection string is required for MySQL storage",
		},
		{
			name:        "nil config",
			config:      nil,
			shouldError: true,
			errorMsg:    "", // Will panic, we'll handle it differently
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Handle nil config case specially to catch panic
			if tt.config == nil {
				defer func() {
					if r := recover(); r == nil {
						t.Error("Expected panic for nil config, but didn't get one")
					}
				}()
				_, _ = NewMySQLStorage(tt.config)
				return
			}

			storage, err := NewMySQLStorage(tt.config)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Expected no error, got %v", err)
				return
			}

			if storage == nil {
				t.Error("Expected storage instance, got nil")
				return
			}

			mysqlStorage, ok := storage.(*MySQLStorage)
			if !ok {
				t.Error("Expected MySQLStorage instance")
				return
			}

			if mysqlStorage.connString != tt.config.ConnectionString {
				t.Errorf("Expected connection string %q, got %q", tt.config.ConnectionString, mysqlStorage.connString)
			}

			if mysqlStorage.tablePrefix != tt.config.TablePrefix {
				t.Errorf("Expected table prefix %q, got %q", tt.config.TablePrefix, mysqlStorage.tablePrefix)
			}
		})
	}
}

func TestMySQLStorage_Initialize(t *testing.T) {
	tests := []struct {
		name          string
		setupStorage  func() (*MySQLStorage, sqlmock.Sqlmock)
		setupMock     func(mock sqlmock.Sqlmock)
		shouldError   bool
		errorContains string
	}{
		{
			name: "already initialized",
			setupStorage: func() (*MySQLStorage, sqlmock.Sqlmock) {
				db, mock, _ := sqlmock.New()
				storage := createTestMySQLStorage(db)
				return storage, mock
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations since it should return early
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, mock := tt.setupStorage()
			defer func() {
				if err := storage.db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			tt.setupMock(mock)

			err := storage.Initialize(context.Background())

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_Close(t *testing.T) {
	tests := []struct {
		name        string
		setupDB     bool
		shouldError bool
	}{
		{
			name:        "close initialized database",
			setupDB:     true,
			shouldError: false,
		},
		{
			name:        "close uninitialized database",
			setupDB:     false,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}

			storage := createTestMySQLStorage(nil)

			if tt.setupDB {
				storage.db = db
				mock.ExpectClose()
			}

			err = storage.Close()

			if tt.shouldError && err == nil {
				t.Error("Expected error, got nil")
			} else if !tt.shouldError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			// After close, db should be nil
			if storage.db != nil {
				t.Error("Expected db to be nil after close")
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_SaveMigrationStatus(t *testing.T) {
	now := time.Now()
	testStatus := createTestMigrationStatus("test/repo", payload.StatusInProgress, "validation")
	testStatus.UpdatedAt = now
	testStatus.StartedAt = now.Add(-time.Hour)

	tests := []struct {
		name          string
		status        *payload.MigrationStatus
		setupMock     func(mock sqlmock.Sqlmock)
		shouldError   bool
		errorContains string
	}{
		{
			name:   "successful save",
			status: testStatus,
			setupMock: func(mock sqlmock.Sqlmock) {
				completedStages, _ := json.Marshal(testStatus.CompletedStages)
				mock.ExpectExec("INSERT INTO `test_migration_status`").
					WithArgs(
						testStatus.Repository,
						testStatus.Status,
						testStatus.Error,
						testStatus.UpdatedAt.UTC().Format("2006-01-02 15:04:05"), // MySQL datetime format in UTC
						testStatus.Stage,
						testStatus.State,
						testStatus.StartedAt.UTC().Format("2006-01-02 15:04:05"), // MySQL datetime format in UTC
						int(testStatus.Duration.Seconds()),
						testStatus.MigrationID,
						testStatus.Progress,
						testStatus.StageProgress,
						string(completedStages),
						testStatus.TotalStages,
						testStatus.CurrentStageIndex,
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			shouldError: false,
		},
		{
			name:   "nil status",
			status: nil,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database expectations
			},
			shouldError:   true,
			errorContains: "cannot save nil migration status",
		},
		{
			name:   "database not initialized",
			status: testStatus,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database expectations
			},
			shouldError:   true,
			errorContains: "database not initialized",
		},
		{
			name:   "database error",
			status: testStatus,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO `test_migration_status`").
					WillReturnError(fmt.Errorf("database error"))
			},
			shouldError:   true,
			errorContains: "failed to save migration status",
		},
		{
			name: "zero started time",
			status: &payload.MigrationStatus{
				Repository:        "test/repo",
				Status:            payload.StatusInProgress,
				Stage:             "validation",
				State:             "processing",
				StartedAt:         time.Time{}, // Zero time
				Duration:          time.Hour,
				UpdatedAt:         now,
				MigrationID:       "mig-123",
				Progress:          50,
				StageProgress:     25,
				CompletedStages:   []string{"validation"},
				TotalStages:       4,
				CurrentStageIndex: 1,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				completedStages, _ := json.Marshal([]string{"validation"})
				mock.ExpectExec("INSERT INTO `test_migration_status`").
					WithArgs(
						"test/repo",
						payload.StatusInProgress,
						"",
						now.UTC().Format("2006-01-02 15:04:05"), // MySQL datetime format in UTC
						"validation",
						"processing",
						nil, // Should be nil for zero time
						int(time.Hour.Seconds()),
						"mig-123",
						50,
						25,
						string(completedStages),
						4,
						1,
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			storage := createTestMySQLStorage(db)

			// For "database not initialized" test, set db to nil
			if tt.name == "database not initialized" {
				storage.db = nil
			}

			tt.setupMock(mock)

			err = storage.SaveMigrationStatus(context.Background(), tt.status)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_GetMigrationStatus(t *testing.T) {
	now := time.Now()
	testRepo := "test/repo"

	tests := []struct {
		name          string
		repoName      string
		setupMock     func(mock sqlmock.Sqlmock)
		expectedRepo  string
		shouldError   bool
		errorContains string
		shouldBeNil   bool
	}{
		{
			name:     "successful get",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				completedStages := []string{"validation", "setup"}
				completedStagesJSON, _ := json.Marshal(completedStages)

				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
				}).AddRow(
					testRepo,
					payload.StatusInProgress,
					"",
					now.Format(time.RFC3339),
					"validation",
					"processing",
					now.Add(-time.Hour).Format(time.RFC3339),
					3600, // 1 hour in seconds
					"mig-123",
					50,
					25,
					string(completedStagesJSON),
					4,
					2,
				)

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WithArgs(testRepo).
					WillReturnRows(rows)
			},
			expectedRepo: testRepo,
			shouldError:  false,
		},
		{
			name:     "not found",
			repoName: "nonexistent/repo",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WithArgs("nonexistent/repo").
					WillReturnError(sql.ErrNoRows)
			},
			shouldBeNil: true,
			shouldError: false,
		},
		{
			name:     "database error",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WithArgs(testRepo).
					WillReturnError(fmt.Errorf("database error"))
			},
			shouldError:   true,
			errorContains: "failed to get migration status",
		},
		{
			name:     "database not initialized",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			shouldError:   true,
			errorContains: "database not initialized",
		},
		{
			name:     "invalid time format",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
				}).AddRow(
					testRepo,
					payload.StatusInProgress,
					"",
					"invalid-time-format", // Invalid time
					"validation",
					"processing",
					now.Add(-time.Hour).Format(time.RFC3339),
					3600,
					"mig-123",
					50,
					25,
					"[]",
					4,
					2,
				)

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WithArgs(testRepo).
					WillReturnRows(rows)
			},
			shouldError:   true,
			errorContains: "failed to parse updated_at time",
		},
		{
			name:     "invalid JSON in completed stages",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
				}).AddRow(
					testRepo,
					payload.StatusInProgress,
					"",
					now.Format(time.RFC3339),
					"validation",
					"processing",
					now.Add(-time.Hour).Format(time.RFC3339),
					3600,
					"mig-123",
					50,
					25,
					"invalid-json", // Invalid JSON
					4,
					2,
				)

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WithArgs(testRepo).
					WillReturnRows(rows)
			},
			shouldError:   true,
			errorContains: "failed to unmarshal completed stages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			storage := createTestMySQLStorage(db)

			// For "database not initialized" test, set db to nil
			if tt.name == "database not initialized" {
				storage.db = nil
			}

			tt.setupMock(mock)

			result, err := storage.GetMigrationStatus(context.Background(), tt.repoName)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
					return
				}

				if tt.shouldBeNil {
					if result != nil {
						t.Error("Expected nil result")
					}
				} else {
					if result == nil {
						t.Error("Expected non-nil result")
						return
					}
					if result.Repository != tt.expectedRepo {
						t.Errorf("Expected repository %q, got %q", tt.expectedRepo, result.Repository)
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_GetAllMigrationStatuses(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		setupMock     func(mock sqlmock.Sqlmock)
		expectedCount int
		shouldError   bool
		errorContains string
	}{
		{
			name: "successful get all",
			setupMock: func(mock sqlmock.Sqlmock) {
				completedStages := []string{"validation"}
				completedStagesJSON, _ := json.Marshal(completedStages)

				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
				}).
					AddRow("repo1", payload.StatusInProgress, "", now.Format(time.RFC3339),
						"validation", "processing", now.Add(-time.Hour).Format(time.RFC3339),
						3600, "mig-1", 25, 10, string(completedStagesJSON), 4, 1).
					AddRow("repo2", payload.StatusSucceeded, "", now.Format(time.RFC3339),
						"completed", "finished", now.Add(-2*time.Hour).Format(time.RFC3339),
						7200, "mig-2", 100, 100, string(completedStagesJSON), 4, 4)

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WillReturnRows(rows)
			},
			expectedCount: 2,
			shouldError:   false,
		},
		{
			name: "empty result",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
				})

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WillReturnRows(rows)
			},
			expectedCount: 0,
			shouldError:   false,
		},
		{
			name: "database error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WillReturnError(fmt.Errorf("database error"))
			},
			shouldError:   true,
			errorContains: "failed to query migration statuses",
		},
		{
			name: "database not initialized",
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			shouldError:   true,
			errorContains: "database not initialized",
		},
		{
			name: "scan error",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
				}).
					AddRow("repo1", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil) // nil values will cause scan error

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_status`").
					WillReturnRows(rows)
			},
			shouldError:   true,
			errorContains: "failed to scan migration status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			storage := createTestMySQLStorage(db)

			// For "database not initialized" test, set db to nil
			if tt.name == "database not initialized" {
				storage.db = nil
			}

			tt.setupMock(mock)

			result, err := storage.GetAllMigrationStatuses(context.Background())

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
					return
				}

				if result == nil {
					t.Error("Expected non-nil result")
					return
				}

				if len(result) != tt.expectedCount {
					t.Errorf("Expected %d statuses, got %d", tt.expectedCount, len(result))
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_DeleteMigrationStatus(t *testing.T) {
	testRepo := "test/repo"

	tests := []struct {
		name          string
		repoName      string
		setupMock     func(mock sqlmock.Sqlmock)
		shouldError   bool
		errorContains string
	}{
		{
			name:     "successful delete",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("DELETE FROM `test_migration_status`").
					WithArgs(testRepo).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			shouldError: false,
		},
		{
			name:     "delete non-existent",
			repoName: "nonexistent/repo",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("DELETE FROM `test_migration_status`").
					WithArgs("nonexistent/repo").
					WillReturnResult(sqlmock.NewResult(0, 0)) // No rows affected
			},
			shouldError: false, // Should not error even if no rows deleted
		},
		{
			name:     "database error",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("DELETE FROM `test_migration_status`").
					WithArgs(testRepo).
					WillReturnError(fmt.Errorf("database error"))
			},
			shouldError:   true,
			errorContains: "failed to delete migration status",
		},
		{
			name:     "database not initialized",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			shouldError:   true,
			errorContains: "database not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			storage := createTestMySQLStorage(db)

			// For "database not initialized" test, set db to nil
			if tt.name == "database not initialized" {
				storage.db = nil
			}

			tt.setupMock(mock)

			err = storage.DeleteMigrationStatus(context.Background(), tt.repoName)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_ArchiveMigrationAttempt(t *testing.T) {
	now := time.Now()
	testAttempt := createTestMigrationStatus("test/repo", payload.StatusSucceeded, "completed")
	testAttempt.UpdatedAt = now
	testAttempt.StartedAt = now.Add(-time.Hour)

	tests := []struct {
		name          string
		attempt       *payload.MigrationStatus
		setupMock     func(mock sqlmock.Sqlmock)
		shouldError   bool
		errorContains string
	}{
		{
			name:    "successful archive",
			attempt: testAttempt,
			setupMock: func(mock sqlmock.Sqlmock) {
				completedStages, _ := json.Marshal(testAttempt.CompletedStages)
				mock.ExpectExec("INSERT INTO `test_migration_history`").
					WithArgs(
						testAttempt.Repository,
						testAttempt.Status,
						testAttempt.Error,
						testAttempt.UpdatedAt.UTC().Format("2006-01-02 15:04:05"), // MySQL datetime format in UTC
						testAttempt.Stage,
						testAttempt.State,
						testAttempt.StartedAt.UTC().Format("2006-01-02 15:04:05"), // MySQL datetime format in UTC
						int(testAttempt.Duration.Seconds()),
						testAttempt.MigrationID,
						testAttempt.Progress,
						testAttempt.StageProgress,
						string(completedStages),
						testAttempt.TotalStages,
						testAttempt.CurrentStageIndex,
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			shouldError: false,
		},
		{
			name:    "nil attempt",
			attempt: nil,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database expectations
			},
			shouldError:   true,
			errorContains: "cannot archive nil migration attempt",
		},
		{
			name:    "database not initialized",
			attempt: testAttempt,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No database expectations
			},
			shouldError:   true,
			errorContains: "database not initialized",
		},
		{
			name:    "database error",
			attempt: testAttempt,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("INSERT INTO `test_migration_history`").
					WillReturnError(fmt.Errorf("database error"))
			},
			shouldError:   true,
			errorContains: "failed to archive migration attempt",
		},
		{
			name: "zero started time",
			attempt: &payload.MigrationStatus{
				Repository:        "test/repo",
				Status:            payload.StatusSucceeded,
				Stage:             "completed",
				State:             "finished",
				StartedAt:         time.Time{}, // Zero time
				Duration:          time.Hour,
				UpdatedAt:         now,
				MigrationID:       "mig-123",
				Progress:          100,
				StageProgress:     100,
				CompletedStages:   []string{"validation", "setup", "migration", "cleanup"},
				TotalStages:       4,
				CurrentStageIndex: 4,
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				completedStages, _ := json.Marshal([]string{"validation", "setup", "migration", "cleanup"})
				mock.ExpectExec("INSERT INTO `test_migration_history`").
					WithArgs(
						"test/repo",
						payload.StatusSucceeded,
						"",
						now.UTC().Format("2006-01-02 15:04:05"), // MySQL datetime format in UTC
						"completed",
						"finished",
						nil, // Should be nil for zero time
						int(time.Hour.Seconds()),
						"mig-123",
						100,
						100,
						string(completedStages),
						4,
						4,
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			storage := createTestMySQLStorage(db)

			// For "database not initialized" test, set db to nil
			if tt.name == "database not initialized" {
				storage.db = nil
			}

			tt.setupMock(mock)

			err = storage.ArchiveMigrationAttempt(context.Background(), tt.attempt)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_GetArchivedMigrationAttempts(t *testing.T) {
	now := time.Now()
	testRepo := "test/repo"

	tests := []struct {
		name          string
		repoName      string
		setupMock     func(mock sqlmock.Sqlmock)
		expectedCount int
		shouldError   bool
		errorContains string
	}{
		{
			name:     "successful get archived attempts",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				completedStages := []string{"validation", "setup"}
				completedStagesJSON, _ := json.Marshal(completedStages)

				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
					"archived_at",
				}).
					AddRow(testRepo, payload.StatusSucceeded, "", now.Format(time.RFC3339),
						"completed", "finished", now.Add(-2*time.Hour).Format(time.RFC3339),
						7200, "mig-1", 100, 100, string(completedStagesJSON), 4, 4,
						now.Add(-time.Hour).Format(time.RFC3339)).
					AddRow(testRepo, payload.StatusFailed, "error occurred", now.Add(-24*time.Hour).Format(time.RFC3339),
						"migration", "failed", now.Add(-25*time.Hour).Format(time.RFC3339),
						3600, "mig-2", 75, 50, string(completedStagesJSON), 4, 3,
						now.Add(-23*time.Hour).Format(time.RFC3339))

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_history`").
					WithArgs(testRepo).
					WillReturnRows(rows)
			},
			expectedCount: 2,
			shouldError:   false,
		},
		{
			name:     "no archived attempts",
			repoName: "empty/repo",
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
					"archived_at",
				})

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_history`").
					WithArgs("empty/repo").
					WillReturnRows(rows)
			},
			expectedCount: 0,
			shouldError:   false,
		},
		{
			name:     "database error",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT (.+) FROM `test_migration_history`").
					WithArgs(testRepo).
					WillReturnError(fmt.Errorf("database error"))
			},
			shouldError:   true,
			errorContains: "failed to query archived migration attempts",
		},
		{
			name:     "database not initialized",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			shouldError:   true,
			errorContains: "database not initialized",
		},
		{
			name:     "scan error",
			repoName: testRepo,
			setupMock: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"repository", "status", "error", "updated_at", "stage", "state",
					"started_at", "duration_seconds", "migration_id", "progress",
					"stage_progress", "completed_stages", "total_stages", "current_stage_index",
					"archived_at",
				}).
					AddRow(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil) // nil values will cause scan error

				mock.ExpectQuery("SELECT (.+) FROM `test_migration_history`").
					WithArgs(testRepo).
					WillReturnRows(rows)
			},
			shouldError:   true,
			errorContains: "failed to scan archived migration attempt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			storage := createTestMySQLStorage(db)

			// For "database not initialized" test, set db to nil
			if tt.name == "database not initialized" {
				storage.db = nil
			}

			tt.setupMock(mock)

			result, err := storage.GetArchivedMigrationAttempts(context.Background(), tt.repoName)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
					return
				}

				// Result should not be nil and length should match expected count
				if len(result) != tt.expectedCount {
					t.Errorf("Expected %d archived attempts, got %d", tt.expectedCount, len(result))
				}

				// Check that attempts are for the correct repository
				for _, attempt := range result {
					if attempt.Repository != tt.repoName {
						t.Errorf("Expected repository %q, got %q", tt.repoName, attempt.Repository)
					}
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_CheckAndRepairDatabase(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(mock sqlmock.Sqlmock)
		shouldError    bool
		errorContains  string
		reportContains []string
	}{
		{
			name: "successful check and repair",
			setupMock: func(mock sqlmock.Sqlmock) {
				// Ping
				mock.ExpectPing()

				// Version query
				mock.ExpectQuery("SELECT VERSION\\(\\)").
					WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow("8.0.25"))

				// Table existence check
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
					WithArgs("test_migration_status").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				// Record count
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM `test_test_migration_status`").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

				// ANALYZE TABLE
				mock.ExpectExec("ANALYZE TABLE `test_test_migration_status`").
					WillReturnResult(sqlmock.NewResult(0, 0))

				// OPTIMIZE TABLE
				mock.ExpectExec("OPTIMIZE TABLE `test_test_migration_status`").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			shouldError: false,
			reportContains: []string{
				"MySQL Database Check Report",
				"✓ Database connection is working",
				"✓ MySQL version: 8.0.25",
				"✓ Table test_migration_status exists",
				"✓ Table contains 5 records",
				"✓ ANALYZE TABLE completed",
				"✓ OPTIMIZE TABLE completed",
				"✓ Database is operational",
			},
		},
		{
			name: "database not initialized",
			setupMock: func(mock sqlmock.Sqlmock) {
				// No expectations
			},
			shouldError:   true,
			errorContains: "database not initialized",
			reportContains: []string{
				"✗ Database not initialized",
			},
		},

		{
			name: "table does not exist",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing()

				// Version query
				mock.ExpectQuery("SELECT VERSION\\(\\)").
					WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow("8.0.25"))

				// Table existence check - table doesn't exist
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
					WithArgs("test_migration_status").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			},
			shouldError: false,
			reportContains: []string{
				"✓ Database connection is working",
				"✗ Table test_migration_status does not exist",
			},
		},
		{
			name: "analyze table failure",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing()

				// Version query
				mock.ExpectQuery("SELECT VERSION\\(\\)").
					WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow("8.0.25"))

				// Table existence check
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema.tables").
					WithArgs("test_migration_status").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				// Record count
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM `test_test_migration_status`").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

				// ANALYZE TABLE fails
				mock.ExpectExec("ANALYZE TABLE `test_test_migration_status`").
					WillReturnError(fmt.Errorf("analyze failed"))

				// OPTIMIZE TABLE
				mock.ExpectExec("OPTIMIZE TABLE `test_test_migration_status`").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			shouldError: false,
			reportContains: []string{
				"✓ Database connection is working",
				"✓ Table test_migration_status exists",
				"✗ ANALYZE TABLE failed",
				"✓ OPTIMIZE TABLE completed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("Failed to create mock: %v", err)
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Logf("Failed to close database: %v", err)
				}
			}()

			storage := createTestMySQLStorage(db)

			// For "database not initialized" test, set db to nil
			if tt.name == "database not initialized" {
				storage.db = nil
			}

			tt.setupMock(mock)

			report, err := storage.CheckAndRepairDatabase(context.Background())

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			// Check report content
			for _, expectedText := range tt.reportContains {
				if !strings.Contains(report, expectedText) {
					t.Errorf("Expected report to contain %q, but it didn't.\nFull report:\n%s", expectedText, report)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMySQLStorage_HelperMethods(t *testing.T) {
	storage := createTestMySQLStorage(nil)

	t.Run("getTableName", func(t *testing.T) {
		tests := []struct {
			name        string
			tablePrefix string
			tableName   string
			expected    string
		}{
			{
				name:        "with prefix",
				tablePrefix: "test",
				tableName:   "migration_status",
				expected:    "test_migration_status",
			},
			{
				name:        "without prefix",
				tablePrefix: "",
				tableName:   "migration_status",
				expected:    "migration_status",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				storage.tablePrefix = tt.tablePrefix
				result := storage.getTableName(tt.tableName)
				if result != tt.expected {
					t.Errorf("Expected %q, got %q", tt.expected, result)
				}
			})
		}
	})

	t.Run("getQuotedTableName", func(t *testing.T) {
		tests := []struct {
			name        string
			tablePrefix string
			tableName   string
			expected    string
		}{
			{
				name:        "normal table name",
				tablePrefix: "test",
				tableName:   "migration_status",
				expected:    "`test_migration_status`",
			},
			{
				name:        "table name with backticks",
				tablePrefix: "test",
				tableName:   "migration`status",
				expected:    "`test_migration``status`",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				storage.tablePrefix = tt.tablePrefix
				result := storage.getQuotedTableName(tt.tableName)
				if result != tt.expected {
					t.Errorf("Expected %q, got %q", tt.expected, result)
				}
			})
		}
	})

	t.Run("prepareTableQuery", func(t *testing.T) {
		storage.tablePrefix = "test"
		query := "SELECT * FROM {table} WHERE id = ?"
		tableName := "migration_status"
		expected := "SELECT * FROM `test_migration_status` WHERE id = ?"

		result := storage.prepareTableQuery(query, tableName)
		if result != expected {
			t.Errorf("Expected %q, got %q", expected, result)
		}
	})
}

func TestFormatTimeOrEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "zero time",
			input:    time.Time{},
			expected: "",
		},
		{
			name:     "valid time",
			input:    time.Date(2023, 12, 25, 15, 30, 45, 0, time.UTC),
			expected: "2023-12-25T15:30:45Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeOrEmpty(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
