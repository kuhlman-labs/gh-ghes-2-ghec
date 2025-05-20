# Migration Dashboard Behavior and Metrics

The migration dashboard provides real-time visibility into the state of repository migrations. This document explains how to interpret the dashboard's key metrics and statuses.

## Migration Statistics
- **Active**: The number of migrations currently in progress (including both archive and migration phases).
- **Succeeded**: The number of migrations that have completed successfully.
- **Failed**: The number of migrations that have failed.
- **Total**: The total number of migrations tracked.

## Queue Statistics
- **Queue Size**: The number of jobs (archive or migration) that are not yet fully completed. Each repository migration consists of two jobs: archive generation and migration import. The queue size reflects all jobs that are still in progress or waiting to be processed.
- **Active Archives**: The number of archive generation jobs currently being processed (up to the configured concurrency limit).
- **Active Migrations**: The number of migration import jobs currently being processed (up to the configured concurrency limit).

## Active Migrations Table
- **Stage: Archive: Exported**: The archive has been generated and the migration job is waiting for a migration worker to become available.
- **Stage: Migration: In_progress**: The migration import is currently being processed.
- **Progress**: Shows the completion percentage of the current stage.

**Note:**
The queue size will only decrease when both the archive and migration phases for a repository are complete. Jobs in the "Archive: Exported" stage are waiting for a migration worker, while jobs in "Migration: In_progress" are actively being migrated. 