# Alerting

The migration server provides Prometheus alerting rules to help monitor system health and migration progress.

## Prometheus Alerting Rules

The project includes a set of recommended Prometheus alerting rules in `docs/alerting/prometheus-alerts.yml`.

## Available Alert Rules

These alerts are designed to notify you of potential issues:

### API Health Alerts

| Alert Name | Description | Threshold | Severity |
|------------|-------------|-----------|----------|
| HighErrorRate | API error rate exceeds threshold | > 5% over 5m | warning |
| APILatencyHigh | API response time exceeds threshold | > 1s over 5m | warning |
| HTTPStatusErrors | Elevated HTTP 5xx errors | > 1% over 5m | critical |
| EndpointDown | API endpoint is unreachable | Unreachable for 1m | critical |

### Migration Alerts

| Alert Name | Description | Threshold | Severity |
|------------|-------------|-----------|----------|
| MigrationFailureRate | Migration failures exceed threshold | > 10% over 30m | warning |
| LongRunningMigration | Migration taking longer than expected | > 3h | warning |
| StuckMigration | Migration stuck in same stage | > 1h no progress | critical |
| RepoSizeTooLarge | Repository exceeds size threshold | > 10GB | warning |

### Resource Alerts

| Alert Name | Description | Threshold | Severity |
|------------|-------------|-----------|----------|
| HighMemoryUsage | Memory usage exceeds threshold | > 85% for 5m | warning |
| HighCPUUsage | CPU usage exceeds threshold | > 90% for 5m | warning |
| TooManyGoroutines | Goroutine count exceeds threshold | > 1000 for 5m | warning |
| DiskSpaceLow | Available disk space low | < 10% for 5m | critical |

### GitHub API Limits

| Alert Name | Description | Threshold | Severity |
|------------|-------------|-----------|----------|
| GitHubRateLimitNearExhaustion | Rate limits approaching exhaustion | < 10% remaining | warning |
| GitHubRateLimitExceeded | Rate limits exceeded | 0 remaining | critical |

## Setting Up Alerts

### Prometheus Configuration

Add these alert rules to your Prometheus configuration:

1. Save the alert rules to a file (e.g., `prometheus-alerts.yml`):
```yaml
groups:
- name: gh-ghes-2-ghec
  rules:
  - alert: HighErrorRate
    expr: sum(rate(ghghe2ec_http_requests_total{status=~"5.*"}[5m])) / sum(rate(ghghe2ec_http_requests_total[5m])) > 0.05
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High error rate detected"
      description: "Error rate is {{ $value | humanizePercentage }} for the last 5 minutes"

  - alert: MigrationFailureRate
    expr: sum(increase(ghghe2ec_migrations_total{status="failed"}[30m])) / sum(increase(ghghe2ec_migrations_total[30m])) > 0.1
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "High migration failure rate"
      description: "Migration failure rate is {{ $value | humanizePercentage }} for the last 30 minutes"

  - alert: GitHubRateLimitNearExhaustion
    expr: min(ghghe2ec_github_rate_limit_remaining) / 5000 < 0.1
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: "GitHub rate limit near exhaustion"
      description: "Only {{ $value | humanizePercentage }} of rate limit remaining for {{ $labels.api }}"

  - alert: LongRunningMigration
    expr: max(ghghe2ec_migration_duration_seconds_sum{stage="overall"} / ghghe2ec_migration_duration_seconds_count{stage="overall"}) > 10800
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "Long-running migration detected"
      description: "Migration running for more than 3 hours"

  - alert: HighMemoryUsage
    expr: ghghe2ec_memory_alloc_bytes / 1000000000 > 2
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High memory usage"
      description: "Memory usage is {{ $value | humanizeBytes }} for the last 5 minutes"
```

2. Include the alerts file in your Prometheus configuration:
```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

rule_files:
  - "prometheus-alerts.yml"

alerting:
  alertmanagers:
  - static_configs:
    - targets:
      - alertmanager:9093

scrape_configs:
  - job_name: 'gh-ghes-2-ghec'
    scrape_interval: 15s
    static_configs:
      - targets: ['migration-server:8080']
    metrics_path: /metrics
```

### AlertManager Configuration

Configure AlertManager to send notifications:

```yaml
# alertmanager.yml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 1h
  receiver: 'slack'

receivers:
- name: 'slack'
  slack_configs:
  - api_url: 'https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK'
    channel: '#alerts'
    title: "{{ .GroupLabels.alertname }}"
    text: "{{ range .Alerts }}{{ .Annotations.description }}\n{{ end }}"

- name: 'email'
  email_configs:
  - to: 'alerts@example.com'
    from: 'prometheus@example.com'
    smarthost: 'smtp.example.com:587'
    auth_username: 'smtp_user'
    auth_password: 'smtp_password'
```

## Docker Deployment

When using Docker Compose, add AlertManager to your stack:

```yaml
services:
  # ... other services ...

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./prometheus-alerts.yml:/etc/prometheus/prometheus-alerts.yml
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --web.enable-lifecycle

  alertmanager:
    image: prom/alertmanager:latest
    ports:
      - "9093:9093"
    volumes:
      - ./alertmanager.yml:/etc/alertmanager/alertmanager.yml
    command:
      - --config.file=/etc/alertmanager/alertmanager.yml
```

## Custom Alert Rules

You can create custom alerts for your specific needs:

1. Create a custom alerts file (e.g., `custom-alerts.yml`)
2. Add it to your Prometheus configuration
3. Restart Prometheus or use the reload API endpoint

Example custom alert:

```yaml
groups:
- name: custom
  rules:
  - alert: MigrationBacklog
    expr: sum(ghghe2ec_migrations_total{status="queued"}) > 10
    for: 15m
    labels:
      severity: warning
    annotations:
      summary: "Migration backlog detected"
      description: "{{ $value }} migrations have been queued for over 15 minutes"
``` 