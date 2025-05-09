# GitHub Enterprise Server to GitHub Enterprise Cloud Migration Tool

A GitHub CLI extension for migrating repositories from GitHub Enterprise Server (GHES) to GitHub Enterprise Cloud (GHEC).

## Installation

```bash
gh extension install kuhlman-labs/gh-ghes-2-ghec
```

## Usage

Start the migration server:

```bash
gh ghes-2-ghec --ghes-token <token> --gh-cloud-token <token> [--webhook-url <url>] [--port <port>]
```

Required flags:
- `--ghes-token`: GitHub Enterprise Server token
- `--gh-cloud-token`: GitHub Enterprise Cloud token

Optional flags:
- `--webhook-url`: URL for notifications (default: empty)
- `--port`: Port to listen on (default: 8080)

## API

### Start Migration

```http
POST /migrate
Content-Type: application/json

{
  "source_org": "source-org",
  "target_org": "target-org",
  "repositories": ["repo1", "repo2"],
  "ghes_api_url": "https://ghes.example.com/api/v3"
}
```

Response:
```json
{
  "status": "migration started"
}
```

## Development

### Prerequisites

- Go 1.21 or later
- GitHub CLI

### Building

```bash
go build
```

### Testing

```bash
go test ./...
```

## License

MIT 