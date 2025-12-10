# Stale Migration Detection and Recovery

## Overview

The stale migration detection feature automatically identifies and handles migrations that were interrupted by server shutdown or other unexpected events. When the server restarts, it checks for migrations that are stuck in "in_progress" status and marks them as failed so they can be retried.

## Problem Description

When the migration server shuts down (gracefully or unexpectedly), migrations that were actively running may be left in an "in_progress" state in the database even though they are no longer actually running. This can happen because:

1. The migration process was interrupted before it could update its final status
2. The final status update was lost during shutdown
3. Network issues prevented the status from being persisted

Without recovery logic, these migrations would remain stuck in "in_progress" status indefinitely, even though they are not actually running.

## How It Works

### Detection Logic

On server startup, the system loads all migration statuses from storage and checks each "in_progress" migration against configurable thresholds:

1. **Update Age Check**: If a migration hasn't been updated for more than `max_update_age` hours, it's considered stale
2. **Migration Age Check**: If a migration has been running for more than `max_migration_age` hours, it's considered stale

### Recovery Process

When a stale migration is detected:

1. **Archive**: The current stale status is archived to the migration history table
2. **Mark as Failed**: The migration status is updated to "failed" with an explanatory error message
3. **Log**: The recovery action is logged for audit purposes

### Error Message

Recovered migrations will show an error message like:
```
Migration interrupted by server shutdown - marked as failed during recovery. Use retry to restart the migration.
```

## Configuration

Configure stale detection in your `config.yaml`:

```yaml
storage:
  enabled: true
  stale_detection:
    enabled: true
    max_update_age: 2    # hours - migrations with no updates for this long are considered stale
    max_migration_age: 6 # hours - migrations running for this long are considered stale
```

### Configuration Options

- **`enabled`**: Whether to enable stale migration detection (default: `true`)
- **`max_update_age`**: Maximum hours since last update before considering a migration stale (default: `2`)
- **`max_migration_age`**: Maximum hours since migration start before considering it stale (default: `6`)

## Recommended Settings

### Conservative Settings (Fewer False Positives)
```yaml
stale_detection:
  enabled: true
  max_update_age: 4    # 4 hours
  max_migration_age: 12 # 12 hours
```

### Aggressive Settings (Faster Recovery)
```yaml
stale_detection:
  enabled: true
  max_update_age: 1    # 1 hour
  max_migration_age: 3  # 3 hours
```

### Production Recommended
```yaml
stale_detection:
  enabled: true
  max_update_age: 2    # 2 hours
  max_migration_age: 6  # 6 hours
```

## Monitoring and Logs

### Log Messages

When stale migrations are detected, you'll see log messages like:

```
WARN Detected stale in-progress migration from previous server session 
     repository_full_name=org/repo status=in_progress stage=migration 
     state=in_progress started_at=2025-01-01T10:00:00Z 
     updated_at=2025-01-01T12:00:00Z migration_id=12345

INFO Archived stale migration status repository=org/repo
INFO Updated stale migration status to failed repository=org/repo
INFO Detected and handled stale in-progress migrations count=1
```

### Metrics

The system tracks:
- Number of stale migrations detected on startup
- Recovery success/failure rates
- Time since last migration update

## Recovery Actions

### Automatic Recovery

The system automatically:
1. Archives the stale migration attempt
2. Marks the migration as failed
3. Logs the recovery action

### Manual Recovery

After automatic recovery, you can:

1. **Retry the Migration**: Use the retry API endpoint to restart the migration
   ```bash
   curl -X POST "http://localhost:8080/api/retry?repository=org/repo"
   ```

2. **Check Migration History**: View archived attempts to understand what happened
   ```bash
   curl "http://localhost:8080/api/status?repository=org/repo"
   ```

## Disabling Stale Detection

To disable stale detection (not recommended for production):

```yaml
storage:
  stale_detection:
    enabled: false
```

When disabled, stale migrations will remain in "in_progress" status and must be manually resolved.

## Best Practices

1. **Enable in Production**: Always enable stale detection in production environments
2. **Monitor Logs**: Watch for frequent stale migrations, which may indicate infrastructure issues
3. **Tune Thresholds**: Adjust thresholds based on your typical migration duration
4. **Regular Monitoring**: Check migration status regularly to catch issues early
5. **Graceful Shutdown**: Use proper shutdown procedures to minimize stale migrations

## Troubleshooting

### High Number of Stale Migrations

If you see many stale migrations:
- Check for infrastructure issues (network, database connectivity)
- Review server shutdown procedures
- Consider increasing timeout values
- Monitor system resources during migrations

### False Positives

If migrations are incorrectly marked as stale:
- Increase `max_update_age` and `max_migration_age` values
- Check if migrations are genuinely taking longer than expected
- Review migration performance and optimization

### Recovery Failures

If recovery fails:
- Check database connectivity and permissions
- Review storage configuration
- Check available disk space
- Examine detailed error logs 