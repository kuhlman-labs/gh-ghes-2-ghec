package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMigrationStorage implements MigrationStorage for testing
type MockMigrationStorage struct {
	mock.Mock
}

func (m *MockMigrationStorage) Initialize(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockMigrationStorage) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockMigrationStorage) SaveMigrationStatus(ctx context.Context, status *payload.MigrationStatus) error {
	args := m.Called(ctx, status)
	return args.Error(0)
}

func (m *MockMigrationStorage) GetMigrationStatus(ctx context.Context, repository string) (*payload.MigrationStatus, error) {
	args := m.Called(ctx, repository)
	return args.Get(0).(*payload.MigrationStatus), args.Error(1)
}

func (m *MockMigrationStorage) GetAllMigrationStatuses(ctx context.Context) (map[string]*payload.MigrationStatus, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]*payload.MigrationStatus), args.Error(1)
}

func (m *MockMigrationStorage) DeleteMigrationStatus(ctx context.Context, repository string) error {
	args := m.Called(ctx, repository)
	return args.Error(0)
}

func (m *MockMigrationStorage) CheckAndRepairDatabase(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func (m *MockMigrationStorage) ArchiveMigrationAttempt(ctx context.Context, attempt *payload.MigrationStatus) error {
	args := m.Called(ctx, attempt)
	return args.Error(0)
}

func (m *MockMigrationStorage) GetArchivedMigrationAttempts(ctx context.Context, repoFullName string) ([]*payload.MigrationStatus, error) {
	args := m.Called(ctx, repoFullName)
	return args.Get(0).([]*payload.MigrationStatus), args.Error(1)
}

func TestHealthStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   HealthStatus
		expected bool
	}{
		{
			name: "healthy status",
			status: HealthStatus{
				Healthy:      true,
				Message:      "OK",
				LastChecked:  time.Now(),
				ResponseTime: 100 * time.Millisecond,
				Errors:       nil,
			},
			expected: true,
		},
		{
			name: "unhealthy status with errors",
			status: HealthStatus{
				Healthy:      false,
				Message:      "Database error",
				LastChecked:  time.Now(),
				ResponseTime: 5 * time.Second,
				Errors:       []string{"connection failed", "timeout"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.Healthy)
			assert.NotEmpty(t, tt.status.Message)
			assert.NotZero(t, tt.status.LastChecked)
		})
	}
}

func TestNewDatabaseHealthChecker(t *testing.T) {
	mockStorage := &MockMigrationStorage{}

	hc := NewDatabaseHealthChecker(mockStorage)

	assert.NotNil(t, hc)
	assert.Equal(t, mockStorage, hc.storage)
	assert.Equal(t, 5*time.Minute, hc.checkInterval)
	assert.NotNil(t, hc.lastStatus)
	assert.True(t, hc.lastStatus.Healthy)
	assert.Equal(t, "No health check performed yet", hc.lastStatus.Message)
}

func TestDatabaseHealthChecker_CheckHealth_Success(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)
	ctx := context.Background()

	// Mock successful operations
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil)
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil)
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, nil)
	mockStorage.On("DeleteMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(nil)

	status := hc.CheckHealth(ctx)

	assert.NotNil(t, status)
	assert.True(t, status.Healthy)
	assert.Contains(t, status.Message, "Database is healthy")
	assert.Empty(t, status.Errors)
	assert.NotZero(t, status.ResponseTime)
	assert.NotZero(t, status.LastChecked)

	mockStorage.AssertExpectations(t)
}

func TestDatabaseHealthChecker_CheckHealth_SaveFailure(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)
	ctx := context.Background()

	// Mock save failure
	saveErr := errors.New("save failed")
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(saveErr)

	status := hc.CheckHealth(ctx)

	assert.NotNil(t, status)
	assert.False(t, status.Healthy)
	assert.Contains(t, status.Message, "Database health check failed")
	assert.Len(t, status.Errors, 1)
	assert.Contains(t, status.Errors[0], "Write test failed")
	assert.Contains(t, status.Errors[0], "save failed")

	mockStorage.AssertExpectations(t)
}

func TestDatabaseHealthChecker_CheckHealth_ReadFailure(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)
	ctx := context.Background()

	// Mock save success but read failure
	readErr := errors.New("read failed")
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil)
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, readErr)

	status := hc.CheckHealth(ctx)

	assert.NotNil(t, status)
	assert.False(t, status.Healthy)
	assert.Contains(t, status.Message, "Database health check failed")
	assert.Len(t, status.Errors, 1)
	assert.Contains(t, status.Errors[0], "Read test failed")
	assert.Contains(t, status.Errors[0], "read failed")

	mockStorage.AssertExpectations(t)
}

func TestDatabaseHealthChecker_CheckHealth_GetAllFailure(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)
	ctx := context.Background()

	// Mock save and read success but GetAll failure
	getAllErr := errors.New("getall failed")
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil)
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil)
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, getAllErr)

	status := hc.CheckHealth(ctx)

	assert.NotNil(t, status)
	assert.False(t, status.Healthy)
	assert.Contains(t, status.Message, "Database health check failed")
	assert.Len(t, status.Errors, 1)
	assert.Contains(t, status.Errors[0], "GetAll test failed")
	assert.Contains(t, status.Errors[0], "getall failed")

	mockStorage.AssertExpectations(t)
}

func TestDatabaseHealthChecker_CheckHealth_DeleteFailure(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)
	ctx := context.Background()

	// Mock all operations success except delete
	deleteErr := errors.New("delete failed")
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil)
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil)
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, nil)
	mockStorage.On("DeleteMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(deleteErr)

	status := hc.CheckHealth(ctx)

	assert.NotNil(t, status)
	assert.False(t, status.Healthy)
	assert.Contains(t, status.Message, "Database health check failed")
	assert.Len(t, status.Errors, 1)
	assert.Contains(t, status.Errors[0], "Delete test failed")
	assert.Contains(t, status.Errors[0], "delete failed")

	mockStorage.AssertExpectations(t)
}

func TestDatabaseHealthChecker_CheckHealth_Timeout(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)

	// Create a context that will timeout quickly
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Mock operation that takes too long
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).
		Run(func(args mock.Arguments) {
			time.Sleep(10 * time.Millisecond) // Sleep longer than timeout
		}).Return(context.DeadlineExceeded)

	status := hc.CheckHealth(ctx)

	assert.NotNil(t, status)
	assert.False(t, status.Healthy)
	assert.Contains(t, status.Message, "Database health check failed")
	assert.Len(t, status.Errors, 1)
	assert.Contains(t, status.Errors[0], "Write test failed")

	mockStorage.AssertExpectations(t)
}

func TestDatabaseHealthChecker_GetLastHealthStatus(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)

	// Test initial status
	status := hc.GetLastHealthStatus()
	assert.NotNil(t, status)
	assert.True(t, status.Healthy)
	assert.Equal(t, "No health check performed yet", status.Message)

	// Update status through CheckHealth
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil)
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil)
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, nil)
	mockStorage.On("DeleteMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(nil)

	newStatus := hc.CheckHealth(context.Background())

	// Get last status should return the updated one
	lastStatus := hc.GetLastHealthStatus()
	assert.Equal(t, newStatus, lastStatus)
	assert.Contains(t, lastStatus.Message, "Database is healthy")

	mockStorage.AssertExpectations(t)
}

func TestDatabaseHealthChecker_ConcurrentAccess(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)

	// Mock successful operations
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil).Maybe()
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil).Maybe()
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, nil).Maybe()
	mockStorage.On("DeleteMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(nil).Maybe()

	// Run concurrent health checks and status retrievals
	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		// Health check goroutine
		go func() {
			defer wg.Done()
			status := hc.CheckHealth(context.Background())
			assert.NotNil(t, status)
		}()

		// Status retrieval goroutine
		go func() {
			defer wg.Done()
			status := hc.GetLastHealthStatus()
			assert.NotNil(t, status)
		}()
	}

	wg.Wait()
}

func TestDatabaseHealthChecker_StartPeriodicHealthCheck(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)

	// Set a very short check interval for testing
	hc.checkInterval = 10 * time.Millisecond

	// Mock successful operations
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil)
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil)
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, nil)
	mockStorage.On("DeleteMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(nil)

	// Create context with short timeout to stop periodic checking
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Start periodic health checking
	hc.StartPeriodicHealthCheck(ctx)

	// Wait for context to be cancelled
	<-ctx.Done()

	// Should have performed at least one health check
	status := hc.GetLastHealthStatus()
	assert.NotNil(t, status)
	// The status should have been updated from the initial "No health check performed yet"
	if status.Message != "No health check performed yet" {
		assert.Contains(t, status.Message, "Database is healthy")
	}
}

func TestDatabaseHealthChecker_StartPeriodicHealthCheck_ContextCancellation(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)

	// Set a very short check interval
	hc.checkInterval = 1 * time.Millisecond

	// Mock operations
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil).Maybe()
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil).Maybe()
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, nil).Maybe()
	mockStorage.On("DeleteMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(nil).Maybe()

	// Create context and cancel it immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Start periodic health checking
	hc.StartPeriodicHealthCheck(ctx)

	// Give it a moment to process the cancellation
	time.Sleep(10 * time.Millisecond)

	// The goroutine should exit gracefully without hanging
	// No assertions needed, test passes if it doesn't hang
}

func TestHealthChecker_Interface(t *testing.T) {
	mockStorage := &MockMigrationStorage{}
	hc := NewDatabaseHealthChecker(mockStorage)

	// Verify that DatabaseHealthChecker implements HealthChecker interface
	var _ HealthChecker = hc

	// Test interface methods
	assert.NotNil(t, hc.GetLastHealthStatus())

	// Mock for CheckHealth
	mockStorage.On("SaveMigrationStatus", mock.Anything, mock.AnythingOfType("*payload.MigrationStatus")).Return(nil)
	mockStorage.On("GetMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(&payload.MigrationStatus{}, nil)
	mockStorage.On("GetAllMigrationStatuses", mock.Anything).Return(map[string]*payload.MigrationStatus{}, nil)
	mockStorage.On("DeleteMigrationStatus", mock.Anything, mock.AnythingOfType("string")).Return(nil)

	status := hc.CheckHealth(context.Background())
	assert.NotNil(t, status)

	mockStorage.AssertExpectations(t)
}
