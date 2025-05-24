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
  "delete_if_exists": false,
  "webhook": {
    "url": "https://your-webhook-url",
    "headers": {
      "X-Custom-Header": "value"
    }
  },
  "scheduled_time": "2023-06-15T02:00:00Z",
  "scheduled_time_zone": "America/New_York",
  "scheduled_days_only": ["Monday", "Wednesday", "Friday"],
  "scheduled_time_start": "22:00",
  "scheduled_time_end": "06:00"
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
| `delete_if_exists` | boolean | No | When set to `true`, deletes the repository in the target organization if it already exists |
| `webhook` | object | No | Custom webhook configuration for this migration only |
| `scheduled_time` | string | No | ISO8601 timestamp when the migration should be executed |
| `scheduled_time_zone` | string | No | Time zone for the scheduled migration (e.g., "America/New_York") |
| `scheduled_days_only` | array | No | Array of weekday names when migrations are allowed (e.g., ["Monday", "Wednesday", "Friday"]) |
| `scheduled_time_start` | string | No | Start time for the allowed migration window in 24-hour format (e.g., "22:00") |
| `scheduled_time_end` | string | No | End time for the allowed migration window in 24-hour format (e.g., "06:00") |

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
  "url": "https://github.com/target-org/repo1",
  "repository_size": 52428800,
  "size_category": "medium",
  "scheduled_time": "2023-06-15T02:00:00Z"
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

### Retry Failed Migration

Retries a failed repository migration.

```
POST /api/retry
```

#### Request Body

```json
{
  "repository": "source-org/repo1",
  "ghes_token": "your-ghes-token",
  "gh_cloud_token": "your-gh-cloud-token",
  "ghes_base_url": "https://github.example.com",
  "target_org": "target-org",
  "use_ghos": true,
  "delete_if_exists": false
}
```

#### Field Descriptions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repository` | string | Yes | Full repository name (org/repo) to retry |
| `ghes_token` | string | Yes | Token for authenticating with GitHub Enterprise Server |
| `gh_cloud_token` | string | Yes | Token for authenticating with GitHub Enterprise Cloud |
| `ghes_base_url` | string | No | Base URL of your GitHub Enterprise Server instance |
| `target_org` | string | No | The target organization in GitHub Enterprise Cloud |
| `use_ghos` | boolean | No | When set to `true`, uses GitHub Owned Storage (GHOS) for migration archives |
| `delete_if_exists` | boolean | No | When set to `true`, deletes the repository in the target organization if it already exists |

#### Response

```json
{
  "status": "accepted",
  "message": "Migration retry request accepted for source-org/repo1",
  "timestamp": "2023-05-16T14:25:30Z",
  "request_id": "7f8d9e2a-1b3c-4d5e-6f7g-8h9i0j1k2l3m",
  "repository": "source-org/repo1"
}
```

#### Status Codes

| Status Code | Description |
|-------------|-------------|
| 202 | Migration retry request accepted |
| 400 | Invalid request parameters |
| 401 | Authentication failed |
| 404 | Repository not found or not in failed state |
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
| `scheduled` | Migration is scheduled for future execution |
| `in_progress` | Migration is currently in progress |
| `completed` | Migration has completed successfully |
| `failed` | Migration has failed |
| `cancelled` | Migration was manually cancelled |

## Migration Stages and States

The migration system tracks progress through multiple stages, each with specific states that provide detailed information about the current activity. Understanding these stages and states helps API consumers monitor migration progress accurately.

### Overview

Migrations progress through the following stages in order:

1. **Initialization** (`init`) - Basic setup and preparation
2. **Validation** (`validation`) - Repository and target validation
3. **Setup** (`setup`) - Migration source creation
4. **Archive** (`archive`) - Repository archive generation
5. **Storage** (`storage`) - Archive upload to GitHub Owned Storage
6. **Migration** (`migration`) - Actual repository migration
7. **Queue** (`queue`) - Special stage for waiting between operations
8. **Error** (`error`) - Terminal error state

### Progress Calculation

Progress is calculated using weighted stages that dynamically adjust based on whether GitHub Owned Storage (GHOS) is enabled:

#### With GHOS Enabled (`use_ghos: true`)
- **Validation**: 10% of total progress
- **Setup**: 10% of total progress  
- **Archive**: 25% of total progress
- **Storage**: 15% of total progress
- **Migration**: 40% of total progress

#### With GHOS Disabled (`use_ghos: false`)
- **Validation**: 10% of total progress
- **Setup**: 10% of total progress  
- **Archive**: 30% of total progress (+5% from storage)
- **Storage**: 0% (completely skipped)
- **Migration**: 50% of total progress (+10% from storage)

Within each stage, the `state` provides granular progress information that contributes to the overall percentage.

**Important**: When GHOS is disabled, the storage stage is completely skipped in both progress calculation and stage progression, ensuring no gaps in progress reporting.

### GHOS vs Non-GHOS Migration Flows

The migration system supports two different flows depending on whether GitHub Owned Storage (GHOS) is enabled:

#### With GHOS Enabled (`use_ghos: true`)

When GHOS is enabled, archives are uploaded to GitHub's storage infrastructure for more reliable migrations:

```
1. Validation (0-10%) → Repository and environment checks
2. Setup (10-20%) → Migration source creation  
3. Archive (20-45%) → Repository archive generation
4. Storage (45-60%) → Upload archive to GHOS
5. Queue (60%) → Wait for migration worker
6. Migration (60-100%) → Import using GHOS archive
```

Key messages you'll see:
- `"uploading archive to GHOS"` → Storage stage
- `"starting repository migration with GHOS archive"` → Migration stage

#### With GHOS Disabled (`use_ghos: false`)

When GHOS is disabled, migrations use direct archive URLs:

```
1. Validation (0-10%) → Repository and environment checks
2. Setup (10-20%) → Migration source creation  
3. Archive (20-45%) → Repository archive generation
4. Queue (45%) → Wait for migration worker (skips storage)
5. Migration (45-100%) → Direct import using archive URL
```

Key messages you'll see:
- `"archive complete, waiting for migration worker"` → Queue stage
- `"starting repository migration"` → Migration stage (no GHOS reference)

**Important**: The storage stage is completely skipped when GHOS is disabled, and progress jumps directly from the archive stage to the migration stage.

### Context-Aware Progress System

The system implements context-aware progress tracking that prevents regression. This means:
- Validation checks during migration are treated as migration activities (not validation stage regression)
- GHOS uploads during migration maintain forward progress
- Queue states preserve accumulated progress from previous stages

### Detailed Stage Reference

#### Initialization Stage (`init`)

The initial setup phase when a migration request is first processed.

| State | Description | Typical Duration |
|-------|-------------|------------------|
| `starting` | Migration process is being initialized | Seconds |

#### Validation Stage (`validation`)

Repository and environment validation before migration begins.

| State | Description | Typical Duration |
|-------|-------------|------------------|
| `checking_source` | Validating that the source repository exists and is accessible | 10-30 seconds |
| `estimating_size` | Calculating the size of the repository for planning purposes | 30-60 seconds |
| `size_estimated` | Repository size has been determined | Immediate |
| `checking_target` | Validating target organization and checking for conflicts | 10-30 seconds |
| `target_exists` | Target repository exists and needs to be handled | Immediate |
| `target_cleaned` | Existing target repository has been successfully removed | 10-30 seconds |

**Progress Range**: 0-10%

#### Setup Stage (`setup`)

Migration infrastructure setup in GitHub Enterprise Cloud.

| State | Description | Typical Duration |
|-------|-------------|------------------|
| `creating_source` | Creating the migration source object in GitHub Enterprise Cloud | 30-60 seconds |

**Progress Range**: 10-20%

#### Archive Stage (`archive`)

Repository archive generation and preparation.

| State | Description | Typical Duration |
|-------|-------------|------------------|
| `preparing` | Initializing the archive generation process | 1-2 minutes |
| `generating` | Creating the repository archive | 5-30 minutes |
| `waiting` | Waiting for archive generation to complete | Variable |
| `exported` | Archive has been successfully generated | Immediate |
| `retrieving_url` | Obtaining the download URL for the archive | 10-30 seconds |
| `ready` | Archive is ready for upload or migration | Immediate |

**Progress Range**: 20-45%

**Note**: Archive generation time varies significantly based on repository size and complexity.

#### Storage Stage (`storage`)

Upload of the repository archive to GitHub Owned Storage (GHOS).

**Note**: This stage only occurs when `use_ghos` is set to `true` in the migration request. When GHOS is disabled, the migration skips directly from the archive stage to the migration stage.

| State | Description | Typical Duration |
|-------|-------------|------------------|
| `uploading` | Uploading archive to GitHub Owned Storage | 5-20 minutes |
| `completed` | Archive upload has been completed successfully | Immediate |

**Progress Range**: 45-60% (only when GHOS is enabled)

**Note**: Upload time depends on archive size and network conditions.

#### Migration Stage (`migration`)

The actual repository migration process in GitHub Enterprise Cloud.

| State | Description | Typical Duration | GHOS Usage |
|-------|-------------|------------------|------------|
| `starting` | Initiating the migration process | 1-2 minutes | Both |
| `pre_migration_validation` | Performing validation checks before migration | 1-3 minutes | Both |
| `uploading_to_ghos` | Uploading archive during migration (if needed) | 5-20 minutes | GHOS only |
| `ghos_upload_complete` | Archive upload completed during migration | Immediate | GHOS only |
| `preparing_archive` | Preparing archive for migration processing | 1-5 minutes | Both |
| `validating` | Performing additional validation checks | 1-3 minutes | Both |
| `created` | Migration job has been created in GitHub | Immediate | Both |
| `waiting` | Waiting for migration processing to begin | Variable | Both |
| `QUEUED` | Migration is queued for processing | Variable | Both |
| `PENDING` | Migration is pending execution | Variable | Both |
| `IN_PROGRESS` | Migration is actively being processed | 10-60 minutes | Both |
| `SUCCEEDED` | Migration has completed successfully | Immediate | Both |
| `FAILED` | Migration has failed | Immediate | Both |
| `completed` | Migration process is fully complete | Immediate | Both |

**Progress Range**: 
- With GHOS: 60-100%
- Without GHOS: 45-100%

**Note**: Migration processing time varies significantly based on repository size, complexity, and GitHub's current load.

#### Queue Stage (`queue`)

Special stage for managing workflow between major operations.

| State | Description | Typical Duration |
|-------|-------------|------------------|
| `pre_validation` | Performing validation before queuing operations | 30-60 seconds |
| `initializing_archive` | Setting up archive generation job | 1-2 minutes |
| `waiting_archive_worker` | Waiting for an available archive worker | Variable |
| `waiting_migration_worker` | Waiting for an available migration worker | Variable |

**Progress Range**: Preserves previous stage progress

**Note**: Queue wait times depend on GitHub's current load and available workers.

#### Error Stage (`error`)

Terminal error state when migration cannot proceed.

| State | Description |
|-------|-------------|
| `failed` | Migration has encountered an unrecoverable error |

**Progress Range**: Preserves progress at point of failure

### Interpreting Progress Information

#### Progress Fields

When querying migration status, you'll receive several progress-related fields:

```json
{
  "progress": 75,
  "stage_progress": 60,
  "stage": "migration",
  "state": "IN_PROGRESS",
  "completed_stages": ["validation", "setup", "archive", "storage"],
  "current_stage_index": 5,
  "total_stages": 6
}
```

| Field | Description |
|-------|-------------|
| `progress` | Overall migration progress (0-100%) |
| `stage_progress` | Progress within the current stage (0-100%) |
| `stage` | Current migration stage |
| `state` | Current state within the stage |
| `completed_stages` | Array of completed stages |
| `current_stage_index` | 1-based index of current stage |
| `total_stages` | Total number of stages in migration |

#### Understanding Progress Behavior

1. **Forward Progress**: Progress generally moves forward and should not regress
2. **Stage Completion**: When a stage completes, it's added to `completed_stages`
3. **Context Awareness**: Some activities (like validation during migration) are treated as migration activities to prevent progress regression
4. **Queue Preservation**: Queue stages preserve progress from previous stages
5. **Error Handling**: Errors preserve progress at the point of failure

#### Estimating Time Remaining

While migration times vary significantly, you can use the following guidelines:

- **Small repositories** (< 10MB): 10-30 minutes total
- **Medium repositories** (10MB-100MB): 30-90 minutes total  
- **Large repositories** (100MB-1GB): 1-4 hours total
- **Extra large repositories** (> 1GB): 4+ hours total

**Factors affecting duration**:
- Repository size and complexity
- Number of branches, tags, and releases
- Amount of Git LFS content
- GitHub's current load
- Network conditions

#### Monitoring Best Practices

1. **Polling Frequency**: Poll status every 30-60 seconds during active migration
2. **Progress Stalls**: If progress doesn't change for more than 30 minutes, consider investigating
3. **Error Handling**: Monitor for `error` stage and implement retry logic for failed migrations
4. **Load Management**: Be aware that migrations are subject to GitHub's rate limits and worker availability

### Stage Values

| Stage | Description |
|-------|-------------|
| `init` | Migration initialization and basic setup |
| `validation` | Repository and environment validation |
| `setup` | Migration infrastructure setup |
| `archive` | Repository archive creation and management |
| `storage` | Archive upload to GitHub Owned Storage |
| `migration` | Actual repository migration process |
| `queue` | Workflow management and worker coordination |
| `error` | Terminal error state |

### State Values

The `state` field provides more detailed information about the current activity within a stage:

| Stage | Possible States |
|-------|----------------|
| `init` | `starting` |
| `validation` | `checking_source`, `estimating_size`, `size_estimated`, `checking_target`, `target_exists`, `target_cleaned` |
| `setup` | `creating_source` |
| `archive` | `preparing`, `generating`, `waiting`, `exported`, `retrieving_url`, `ready` |
| `storage` | `uploading`, `completed` |
| `migration` | `starting`, `pre_migration_validation`, `uploading_to_ghos`, `ghos_upload_complete`, `preparing_archive`, `validating`, `created`, `waiting`, `QUEUED`, `PENDING`, `IN_PROGRESS`, `SUCCEEDED`, `FAILED`, `completed` |
| `queue` | `pre_validation`, `initializing_archive`, `waiting_archive_worker`, `waiting_migration_worker` |
| `error` | `failed` |

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

## Queue Management

The migration server implements a smart queueing system that respects GitHub's concurrency limits. The following endpoints provide information about the queue status and configuration.

### Get Queue Statistics

Retrieves current statistics about the migration queue.

```
GET /api/queue/stats
```

#### Response

```json
{
  "queue_enabled": true,
  "queue_size": 5,
  "max_queue_size": 1000,
  "active_archive_generations": 3,
  "max_archive_generations": 5,
  "active_migrations": 7,
  "max_migration_threads": 10,
  "default_priority": 50
}
```

#### Field Descriptions

| Field | Type | Description |
|-------|------|-------------|
| `queue_enabled` | boolean | Whether the queue system is enabled |
| `queue_size` | integer | Current number of jobs in the queue |
| `max_queue_size` | integer | Maximum number of jobs that can be queued |
| `active_archive_generations` | integer | Current number of active archive generations |
| `max_archive_generations` | integer | Maximum number of concurrent archive generations (GitHub limit: 5) |
| `active_migrations` | integer | Current number of active migrations |
| `max_migration_threads` | integer | Maximum number of concurrent migrations (GitHub limit: 10) |
| `default_priority` | integer | Default priority for new migration jobs |

#### Status Codes

| Status Code | Description |
|-------------|-------------|
| 200 | Success |
| 500 | Server error |

### Queue Configuration

The queue system can be configured through the server's configuration file (`config.yaml`). The following settings are available:

```yaml
queue:
  enabled: true                    # Whether to enable the queue system
  max_queue_size: 1000            # Maximum number of jobs that can be queued
  max_archive_threads: 5          # Maximum number of concurrent archive generations
  max_migration_threads: 10       # Maximum number of concurrent migrations
  default_priority: 50            # Default priority for new migration jobs
  queue_stats_interval: 300       # Interval in seconds for logging queue stats
```

### Queue Priority Levels

The queue system supports three priority levels:

| Priority | Value | Description |
|----------|-------|-------------|
| High | 100 | Used for retry operations and critical migrations |
| Medium | 50 | Default priority for new migrations |
| Low | 10 | Used for non-critical migrations |

### Queue Behavior

1. **Archive Generation**:
   - Limited to 5 concurrent archive generations (GitHub's limit)
   - Additional requests are queued and processed in priority order
   - Archive generation is the first phase of the migration process

2. **Migration Processing**:
   - Limited to 10 concurrent migrations (GitHub's limit)
   - Migrations are queued after archive generation completes
   - Higher priority jobs are processed first
   - Within the same priority level, jobs are processed in FIFO order

3. **Retry Operations**:
   - Failed migrations can be retried with higher priority
   - Retry operations automatically get priority over new migrations
   - Previous failed attempts are archived for reference

4. **Queue Limits**:
   - When the queue is full (reaches max_queue_size), new requests are rejected
   - The server returns a 429 Too Many Requests response when the queue is full
   - Clients should implement backoff and retry logic when receiving 429 responses

### Error Handling

When the queue is full or encounters issues, the following error responses may be returned:

```json
{
  "status": "error",
  "message": "Queue is full",
  "code": 429,
  "timestamp": "2023-05-16T14:45:30Z",
  "request_id": "7f8d9e2a-1b3c-4d5e-6f7g-8h9i0j1k2l3m"
}
```

### Best Practices

1. **Monitoring Queue Status**:
   - Regularly check queue statistics to monitor system load
   - Implement alerts for queue size approaching limits
   - Monitor active archive generations and migrations

2. **Handling Queue Full Errors**:
   - Implement exponential backoff when receiving 429 responses
   - Consider implementing a client-side queue for retries
   - Monitor queue statistics to predict when to retry

3. **Priority Usage**:
   - Use high priority sparingly for critical migrations
   - Default to medium priority for most migrations
   - Use low priority for non-critical or background migrations

4. **Rate Limiting**:
   - Consider the queue system when implementing rate limiting
   - Monitor both queue size and API rate limits
   - Implement appropriate backoff strategies

## Repository Size Categories

The migration tool categorizes repositories by size to help with planning and resource allocation:

| Size Category | Range | Description |
|---------------|-------|-------------|
| `small` | < 10MB | Small repositories with minimal content |
| `medium` | 10MB - 100MB | Medium-sized repositories with moderate content |
| `large` | 100MB - 1GB | Large repositories with substantial content |
| `x_large` | > 1GB | Extra large repositories with extensive content and history |

The size category is included in the migration status response as `size_category` and can be used for filtering and sorting migrations.

## Scheduler Management

The migration tool includes a scheduler that manages future migrations according to the specified parameters:

- **Scheduled Time**: The ISO8601 timestamp when the migration should be executed.
- **Time Zone**: The time zone for interpreting the scheduled time.
- **Day Restrictions**: Optionally restrict migrations to specific days of the week.
- **Time Window**: Optionally restrict migrations to a specific time window during the day.

When scheduling a migration, keep in mind:

1. If a migration is scheduled outside of allowed days/times, it will wait until the next valid time window.
2. Time windows that span midnight (e.g., 22:00-06:00) are properly handled.
3. Scheduled migrations appear with the `scheduled` status in status responses.
4. If no scheduling parameters are provided, the migration is executed immediately.
