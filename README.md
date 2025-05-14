# GitHub GHES to GHEC Migration Server

A server application that provides an HTTP API for migrating repositories from GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC). The server handles repository migrations asynchronously, provides real-time status updates, and supports webhook notifications for migration progress.

## Table of Contents
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
  - [Docker Deployment](#docker-deployment)
  - [Migration Process](#migration-process)
- [API Reference](#api-reference)
- [Webhooks](#webhooks)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)
- [Contributing](#contributing)
- [CI/CD](#ci-cd)
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
- Works with GHOS based migrations

## Prerequisites

<details>
<summary>Click to expand</summary>

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
</details>

## Installation

<details>
<summary>From Source</summary>

1. Clone the repository:
```bash
git clone https://github.com/kuhlman-labs/gh-ghes-2-ghec.git
cd gh-ghes-2-ghec
```

2. Build the binary:
```bash
go build -o gh-ghes-2-ghec
```
</details>

<details>
<summary>Using the Makefile</summary>

The project includes a Makefile to simplify building and testing:

```bash
# Build the application
make build

# Run tests
make test

# Format the code
make fmt

# Run linter
make lint

# Build Docker image
make docker

# Run Docker container
make docker-run

# Show all available commands
make help
```
</details>

<details>
<summary>Using the GitHub CLI</summary>

```bash
gh extension install kuhlman-labs/gh-ghes-2-ghec
```
</details>

<details>
<summary>Using Docker</summary>

```bash
# Pull from GitHub Container Registry
docker pull ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest

# Run the container
docker run -p 8080:8080 ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest

# Or use a specific version
docker pull ghcr.io/kuhlman-labs/gh-ghes-2-ghec:v1.0.0
docker run -p 8080:8080 ghcr.io/kuhlman-labs/gh-ghes-2-ghec:v1.0.0
```
</details>

## Configuration

<details>
<summary>Server Configuration</summary>

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

The application uses a configuration template (`config.yaml.template`) that is used to generate the default configuration during the Docker build. This template includes these default settings:

```yaml
server:
  port: 8080
  shutdown_timeout: 30
  read_timeout: 15
  write_timeout: 15
  rate_limit: 60

webhook:
  url: ""

logging:
  level: "info"

tracing:
  enabled: false
  endpoint: "localhost:4317"
  service_name: "gh-ghes-2-ghec"
  sample_rate: 1.0
```
</details>

<details>
<summary>Command Line Options</summary>

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
</details>

## Usage

### Starting the Server

```bash
./gh-ghes-2-ghec
```

The server will start on the configured port and begin listening for migration requests.

### Docker Deployment

<details>
<summary>Building the Docker Image</summary>

To build the Docker image from source:

```bash
# Clone the repository
git clone https://github.com/kuhlman-labs/gh-ghes-2-ghec.git
cd gh-ghes-2-ghec

# Build the Docker image
docker build -t gh-ghes-2-ghec .

# Run the container
docker run -p 8080:8080 gh-ghes-2-ghec
```
</details>

<details>
<summary>Basic Usage</summary>

```bash
# Pull the latest image
docker pull ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest

# Run with default configuration
docker run -p 8080:8080 ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```
</details>

<details>
<summary>Configuration Options</summary>

#### Custom Port Mapping

You can map the container's port 8080 to any port on your host:

```bash
# Map to port 9000 on the host
docker run -p 9000:8080 ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

#### Using a Custom Configuration File

To use your own configuration file:

```bash
# Mount your config.yaml into the container
docker run -p 8080:8080 \
  -v /path/to/your/config.yaml:/app/config.yaml \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

#### Persistent Storage for Logs and Temporary Files

For persistent storage of logs and migration temporary files:

```bash
docker run -p 8080:8080 \
  -v /path/to/logs:/var/log \
  -v /path/to/temp:/tmp/migrations \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

#### Running in Background

```bash
docker run -d --name ghes-migration-server \
  -p 8080:8080 \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

#### Using Environment Variables

```bash
docker run -p 8080:8080 \
  -e SERVER_PORT=9000 \
  -e LOGGING_LEVEL=debug \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```
</details>

<details>
<summary>Docker Compose Example</summary>

Create a `docker-compose.yml` file:

```yaml
version: '3'
services:
  migration-server:
    image: ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
    # Or build from source
    # build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - ./logs:/var/log
      - ./temp:/tmp/migrations
    environment:
      - SERVER_PORT=8080
      - LOGGING_LEVEL=info
    restart: unless-stopped
```

Run with Docker Compose:

```bash
docker-compose up -d
```
</details>

### Validating Migration Requests

<details>
<summary>Click to expand</summary>

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
</details>

### Migration Process

<details>
<summary>Click to expand</summary>

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
</details>

## API Reference

<details>
<summary>Start Migration</summary>

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
</details>

<details>
<summary>Check Migration Status</summary>

For a specific repository:
```
GET /status?repository=repo1
```

For all repositories:
```
GET /status
```
</details>

<details>
<summary>Health Check</summary>

```
GET /health
```
</details>

## Webhooks

<details>
<summary>Overview and Benefits</summary>

Webhook notifications provide real-time updates about migration progress and status changes. They are particularly useful for:
- Monitoring migrations in a central dashboard
- Integrating with existing notification systems
- Triggering automated actions based on migration events
- Maintaining an audit trail of migration activities
</details>

<details>
<summary>Configuration Options</summary>

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
</details>

<details>
<summary>Common Use Cases</summary>

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
</details>

## Security

<details>
<summary>Click to expand</summary>

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
</details>

## Troubleshooting

<details>
<summary>Common Issues</summary>

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
</details>

<details>
<summary>Logging</summary>

Logs are written to:
- Standard output (with color-coded formatting)
- Rotating log files in `/tmp/gh-ghes-2-ghec/logs/`

Log files are automatically rotated when they reach 10MB, with up to 5 backup files kept for 30 days.
</details>

## Architecture

<details>
<summary>Components</summary>

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
</details>

<details>
<summary>Data Flow</summary>

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
</details>

## Contributing

Please see the [CONTRIBUTING.md](CONTRIBUTING.md) guide for details on how to contribute to this project.

## CI/CD

<details>
<summary>Docker Image Publishing</summary>

This project uses GitHub Actions for continuous integration and delivery:

The repository is configured to automatically build and publish Docker images to GitHub Container Registry (GHCR) when a new tag is pushed. The workflow:

1. Builds the Docker image with proper version information
2. Tags the image with both the specific version (e.g., v1.2.3) and the major.minor version (e.g., 1.2)
3. Pushes the tagged images to GitHub Container Registry

To create a new release:

```bash
# Tag a new version
git tag v1.2.3

# Push the tag to trigger the workflow
git push origin v1.2.3
```

Once the workflow completes, the image will be available at:
```
ghcr.io/kuhlman-labs/gh-ghes-2-ghec:1.2.3
ghcr.io/kuhlman-labs/gh-ghes-2-ghec:1.2
```
</details>

## License

MIT 