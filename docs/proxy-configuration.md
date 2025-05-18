# Proxy Configuration Guide

This document details how to configure and use the HTTP proxy support for GitHub API communications in the GitHub GHES to GHEC Migration Server.

## Overview

In enterprise environments, outbound HTTP traffic often must route through a corporate proxy server for security monitoring, access control, and compliance purposes. The migration tool supports configuring HTTP proxies for GitHub API communications, with advanced features like:

- Separate proxy configuration for different environments
- Authentication support for proxies requiring credentials
- No-proxy lists for internal/direct connections
- Wildcard pattern matching for exempting internal domains

## Configuration Options

Proxy settings are configured in the `github.proxy` section of the configuration file:

```yaml
github:
  ghes_base_url: "https://github.internal.company.com"  # Optional default GHES URL
  proxy:
    enabled: true                     # Enable proxy support
    url: "http://proxy.company.com:8080"  # Proxy server URL
    username: "proxyuser"             # Username for proxy authentication (if required)
    password: "proxypass"             # Password for proxy authentication (if required)
    no_proxy_list: "github.internal.company.com,*.internal,10.0.0.*"  # Comma-separated list of hosts to bypass proxy
```

### Detailed Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable or disable proxy support |
| `url` | string | `""` | HTTP/HTTPS proxy server URL (e.g., `http://proxy.example.com:8080`) |
| `username` | string | `""` | Username for proxy authentication (if required) |
| `password` | string | `""` | Password for proxy authentication (if required) |
| `no_proxy_list` | string | `""` | Comma-separated list of hosts, domains, or IP patterns that should bypass the proxy |

## No-Proxy List Configuration

The `no_proxy_list` setting allows you to specify which hosts should bypass the proxy and connect directly. This is especially useful for:

1. Internal GitHub Enterprise Server instances that are accessible directly
2. Other internal services that don't require proxy access
3. Local development environments

### Supported Patterns

The no-proxy list supports several pattern types:

* **Exact hostname matches**: `github.internal.company.com`
* **Wildcard domain matches**: `*.internal.company.com`
* **IP address patterns**: `10.0.0.*`

### Examples

```yaml
# Only bypass proxy for a specific GitHub Enterprise Server
no_proxy_list: "github.internal.company.com"

# Bypass proxy for all hosts in specific domains
no_proxy_list: "*.internal.company.com,*.local"

# Bypass proxy for internal network and specific hosts
no_proxy_list: "10.0.0.*,github.internal,artifactory.internal"

# Bypass proxy for loopback and internal hosts
no_proxy_list: "localhost,127.0.0.1,*.internal"
```

## Common Deployment Scenarios

### Enterprise Environment with Internal GHES and External GHEC

In a typical enterprise environment, GitHub Enterprise Server is deployed internally, while GitHub Enterprise Cloud is accessed externally. In this scenario:

```yaml
github:
  proxy:
    enabled: true
    url: "http://corporate-proxy.example.com:8080"
    username: "proxy_user"     # If proxy requires authentication
    password: "proxy_password" # If proxy requires authentication
    no_proxy_list: "github.internal.example.com,*.internal.example.com"
```

This configuration will:
- Route all GitHub Cloud API requests through the corporate proxy
- Access the internal GitHub Enterprise Server directly, bypassing the proxy

### Development Environment

For local development and testing, you might want to bypass proxy configuration:

```yaml
github:
  proxy:
    enabled: false  # Disable proxy for local development
```

### Multiple Internal GitHub Instances

If you have multiple internal GitHub instances:

```yaml
github:
  proxy:
    enabled: true
    url: "http://proxy.example.com:8080"
    no_proxy_list: "ghes1.internal.example.com,ghes2.internal.example.com,*.dev.example.com"
```

## Environment Variables

Proxy configuration can also be set using environment variables:

```
GITHUB_PROXY_ENABLED=true
GITHUB_PROXY_URL=http://proxy.example.com:8080
GITHUB_PROXY_USERNAME=proxyuser
GITHUB_PROXY_PASSWORD=proxypass
GITHUB_PROXY_NO_PROXY_LIST=github.internal.example.com,*.internal
```

## Troubleshooting

### Verify Proxy Configuration

To verify your proxy settings are being applied correctly:

1. Enable debug logging by setting `logging.level: "debug"` in your configuration
2. Look for log entries containing `proxy configuration` to confirm settings are loaded
3. HTTP client actions will log whether they're using a proxy or connecting directly

### Common Issues

1. **Proxy authentication failures**: Ensure username and password are correct and properly encoded
2. **Certificate validation errors**: Your proxy might require adding CA certificates to the trust store
3. **No-proxy patterns not matching**: Check for exact format matching, especially with wildcards
4. **Connection timeouts**: Verify proxy URL and port are correct

### Testing Connectivity

You can test connectivity using the health check endpoint:

```
curl http://localhost:8080/api/healthz
```

The response should include proxy information if debug logging is enabled.

## Security Considerations

1. **Password protection**: Proxy passwords in configuration files should be protected with appropriate file permissions
2. **Environment variables**: Using environment variables for proxy credentials can be more secure than configuration files
3. **Network segregation**: Ensure your no-proxy list doesn't accidentally expose internal services to external access

## Performance Impact

Using a proxy server may introduce additional latency for API requests. If you notice slower migration performance:

1. Ensure your proxy server has sufficient capacity
2. Use the no-proxy list to bypass proxying for high-volume internal traffic
3. Consider adjusting timeout settings if proxy processing introduces delays 