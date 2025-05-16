# Webhooks

Webhook notifications provide real-time updates about migration progress and status changes to external systems.

## Overview and Benefits

Webhooks are particularly useful for:

- Monitoring migrations in a central dashboard
- Integrating with existing notification systems
- Triggering automated actions based on migration events
- Maintaining an audit trail of migration activities

## Configuration Options

Webhooks can be configured in two ways:

### Global Configuration (applies to all migrations)

```yaml
# config.yaml
webhook:
  url: "https://your-webhook-url"
  timeout: 10  # seconds
  max_retries: 3
  retry_backoff: 1.5  # exponential backoff multiplier
  headers:
    X-Custom-Auth: "your-auth-token"
    Content-Type: "application/json"
```

### Command Line (overrides config file)

```bash
./gh-ghes-2-ghec --webhook-url="https://your-webhook-url"
```

### Per-Request Webhooks

You can also specify webhooks on a per-migration basis in the API request:

```json
{
  "source_org": "source-organization",
  "target_org": "target-organization",
  "repositories": ["repo1", "repo2"],
  "ghes_base_url": "https://github.example.com",
  "ghes_token": "your-ghes-token",
  "gh_cloud_token": "your-gh-cloud-token",
  "webhook": {
    "url": "https://your-webhook-url/specific-handler",
    "custom_id": "migration-12345",
    "headers": {
      "X-API-Key": "your-api-key"
    }
  }
}
```

## Webhook Payload

Every webhook delivery includes a standardized JSON payload:

```json
{
  "event_type": "migration_status_changed",
  "timestamp": "2023-05-16T14:30:45Z",
  "request_id": "7f8d9e2a-1b3c-4d5e-6f7g-8h9i0j1k2l3m",
  "migration": {
    "id": "mig_123456789",
    "source_org": "source-org",
    "target_org": "target-org",
    "repository": "example-repo",
    "status": "in_progress",
    "stage": "archive",
    "progress": 45,
    "started_at": "2023-05-16T14:25:30Z",
    "updated_at": "2023-05-16T14:30:45Z",
    "completed_at": null,
    "error": null
  },
  "previous_status": {
    "status": "in_progress",
    "stage": "validation",
    "progress": 30
  },
  "custom_data": {
    "custom_id": "migration-12345"
  }
}
```

### Event Types

| Event Type | Description |
|------------|-------------|
| `migration_created` | A new migration has been created |
| `migration_status_changed` | Migration status has changed |
| `migration_stage_changed` | Migration entered a new stage |
| `migration_completed` | Migration has completed (success) |
| `migration_failed` | Migration has failed |
| `migration_progress` | Progress percentage updated |

### Status Fields

| Field | Description | Example Values |
|-------|-------------|----------------|
| `status` | High-level migration status | `queued`, `in_progress`, `completed`, `failed` |
| `stage` | Current migration stage | `validation`, `archive`, `import`, `cleanup` |
| `progress` | Percentage complete (0-100) | `45` |
| `error` | Error message if failed | `"Rate limit exceeded"` |

## Webhook Delivery

### Delivery Process

1. The server attempts to deliver the webhook payload to the configured URL
2. HTTP POST request with JSON payload
3. Custom headers are included if configured
4. Server waits for response with configured timeout
5. Success is a 2xx response code

### Retry Mechanism

If delivery fails:

1. The server logs the failure
2. Waits according to the configured backoff (default: exponential)
3. Retries up to the configured maximum (default: 3)
4. Records final delivery status

## Common Use Cases

### Status Dashboard

- Update a central dashboard with migration progress
- Display real-time status of all migrations
- Show historical migration data

### Notification Systems

- Send Slack/Teams notifications for status changes
- Email notifications for completed migrations
- SMS alerts for critical failures

### Automation

- Trigger post-migration tasks
- Update inventory systems
- Generate migration reports
- Clean up temporary resources

### Audit Trail

- Log all migration events
- Track migration history
- Generate compliance reports

## Webhook Security

To secure webhook communications:

1. **Use HTTPS URLs** to ensure encrypted transmission
2. **Include authentication headers** in your webhook configuration:
   ```yaml
   webhook:
     url: "https://your-webhook-url"
     headers:
       Authorization: "Bearer your-secret-token"
   ```
3. **Verify request origins** in your webhook receiver
4. **Enable signature verification** by configuring a shared secret:
   ```yaml
   webhook:
     url: "https://your-webhook-url"
     secret: "your-signing-secret"
   ```
   The server will include an `X-Hub-Signature` header with an HMAC-SHA256 signature.

## Testing Webhooks

You can test webhook delivery using:

### Command Line Testing

```bash
./gh-ghes-2-ghec test-webhook --url="https://your-webhook-url" --event=migration_completed
```

### Webhook Debugging

For debugging, you can use a service like [webhook.site](https://webhook.site) or [Beeceptor](https://beeceptor.com) to inspect webhook payloads.

### Local Forwarding

For local development, use [ngrok](https://ngrok.com) to expose your local webhook receiver:

```bash
ngrok http 8000
```

Then use the generated URL in your webhook configuration. 