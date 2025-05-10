# GitHub GHES to GHEC Migration Server

A server application that provides an HTTP API for migrating repositories from GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC). The server handles repository migrations asynchronously, provides real-time status updates, and supports webhook notifications for migration progress.

## Table of Contents
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [API Reference](#api-reference)
- [Webhooks](#webhooks)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)
- [Contributing](#contributing)
- [License](#license)

## Features

- API for initiating and monitoring migrations
- Asynchronous processing of multiple repositories
- Real-time status tracking and progress updates
- Webhook notifications for migration events
- Support for large archives (>5GB) via GitHub Owned Storage (GHOS)
- Comprehensive logging and monitoring
- Graceful shutdown handling
- Automatic retry mechanism for API calls
- Concurrent migration support
- Progress tracking with detailed stage information
- Configurable timeouts and retry policies

## Prerequisites

- Go 1.21 or later
- Access to GitHub Enterprise Server (GHES) instance
- Access to GitHub Enterprise Cloud (GHEC) organization
- Valid GitHub tokens with appropriate permissions:
  - GHES token with `repo` and `admin:org` scopes
  - GHEC token with `repo` and `admin:org` scopes
- Network access to both GHES and GHEC APIs
- Port availability for the server (default: 8080)
- Sufficient disk space for temporary files
- Network bandwidth for repository transfers

## Installation

### From Source

1. Clone the repository:
```bash
git clone https://github.com/kuhlman-labs/gh-ghes-2-ghec.git
cd gh-ghes-2-ghec
```

2. Build the binary:
```bash
go build -o gh-ghes-2-ghec
```

### Using the GitHub CLI

```bash
gh extension install kuhlman-labs/gh-ghes-2-ghec
```

### Docker Installation

```bash
docker pull ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
docker run -p 8080:8080 ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

## Configuration

### Server Configuration

Create or modify a `config.yaml` file in the root directory:

```yaml
server:
  port: 8080
  shutdown_timeout: 30
  read_timeout: 15
  write_timeout: 15
  max_concurrent_migrations: 5  # Maximum number of concurrent migrations
  temp_dir: "/tmp/migrations"   # Directory for temporary files
webhook:
  url: "https://your-webhook-url"   # Global webhook URL for all migration notifications
  timeout: 10                       # Webhook delivery timeout in seconds
  max_retries: 3                    # Maximum number of webhook delivery retries
logging:
  level: "debug"                    # Logging level (debug, info, warn, error)
  format: "json"                    # Log format (json or text)
  output: "stdout"                  # Log output (stdout or file)
  file: "/var/log/migrations.log"   # Log file path (if output is file)
```

### Command Line Options

```bash
./gh-ghes-2-ghec [flags]

Flags:
  --port int                    Port to listen on (default 8080)
  --webhook-url string         Global webhook URL for all migration notifications
  --log-level string           Logging level (debug, info, warn, error) (default "info")
  --config string              Path to config file (default "config.yaml")
  --temp-dir string            Directory for temporary files
  --max-concurrent int         Maximum number of concurrent migrations
```

## Usage

### Starting the Server

```bash
./gh-ghes-2-ghec
```

The server will start on the configured port and begin listening for migration requests.

### Validating Migration Requests

Before submitting a migration request, you can validate it:

```bash
./gh-ghes-2-ghec validate migration.json

# With connection testing
./gh-ghes-2-ghec validate migration.json --test-connections
```

A successful validation will show:

```
✅ Migration request is valid!

Summary:
  Source Organization: source-org
  Target Organization: target-org
  GHES Instance: https://github.example.com
  Repositories: 5
  Maximum Duration: Default (24h)
```

### Migration Process

The migration process consists of several stages:

1. **Validation**
   - Verify repository existence
   - Check permissions
   - Validate configuration

2. **Setup**
   - Create migration source
   - Initialize migration environment
   - Prepare repositories

3. **Archive**
   - Generate migration archives
   - Upload to storage (GHOS or direct)
   - Verify archive integrity

4. **Migration**
   - Transfer repository data
   - Migrate metadata
   - Verify migration success

## API Reference

### Start Migration

```
POST /migrate
```

Request body:
```json
{
  "source_org": "source-organization",
  "target_org": "target-organization",
  "repositories": ["repo1", "repo2"],
  "ghes_base_url": "https://github.example.com",
  "ghes_token": "your-ghes-token",
  "gh_cloud_token": "your-gh-cloud-token",
  "max_duration": "24h",
  "use_ghos": true
}
```

Field Descriptions:
- `source_org`: The source organization in GitHub Enterprise Server
- `target_org`: The target organization in GitHub Enterprise Cloud
- `repositories`: Array of repository names to migrate
- `ghes_base_url`: Base URL of your GitHub Enterprise Server instance
- `ghes_token`: Token for authenticating with GitHub Enterprise Server
- `gh_cloud_token`: Token for authenticating with GitHub Enterprise Cloud
- `max_duration` (optional): Maximum duration for the migration
- `use_ghos` (optional): When set to `true`, uses GitHub Owned Storage (GHOS) for migration archives. This is required for some enterprises and enables handling of large archives (>5GB) through chunked uploads.

### Check Migration Status

For a specific repository:
```
GET /status?repository=repo1
```

For all repositories:
```
GET /status
```

### Health Check
```
GET /health
```

## Webhooks

Webhook notifications provide real-time updates about migration progress and status changes. They are particularly useful for:
- Monitoring migrations in a central dashboard
- Integrating with existing notification systems
- Triggering automated actions based on migration events
- Maintaining an audit trail of migration activities

### Configuration Options

Webhooks can be configured in two ways:

1. **Global Configuration** (applies to all migrations):
   ```yaml
   # config.yaml
   webhook:
     url: "https://your-webhook-url"
     timeout: 10  # seconds
     max_retries: 3
   ```

2. **Command Line** (overrides config file):
   ```bash
   ./gh-ghes-2-ghec --webhook-url="https://your-webhook-url"
   ```

### Common Use Cases

1. **Status Dashboard**:
   - Update a central dashboard with migration progress
   - Display real-time status of all migrations
   - Show historical migration data

2. **Notification Systems**:
   - Send Slack/Teams notifications for status changes
   - Email notifications for completed migrations
   - SMS alerts for critical failures

3. **Automation**:
   - Trigger post-migration tasks
   - Update inventory systems
   - Generate migration reports
   - Clean up temporary resources

4. **Audit Trail**:
   - Log all migration events
   - Track migration history
   - Generate compliance reports

## Security

### Token Protection
- Tokens are sanitized in logs
- Tokens are never stored persistently
- Each migration can use different tokens

### Security Headers
- Standard security headers on all responses
- CORS configuration for API access
- Rate limiting to prevent abuse

### Request Validation
- Comprehensive input validation
- Connection testing before migration
- Duration limits to prevent resource exhaustion

## Troubleshooting

### Common Issues

1. **Webhook Delivery Issues**:
   - Check webhook URL accessibility
   - Verify endpoint response codes
   - Check server logs for delivery attempts
   - Ensure endpoint can handle payload size
   - Verify network connectivity

2. **Migration Failures**:
   - Check token permissions
   - Verify repository access
   - Check network connectivity
   - Review server logs
   - Validate configuration

3. **Performance Issues**:
   - Monitor server resources
   - Check network latency
   - Review concurrent migration limits
   - Verify webhook processing times

### Logging

Logs are written to:
- Standard output (with color-coded formatting)
- Rotating log files in `/tmp/gh-ghes-2-ghec/logs/`

Log files are automatically rotated when they reach 10MB, with up to 5 backup files kept for 30 days.

## Architecture

### Components

1. **API Server**
   - Handles HTTP requests
   - Manages migration lifecycle
   - Provides status updates

2. **Migration Engine**
   - Coordinates migration process
   - Manages concurrent migrations
   - Handles retries and failures

3. **Storage Manager**
   - Handles archive storage
   - Manages GHOS integration
   - Handles chunked uploads

4. **Webhook System**
   - Manages webhook delivery
   - Handles retries
   - Provides delivery status

### Data Flow

1. **Migration Request**
   ```
   Client -> API Server -> Migration Engine
   ```

2. **Archive Process**
   ```
   Migration Engine -> GHES -> Storage Manager -> GHOS
   ```

3. **Migration Process**
   ```
   Storage Manager -> GHEC -> Migration Engine
   ```

4. **Status Updates**
   ```
   Migration Engine -> Webhook System -> External Systems
   ```

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `go test ./...`
5. Submit a pull request

### Testing

```bash
# Run all tests
go test ./...

# Run specific test
go test ./internal/migrator -run TestMigration

# Run with coverage
go test ./... -cover
```

## License

MIT 