# Dashboards

The migration server includes a pre-configured Grafana dashboard for visualizing metrics and monitoring system health.

## Grafana Dashboard

The project includes a pre-configured Grafana dashboard in `docs/dashboards/migration-dashboard.json` that you can import into your Grafana instance.

![Migration Dashboard](../images/dashboard.png)

## Dashboard Features

The dashboard provides visualizations for:

### Migration Statistics

- **Success and Failure Rates:** View migration success percentages and trends
- **Duration by Stage:** Track how long each migration stage takes
- **Repository Sizes:** Monitor the distribution of repository sizes
- **Current Migration Status:** See at-a-glance status of in-progress migrations

### API Metrics

- **GitHub API Request Rates:** Monitor call volumes to GHES and GHEC
- **Rate Limit Usage:** Track remaining quota for GitHub API calls
- **API Error Rates:** Identify problematic API endpoints
- **API Latency:** Monitor performance of GitHub APIs

### System Metrics

- **Memory Usage:** Track application memory consumption
- **Goroutine Count:** Monitor concurrent execution trends
- **Request Throughput:** View HTTP request volume and throughput
- **Error Rates:** Catch increases in error rates immediately

## Using the Dashboard

### Installation

1. **Install Grafana:**
   - Follow the [Grafana installation instructions](https://grafana.com/docs/grafana/latest/installation/)
   - For Docker: `docker run -d -p 3000:3000 grafana/grafana`

2. **Configure a Prometheus Data Source:**
   - In Grafana, go to Configuration > Data Sources > Add data source
   - Select Prometheus
   - Set the URL to your Prometheus server (e.g. `http://prometheus:9090`)
   - Click "Save & Test" to verify the connection

3. **Import the Dashboard:**
   - In Grafana, go to Dashboards > Import
   - Upload the JSON file or paste the contents
   - Select your Prometheus data source
   - Click "Import"

### Dashboard Customization

The dashboard can be customized to fit your needs:

- **Time Range:** Adjust the time range in the top-right corner
- **Variables:** Use the dashboard variables to filter by organization or repository
- **Panels:** Drag, resize, or edit panels to customize the layout
- **Alerting:** Set up alerts on key metrics by clicking the bell icon on a panel

### Dashboard Sections

#### Migration Overview

The top row provides high-level statistics about migrations:
- Total migrations
- Success rate
- Average duration
- Active migrations

#### Repository Details

The second section shows repository-specific metrics:
- Size distribution
- Migration duration by repository
- Status by repository

#### GitHub API Performance

This section monitors GitHub API usage:
- Request rates by endpoint
- Error rates
- Rate limit consumption
- API latency

#### System Health

The bottom section monitors application health:
- Memory usage
- Goroutine count
- HTTP request rates
- Error rates

## Creating Your Own Dashboards

You can create additional dashboards using the exposed metrics:

1. In Grafana, click "Create Dashboard"
2. Add panels using Prometheus as the data source
3. Use PromQL queries to visualize metrics

Example queries:

```promql
# Migration success rate over time
sum(increase(ghghe2ec_migrations_total{status="success"}[1h])) / sum(increase(ghghe2ec_migrations_total[1h]))

# Average migration duration by organization
sum(rate(ghghe2ec_migration_duration_seconds_sum{stage="overall"}[5m])) by (source_org, target_org) / 
sum(rate(ghghe2ec_migration_duration_seconds_count{stage="overall"}[5m])) by (source_org, target_org)

# GitHub API error rate
sum(rate(ghghe2ec_github_api_requests_total{status=~"(4|5).*"}[5m])) / 
sum(rate(ghghe2ec_github_api_requests_total[5m]))
``` 