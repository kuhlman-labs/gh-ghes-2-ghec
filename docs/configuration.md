# Configuration Guide

This document details the configuration options for the GitHub GHES to GHEC Migration Server.

## Configuration File

The application uses a YAML configuration file (`config.yaml`) to control its behavior. A template (`config.yaml.template`) is provided and used to generate default configuration during Docker builds.

## Example Configuration

```yaml
server:
  port: 8080
  shutdown_timeout: 30
  read_timeout: 15
  write_timeout: 15
  rate_limit: 60
  dashboard: true
  max_concurrent_migrations: 5  # Maximum number of concurrent migrations
  temp_dir: "/tmp/migrations"   # Directory for temporary files

webhook:
  url: "https://your-webhook-url"   # Global webhook URL for all migration notifications
  timeout: 10                       # Webhook delivery timeout in seconds
  max_retries: 3                    # Maximum number of webhook delivery retries
  retry_backoff: 1.5                # Exponential backoff multiplier

logging:
  level: "info"                     # Logging level (debug, info, warn, error)
  format: "json"                    # Log format (json or text)
  output: "stdout"                  # Log output (stdout or file)
  file: "/var/log/migrations.log"   # Log file path (if output is file)

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
  table_prefix: "ghes2ghec_"
  retention_days: 30   # Keep migration data for 30 days (0 = forever)
  auto_prune: false    # Automatically remove old data
```

## Configuration Sections

### Server Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `port` | int | `8080` | HTTP server port |
| `shutdown_timeout` | int | `30` | Graceful shutdown timeout in seconds |
| `read_timeout` | int | `15` | HTTP read timeout in seconds |
| `write_timeout` | int | `15` | HTTP write timeout in seconds |
| `rate_limit` | int | `60` | Requests per minute limit (0 = unlimited) |
| `dashboard` | bool | `true` | Enable or disable the web dashboard UI |
| `max_concurrent_migrations` | int | `5` | Maximum number of concurrent migrations |
| `temp_dir` | string | `/tmp/migrations` | Directory for temporary files |

### Webhook Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `url` | string | `""` | Global webhook URL for all migration notifications |
| `timeout` | int | `10` | Webhook delivery timeout in seconds |
| `max_retries` | int | `3` | Maximum number of webhook delivery retries |
| `retry_backoff` | float | `1.5` | Exponential backoff multiplier |
| `headers` | map | `{}` | Custom headers to include in webhook requests |
| `secret` | string | `""` | Secret for signing webhook payloads |

### Logging Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `level` | string | `"info"` | Logging level (debug, info, warn, error) |
| `format` | string | `"json"` | Log format (json or text) |
| `output` | string | `"stdout"` | Log output (stdout or file) |
| `file` | string | `""` | Log file path (if output is file) |

### Tracing Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable OpenTelemetry tracing |
| `endpoint` | string | `"localhost:4317"` | OTLP gRPC endpoint |
| `service_name` | string | `"gh-ghes-2-ghec"` | Service name for traces |
| `sample_rate` | float | `1.0` | Sampling rate (1.0 = 100% of traces) |
| `prometheus_metrics` | bool | `false` | Export OpenTelemetry metrics to Prometheus |

### Metrics Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable Prometheus metrics |
| `port` | int | `0` | Dedicated metrics port (0 = use server port) |
| `path` | string | `"/metrics"` | Metrics endpoint path |
| `service_name` | string | `"gh-ghes-2-ghec"` | Service name for metrics namespace |

### Storage Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable persistent storage |
| `type` | string | `"sqlite"` | Storage type (sqlite, mysql, postgres) |
| `connection_string` | string | `"migrations.db"` | Database connection string |
| `table_prefix` | string | `"ghes2ghec_"` | Prefix for database tables |
| `retention_days` | int | `0` | Keep migration data for days (0 = forever) |
| `auto_prune` | bool | `false` | Automatically remove old data |

## Environment Variables

All configuration options can also be set via environment variables. The format is `SECTION_OPTION` in uppercase.

Examples:

```
SERVER_PORT=9000
LOGGING_LEVEL=debug
WEBHOOK_URL=https://your-webhook-url
METRICS_ENABLED=true
TRACING_ENABLED=false
STORAGE_ENABLED=true
STORAGE_TYPE=postgres
STORAGE_CONNECTION_STRING=postgres://user:password@host:5432/db
```

## Command Line Options

The most commonly used options can also be set via command line flags:

```bash
./gh-ghes-2-ghec [flags]

Flags:
  --port int                    Port to listen on (default 8080)
  --webhook-url string          Global webhook URL for all migration notifications
  --log-level string            Logging level (debug, info, warn, error) (default "info")
  --dashboard                   Enable the web dashboard UI (default true)
  --config string               Path to config file (default "config.yaml")
  --temp-dir string             Directory for temporary files
  --max-concurrent int          Maximum number of concurrent migrations
  --metrics-enabled             Enable Prometheus metrics
  --metrics-port int            Port for Prometheus metrics
  --storage-enabled             Enable persistent storage
  --storage-type string         Storage type (sqlite, mysql, postgres)
```

## Configuration Precedence

Configuration options are loaded in the following order of precedence (highest to lowest):

1. Command line flags
2. Environment variables
3. Configuration file
4. Default values

This means that command line flags will override environment variables, which will override the configuration file. 