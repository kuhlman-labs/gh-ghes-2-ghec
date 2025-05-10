# GitHub GHES to GHEC Migration Server

A server application that provides an HTTP API for migrating repositories from GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC). The server handles repository migrations asynchronously, provides real-time status updates, and supports webhook notifications for migration progress.

## Features

- API for initiating and monitoring migrations
- Asynchronous processing of multiple repositories
- Real-time status tracking and progress updates
- Webhook notifications for migration events

## Prerequisites

- Go 1.21 or later
- Access to GitHub Enterprise Server (GHES) instance
- Access to GitHub Enterprise Cloud (GHEC) organization
- Valid GitHub tokens with appropriate permissions:
  - GHES token with `repo` and `admin:org` scopes
  - GHEC token with `repo` and `admin:org` scopes
- Network access to both GHES and GHEC APIs
- Port availability for the server (default: 8080)

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

## Usage

### Initial Setup

1. Initialize the configuration:
```bash
./gh-ghes-2-ghec config init
```

This will create a default `config.yaml` file in the current directory.

### Configuration

You can generate a config file in two ways:

1. Using the `config init` command:
```bash
./gh-ghes-2-ghec config init
```

Create or modify a `config.yaml` file in the root directory with the following structure:

<details>
<summary>config.yaml example</summary>

```yaml
server:
  port: 8080
  shutdown_timeout: 30
  read_timeout: 15
  write_timeout: 15
webhook:
  url: "https://your-webhook-url"   # Global webhook URL for all migration notifications
logging:
  level: "debug"
```
</details>

Note that GitHub tokens are provided in the migration request payload, while the webhook URL is configured globally in the config file or via the command line.

### Command Line Options

The server supports the following command line options:

```bash
./gh-ghes-2-ghec [flags]

Flags:
  --port int           Port to listen on (default 8080)
  --webhook-url string Global webhook URL for all migration notifications
  --log-level string   Logging level (debug, info, warn, error) (default "info")
```

### Input Validation and Security

You can validate migration requests before submitting them:

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

To run this migration, start the server and submit this JSON to the /migrate endpoint.
```

#### Security Features

- **Token Protection**: Tokens are sanitized in logs
- **Security Headers**: Standard security headers on all responses
- **Request Throttling**: Per-IP rate limiting to prevent abuse
- **Timeouts**: Configurable connection timeouts to prevent resource exhaustion

### Starting the Server

```bash
./gh-ghes-2-ghec
```

The server will start on the configured port (default: 8080) and begin listening for migration requests. It will handle graceful shutdown on SIGTERM or SIGINT signals.

### Server Architecture

The server is designed to handle multiple concurrent migrations with the following features:

- **Asynchronous Processing**: Migrations run in the background, allowing the API to remain responsive
- **Concurrent Migrations**: Multiple repositories can be migrated simultaneously
- **Status Tracking**: Real-time status updates for each migration
- **Webhook Integration**: Configurable webhook notifications for migration events
- **Graceful Shutdown**: Proper handling of in-progress migrations during shutdown
- **Automatic Retries**: Built-in retry mechanism with exponential backoff for API calls

### Logging

Logs are written to both:
- Standard output (with color-coded formatting)
- Rotating log files in the system's temp directory (`/tmp/gh-ghes-2-ghec/logs/`)

Log files are automatically rotated when they reach 10MB, with up to 5 backup files kept for 30 days.

### API Endpoints

#### Start Migration

```
POST /migrate
```

<details>
<summary>Request body example</summary>

```json
{
  "source_org": "source-organization",
  "target_org": "target-organization",
  "repositories": ["repo1", "repo2"],
  "ghes_base_url": "https://github.example.com",
  "ghes_token": "your-ghes-token",
  "gh_cloud_token": "your-gh-cloud-token",
  "use_ghos": true
}
```
</details>

Note: 
- `ghes_base_url` should be the base URL of your GitHub Enterprise Server instance (e.g., `https://github.example.com`) without any API paths.
- You must provide valid tokens for both GHES and GHEC.
- `use_ghos` (optional) enables GitHub Owned Storage for migration archives. When enabled, archives are uploaded directly to GitHub's storage service, which is required for some enterprises.

<details>
<summary>Response example</summary>

```json
{
  "status": "migration started"
}
```
</details>

#### Check Migration Status

For a specific repository:

```
GET /status?repository=repo1
```

<details>
<summary>Response example</summary>

```json
{
  "repository": "repo1",
  "status": "in_progress",
  "updated_at": "2023-06-01T12:00:00Z"
}
```
</details>

For all repositories:

```
GET /status
```

<details>
<summary>Response example</summary>

```json
{
  "repo1": {
    "repository": "repo1",
    "status": "in_progress",
    "updated_at": "2023-06-01T12:00:00Z"
  },
  "repo2": {
    "repository": "repo2",
    "status": "succeeded",
    "updated_at": "2023-06-01T12:05:00Z"
  }
}
```
</details>

#### Health Check

```
GET /health
```

<details>
<summary>Response example</summary>

```json
{
  "status": "healthy"
}
```
</details>

## Webhook Notifications

If configured, the tool will send webhook notifications when the status of a migration changes. The webhook payload includes detailed information about the migration progress, stages, and timing.

<details>
<summary>Webhook payload example</summary>

```json
{
  "repository": "example-repo",
  "status": "in_progress",
  "stage": "migration",
  "state": "IN_PROGRESS",
  "timestamp": "2024-03-20T15:30:45Z",
  "migration_id": "MDE2Ok1pZ3JhdGlvbjEyMzQ1Njc4OQ==",
  "details": {
    "stage_description": "Repository migration",
    "state_description": "Migration is in progress"
  },
  "source_org": "source-organization",
  "target_org": "target-organization",
  "started_at": "2024-03-20T15:25:30Z",
  "duration_seconds": 315,
  "duration_string": "5m15s",
  "progress": 75,
  "stage_progress": 70,
  "completed_stages": ["validation", "setup", "archive"],
  "total_stages": 4,
  "current_stage_index": 4
}
```
</details>

### Webhook Payload Fields

- `repository`: Name of the repository being migrated
- `status`: Overall migration status (`in_progress`, `succeeded`, or `failed`)
- `stage`: Current migration stage (`validation`, `setup`, `archive`, or `migration`)
- `state`: Current state within the stage
- `timestamp`: When the status update occurred
- `migration_id`: GitHub's migration ID (when available)
- `details`: Human-readable descriptions of the current stage and state
- `source_org`: Source organization in GHES
- `target_org`: Target organization in GHEC
- `started_at`: When the migration started
- `duration_seconds`: Total elapsed time in seconds
- `duration_string`: Human-readable duration
- `progress`: Overall progress percentage (0-100)
- `stage_progress`: Progress within current stage (0-100)
- `completed_stages`: List of completed migration stages
- `total_stages`: Total number of stages in the migration process
- `current_stage_index`: Index of current stage (1-based)

The webhook will be sent for each significant status change during the migration process, allowing you to track the progress in real-time.

## JSON Payload Format

When making a migration request, you need to provide a JSON payload with the following format:

<details>
<summary>Migration request payload example</summary>

```json
{
  "source_org": "your-ghes-org",
  "target_org": "your-ghec-org",
  "repositories": ["repo1", "repo2", "repo3"],
  "ghes_base_url": "https://github.example.com",
  "ghes_token": "your-ghes-token",
  "gh_cloud_token": "your-ghec-token",
  "max_duration": "24h"
}
```
</details>

### Field Descriptions:

- `source_org`: The source organization in GitHub Enterprise Server
- `target_org`: The target organization in GitHub Enterprise Cloud
- `repositories`: Array of repository names to migrate
- `ghes_base_url`: Base URL of your GitHub Enterprise Server instance
- `ghes_token`: Token for authenticating with GitHub Enterprise Server
- `gh_cloud_token`: Token for authenticating with GitHub Enterprise Cloud
- `max_duration` (optional): Maximum duration for the migration

This approach allows different tokens to be used for different migrations, enabling scenarios where you need to migrate from multiple GHES sources to different GHEC destinations.

### Webhook Configuration

Webhook notifications are configured through the `config.yaml` file or via the `--webhook-url` command line argument. This configuration applies globally to all migrations.

## License

MIT 