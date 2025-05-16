# Storage Options

The migration server provides options for persistent storage of migration states and data.

## Overview

By default, all migration data is stored in memory and will be lost when the server restarts. Enabling persistent storage allows the server to:

- Survive restarts while preserving migration state
- Maintain historical records of past migrations
- Provide migration statistics across server sessions
- Support the dashboard with historical data

Storage is entirely optional but recommended for production deployments.

## Configuration

Storage can be configured in the config.yaml file:

```yaml
storage:
  enabled: true                           # Set to true to enable persistent storage
  type: "sqlite"                          # Options: sqlite, mysql, postgres
  connection_string: "migrations.db"      # SQLite: file path, MySQL/Postgres: connection string
  table_prefix: "ghes2ghec_"              # Optional prefix for database tables
```

## Storage Types

### SQLite (default)
- Lightweight file-based database
- Suitable for small to medium deployments
- Example: `connection_string: "migrations.db"`
- Pros: Simple setup, no external dependencies
- Cons: Limited concurrent access, not ideal for high-throughput environments

### MySQL
- More robust for higher concurrency
- Example: `connection_string: "user:password@tcp(localhost:3306)/migrations"`
- Pros: Better concurrency support, widely available
- Cons: Requires external database server

### PostgreSQL
- Enterprise-grade database with advanced features
- Example: `connection_string: "postgres://user:password@localhost:5432/migrations"`
- Pros: Best concurrency and reliability, advanced features
- Cons: More complex setup, requires external database server

## Table Prefix

The `table_prefix` parameter adds a prefix to all database table names. This is useful when:
- Using a shared database with other applications
- Running multiple instances of the migration server with the same database
- Implementing database sharding strategies

## Data Persistence Behavior

When storage is enabled:

### Server Startup
- The server loads all previously saved migration states from the database
- Migrations that were in progress when the server last shut down remain in their last known state
- The dashboard will display historical migration data

### During Operation
- Each status change is persisted to the database in real-time
- Migration metadata and progress information are stored
- Webhooks delivery status is recorded

### Shutdown and Restart
- In-flight migrations that didn't complete will be marked as failed after restart
- Historical data remains available for reporting
- Restart detection prevents duplicate processing

## Database Schema Management

The server automatically handles database schema creation and migrations:

- Tables are created if they don't exist
- Schema migrations are applied automatically
- No manual database setup is required

Tables created:
- `migrations` - Core migration data
- `migration_events` - Historical events for each migration
- `repositories` - Repository-specific migration data

## Backup Recommendations

For production deployments with storage enabled:

### SQLite
- Regular file backups of the database file
- Consider using an external volume with your container
- Example Docker mount: `-v /path/to/data:/app/data`

### MySQL/PostgreSQL
- Use standard database backup procedures
- Consider point-in-time recovery options
- Implement database replication where appropriate

## Docker Configuration

When using Docker, configure storage with environment variables:

```bash
docker run -p 8080:8080 \
  -e STORAGE_ENABLED=true \
  -e STORAGE_TYPE=sqlite \
  -e STORAGE_CONNECTION_STRING=/data/migrations.db \
  -v /path/on/host:/data \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

For MySQL:

```bash
docker run -p 8080:8080 \
  -e STORAGE_ENABLED=true \
  -e STORAGE_TYPE=mysql \
  -e STORAGE_CONNECTION_STRING="user:password@tcp(mysql-host:3306)/migrations" \
  ghcr.io/kuhlman-labs/gh-ghes-2-ghec:latest
```

## Migration Data Lifecycle

The storage system manages migration data through different lifecycle stages:

1. **Creation**: Initial migration request is stored
2. **Progress Updates**: Status changes stored as events
3. **Completion**: Final state (success/failure) recorded
4. **Archival**: Older migrations flagged as archived (but data retained)
5. **Cleanup**: Optional automatic pruning of old migration data

You can configure data retention policies in the configuration:

```yaml
storage:
  enabled: true
  retention_days: 30   # Keep migration data for 30 days (default: 0 = forever)
  auto_prune: true     # Automatically remove old data (default: false)
```

## Query Interface

When using persistent storage, the server provides additional API endpoints for querying historical migration data:

```
GET /api/history?days=7                 # Get migrations from the last 7 days
GET /api/history?status=failed          # Get all failed migrations
GET /api/history?org=my-org             # Get migrations for a specific org
GET /api/stats                          # Get aggregated migration statistics
``` 