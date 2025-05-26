# Migration Dashboard Guide

The migration dashboard provides comprehensive visibility and control over your GitHub GHES to GHEC migrations. Access the dashboard at `/dashboard` to monitor active migrations, analyze trends, start new migrations, and troubleshoot issues.

## Dashboard Overview

The dashboard consists of five main sections accessible via the navigation menu:

- **Overview**: Real-time monitoring of active migrations
- **Analytics**: Charts and visualizations for migration analysis  
- **New Migration**: Guided wizard for starting migrations
- **History**: Complete record of all migrations
- **Errors**: Error tracking and categorization

## Migration Overview

The Overview page is your primary monitoring hub for active migrations.

### Migration Statistics
- **Active**: Migrations currently in progress (including both archive and migration phases)
- **Succeeded**: Migrations that have completed successfully
- **Failed**: Migrations that have encountered errors
- **Total**: Total number of migrations tracked

### Queue Statistics
- **Queue Size**: Jobs (archive or migration) that are not yet fully completed. Each repository migration consists of two jobs: archive generation and migration import
- **Active Archives**: Archive generation jobs currently being processed (up to configured concurrency limit)
- **Active Migrations**: Migration import jobs currently being processed (up to configured concurrency limit)

### Active Migrations Table
The main table shows all active migrations with the following information:

- **Repository**: Name of the repository being migrated
- **Stage**: Current migration phase (Archive, Migration, etc.)
- **State**: Detailed status within the current stage
- **Progress**: Completion percentage of the current stage
- **Duration**: Time elapsed since migration started
- **Actions**: Available operations (view details, retry if failed)

#### Migration Stages
- **Archive: Exported**: Archive has been generated and migration job is waiting for a worker
- **Migration: In_progress**: Migration import is actively being processed
- **Queued**: Job is waiting in the queue for an available worker

### Filtering and Sorting
Use the filter controls to refine the view:
- **Status Filter**: Show only active, succeeded, failed, or all migrations
- **Repository Filter**: Search for specific repository names
- **Time Range**: Filter by when migrations were started
- **Sort Options**: Sort by repository name, status, start time, or duration

### Real-Time Updates
The overview page automatically refreshes to show the latest migration status. You can also manually refresh using the refresh button.

## Analytics Dashboard

The Analytics page provides detailed visualizations and insights into your migration patterns.

### Status Distribution Chart
Pie chart showing the breakdown of migration statuses (succeeded, running, failed, pending).

### Migration Trends
Line chart displaying migration activity over time, including:
- Successful migrations per day
- Failed migrations per day  
- Total migration volume

### Repository Size Distribution
Chart showing the distribution of repository sizes being migrated:
- Small repositories (< 100MB)
- Medium repositories (100MB - 1GB)
- Large repositories (1GB - 10GB)
- Extra large repositories (> 10GB)

### Performance Metrics
Charts showing:
- Average migration duration over time
- Success rate trends
- Activity heatmaps showing peak migration times

### Export Capabilities
Export analytics data in CSV or JSON format for external analysis.

## Migration Wizard

The New Migration wizard guides you through setting up and starting new migrations.

### Step 1: Source Configuration
- **GitHub Enterprise Server URL**: Your GHES instance URL
- **Access Token**: Personal access token with required permissions
- **Organization**: Source organization name

### Step 2: Target Configuration  
- **GitHub Enterprise Cloud Organization**: Destination organization
- **Access Token**: GHEC token with migration permissions

### Step 3: Connection Testing
The wizard automatically tests connectivity to both source and target before proceeding.

### Step 4: Repository Selection
- Browse available repositories in the source organization
- Search and filter repositories
- Select multiple repositories for batch migration
- View repository metadata (size, last updated, visibility)

### Step 5: Migration Options
Configure migration settings:
- Migration priority
- Retry behavior
- Notification preferences

### Draft Saving
Save migration configurations as drafts to complete setup later.

## Migration History

The History page provides a complete record of all migrations with advanced search and filtering capabilities.

### Search and Filtering
- **Text Search**: Search across repository names and error messages
- **Status Filter**: Filter by migration outcome
- **Date Range**: Filter by migration start/end dates
- **Advanced Filters**: Repository size, duration, organization

### Sorting Options
Sort the history by:
- Repository name
- Status
- Start time
- End time  
- Duration
- Repository size

### Migration Details
Click on any migration to view detailed information:
- Complete migration timeline
- Stage-by-stage progress
- Error details and logs
- Retry history
- Performance metrics

### Export Functionality
Export filtered migration history in multiple formats:
- CSV for spreadsheet analysis
- JSON for programmatic processing
- Include error details and metadata

## Error Dashboard

The Error Dashboard provides centralized error tracking and categorization for troubleshooting failed migrations.

### Error Categories
Errors are automatically categorized for easier analysis:
- **Transient**: Temporary issues that may resolve on retry
- **Permanent**: Issues requiring manual intervention
- **Rate Limit**: API rate limiting errors
- **Authentication**: Token or permission issues
- **Authorization**: Access control problems
- **Resource Not Found**: Missing repositories or resources
- **Resource Conflict**: Naming conflicts or existing resources
- **Validation**: Input validation failures
- **Migration Canceled**: User-initiated cancellations
- **Internal Error**: System or unexpected errors

### Error Statistics
View error distribution across categories with visual charts showing:
- Error count by category
- Error trends over time
- Most common error types

### Error Investigation
For each error category, view:
- Detailed error messages
- Affected repositories
- Frequency and patterns
- Recommended resolution steps

## Best Practices

### Monitoring Active Migrations
1. Check the Overview page regularly during active migration periods
2. Monitor queue statistics to ensure workers are processing jobs
3. Watch for migrations stuck in specific stages
4. Use filters to focus on problem areas

### Using Analytics
1. Review trends before planning large migration batches
2. Use size distribution data to plan worker capacity
3. Monitor success rates to identify systemic issues
4. Export data for capacity planning and reporting

### Error Resolution
1. Check the Error Dashboard for patterns in failed migrations
2. Use error categories to prioritize resolution efforts
3. Review detailed error messages in migration history
4. Use the retry functionality for transient errors

### Performance Optimization
1. Monitor migration duration trends in Analytics
2. Balance concurrent migrations based on repository sizes
3. Schedule large migrations during off-peak hours
4. Use the activity heatmap to identify optimal migration windows

## Navigation and Access

Access dashboard sections directly via URLs:
- Overview: `/dashboard`
- Analytics: `/dashboard/analytics`  
- New Migration: `/dashboard/wizard`
- History: `/dashboard/history`
- Errors: `/dashboard/errors`

The dashboard is designed to be responsive and works well on both desktop and mobile devices. 