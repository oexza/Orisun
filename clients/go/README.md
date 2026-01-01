# Orisun Go Client

A Go client for the Orisun event store, providing a simple and intuitive interface for interacting with the Orisun gRPC service.

## Features

- **Builder Pattern**: Easy configuration with a fluent builder API
- **Authentication**: Support for basic authentication and token caching
- **Logging**: Configurable logging with multiple log levels
- **Retry Logic**: Built-in retry mechanisms for resilient operations
- **Event Subscriptions**: Support for streaming event subscriptions
- **Connection Management**: Automatic connection lifecycle management
- **Load Balancing**: Support for DNS and static load balancing
- **TLS Support**: Configurable TLS encryption
- **Keep-alive**: Configurable keep-alive settings

## Installation

### Install from GitHub

```bash
go get github.com/oexza/Orisun/clients/go
```

### Build from Source

If you prefer to build from source or need the latest development version:

```bash
# Clone the repository
git clone https://github.com/oexza/Orisun.git
cd Orisun/clients/go

# Build the client
go build ./...

# Or install it locally
go install ./...
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/oexza/Orisun/clients/go/orisun"
)

func main() {
    // Create a client with default configuration
    client, err := orisun.NewClientBuilder().
        WithHost("localhost").
        WithPort(5005).
        WithTimeout(30).
        Build()
    
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()

    // Ping the server
    ctx, cancel := orisun.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    if err := client.Ping(ctx); err != nil {
        log.Printf("Ping failed: %v", err)
    } else {
        log.Println("Ping successful!")
    }
}
```

## Configuration

The client uses a builder pattern for configuration:

```go
client, err := orisun.NewClientBuilder().
    WithServer("localhost", 5005).
    WithTimeout(30).
    WithTLS(true).
    WithBasicAuth("username", "password").
    WithLogging(true).
    WithLogLevel(orisun.DEBUG).
    WithLoadBalancingPolicy("round_robin").
    WithKeepAliveTime(30*time.Second).
    WithKeepAliveTimeout(10*time.Second).
    WithKeepAlivePermitWithoutCalls(true).
    Build()
```

### Configuration Options

- **WithHost(host)**: Add a server with default port 50051
- **WithPort(port)**: Set the port for the last added server
- **WithServer(host, port)**: Add a server with specific host and port
- **WithServers(servers)**: Add multiple servers
- **WithTimeout(seconds)**: Set default timeout in seconds
- **WithTLS(useTLS)**: Enable/disable TLS encryption
- **WithBasicAuth(username, password)**: Set basic authentication credentials
- **WithLogger(logger)**: Set a custom logger
- **WithLogging(enableLogging)**: Enable/disable default logging
- **WithLogLevel(level)**: Set log level (DEBUG, INFO, WARN, ERROR)
- **WithLoadBalancingPolicy(policy)**: Set load balancing policy
- **WithDnsTarget(target)**: Set DNS target for DNS-based load balancing
- **WithStaticTarget(target)**: Set static target for static-based load balancing
- **WithDnsResolver(useDns)**: Enable/disable DNS resolver
- **WithKeepAliveTime(duration)**: Set keep-alive time
- **WithKeepAliveTimeout(duration)**: Set keep-alive timeout
- **WithKeepAlivePermitWithoutCalls(permit)**: Permit keep-alive without calls
- **WithChannel(channel)**: Use a custom gRPC channel

## Core Operations

### Save Events

```go
events := []*eventstore.EventToSave{
    {
        EventId:   "event-123",
        EventType: "UserAction",
        Data:      `{"action": "login", "userId": "12345"}`,
        Metadata:  `{"source": "web-app"}`,
    },
}

request := &eventstore.SaveEventsRequest{
    Boundary: "my-boundary",
    Stream: &eventstore.SaveStreamQuery{
        Name: "user-stream",
        ExpectedPosition: &eventstore.Position{
            CommitPosition: 100,
            PreparePosition: 100,
        },
    },
    Events: events,
}

result, err := client.SaveEvents(ctx, request)
```

### Get Events

```go
request := &eventstore.GetEventsRequest{
    Boundary: "my-boundary",
    Stream: &eventstore.GetStreamQuery{
        Name: "user-stream",
        FromPosition: &eventstore.Position{
            CommitPosition: 100,
            PreparePosition: 100,
        },
    },
    Count:     100,
    Direction: eventstore.Direction_ASC,
}

response, err := client.GetEvents(ctx, request)
```

### Event Subscriptions

```go
handler := orisun.NewSimpleEventHandler().
    WithOnEvent(func(event *eventstore.Event) error {
        fmt.Printf("Received event: %s - %s\n", event.EventId, event.EventType)
        return nil
    }).
    WithOnError(func(err error) {
        fmt.Printf("Subscription error: %v\n", err)
    }).
    WithOnCompleted(func() {
        fmt.Println("Subscription completed")
    })

request := &eventstore.CatchUpSubscribeToEventStoreRequest{
    Boundary:      "my-boundary",
    SubscriberName: "my-subscriber",
}

subscription, err := client.SubscribeToEvents(ctx, request, handler)
defer subscription.Close()
```

### Health Check

```go
healthy, err := client.HealthCheck(ctx, "my-boundary")
```

## Error Handling

The client provides structured error handling with context:

```go
if err != nil {
    if orisunErr, ok := err.(*orisun.OrisunException); ok {
        fmt.Printf("Orisun error: %s\n", orisunErr.GetMessage())
        
        if operation, exists := orisunErr.GetContext("operation"); exists {
            fmt.Printf("Operation: %s\n", operation)
        }
    } else if concurrencyErr, ok := err.(*orisun.OptimisticConcurrencyException); ok {
        fmt.Printf("Version conflict: expected=%d, actual=%d\n", 
            concurrencyErr.GetExpectedVersion(), 
            concurrencyErr.GetActualVersion())
    }
}
```

## Logging

The client includes configurable logging:

```go
// Enable logging with DEBUG level
client, err := orisun.NewClientBuilder().
    WithLogging(true).
    WithLogLevel(orisun.DEBUG).
    Build()

// Custom logger
type CustomLogger struct{}

func (l *CustomLogger) Debug(msg string, args ...interface{}) {
    fmt.Printf("[DEBUG] %s\n", msg)
}

func (l *CustomLogger) Info(msg string, args ...interface{}) {
    fmt.Printf("[INFO] %s\n", msg)
}

func (l *CustomLogger) Error(msg string, args ...interface{}) {
    fmt.Printf("[ERROR] %s\n", msg)
}

// Use custom logger
client, err := orisun.NewClientBuilder().
    WithLogger(&CustomLogger{}).
    Build()
```

## Testing

Run the tests:

```bash
cd clients/go
go test -v
```

## Development

To build the client:

```bash
cd clients/go
go build ./...
```

## API Reference

### Client

- `NewClientBuilder()`: Create a new client builder
- `Build()`: Build the client instance

### ClientBuilder

- `WithHost(host)`: Add a server with default port
- `WithPort(port)`: Set the port for the last added server
- `WithServer(host, port)`: Add a server with specific host and port
- `WithServers(servers)`: Add multiple servers
- `WithTimeout(seconds)`: Set default timeout in seconds
- `WithTLS(useTLS)`: Enable/disable TLS encryption
- `WithBasicAuth(username, password)`: Set basic authentication credentials
- `WithLogger(logger)`: Set a custom logger
- `WithLogging(enableLogging)`: Enable/disable default logging
- `WithLogLevel(level)`: Set log level
- `WithLoadBalancingPolicy(policy)`: Set load balancing policy
- `WithDnsTarget(target)`: Set DNS target for DNS-based load balancing
- `WithStaticTarget(target)`: Set static target for static-based load balancing
- `WithDnsResolver(useDns)`: Enable/disable DNS resolver
- `WithKeepAliveTime(duration)`: Set keep-alive time
- `WithKeepAliveTimeout(duration)`: Set keep-alive timeout
- `WithKeepAlivePermitWithoutCalls(permit)`: Permit keep-alive without calls
- `WithChannel(channel)`: Use a custom gRPC channel

### OrisunClient

- `SaveEvents(ctx, request)`: Save events to a stream
- `GetEvents(ctx, request)`: Get events from the event store
- `SubscribeToEvents(ctx, request, handler)`: Subscribe to events
- `SubscribeToStream(ctx, request, handler)`: Subscribe to a specific stream
- `Ping(ctx)`: Ping the server
- `HealthCheck(ctx, boundary)`: Perform a health check
- `Close()`: Close the client connection
- `GetLogger()`: Get the client's logger
- `GetTokenCache()`: Get the client's token cache
- `GetDefaultTimeout()`: Get the default timeout
- `GetConnection()`: Get the underlying gRPC connection
- `IsClosed()`: Check if the client is closed

### Utilities

- `NewServerAddress(host, port)`: Create a server address
- `WithTimeout(parent, timeout)`: Create a context with timeout
- `WithDeadline(parent, deadline)`: Create a context with deadline
- `WithCancel(parent)`: Create a context with cancel function
- `NewTokenCache(logger)`: Create a token cache
- `NewDefaultLogger(level)`: Create a default logger
- `NewNoOpLogger()`: Create a no-op logger
- `NewRetryHelper(config)`: Create a retry helper
- `DefaultRetryConfig()`: Get default retry configuration
- `NewContextHelper()`: Create a context helper
- `NewStringHelper()`: Create a string helper

### Types

- `ServerAddress`: Represents a server address with host and port
- `OrisunException`: Base exception with context
- `OptimisticConcurrencyException`: Version conflict exception
- `LogLevel`: Log level enumeration (DEBUG, INFO, WARN, ERROR)
- `Logger`: Logger interface
- `EventHandler`: Event handler interface
- `SimpleEventHandler`: Simple event handler implementation
- `TokenCache`: Token cache for authentication
- `RetryConfig`: Retry configuration
- `RetryHelper`: Retry helper utility

## License

This project is licensed under the MIT License.