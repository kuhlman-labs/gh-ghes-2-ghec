# API Reference

This document details the available API endpoints provided by the GitHub GHES to GHEC Migration Server.

## API Endpoints

### Start Migration

Initiates a new migration from GHES to GHEC.

```
POST /api/migrate
```

#### Request Body

```json
{
  "source_org": "source-organization",
  "target_org": "target-organization",
  "repositories": ["repo1", "repo2"],
  "ghes_base_url": "https://github.example.com",
  "ghes_token": "your-ghes-token",
  "gh_cloud_token": "your-gh-cloud-token",
  "max_duration": "24h",
  "use_ghos": true,
  "webhook": {
    "url": "https://your-webhook-url",
    "headers": {
      "X-Custom-Header": "value"
    }
  }
}
```

#### Field Descriptions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `source_org` | string | Yes | The source organization in GitHub Enterprise Server |
| `target_org` | string | Yes | The target organization in GitHub Enterprise Cloud |
| `repositories` | array | Yes | Array of repository names to migrate |
| `ghes_base_url` | string | Yes | Base URL of your GitHub Enterprise Server instance |
| `ghes_token` | string | Yes | Token for authenticating with GitHub Enterprise Server |
| `gh_cloud_token` | string | Yes | Token for authenticating with GitHub Enterprise Cloud |
| `max_duration` | string | No | Maximum duration for the migration (default: "24h") |
| `use_ghos` | boolean | No | When set to `true`, uses GitHub Owned Storage (GHOS) for migration archives |
| `webhook` | object | No | Custom webhook configuration for this migration only |

#### Response

```json
{
  "status": "accepted",
  "message": "Migration request accepted for 2 repositories",
  "timestamp": "2023-05-16T14:25:30Z",
  "request_id": "7f8d9e2a-1b3c-4d5e-6f7g-8h9i0j1k2l3m",
  "repositories": ["repo1", "repo2"]
}
```

#### Status Codes

| Status Code | Description |
|-------------|-------------|
| 202 | Migration request accepted |
| 400 | Invalid request parameters |
| 401 | Authentication failed |
| 409 | Migration already in progress for repository |
| 429 | Rate limit exceeded |
| 500 | Server error |

### Check Migration Status

Retrieves the status of a specific repository migration or all migrations.

#### For a specific repository

```
GET /api/status?repository=org/repo1
```

#### Response

```json
{
  "migration_id": "mig_123456789",
  "repository": "source-org/repo1",
  "source_org": "source-org",
  "target_org": "target-org",
  "status": "in_progress",
  "stage": "archive",
  "state": "uploading",
  "progress": 45,
  "started_at": "2023-05-16T14:25:30Z",
  "updated_at": "2023-05-16T14:30:45Z",
  "completed_at": null,
  "error": null,
  "url": "https://github.com/target-org/repo1"
}
```

#### For all repositories

```
GET /api/status
```

#### Response

```json
[
  {
    "migration_id": "mig_123456789",
    "repository": "source-org/repo1",
    "source_org": "source-org",
    "target_org": "target-org",
    "status": "in_progress",
    "stage": "archive",
    "state": "uploading",
    "progress": 45,
    "started_at": "2023-05-16T14:25:30Z",
    "updated_at": "2023-05-16T14:30:45Z",
    "completed_at": null,
    "error": null,
    "url": null
  },
  {
    "migration_id": "mig_987654321",
    "repository": "source-org/repo2",
    "source_org": "source-org",
    "target_org": "target-org",
    "status": "completed",
    "stage": "complete",
    "state": "done",
    "progress": 100,
    "started_at": "2023-05-16T14:20:00Z",
    "updated_at": "2023-05-16T14:35:10Z",
    "completed_at": "2023-05-16T14:35:10Z",
    "error": null,
    "url": "https://github.com/target-org/repo2"
  }
]
```

#### Status Codes

| Status Code | Description |
|-------------|-------------|
| 200 | Success |
| 404 | Repository migration not found |
| 500 | Server error |

### Health Check

Returns the health status of the server.

```
GET /api/healthz
```

#### Response

```json
{
  "status": "ok"
}
```

#### Status Codes

| Status Code | Description |
|-------------|-------------|
| 200 | Server is healthy |
| 503 | Server is unhealthy |

## Migration Status Details

### Status Values

| Status | Description |
|--------|-------------|
| `queued` | Migration is queued but not yet started |
| `in_progress` | Migration is currently in progress |
| `completed` | Migration has completed successfully |
| `failed` | Migration has failed |
| `cancelled` | Migration was manually cancelled |

### Stage Values

| Stage | Description |
|-------|-------------|
| `validation` | Validating repository and permissions |
| `init` | Initializing migration |
| `archive` | Creating and transferring repository archive |
| `import` | Importing repository into target organization |
| `cleanup` | Performing post-migration cleanup |
| `complete` | Migration process completed |

### State Values

The `state` field provides more detailed information about the current activity within a stage:

| Stage | Possible States |
|-------|----------------|
| `validation` | `checking_source`, `checking_target`, `validating_permissions` |
| `init` | `starting`, `creating_migration_source` |
| `archive` | `creating_archive`, `uploading`, `verifying` |
| `import` | `importing`, `processing_import`, `importing_issues` |
| `cleanup` | `cleaning_temp_files`, `finalizing` |
| `complete` | `done`, `failed`, `cancelled` |

## Error Handling

When a migration fails, the `error` field in the status response will contain details:

```json
{
  "migration_id": "mig_123456789",
  "repository": "source-org/repo1",
  "status": "failed",
  "error": {
    "code": "rate_limit_exceeded",
    "message": "GitHub API rate limit exceeded for GHES",
    "details": "Rate limit will reset at 2023-05-16T15:30:00Z",
    "timestamp": "2023-05-16T14:45:30Z"
  }
}
```

### Common Error Codes

| Error Code | Description |
|------------|-------------|
| `invalid_token` | GitHub token is invalid or has insufficient permissions |
| `repo_not_found` | Repository not found in source organization |
| `permission_denied` | Insufficient permissions to access repository |
| `rate_limit_exceeded` | GitHub API rate limit exceeded |
| `archive_too_large` | Repository archive exceeds maximum size |
| `timeout` | Migration operation timed out |
| `internal_error` | Internal server error |

## Rate Limiting

The API enforces rate limits to prevent abuse and ensure service stability. Rate limits are applied per client IP address.

The default rate limit is 60 requests per minute. This can be configured in the server configuration.

When a rate limit is exceeded, the server returns a 429 Too Many Requests response with a Retry-After header indicating how many seconds to wait before retrying.

```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 30

{
  "status": "error",
  "message": "Rate limit exceeded",
  "code": 429,
  "timestamp": "2023-05-16T14:45:30Z",
  "request_id": "7f8d9e2a-1b3c-4d5e-6f7g-8h9i0j1k2l3m"
}
``` 