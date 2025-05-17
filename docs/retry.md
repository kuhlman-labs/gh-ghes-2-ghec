# Migration Retry Functionality

The GitHub GHES to GHEC Migration Server includes comprehensive retry functionality for failed migrations. This document explains how to use the retry features through both the web dashboard and API.

## Overview

Repository migrations can fail for various reasons, such as:
- Network connectivity issues
- API rate limits
- Token expiration
- Repository conflicts
- Service unavailability
- Issues with Repository Size (total and individual file)
- GitHub RuleSet violations

The retry functionality allows you to restart a failed migration without having to re-create the entire migration request. It preserves the original migration configuration and archives the failed attempt for reference.

## Using Retry via Web Dashboard

### Prerequisites

To retry a failed migration through the web dashboard:
1. The migration must be in a `failed` state
2. You must have access to the migration details page
3. You must provide valid GitHub tokens for both GHES and GHEC

### Steps to Retry a Migration

1. Navigate to the migration details page for the failed migration
2. Look for the "Retry Migration" button at the top of the page (only visible for failed migrations)
3. Click the "Retry Migration" button to open the retry form modal
4. The form will be pre-populated with non-sensitive values from the original migration:
   - Target Organization
   - GitHub Enterprise Server URL
   - GitHub Owned Storage (GHOS) usage option
5. Enter the required authentication tokens:
   - GitHub Enterprise Server token
   - GitHub Cloud token
6. Click "Retry Migration" to submit the form
7. The modal will automatically close, and you'll see the migration status update to "In Progress"
8. The page will display the number of retry attempts in the migration information section

## Using Retry via API

The server provides a REST API endpoint for retrying migrations programmatically.

### API Endpoint

```
POST /api/retry
```

### Request Body

```json
{
  "repository": "source-org/repo1",
  "ghes_token": "your-ghes-token",
  "gh_cloud_token": "your-gh-cloud-token",
  "ghes_base_url": "https://github.example.com",
  "target_org": "target-org",
  "use_ghos": true
}
```

### Required Fields

- `repository`: Full repository name (org/repo) to retry
- `ghes_token`: Token for authenticating with GitHub Enterprise Server
- `gh_cloud_token`: Token for authenticating with GitHub Enterprise Cloud

### Optional Fields

- `ghes_base_url`: Base URL of your GitHub Enterprise Server instance (if different from original)
- `target_org`: The target organization in GitHub Enterprise Cloud (if different from original)
- `use_ghos`: When set to `true`, uses GitHub Owned Storage (GHOS) for migration archives

### Example Request

```bash
curl -X POST \
  https://your-migration-server/api/retry \
  -H 'Content-Type: application/json' \
  -d '{
    "repository": "source-org/repo1",
    "ghes_token": "ghp_xxxxxxxxxxxxxxxxxxxx",
    "gh_cloud_token": "ghp_xxxxxxxxxxxxxxxxxxxx",
    "target_org": "target-org"
  }'
```

### Response

```json
{
  "status": "accepted",
  "message": "Migration retry request accepted for source-org/repo1",
  "timestamp": "2023-05-16T14:25:30Z",
  "request_id": "7f8d9e2a-1b3c-4d5e-6f7g-8h9i0j1k2l3m",
  "repository": "source-org/repo1"
}
```

## Archived Attempts

Each time a migration is retried, the previous failed attempt is archived for reference. You can view all archived attempts in the "Archived Migration Attempts" section on the migration details page.

The archived attempts include:
- When the attempt started and finished
- Duration of the attempt
- Final status
- Migration ID (if available)
- Detailed error message

## Monitoring Retry Status

You can monitor the status of a retried migration in the same way as the original migration:
- Through the web dashboard on the migration details page
- Via the `/api/status` endpoint in the API
- Through webhook notifications if configured

## Troubleshooting Retries

If a retry also fails, consider:

1. Checking the error message from the failed retry for specific issues
2. Verifying that the provided tokens have the correct permissions
3. Ensuring the target organization exists and you have access to it
4. Checking if the repository already exists in the target organization
5. Verifying network connectivity to both GitHub Enterprise Server and GitHub Cloud
6. Checking GitHub API status for any service disruptions
7. Reviewing server logs for more detailed error information

Each retry attempt is recorded and can be viewed in the migration history, helping to diagnose persistent issues. 