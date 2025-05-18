# Database Optimization Guide

This document provides information about the database optimization features implemented in the GitHub GHES to GHEC Migration Tool.

## Connection Pooling

The migration tool uses optimized connection pooling for all database operations. Each database type (SQLite, MySQL, PostgreSQL) has a tailored configuration for optimal performance.

### Configuration Options

The connection pool can be configured with the following parameters:

- `MaxOpenConns`: Maximum number of open connections to the database
- `MaxIdleConns`: Maximum number of idle connections in the pool
- `ConnMaxLifetime`: Maximum amount of time a connection may be reused
- `ConnMaxIdleTime`: Maximum amount of time a connection may be idle

### Database-Specific Configurations

#### SQLite

For SQLite, we use a conservative pool configuration due to its single-writer nature:

```go
MaxOpenConns:    1, // SQLite supports only one writer at a time
MaxIdleConns:    1,
ConnMaxLifetime: 5 * time.Minute,
ConnMaxIdleTime: 1 * time.Minute,
```

#### MySQL and PostgreSQL

For MySQL and PostgreSQL, we use more aggressive connection pooling:

```go
MaxOpenConns:    25,
MaxIdleConns:    10,
ConnMaxLifetime: 15 * time.Minute,
ConnMaxIdleTime: 5 * time.Minute,
```

## Query Optimization

All database operations use prepared statements for improved performance and security. This reduces parsing overhead and helps prevent SQL injection attacks.

### Key Optimizations

- **Prepared Statements**: All frequently executed queries use prepared statements
- **Transaction Support**: Critical operations are performed within transactions
- **Connection Reuse**: Connections are properly managed and reused when possible
- **Statement Caching**: Prepared statements are cached for reuse
- **Optimized Data Types**: Data types are carefully chosen for optimal storage and performance

## Database Schema Migrations

The tool includes a schema migration system that manages database schema versions and ensures smooth upgrades.

### Migration Features

- **Version Tracking**: Each schema version is tracked in a dedicated table
- **Automatic Upgrades**: Migrations are automatically applied when needed
- **Transaction Safety**: Migrations are performed within transactions when possible
- **Failure Recovery**: Failed migrations are properly rolled back

## Indexing Strategy

The tool uses a comprehensive indexing strategy to optimize query performance:

### Key Indexes

- `repository`: Primary key for fast migration lookups
- `updated_at`: For sorting and time-based filtering
- `status`: For filtering migrations by status
- `repository_date`: Compound index for repository history queries

## Maintenance Routines

Automatic maintenance routines keep the database performing optimally:

### Maintenance Operations

- **ANALYZE**: Updates statistics for the query planner
- **REINDEX**: Rebuilds indexes for optimal performance
- **VACUUM**: Reclaims unused space and defragments the database
- **Integrity Checks**: Regular checks ensure database integrity

## Metrics and Monitoring

Comprehensive metrics provide visibility into database performance:

### Available Metrics

- **Connection Stats**: Open, in-use, and idle connections
- **Query Duration**: Time spent on different query types
- **Wait Statistics**: Connection wait times and counts
- **Error Rates**: Database operation error counts by category
- **Pool Exhaustion**: Detection of connection pool exhaustion

## Health Checks

Regular health checks verify database functionality:

### Health Check Operations

- **Connectivity**: Verifies basic database connectivity
- **Write Tests**: Tests write operations
- **Read Tests**: Tests read operations
- **Performance**: Measures operation latency
- **Repair**: Automatic repair of common issues

## Configuration Recommendations

### Development Environment

```yaml
storage:
  type: sqlite
  connection_string: migrations.db
  timeout: 60
```

### Production Environment

```yaml
storage:
  type: postgresql  # or mysql for MySQL
  connection_string: "host=db.example.com port=5432 user=migrator password=secret dbname=migrations sslmode=require"
  table_prefix: ghec_
  timeout: 120
```

## Troubleshooting

If you encounter database performance issues, consider:

1. **Check Connection Pool Metrics**: Look for connection pool exhaustion
2. **Analyze Query Performance**: Identify slow queries
3. **Run Database Maintenance**: Use the maintenance endpoint
4. **Check Database Health**: Use the health check endpoint
5. **Increase Timeouts**: Adjust timeout settings for long-running operations 