# Contributing to GHES to GHEC Migration Server

Thank you for your interest in contributing to the GHES to GHEC Migration Server! This document provides guidelines and instructions for contributing to the project.

## Table of Contents
- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Code Style](#code-style)
- [Testing](#testing)
- [Documentation](#documentation)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)

## Code of Conduct

By participating in this project, you agree to abide by our Code of Conduct. Please read it before contributing.

## Getting Started

### Prerequisites

- Go 1.21 or later
- Git
- Make (optional, for using Makefile commands)
- Docker (optional, for containerized development)

### Setting Up Your Development Environment

1. Fork the repository:
   ```bash
   # Clone your fork
   git clone https://github.com/YOUR_USERNAME/gh-ghes-2-ghec.git
   cd gh-ghes-2-ghec

   # Add upstream remote
   git remote add upstream https://github.com/kuhlman-labs/gh-ghes-2-ghec.git
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Set up pre-commit hooks (optional):
   ```bash
   # Install pre-commit
   pip install pre-commit

   # Install the hooks
   pre-commit install
   ```

## Development Workflow

### Branch Strategy

- `main`: Production-ready code
- `develop`: Integration branch for features
- Feature branches: `feature/your-feature-name`
- Bug fix branches: `fix/issue-description`
- Release branches: `release/vX.Y.Z`

### Creating a New Feature

1. Create a new branch from `develop`:
   ```bash
   git checkout develop
   git pull upstream develop
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and commit them:
   ```bash
   git add .
   git commit -m "feat: add new feature"
   ```

3. Push your branch:
   ```bash
   git push origin feature/your-feature-name
   ```

### Commit Message Format

We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Adding or modifying tests
- `chore`: Maintenance tasks

## Code Style

### Go Code Style

1. Follow the [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
2. Use `gofmt` for formatting
3. Run `golangci-lint` for linting:
   ```bash
   golangci-lint run
   ```

### Project Structure

```
.
├── .github/                # GitHub-specific files (workflows, templates)
├── cmd/                    # Command-line applications
│   ├── config.go          # Configuration handling
│   ├── root.go            # Root command implementation
│   └── validate.go        # Validation command implementation
├── internal/              # Private application code
│   ├── config/           # Configuration management
│   ├── github/           # GitHub API client and operations
│   ├── logging/          # Logging utilities
│   ├── migrator/         # Migration logic and operations
│   ├── payload/          # Request/response payloads
│   ├── server/           # HTTP server implementation
│   ├── utils/            # Utility functions
│   └── validation/       # Input validation logic
├── main.go               # Application entry point
├── config.yaml           # Default configuration file
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
├── LICENSE              # Project license
└── README.md            # Project documentation
```

Key directories and their purposes:

- `.github/`: Contains GitHub-specific configurations like workflows, issue templates, and PR templates
- `cmd/`: Command-line interface implementations
  - `config.go`: Handles configuration loading and validation
  - `root.go`: Main command implementation
  - `validate.go`: Migration request validation command
- `internal/`: Private application code
  - `config/`: Configuration management and validation
  - `github/`: GitHub API client implementation and operations
  - `logging/`: Logging utilities and configuration
  - `migrator/`: Core migration logic and operations
  - `payload/`: Request and response data structures
  - `server/`: HTTP server implementation and handlers
  - `utils/`: Shared utility functions
  - `validation/`: Input validation logic
- Root files:
  - `main.go`: Application entry point
  - `config.yaml`: Default configuration file
  - `go.mod` and `go.sum`: Go module definition and checksums
  - `LICENSE`: Project license
  - `README.md`: Project documentation

When adding new code:
1. Place command-line related code in `cmd/`
2. Put internal implementation details in `internal/`
3. Keep configuration in `config.yaml` or `internal/config/`
4. Add new GitHub API operations in `internal/github/`
5. Implement new migration features in `internal/migrator/`

## Testing

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test ./... -cover

# Run specific test
go test ./internal/migrator -run TestMigration

# Run integration tests
go test ./test/integration/...
```

### Writing Tests

1. Unit tests should be in the same package as the code they test
2. Integration tests should be in the `test/integration` directory
3. Use table-driven tests where appropriate
4. Mock external dependencies using interfaces

Example test:
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

## Documentation

### Code Documentation

1. Document all exported types, functions, and methods
2. Use [godoc](https://pkg.go.dev/golang.org/x/tools/cmd/godoc) style comments
3. Include examples where helpful

Example:
```go
// Migration represents a repository migration process.
// It handles the transfer of repositories from GHES to GHEC.
type Migration struct {
    // ... fields
}

// NewMigration creates a new Migration instance with the given configuration.
// It returns an error if the configuration is invalid.
func NewMigration(cfg *Config) (*Migration, error) {
    // ... implementation
}
```

### README Updates

1. Update the README.md when adding new features
2. Include examples of new functionality
3. Update configuration documentation
4. Add troubleshooting information if relevant

## Pull Request Process

1. Ensure your code follows the style guidelines
2. Add tests for new functionality
3. Update documentation
4. Run all tests and verify they pass
5. Create a pull request against the `develop` branch
6. Fill out the pull request template
7. Request review from maintainers

### Pull Request Template

```markdown
## Description
<!-- Describe your changes -->

## Related Issues
<!-- Link to related issues -->

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing performed

## Documentation
- [ ] README updated
- [ ] Code comments added/updated
- [ ] API documentation updated

## Checklist
- [ ] Code follows style guidelines
- [ ] Tests pass
- [ ] Documentation updated
- [ ] Changes are backward compatible
```

## Release Process

1. Create a release branch from `develop`:
   ```bash
   git checkout develop
   git pull upstream develop
   git checkout -b release/vX.Y.Z
   ```

2. Update version numbers and changelog:
   ```bash
   # Update version in go.mod
   go mod edit -go=1.21

   # Update CHANGELOG.md
   ```

3. Create a pull request to merge into `main`
4. After approval, tag the release:
   ```bash
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. Merge changes back to `develop`

## Additional Resources

- [GitHub Issues](https://github.com/kuhlman-labs/gh-ghes-2-ghec/issues)
- [Project Wiki](https://github.com/kuhlman-labs/gh-ghes-2-ghec/wiki)
- [Go Documentation](https://golang.org/doc/)
- [GitHub API Documentation](https://docs.github.com/en/rest) 