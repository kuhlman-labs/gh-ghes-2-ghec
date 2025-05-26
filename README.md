# GitHub GHES to GHEC Migration Server

A server application that provides an HTTP API for migrating repositories from GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC). The server handles repository migrations asynchronously, provides real-time status updates, and supports webhook notifications for migration progress.

## Table of Contents
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Documentation](#documentation)
- [Dashboard](#dashboard)
- [License](#license)

## Features

- API for initiating and monitoring migrations
- Real-time status tracking and progress updates via webhooks
- Web dashboard for visualizing migration status
- Comprehensive logging and monitoring
- Automatic retry mechanism for API calls
- Failed migration retry functionality via both API and UI
- Concurrent migration support
- Works with GHOS based migrations
- Smart queueing system with priority-based processing
- Respects GitHub's concurrency limits (5 archives, 10 migrations)
- Queue statistics and metrics via Prometheus

## Migration Dashboard

![Migration Dashboard](docs/images/dashboard.png)

The GitHub GHES to GHEC Migration Server includes a comprehensive web-based dashboard that provides complete visibility and control over your migration process. Access the dashboard at `/dashboard` to monitor and manage all your migrations.

### Key Features

- **Migration Overview**: Real-time monitoring of active migrations with progress tracking and queue statistics
- **Advanced Analytics**: Interactive charts showing migration trends, success rates, and performance metrics  
- **Migration Wizard**: Guided setup for new migrations with connection testing and repository selection
- **Migration History**: Complete searchable record with filtering, sorting, and export capabilities
- **Error Dashboard**: Centralized error tracking and categorization for troubleshooting

For detailed information about dashboard features and usage, see the [Migration Dashboard Guide](docs/dashboard.md).

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

See [Docker Deployment](docs/deployment/docker.md) for detailed information.
</details>

## Configuration

<details>
<summary>Basic Configuration</summary>

Create or modify a `config.yaml` file in the root directory. See the [Configuration Guide](docs/configuration.md) for full details.

Basic example:
```yaml
server:
  port: 8080
  dashboard: true
  
webhook:
  url: "https://your-webhook-url"

logging:
  level: "info"

metrics:
  enabled: true
  path: "/metrics"
```
</details>

## Documentation

Detailed documentation is available in the `docs/` directory:

- **Monitoring & Observability**
  - [Metrics with Prometheus](docs/monitoring/metrics.md)
  - [Distributed Tracing with OpenTelemetry](docs/monitoring/tracing.md)
  - [Grafana Dashboards](docs/monitoring/dashboards.md)
  - [Alerting](docs/monitoring/alerting.md)

- **Deployment Options**
  - [Docker Deployment](docs/deployment/docker.md)

- **API & Integration**
  - [API Reference](docs/api/reference.md)
  - [Webhooks](docs/webhooks.md)

- **Storage & Management**
  - [Storage Options](docs/storage.md)
  - [Queue Implementation](docs/queue.md)

## License

MIT 