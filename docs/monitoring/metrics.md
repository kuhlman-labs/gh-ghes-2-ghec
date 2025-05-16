# Metrics with Prometheus

The migration server includes comprehensive metrics to help monitor system health, performance, and migration progress.

## Configuration

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

## Available Metrics

The server exposes the following metrics categories:

### Migration Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `ghghe2ec_migrations_total` | Counter | Total number of migration operations | `source_org`, `target_org`, `status` |
| `ghghe2ec_migration_duration_seconds` | Histogram | Duration of migration operations | `source_org`, `target_org`, `stage`, `status` |
| `ghghe2ec_migration_size_bytes` | Histogram | Size of migrated repositories | `source_org`, `target_org` |

### HTTP Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `ghghe2ec_http_requests_total` | Counter | Total number of HTTP requests | `handler`, `method`, `status` |
| `ghghe2ec_http_request_duration_seconds` | Histogram | Duration of HTTP requests | `handler`, `method` |

### GitHub API Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `ghghe2ec_github_api_requests_total` | Counter | Total number of GitHub API requests | `api`, `endpoint`, `status` |
| `ghghe2ec_github_api_request_duration_seconds` | Histogram | Duration of GitHub API requests | `api`, `endpoint` |
| `ghghe2ec_github_rate_limit_remaining` | Gauge | Number of GitHub API rate limit calls remaining | `api` |

### System Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `ghghe2ec_goroutines` | Gauge | Number of active goroutines | - |
| `ghghe2ec_memory_alloc_bytes` | Gauge | Memory allocation metrics | - |

### Storage Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `ghghe2ec_storage_operations_total` | Counter | Total number of storage operations | `operation`, `status` |
| `ghghe2ec_storage_operation_duration_seconds` | Histogram | Duration of storage operations | `operation` |

## Example Queries

### Migration Success Rate
```promql
sum(rate(ghghe2ec_migrations_total{status="success"}[1h])) / sum(rate(ghghe2ec_migrations_total[1h]))
```

### Average Migration Duration
```promql
sum(rate(ghghe2ec_migration_duration_seconds_sum{stage="overall"}[1h])) / sum(rate(ghghe2ec_migration_duration_seconds_count{stage="overall"}[1h]))
```

### API Error Rate
```promql
sum(rate(ghghe2ec_github_api_requests_total{status=~"5.*"}[5m])) / sum(rate(ghghe2ec_github_api_requests_total[5m])) 
```

### Rate Limit Monitoring
```promql
min(ghghe2ec_github_rate_limit_remaining) by (api)
```

## Integration with Prometheus

### Prometheus Configuration

Add this job to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'gh-ghes-2-ghec'
    scrape_interval: 15s
    static_configs:
      - targets: ['migration-server:8080']
    metrics_path: /metrics
```

If you're using a dedicated metrics port:

```yaml
scrape_configs:
  - job_name: 'gh-ghes-2-ghec'
    scrape_interval: 15s
    static_configs:
      - targets: ['migration-server:9090']
``` 