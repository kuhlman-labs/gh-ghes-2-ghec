# Queue Implementation

The migration server implements a smart queueing system that manages repository migrations while respecting GitHub's concurrency limits and providing priority-based processing.

## Overview

The queue system is designed to handle two main types of operations:
1. Archive Generation (limited to 5 concurrent operations by GitHub)
2. Repository Migration (limited to 10 concurrent operations by GitHub)

## Features

### Priority-Based Queueing
- Three priority levels: High (100), Medium (50), and Low (10)
- FIFO ordering within the same priority level
- Automatic higher priority for retry operations
- Configurable default priority for new migrations

### Concurrency Management
- Respects GitHub's limits:
  - Maximum 5 concurrent archive generations
  - Maximum 10 concurrent migrations
- Separate worker pools for archive and migration operations
- Automatic promotion of jobs from archive to migration phase

### Queue Configuration
```yaml
queue:
  enabled: true                    # Enable/disable queueing
  max_queue_size: 1000            # Maximum number of jobs in queue
  max_archive_threads: 5          # GitHub's limit for archive generation
  max_migration_threads: 10       # GitHub's limit for concurrent migrations
  default_priority: 50            # Default priority for new migrations
  queue_stats_interval: 300       # Queue stats logging interval (seconds)
```

### Metrics
The queue system exposes several Prometheus metrics:
- `migration_queue_size`: Current number of jobs in queue
- `migration_queued_jobs_total`: Total jobs queued
- `migration_active_archives`: Current active archive generations
- `migration_active_migrations`: Current active migrations
- `migration_completed_archives_total`: Total completed archives
- `migration_completed_migrations_total`: Total completed migrations

### Queue Statistics
The queue manager provides real-time statistics through the API:
- Current queue size
- Maximum queue size
- Active archive generations
- Maximum archive threads
- Active migrations
- Maximum migration threads

## Implementation Details

### Queue Manager
The `QueueManager` handles all queue operations:
- Job enqueuing with priority
- Worker pool management
- Concurrency control
- Job processing coordination
- Metrics collection

### Job Processing
1. Archive Generation Phase:
   - Job is added to queue with specified priority
   - Worker picks up job when archive slot available
   - Archive generation is performed
   - Job is automatically promoted to migration phase

2. Migration Phase:
   - Job is added to migration queue
   - Worker picks up job when migration slot available
   - Migration is performed
   - Status is updated on completion

### Error Handling
- Failed jobs are logged with detailed error information
- Queue continues processing other jobs
- Failed migrations can be retried with higher priority

### Graceful Shutdown
- Queue manager supports graceful shutdown
- In-progress jobs are allowed to complete
- New jobs are rejected during shutdown
- Metrics collection is stopped cleanly

## Usage

### API Integration
The queue system is integrated with the migration API:
- New migrations are automatically queued
- Retry operations use higher priority
- Queue statistics are available via API

### Dashboard Integration
The web dashboard shows:
- Current queue status
- Active jobs
- Queue statistics
- Job priorities
- Processing history

## Best Practices

1. **Queue Size Management**
   - Monitor queue size metrics
   - Adjust max_queue_size based on workload
   - Consider system resources when setting limits

2. **Priority Usage**
   - Use high priority sparingly
   - Reserve high priority for critical migrations
   - Use medium priority for normal operations
   - Use low priority for non-urgent migrations

3. **Monitoring**
   - Watch queue size trends
   - Monitor processing times
   - Track error rates
   - Set up alerts for queue issues

4. **Performance Tuning**
   - Adjust worker counts based on system capacity
   - Monitor memory usage
   - Watch for queue buildup
   - Tune metrics collection interval 