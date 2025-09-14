# Testing Guide

This document describes the comprehensive testing setup and best practices for the gh-ghes-2-ghec project.

## Overview

The project uses a multi-layered testing strategy with the following test types:

- **Unit Tests**: Fast, isolated tests that don't require external dependencies
- **Integration Tests**: Tests that use real database containers and external services
- **End-to-End Tests**: Complete workflow tests that simulate real migration scenarios
- **Load Tests**: Performance and scalability tests under various load conditions
- **Security Tests**: Vulnerability scanning and security compliance testing
- **Performance Regression Tests**: Automated performance monitoring and baseline comparison

## Test Organization

### Directory Structure

```
test/
├── config/                 # Test configuration files
│   └── test_config.yaml    # Main test configuration
├── helpers/                # Common test utilities and helpers
│   ├── test_utils.go       # Test utility functions and TestSuite
│   └── test_utils_test.go  # Tests for the test utilities
├── integration/            # Integration tests
├── e2e/                    # End-to-end tests
│   └── migration_e2e_test.go
└── load/                   # Load and performance tests

internal/                   # Unit tests alongside source code
├── config/config_test.go
├── github/api_test.go
├── logging/
│   ├── logging_test.go
│   └── structured_test.go
├── migrator/              # Migration logic tests
├── payload/payload_test.go
├── sanitization/sanitization_test.go
├── server/server_test.go
├── storage/
│   ├── storage_test.go
│   ├── sqlite_test.go
│   └── pool_test.go
├── validation/validation_test.go
└── version/version_test.go

cmd/                       # CLI command tests
├── config_test.go
├── root_test.go
└── validate_test.go
```

## Running Tests

### Using Make Targets

```bash
# Run all tests with automatic container cleanup
make test

# Run only unit tests (fast, no external dependencies)
make test-unit

# Run integration tests with container management
make test-integration

# Run tests with coverage report (generates coverage.html)
make test-coverage

# Run tests in CI environment with race detection
make test-ci

# Clean up test containers and resources
make test-clean
```

### Manual Test Execution

```bash
# Clean up first
make test-clean

# Run specific test packages
go test -v ./internal/storage/...
go test -v ./test/integration/...

# Run with specific flags
go test -v -timeout=30m -race ./...
go test -v -short ./...  # Skip integration tests

# Clean up after
make test-clean
```

## Test Configuration

### Main Test Configuration

Test configuration is centralized in `test/config/test_config.yaml`:

```yaml
# Unit Test Configuration
unit:
  coverage_threshold: 90.0
  timeout: 5m
  parallel: true
  short: true

# Integration Test Configuration  
integration:
  timeout: 20m
  database:
    sqlite:
      enabled: true
      path: ":memory:"
    postgres:
      enabled: true
      container:
        startup_timeout: 180s
        health_check_timeout: 60s
        connection_retry_attempts: 5
    mysql:
      enabled: true
      container:
        startup_timeout: 240s
        health_check_timeout: 90s
        connection_retry_attempts: 5
  containers:
    cleanup_timeout: 60s
    termination_grace_period: 30s
    force_remove_after: 90s
    orphan_cleanup_enabled: true

# E2E, Load, Security, and Performance test configurations...
```

## Test Types and Standards

### Unit Tests

**Location**: Alongside source code in `internal/` and `cmd/` directories
**Naming**: `*_test.go` files
**Standards**:
- Must achieve 90% coverage threshold
- Should run in under 5 minutes
- Use table-driven tests where appropriate
- Mock external dependencies using interfaces
- Use `testing.Short()` for tests that can be skipped

**Example Pattern**:
```go
func TestMigration(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {
            name:    "valid migration",
            input:   "test-repo",
            want:    "success",
            wantErr: false,
        },
        // Add more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Integration Tests

**Location**: `test/integration/` directory
**Standards**:
- Use `helpers.NewTestSuite(t)` for setup
- Always defer cleanup functions
- Use appropriate timeouts for container operations
- Handle container lifecycle properly
- Test real database interactions

**TestSuite Usage**:
```go
func TestStorageIntegration(t *testing.T) {
    suite := helpers.NewTestSuite(t)
    defer suite.Cleanup()
    
    // Test implementation with real database
}
```

### End-to-End Tests

**Location**: `test/e2e/` directory
**Standards**:
- Test complete migration workflows
- Use mock GitHub API services
- Simulate real-world scenarios
- Test error handling and recovery
- Validate migration outcomes

### Load Tests

**Configuration**: Defined in `test_config.yaml`
**Standards**:
- Test with varying concurrent loads (1, 5, 10, 25, 50)
- Test different repository sizes (small, medium, large)
- Include ramp-up periods and sustained load testing
- Monitor resource usage and performance metrics

### Security Tests

**Types**:
- Dependency vulnerability scanning with `gosec`

**Running Security Tests**:
```bash
make sec  # Run gosec security scanner
```

## Testing Utilities and Helpers

### TestSuite Helper

The `helpers.TestSuite` provides comprehensive testing utilities:

```go
// Create a new test suite
suite := helpers.NewTestSuite(t)

// Create temporary directories
tempDir := suite.CreateTempDir()

// Setup GitHub API mocks
suite.SetupGitHubMocks()

// Create test configurations
configPath := suite.CreateTestConfig(overrides)

// Setup mock services
suite.SetupMockServices()

// Generate mock data
mockRepo := helpers.GenerateMockRepository()
```

### Mock Services

- **GitHub API Mocking**: Using `httpmock` for HTTP API mocking
- **Database Mocking**: Using `sqlmock` for database interaction testing
- **Container Services**: Using `testcontainers-go` for real container testing

## Best Practices

### For Developers

1. **Use Make Targets**: Always use `make test` instead of `go test` directly
2. **Container Cleanup**: Run `make test-clean` if containers get stuck
3. **Local Script**: Use `./scripts/local/test-local.sh` for better output and error handling
4. **Docker Dependency**: Ensure Docker is running before integration tests
5. **Resource Monitoring**: Watch for container accumulation during development
6. **Test Isolation**: Use unique identifiers and cleanup properly
7. **Coverage Standards**: Aim for >90% test coverage on new code
8. **Test Documentation**: Document complex test scenarios and edge cases

### For CI/CD

1. **CI-Specific Targets**: Use `make test-ci` in CI environments
2. **Container Cleanup**: Always clean up before and after test runs
3. **Timeout Configuration**: Use longer timeouts in CI (35m vs 25m locally)
4. **Resource Monitoring**: Monitor container accumulation and cleanup
5. **Environment Variables**: Set `CI=true` for CI-specific behavior
6. **Race Detection**: Enable race detection in CI environments
7. **Parallel Execution**: Configure appropriate parallelism for CI resources

### Writing Tests

1. **TestSuite Usage**: Use `helpers.NewTestSuite(t)` for integration tests
2. **Proper Cleanup**: Always defer cleanup functions
3. **Error Handling**: Test both success and failure scenarios
4. **Timeouts**: Use appropriate timeouts for container operations
5. **Mocking**: Mock external services for unit tests
6. **Table-Driven Tests**: Use table-driven tests for multiple scenarios
7. **Test Data**: Use the faker library for generating realistic test data
8. **Goroutine Leaks**: Use `goleak` to detect goroutine leaks

### Manual Cleanup

If tests leave containers running:

```bash
# Stop all test containers
docker container stop $(docker container ls -q --filter "label=test-suite=gh-ghes-2-ghec")

# Remove all test containers
docker container rm $(docker container ls -aq --filter "label=test-suite=gh-ghes-2-ghec")

# Clean up volumes and networks
docker volume prune -f
docker network prune -f

# Or use the make target
make test-clean
```

### Debugging Test Issues

```bash
# List test containers
docker container ls --filter "label=test-suite=gh-ghes-2-ghec"

# Check container logs
docker logs <container-id>

# Inspect container state
docker inspect <container-id>

# Monitor resource usage
docker stats --filter "label=test-suite=gh-ghes-2-ghec"

# Run specific test with debugging
go test -v -run TestSpecificTest ./internal/package/
```

### Performance Debugging

```bash
# Run tests with CPU profiling
go test -cpuprofile=cpu.prof ./...

# Run tests with memory profiling
go test -memprofile=mem.prof ./...

# Analyze profiles
go tool pprof cpu.prof
go tool pprof mem.prof

# Run benchmarks
go test -bench=. ./...
```

## Environment Variables

- `CI`: Set to enable CI-specific behavior (longer timeouts, different logging)
- `GITHUB_ACTIONS`: Automatically detected in GitHub Actions
- `DOCKER_HOST`: Override Docker daemon connection
- `SKIP_DB_CONNECTION_TEST`: Skip database connection testing for driver issues
- `TEST_TIMEOUT`: Override default test timeouts
- `TEST_VERBOSE`: Enable verbose test output

## Continuous Integration

### GitHub Actions Integration

The project includes GitHub Actions workflows that:
- Run all test types in parallel
- Generate and upload coverage reports
- Perform security scanning
- Run performance regression tests
- Clean up resources automatically

### Test Automation

- **Automatic Test Execution**: Tests run on every PR and push
- **Coverage Reporting**: Coverage reports are generated and tracked
- **Performance Monitoring**: Performance baselines are maintained and monitored
- **Security Scanning**: Regular security scans for vulnerabilities
- **Container Management**: Automatic cleanup of test containers in CI

## Advanced Testing Features

### Chaos Engineering

The project includes chaos engineering tests to validate system resilience:
- Database failure simulation
- GitHub API failure testing
- Network partition simulation
- Memory pressure testing
- Disk space exhaustion scenarios

### Load Testing Scenarios

- **Steady Load**: Sustained moderate load testing
- **Spike Load**: High burst load testing
- **Bulk Migration**: Testing with multiple simultaneous migrations
- **Resource Scaling**: Testing under various resource constraints

### Mock Data Generation

Comprehensive mock data generation for:
- Repository structures with realistic metadata
- GitHub API responses
- Migration status scenarios
- Error conditions and edge cases

## Future Improvements

- **Test Parallelization**: Enhanced parallel execution for faster test runs
- **Container Reuse**: Container reuse strategies for improved performance
- **Advanced Mocking**: More sophisticated GitHub API mocking scenarios
- **Test Result Caching**: Intelligent test result caching for faster feedback
- **Performance Monitoring**: Real-time performance monitoring and alerting
- **Automated Test Generation**: AI-assisted test case generation
- **Cross-Platform Testing**: Testing across different operating systems and architectures
