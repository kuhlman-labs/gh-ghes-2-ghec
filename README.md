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
github:
  ghes_token: "your-ghes-token"
  gh_cloud_token: "your-gh-cloud-token"
webhook:
  url: "https://your-webhook-url"
```

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
  "ghes_base_url": "https://github.example.com"
}
```

Note: `ghes_base_url` should be the base URL of your GitHub Enterprise Server instance (e.g., `https://github.example.com`) without any API paths.

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

## License

MIT 