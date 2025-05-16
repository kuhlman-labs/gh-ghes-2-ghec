# Distributed Tracing with OpenTelemetry

The migration server supports distributed tracing using the OpenTelemetry standard, providing detailed insights into request flows, migration operations, and performance bottlenecks.

## Configuration

Configure tracing in your `config.yaml`:

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

## Trace Coverage

The application traces the following operations:

### HTTP Requests and API Operations
- All incoming HTTP requests
- Route handling and middleware execution
- Response generation and status codes

### Migration Lifecycle
- Migration initiation and preparation
- Repository validation
- Archive creation and upload
- Migration execution
- Status updates and completion

### GitHub API Interactions
- API calls to GHES and GHEC
- Rate limit checking
- Error handling and retries

### Storage Operations
- File reads and writes
- Archive handling
- Database operations (when persistent storage is enabled)

### Webhook Deliveries
- Webhook preparation
- Delivery attempts
- Response handling

## Span Attributes

Spans include useful attributes such as:

| Attribute | Description | Example |
|-----------|-------------|---------|
| `repository_full_name` | Repository being migrated | `"source-org/repo1"` |
| `source_org` | Source organization | `"source-org"` |
| `target_org` | Target organization | `"target-org"` |
| `migration.id` | Migration identifier | `"mig_12345"` |
| `migration.stage` | Current migration stage | `"archive"` |
| `migration.status` | Migration status | `"in_progress"` |
| `http.status_code` | HTTP response code | `200` |
| `error.message` | Error description | `"Rate limit exceeded"` |
| `gh.api` | GitHub API type | `"ghes"` or `"ghec"` |
| `gh.endpoint` | GitHub API endpoint | `"/repos/org/repo"` |

## Integration with Backend Services

### OpenTelemetry Collector

We recommend using the OpenTelemetry Collector to receive, process, and export traces.

Example collector configuration (`otel-collector-config.yaml`):

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 1s
  resourcedetection:
    detectors: [env]

exporters:
  jaeger:
    endpoint: jaeger:14250
    tls:
      insecure: true
  zipkin:
    endpoint: http://zipkin:9411/api/v2/spans
  otlp/elastic:
    endpoint: apm-server:8200
    tls:
      insecure: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch, resourcedetection]
      exporters: [jaeger, zipkin, otlp/elastic]
```

### Jaeger UI

When using Jaeger for trace visualization, you can search for traces by service name (`gh-ghes-2-ghec`) or filter by attributes like:

- `repository_full_name`
- `migration.status`
- `http.status_code` (for error investigation)

## Trace Context Propagation

The application properly propagates trace context:

- From HTTP headers to internal processing
- To outgoing GitHub API requests
- To webhook deliveries
- Across goroutines for asynchronous operations

This enables end-to-end tracing across system boundaries.

## Docker Deployment with Tracing

To enable tracing in a Docker deployment:

```bash
docker run -p 8080:8080 \
  -e TRACING_ENABLED=true \
  -e TRACING_ENDPOINT=otel-collector:4317 \
  -e TRACING_SAMPLE_RATE=1.0 \
  --network monitoring-network \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
``` 