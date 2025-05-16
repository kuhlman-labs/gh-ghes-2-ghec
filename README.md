# GitHub GHES to GHEC Migration Server

A server application that provides an HTTP API for migrating repositories from GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC). The server handles repository migrations asynchronously, provides real-time status updates, and supports webhook notifications for migration progress.

## Table of Contents
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Monitoring and Observability](#monitoring-and-observability)
  - [Metrics with Prometheus](#metrics-with-prometheus)
  - [Distributed Tracing with OpenTelemetry](#distributed-tracing-with-opentelemetry)
  - [Dashboards](#dashboards)
  - [Alerting](#alerting)
- [Usage](#usage)
  - [Docker Deployment](#docker-deployment)
  - [Migration Process](#migration-process)
- [Dashboard](#dashboard)
- [API Reference](#api-reference)
- [Webhooks](#webhooks)
- [Storage Options](#storage-options)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)
- [Contributing](#contributing)
- [CI/CD](#ci-cd)
- [License](#license)

## Features

- API for initiating and monitoring migrations
- Real-time status tracking and progress updates via webhooks
- Web dashboard for visualizing migration status
- Comprehensive logging and monitoring
- Automatic retry mechanism for API calls
- Concurrent migration support
- Works with GHOS based migrations

## Dashboard

![Migration Dashboard](docs/images/dashboard.png)

The migration server includes a web-based dashboard for monitoring and managing migrations:

- **Migration Overview**: View all migration jobs with status summaries and progress indicators
- **Real-time Updates**: Get live status updates on migration progress
- **Detailed Progress**: Track each migration stage with visual progress indicators
- **Self-service Form**: Submit new migrations directly through the web UI without constructing API requests
- **Historical Data**: Access past migration records when storage is enabled

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
  dashboard: true               # Enable or disable the web dashboard UI
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
  dashboard: true

webhook:
  url: ""

logging:
  level: "info"

tracing:
  enabled: false
  endpoint: "localhost:4317"
  service_name: "gh-ghes-2-ghec"
  sample_rate: 1.0
  prometheus_metrics: false

metrics:
  enabled: true
  port: 0  # When set to 0, uses the same port as the server
  path: "/metrics"
  service_name: "gh-ghes-2-ghec"

storage:
  enabled: false
  type: "sqlite"
  connection_string: "migrations.db"
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
  --dashboard                  Enable the web dashboard UI (default true)
  --config string              Path to config file (default "config.yaml")
  --temp-dir string            Directory for temporary files
  --max-concurrent int         Maximum number of concurrent migrations
```
</details>

## Monitoring and Observability

The migration server includes comprehensive monitoring and observability features to help track system health, performance, and migration progress.

### Metrics with Prometheus

<details>
<summary>Configuration</summary>

The application can expose Prometheus metrics for monitoring. Configure this in your `config.yaml`:

```yaml
metrics:
  enabled: true                     # Enable Prometheus metrics collection
  port: 9090                        # Dedicated port for metrics endpoint (optional)
  path: "/metrics"                  # Metrics endpoint path
  service_name: "gh-ghes-2-ghec"   # Service name for metrics namespace
```

With this configuration:
- If `port` is specified, metrics will be exposed on a separate HTTP server on that port
- If no `port` is specified, metrics will be exposed on the main server at the configured `path`
- Setting `enabled: false` disables metrics collection entirely

You can also configure metrics using environment variables:
```bash
METRICS_ENABLED=true
METRICS_PORT=9090
METRICS_PATH=/metrics
METRICS_SERVICE_NAME=gh-ghes-2-ghec
```
</details>

<details>
<summary>Available Metrics</summary>

The server exposes the following metrics categories:

**Migration Metrics:**
- `ghghe2ec_migrations_total` - Total number of migration operations
- `ghghe2ec_migration_duration_seconds` - Duration of migration operations
- `ghghe2ec_migration_size_bytes` - Size of migrated repositories

**HTTP Metrics:**
- `ghghe2ec_http_requests_total` - Total number of HTTP requests
- `ghghe2ec_http_request_duration_seconds` - Duration of HTTP requests

**GitHub API Metrics:**
- `ghghe2ec_github_api_requests_total` - Total number of GitHub API requests
- `ghghe2ec_github_api_request_duration_seconds` - Duration of GitHub API requests
- `ghghe2ec_github_rate_limit_remaining` - Number of GitHub API rate limit calls remaining

**System Metrics:**
- `ghghe2ec_goroutines` - Number of active goroutines
- `ghghe2ec_memory_alloc_bytes` - Memory allocation metrics

**Storage Metrics:**
- `ghghe2ec_storage_operations_total` - Total number of storage operations
- `ghghe2ec_storage_operation_duration_seconds` - Duration of storage operations
</details>

### Distributed Tracing with OpenTelemetry

<details>
<summary>Configuration</summary>

The application supports distributed tracing with OpenTelemetry. Configure this in your `config.yaml`:

```yaml
tracing:
  enabled: true                      # Enable OpenTelemetry tracing
  endpoint: "localhost:4317"         # OTLP gRPC endpoint
  service_name: "gh-ghes-2-ghec"     # Service name for traces
  sample_rate: 1.0                   # Sampling rate (1.0 = 100% of traces)
  prometheus_metrics: true           # Export OpenTelemetry metrics to Prometheus
```

The tracing configuration can be controlled via environment variables:
```bash
TRACING_ENABLED=true
TRACING_ENDPOINT=localhost:4317
TRACING_SERVICE_NAME=gh-ghes-2-ghec
TRACING_SAMPLE_RATE=1.0
TRACING_PROMETHEUS_METRICS=true
```
</details>

<details>
<summary>Trace Coverage</summary>

The application traces the following operations:

- HTTP requests and responses
- Migration lifecycles (start to completion)
- GitHub API calls
- Storage operations
- Webhook deliveries

Spans include useful attributes such as:
- Operation status (success/failure)
- Repository names
- Source and target organizations
- Error details
- Operation sizes and durations
</details>

### Dashboards

<details>
<summary>Grafana Dashboard</summary>

The project includes a pre-configured Grafana dashboard in `docs/dashboards/migration-dashboard.json` that you can import into your Grafana instance.

The dashboard provides visualizations for:

- **Migration Statistics:**
  - Success and failure rates
  - Duration by stage
  - Repository sizes
  - Current migration status

- **API Metrics:**
  - GitHub API request rates
  - Rate limit usage
  - API error rates
  - API latency

- **System Metrics:**
  - Memory usage
  - Goroutine count
  - Request throughput
  - Error rates

To use the dashboard:
1. Install Grafana
2. Configure a Prometheus data source
3. Import the dashboard JSON file
4. (Optional) Customize panels as needed
</details>

### Alerting

<details>
<summary>Prometheus Alerting Rules</summary>

The project includes a set of recommended Prometheus alerting rules in `docs/alerting/prometheus-alerts.yml`.

Key alerts include:

- **HighErrorRate** - Triggers when the API error rate exceeds 5%
- **MigrationFailureRate** - Alerts when migration failures exceed threshold
- **GitHubRateLimitNearExhaustion** - Warns when rate limits are nearing exhaustion
- **LongRunningMigration** - Alerts on migrations taking longer than expected
- **HighMemoryUsage** - Monitors system resource consumption

To use these alerts:
1. Include the rules file in your Prometheus configuration
2. Configure alert manager for notifications
3. Adjust thresholds as appropriate for your environment
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
<summary>Monitoring and Tracing with Docker</summary>

#### Exposing Prometheus Metrics

To expose Prometheus metrics from the container:

```bash
# Expose metrics on the same port as the application
docker run -p 8080:8080 \
  -e METRICS_ENABLED=true \
  -e METRICS_PATH=/metrics \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest

# Expose metrics on a dedicated port
docker run -p 8080:8080 -p 9090:9090 \
  -e METRICS_ENABLED=true \
  -e METRICS_PORT=9090 \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

#### Setting Up OpenTelemetry Tracing

To enable distributed tracing with an external collector:

```bash
# Connect to an OpenTelemetry collector
docker run -p 8080:8080 \
  -e TRACING_ENABLED=true \
  -e TRACING_ENDPOINT=otel-collector:4317 \
  -e TRACING_SERVICE_NAME=gh-ghes-2-ghec \
  -e TRACING_SAMPLE_RATE=0.5 \
  --network monitoring-network \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

#### Complete Monitoring Stack with Docker Compose

Here's an example `docker-compose.yml` for a complete monitoring stack:

```yaml
version: '3'
services:
  migration-server:
    image: ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
    ports:
      - "8080:8080"
    environment:
      - METRICS_ENABLED=true
      - METRICS_PATH=/metrics
      - TRACING_ENABLED=true
      - TRACING_ENDPOINT=otel-collector:4317
    volumes:
      - ./config.yaml:/app/config.yaml
    depends_on:
      - otel-collector
      - prometheus

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./prometheus-alerts.yml:/etc/prometheus/alerts.yml
    command:
      - --config.file=/etc/prometheus/prometheus.yml

  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    ports:
      - "4317:4317"   # OTLP gRPC
    volumes:
      - ./otel-collector-config.yaml:/etc/otel-collector-config.yaml
    command:
      - --config=/etc/otel-collector-config.yaml

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning
      - ./docs/dashboards:/var/lib/grafana/dashboards
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
      - GF_AUTH_ANONYMOUS_ORG_ROLE=Admin
    depends_on:
      - prometheus
```

This setup provides:
- Prometheus for metrics collection
- OpenTelemetry Collector for distributed tracing
- Grafana for visualization with our pre-configured dashboards
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


The dashboard is accessible at `http://your-server:8080/dashboard` and can be enabled or disabled via configuration.

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

## Storage Options

<details>
<summary>Overview</summary>

The migration server provides options for persistent storage of migration states and data. By default, all migration data is stored in memory and will be lost when the server restarts. Enabling persistent storage allows the server to:

- Survive restarts while preserving migration state
- Maintain historical records of past migrations
- Provide migration statistics across server sessions
- Support the dashboard with historical data

Storage is entirely optional but recommended for production deployments.
</details>

<details>
<summary>Configuration Options</summary>

Storage can be configured in the config.yaml file:

```yaml
storage:
  enabled: true                           # Set to true to enable persistent storage
  type: "sqlite"                          # Options: sqlite, mysql, postgres
  connection_string: "migrations.db"      # SQLite: file path, MySQL/Postgres: connection string
  table_prefix: "ghes2ghec_"              # Optional prefix for database tables
```

### Storage Types

1. **SQLite** (default):
   - Lightweight file-based database
   - Suitable for small to medium deployments
   - Example: `connection_string: "migrations.db"`

2. **MySQL**:
   - More robust for higher concurrency
   - Example: `connection_string: "user:password@tcp(localhost:3306)/migrations"`

3. **PostgreSQL**:
   - Enterprise-grade database with advanced features
   - Example: `connection_string: "postgres://user:password@localhost:5432/migrations"`

### Table Prefix
The `table_prefix` parameter adds a prefix to all database table names. This is useful when:
- Using a shared database with other applications
- Running multiple instances of the migration server with the same database
- Implementing database sharding strategies
</details>

<details>
<summary>Data Persistence Behavior</summary>

When storage is enabled:

1. **Server Startup**:
   - The server loads all previously saved migration states from the database
   - Migrations that were in progress when the server last shut down remain in their last known state
   - The dashboard will display historical migration data

2. **During Operation**:
   - Each status change is persisted to the database in real-time
   - Migration metadata and progress information are stored

3. **Shutdown and Restart**:
   - In-flight migrations that didn't complete will be marked as failed after restart
   - Historical data remains available for reporting
</details>

<details>
<summary>Database Schema Management</summary>

The server automatically handles database schema creation and migrations:

- Tables are created if they don't exist
- Schema migrations are applied automatically
- No manual database setup is required

Example tables created:
- `migrations` - Core migration data
- `migration_events` - Historical events for each migration
- `repositories` - Repository-specific migration data
</details>

<details>
<summary>Backup Recommendations</summary>

For production deployments with storage enabled:

1. **SQLite**:
   - Regular file backups of the database file
   - Consider using an external volume with your container
   - Example Docker mount: `-v /path/to/data:/app/data`

2. **MySQL/PostgreSQL**:
   - Use standard database backup procedures
   - Consider point-in-time recovery options
   - Implement database replication where appropriate
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

### Dashboard UI

<details>
<summary>Click to expand</summary>

The migration server includes a web-based dashboard for visualizing migration status and progress. The dashboard is accessible at the `/dashboard` endpoint.

#### Features
- Overview of all migrations with status summaries
- Detailed view of individual migration progress
- Real-time updates using HTMX for a dynamic experience
- Visual progress indicators for each migration stage
- Works with or without persistent storage enabled

#### Accessing the Dashboard
By default, the dashboard is enabled and accessible at:
```
http://your-server:8080/dashboard
```

#### Configuration
You can enable or disable the dashboard in your configuration:

```yaml
server:
  dashboard: true  # Set to false to disable the dashboard
```

Or using the command line flag:
```bash
./gh-ghes-2-ghec --dashboard=false  # Disable the dashboard
```

#### Persistent Storage
The dashboard works without persistent storage enabled, using in-memory state. However, when storage is enabled, the dashboard will also display historical migration data loaded from the database even after server restarts.

#### Self-Service Migration Form
The dashboard includes a self-service migration form that allows users to:
- Submit new migrations directly from the web UI
- Specify source and target organizations
- Enter GitHub Enterprise Server and Cloud tokens
- Select repositories to migrate
- Configure migration options like GHOS and max duration

To use the form:
1. Navigate to `/dashboard/new` or click "New Migration" in the dashboard navigation
2. Fill out the required fields
3. Submit the form to start the migration process
4. You'll be redirected to the dashboard overview to monitor progress

This feature makes it easy for teams to initiate migrations without having to construct API requests manually.
</details> 