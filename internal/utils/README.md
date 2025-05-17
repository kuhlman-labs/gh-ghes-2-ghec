# Utils Package

This package provides utility functions and helpers for the migration tool, including circuit breakers and retry mechanisms for building resilient applications.

## Circuit Breaker

The circuit breaker pattern helps prevent cascading failures in distributed systems. It's like an electrical circuit breaker that trips when too many failures occur, giving downstream systems time to recover.

### Usage

```go
// Create a circuit breaker with default configuration
logger := slog.Default()
cb := NewCircuitBreaker(DefaultCircuitConfig("my-service", logger))

// Execute code with circuit breaker protection
err := cb.Execute(func() error {
    // Your code here, e.g. API calls, database operations, etc.
    return callExternalService()
})

// Handle errors
if err != nil {
    // If circuit is open, the Execute method will return an error
    // without executing your code.
    // Handle the error appropriately
}
```

### Configuration

You can customize circuit breaker behavior using the configuration options:

```go
config := DefaultCircuitConfig("my-service", logger).
    WithFailureThreshold(10).               // Trip after 10 consecutive failures
    WithResetTimeout(5 * time.Minute).      // Wait 5 minutes before half-open state
    WithHalfOpenSuccessThreshold(3).        // Need 3 successful calls to close circuit
    WithMaxConcurrentRequests(100).         // Limit concurrent calls to 100
    WithRequestTimeout(10 * time.Second)    // Timeout calls after 10 seconds

cb := NewCircuitBreaker(config)
```

### States

The circuit breaker can be in one of three states:

1. **CLOSED**: Normal operation. All requests are allowed through.
2. **OPEN**: Circuit is tripped. All requests are rejected immediately.
3. **HALF_OPEN**: Testing recovery. Only one request is allowed through to test if the service has recovered.

### Monitoring

You can register callbacks to be notified when the circuit breaker changes state:

```go
cb.OnStateChange(func(oldState, newState CircuitState) {
    logger.Info("Circuit state changed",
        "from", string(oldState),
        "to", string(newState),
    )
})
```

You can also access metrics:

```go
metrics := cb.Metrics()
fmt.Printf("Total calls: %d\n", metrics["total_calls"])
fmt.Printf("Failed calls: %d\n", metrics["failed_calls"])
```

## Retry Pattern

The retry pattern handles temporary failures by automatically retrying operations.

### Usage

```go
// Create a retry configuration
retryConfig := DefaultRetryConfig(logger)

// Execute with retries
err := Retry(ctx, retryConfig, "operation-name", func() error {
    // Your code here
    return callExternalService()
})
```

### HTTP Retry Middleware

For HTTP requests, you can use the retry middleware:

```go
client := &http.Client{}
executeRequest := RetryMiddleware(client, retryConfig, "api-call")

// Now use it to make requests
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com", nil)
resp, err := executeRequest(req)
```

## Combining Patterns

For maximum resilience, you can combine circuit breakers with retries:

```go
// Create a retry configuration and a circuit breaker
retryConfig := DefaultRetryConfig(logger)
cb := NewCircuitBreaker(DefaultCircuitConfig("my-service", logger))

// Use them together
err := cb.Execute(func() error {
    return Retry(ctx, retryConfig, "operation", func() error {
        return callExternalService()
    })
})
```

This setup:
1. First attempts to execute the operation with retries (for transient failures)
2. If too many failures occur even with retries, the circuit breaker opens (for persistent failures)

By using both patterns together, you get the best of both worlds - resilience against both transient and persistent failures. 