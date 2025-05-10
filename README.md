# GitHub GHES to GHEC Migration Tool

A tool to migrate repositories from GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).

## Usage

### Configuration

Create a `config.yaml` file in the root directory with the following structure:

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

Note that GitHub tokens are provided in the migration request payload, while the webhook URL is configured globally in the config file or via the command line.

### Starting the Server

```bash
./gh-ghes-2-ghec
```

### API Endpoints

#### Start Migration

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
  "gh_cloud_token": "your-gh-cloud-token"
}
```

Note: `ghes_base_url` should be the base URL of your GitHub Enterprise Server instance (e.g., `https://github.example.com`) without any API paths. You must provide valid tokens for both GHES and GHEC.

Response:

```json
{
  "status": "migration started"
}
```

#### Check Migration Status

For a specific repository:

```
GET /status?repository=repo1
```

Response:

```json
{
  "repository": "repo1",
  "status": "in_progress",
  "updated_at": "2023-06-01T12:00:00Z"
}
```

For all repositories:

```
GET /status
```

Response:

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

#### Health Check

```
GET /health
```

Response:

```json
{
  "status": "healthy"
}
```

## Webhook Notifications

If configured, the tool will send webhook notifications when the status of a migration changes. The payload will be the same as the response from the status endpoint.

## JSON Payload Format

When making a migration request, you need to provide a JSON payload with the following format:

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