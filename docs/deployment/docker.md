# Docker Deployment

This guide covers deploying the GitHub GHES to GHEC Migration Server using Docker.

## Quick Start

```bash
# Pull the latest image
docker pull ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest

# Run with default configuration
docker run -p 8080:8080 ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

## Building the Docker Image

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

## Configuration Options

### Custom Port Mapping

You can map the container's port 8080 to any port on your host:

```bash
# Map to port 9000 on the host
docker run -p 9000:8080 ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

### Using a Custom Configuration File

To use your own configuration file:

```bash
# Mount your config.yaml into the container
docker run -p 8080:8080 \
  -v /path/to/your/config.yaml:/app/config.yaml \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

### Persistent Storage for Logs and Temporary Files

For persistent storage of logs and migration temporary files:

```bash
docker run -p 8080:8080 \
  -v /path/to/logs:/var/log \
  -v /path/to/temp:/tmp/migrations \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

### Running in Background

```bash
docker run -d --name ghes-migration-server \
  -p 8080:8080 \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

### Using Environment Variables

```bash
docker run -p 8080:8080 \
  -e SERVER_PORT=9000 \
  -e LOGGING_LEVEL=debug \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

## Monitoring and Tracing with Docker

### Exposing Prometheus Metrics

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

### Setting Up OpenTelemetry Tracing

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

## Docker Compose Examples

### Basic Setup

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

### Complete Monitoring Stack

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

### With Database Storage

For persistent storage with a database:

```yaml
version: '3'
services:
  migration-server:
    image: ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
    ports:
      - "8080:8080"
    environment:
      - STORAGE_ENABLED=true
      - STORAGE_TYPE=postgres
      - STORAGE_CONNECTION_STRING=postgres://user:password@postgres:5432/migrations
    depends_on:
      - postgres

  postgres:
    image: postgres:13
    environment:
      - POSTGRES_PASSWORD=password
      - POSTGRES_USER=user
      - POSTGRES_DB=migrations
    volumes:
      - postgres-data:/var/lib/postgresql/data

volumes:
  postgres-data:
```

## Environment Variables Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_PORT` | HTTP server port | `8080` |
| `SERVER_READ_TIMEOUT` | HTTP read timeout (seconds) | `15` |
| `SERVER_WRITE_TIMEOUT` | HTTP write timeout (seconds) | `15` |
| `SERVER_SHUTDOWN_TIMEOUT` | Graceful shutdown timeout (seconds) | `30` |
| `SERVER_RATE_LIMIT` | Requests per minute limit | `60` |
| `SERVER_DASHBOARD` | Enable web dashboard | `true` |
| `LOGGING_LEVEL` | Log level (debug, info, warn, error) | `info` |
| `LOGGING_FORMAT` | Log format (json, text) | `json` |
| `WEBHOOK_URL` | Global webhook URL | `""` |
| `WEBHOOK_TIMEOUT` | Webhook timeout (seconds) | `10` |
| `WEBHOOK_MAX_RETRIES` | Maximum retry attempts | `3` |
| `METRICS_ENABLED` | Enable Prometheus metrics | `false` |
| `METRICS_PORT` | Dedicated metrics port (0 = use server port) | `0` |
| `METRICS_PATH` | Metrics endpoint path | `/metrics` |
| `TRACING_ENABLED` | Enable OpenTelemetry tracing | `false` |
| `TRACING_ENDPOINT` | OTLP gRPC endpoint | `localhost:4317` |
| `TRACING_SAMPLE_RATE` | Trace sampling rate (0.0-1.0) | `1.0` |
| `STORAGE_ENABLED` | Enable persistent storage | `false` |
| `STORAGE_TYPE` | Storage type (sqlite, mysql, postgres) | `sqlite` |
| `STORAGE_CONNECTION_STRING` | Database connection string | `migrations.db` |

## Resource Requirements

Recommended container resources:

- **CPU**: 2 cores minimum, 4 cores recommended for production
- **Memory**: 2GB minimum, 4GB recommended for production
- **Disk**: 10GB minimum for temporary files and logs

Adjust these based on your expected migration volume and repository sizes. 