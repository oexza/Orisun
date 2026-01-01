# Orisun Event Store - Java Client

A Java client library for interacting with the Orisun Event Store, providing a simple and intuitive API for event sourcing operations.

## Features

- **Event Storage**: Save events to streams with optimistic concurrency control
- **Event Retrieval**: Read events from streams with version filtering
- **Event Subscriptions**: Subscribe to real-time event streams
- **Multi-tenancy**: Support for boundary-based tenant isolation
- **Authentication**: Built-in support for basic authentication with token caching
- **Load Balancing**: Support for multiple servers and DNS-based load balancing
- **Connection Management**: Built-in keep-alive settings and connection pooling
- **Logging**: Configurable logging framework with multiple levels
- **Error Handling**: Enhanced error context preservation
- **Health Checks**: Built-in ping and health check functionality
- **Request Validation**: Comprehensive input validation with detailed error messages

## Installation

### Build from Source

The recommended way to use the Java client is to build it from source:

```bash
# Clone the repository
git clone https://github.com/oexza/Orisun.git
cd Orisun/clients/java

# Build with Gradle
./gradlew build

# Install to local Maven repository
./gradlew publishToMavenLocal
```

Then add to your `pom.xml`:

```xml
<dependency>
    <groupId>com.orisunlabs</groupId>
    <artifactId>orisun-java-client</artifactId>
    <version>0.0.1</version>
</dependency>
```

Or for Gradle:

```groovy
implementation 'com.orisunlabs:orisun-java-client:0.0.1'
```

### From JitPack (Alternative)

JitPack can build the client directly from GitHub:

```xml
<repository>
    <id>jitpack.io</id>
    <url>https://jitpack.io</url>
</repository>

<dependency>
    <groupId>com.github.oexza</groupId>
    <artifactId>Orisun</artifactId>
    <version>main-SNAPSHOT</version>
</dependency>
```

Note: When using JitPack, you'll need to specify the full package path in your imports:
```java
import com.oexza.orisun.clients.java.*;
```

## Quick Start

```java
import com.orisunlabs.orisun.client.OrisunClient;
import com.orisunlabs.orisun.client.EventStoreClient.ServerAddress;
import com.orisun.eventstore.Eventstore;

// Create a client
OrisunClient client = OrisunClient.newBuilder()
    .withServer("localhost", 5005)
    .withBasicAuth("admin", "changeit")
    .withLogging(true)
    .withLogLevel(DefaultLogger.LogLevel.INFO)
    .withKeepAliveTime(30000)
    .withKeepAliveTimeout(10000)
    .build();

try {
    // Save events
    Eventstore.SaveEventsRequest saveRequest = Eventstore.SaveEventsRequest.newBuilder()
        .setBoundary("tenant-1")
        .setStream(Eventstore.SaveStreamQuery.newBuilder()
            .setName("user-123")
            .setExpectedPosition(Eventstore.Position.newBuilder()
                .setCommitPosition(-1)
                .setPreparePosition(-1))
            .build())
        .addEvents(Eventstore.EventToSave.newBuilder()
            .setEventId(java.util.UUID.randomUUID().toString())
            .setEventType("UserCreated")
            .setData("{\"userId\":\"user-123\",\"email\":\"john@example.com\"}")
            .setMetadata("{\"source\":\"user-service\"}")
            .build())
        .build();

    Eventstore.WriteResult result = client.saveEvents(saveRequest);
    System.out.println("Events saved at position: " + result.getLogPosition());

    // Read events
    Eventstore.GetEventsRequest getRequest = Eventstore.GetEventsRequest.newBuilder()
        .setBoundary("tenant-1")
        .setStream(Eventstore.GetStreamQuery.newBuilder()
            .setName("user-123")
            .build())
        .setCount(100)
        .build();

    Eventstore.GetEventsResponse response = client.getEvents(getRequest);
    response.getEventsList().forEach(event -> {
        System.out.println("Event: " + event.getEventType() + " - " + event.getData());
    });

    // Subscribe to events
    Eventstore.CatchUpSubscribeToEventStoreRequest subscribeRequest = 
        Eventstore.CatchUpSubscribeToEventStoreRequest.newBuilder()
            .setBoundary("tenant-1")
            .setSubscriberName("my-subscriber")
            .build();

    EventSubscription subscription = client.subscribeToEvents(subscribeRequest, 
        new EventSubscription.EventHandler() {
            @Override
            public void onEvent(Eventstore.Event event) {
                System.out.println("Received event: " + event.getEventType());
            }

            @Override
            public void onError(Throwable error) {
                System.err.println("Subscription error: " + error.getMessage());
            }

            @Override
            public void onCompleted() {
                System.out.println("Subscription completed");
            }
        });

    // Let subscription run for a while
    Thread.sleep(5000);
    subscription.close();

} finally {
    client.close();
}
```

## API Reference

### OrisunClient.Builder

The builder provides a fluent API for configuring the client:

#### Connection Options

- `withServer(String host, int port)` - Add a server to the connection list
- `withServers(List<ServerAddress> servers)` - Add multiple servers
- `withHost(String host)` - Set host (uses default port 50051)
- `withPort(int port)` - Set port for the last added host
- `withTls(boolean useTls)` - Enable/disable TLS
- `withChannel(ManagedChannel channel)` - Use existing channel
- `withLoadBalancingPolicy(String policy)` - Set load balancing policy ("round_robin" or "pick_first")
- `withDnsResolver(boolean useDns)` - Enable/disable DNS resolver

#### Authentication Options

- `withBasicAuth(String username, String password)` - Set basic authentication credentials

#### Logging Options

- `withLogging(boolean enableLogging)` - Enable/disable logging
- `withLogger(Logger logger)` - Set custom logger implementation
- `withLogLevel(DefaultLogger.LogLevel level)` - Set logging level (DEBUG, INFO, WARN, ERROR)

#### Connection Management Options

- `withTimeout(int seconds)` - Set operation timeout
- `withKeepAliveTime(long keepAliveTimeMs)` - Set keep-alive time in milliseconds
- `withKeepAliveTimeout(long keepAliveTimeoutMs)` - Set keep-alive timeout in milliseconds
- `withKeepAlivePermitWithoutCalls(boolean permitWithoutCalls)` - Allow keep-alive without active calls

### OrisunClient Methods

#### Synchronous Operations

- `saveEvents(SaveEventsRequest request)` - Save events to a stream
- `getEvents(GetEventsRequest request)` - Read events from a stream

#### Asynchronous Operations

- `saveEventsAsync(SaveEventsRequest request)` - Save events asynchronously (returns CompletableFuture)

#### Subscription Operations

- `subscribeToEvents(CatchUpSubscribeToEventStoreRequest request, EventHandler handler)` - Subscribe to all events
- `subscribeToStream(CatchUpSubscribeToStreamRequest request, EventHandler handler)` - Subscribe to specific stream

#### Health Check Operations

- `ping()` - Ping the server to check connectivity
- `healthCheck()` - Comprehensive health check (returns boolean)

#### Connection Management

- `close()` - Close the client connection

### ServerAddress

Represents a server address:

```java
ServerAddress server = new ServerAddress("localhost", 5005);
String host = server.getHost(); // "localhost"
int port = server.getPort();     // 5005
```

### Logger Interface

Custom logger implementation:

```java
public class CustomLogger implements Logger {
    @Override
    public void debug(String message, Object... args) {
        // Custom debug logging
    }
    
    @Override
    public void info(String message, Object... args) {
        // Custom info logging
    }
    
    @Override
    public void warn(String message, Object... args) {
        // Custom warn logging
    }
    
    @Override
    public void error(String message, Object... args) {
        // Custom error logging
    }
    
    @Override
    public void error(String message, Throwable throwable, Object... args) {
        // Custom error logging with exception
    }
    
    @Override
    public boolean isDebugEnabled() {
        return true; // Enable debug logging
    }
    
    @Override
    public boolean isInfoEnabled() {
        return true; // Enable info logging
    }
}
```

## Error Handling

The client provides enhanced error handling with context:

```java
try {
    client.saveEvents(request);
} catch (OrisunException e) {
    // Get error context
    String operation = e.getContext("operation");
    String statusCode = e.getContext("statusCode");
    
    System.err.println("Operation failed: " + operation + " with status: " + statusCode);
    System.err.println("Error message: " + e.getMessage());
    
    // Get original exception
    Throwable originalError = e.getCause();
} catch (OptimisticConcurrencyException e) {
    // Handle concurrency conflicts
    long expectedVersion = e.getExpectedVersion();
    long actualVersion = e.getActualVersion();
    
    System.err.println("Concurrency conflict - expected: " + expectedVersion + ", actual: " + actualVersion);
}
```

## Load Balancing

### Multiple Servers

```java
OrisunClient client = OrisunClient.newBuilder()
    .withServer("server1.example.com", 5005)
    .withServer("server2.example.com", 5005)
    .withServer("server3.example.com", 5005)
    .withLoadBalancingPolicy("round_robin")
    .build();
```

### DNS-based Load Balancing

```java
OrisunClient client = OrisunClient.newBuilder()
    .withServer("eventstore.example.com", 5005)
    .withDnsResolver(true)
    .withLoadBalancingPolicy("round_robin")
    .build();
```

## Token Caching

The client automatically caches authentication tokens from response headers and reuses them for subsequent requests:

```java
// First request uses basic auth and caches token
client.saveEvents(firstRequest);

// Subsequent requests use cached token automatically
client.saveEvents(secondRequest);
client.getEvents(getRequest);
```

## Health Monitoring

```java
// Simple ping
try {
    client.ping();
    System.out.println("Server is reachable");
} catch (OrisunException e) {
    System.err.println("Server unreachable: " + e.getMessage());
}

// Comprehensive health check
boolean isHealthy = client.healthCheck();
if (isHealthy) {
    System.out.println("Event store is healthy");
} else {
    System.err.println("Event store health check failed");
}
```

## Examples

### Basic Event Sourcing

```java
public class UserAggregate {
    private final OrisunClient client;
    private final String streamId;
    private final String boundary;
    private long expectedVersion = -1;

    public UserAggregate(OrisunClient client, String userId, String boundary) {
        this.client = client;
        this.streamId = "user-" + userId;
        this.boundary = boundary;
    }

    public void createUser(String email, String name) throws Exception {
        Eventstore.SaveEventsRequest request = Eventstore.SaveEventsRequest.newBuilder()
            .setBoundary(boundary)
            .setStream(Eventstore.SaveStreamQuery.newBuilder()
                .setName(streamId)
                .setExpectedPosition(createPosition(expectedVersion))
                .build())
            .addEvents(Eventstore.EventToSave.newBuilder()
                .setEventId(UUID.randomUUID().toString())
                .setEventType("UserCreated")
                .setData(String.format("{\"email\":\"%s\",\"name\":\"%s\"}", email, name))
                .setMetadata("{\"source\":\"user-service\"}")
                .build())
            .build();

        Eventstore.WriteResult result = client.saveEvents(request);
        expectedVersion++;
    }

    public void updateEmail(String newEmail) throws Exception {
        Eventstore.SaveEventsRequest request = Eventstore.SaveEventsRequest.newBuilder()
            .setBoundary(boundary)
            .setStream(Eventstore.SaveStreamQuery.newBuilder()
                .setName(streamId)
                .setExpectedPosition(createPosition(expectedVersion))
                .build())
            .addEvents(Eventstore.EventToSave.newBuilder()
                .setEventId(UUID.randomUUID().toString())
                .setEventType("EmailUpdated")
                .setData(String.format("{\"newEmail\":\"%s\"}", newEmail))
                .setMetadata("{\"source\":\"user-service\"}")
                .build())
            .build();

        Eventstore.WriteResult result = client.saveEvents(request);
        expectedVersion++;
    }

    private Eventstore.Position createPosition(long version) {
        return Eventstore.Position.newBuilder()
            .setCommitPosition(version)
            .setPreparePosition(version)
            .build();
    }
}
```

### Event Processing with Subscriptions

```java
public class EventProcessor {
    private final OrisunClient client;

    public EventProcessor(OrisunClient client) {
        this.client = client;
    }

    public void startProcessing(String boundary) {
        Eventstore.CatchUpSubscribeToEventStoreRequest request = 
            Eventstore.CatchUpSubscribeToEventStoreRequest.newBuilder()
                .setBoundary(boundary)
                .setSubscriberName("event-processor")
                .build();

        client.subscribeToEvents(request, new EventSubscription.EventHandler() {
            @Override
            public void onEvent(Eventstore.Event event) {
                processEvent(event);
            }

            @Override
            public void onError(Throwable error) {
                System.err.println("Event processing error: " + error.getMessage());
            }

            @Override
            public void onCompleted() {
                System.out.println("Event processing completed");
            }
        });
    }

    private void processEvent(Eventstore.Event event) {
        switch (event.getEventType()) {
            case "UserCreated":
                handleUserCreated(event);
                break;
            case "EmailUpdated":
                handleEmailUpdated(event);
                break;
            default:
                System.out.println("Unknown event type: " + event.getEventType());
        }
    }

    private void handleUserCreated(Eventstore.Event event) {
        // Process user creation
        System.out.println("Processing user creation: " + event.getData());
    }

    private void handleEmailUpdated(Eventstore.Event event) {
        // Process email update
        System.out.println("Processing email update: " + event.getData());
    }
}
```

## Building

```bash
./gradlew build
```

## Testing

```bash
./gradlew test
```

## Configuration

The client supports various configuration options:

- Connection timeout: 30 seconds (default)
- Keep-alive time: 30 seconds (default)
- Keep-alive timeout: 10 seconds (default)
- Load balancing: round_robin (default)
- Logging: WARN level (default)

## Thread Safety

The client is thread-safe and can be shared across multiple threads. Each operation maintains its own context and metadata.

## Performance Considerations

- Use connection pooling for high-throughput applications
- Configure appropriate keep-alive settings for your network
- Consider DNS-based load balancing for dynamic environments
- Monitor health check results for proactive failover

## Troubleshooting

### Common Issues

1. **Connection Timeouts**
   - Increase timeout with `withTimeout()`
   - Check network connectivity
   - Verify server is running

2. **Authentication Failures**
   - Verify credentials are correct
   - Check if token caching is interfering
   - Ensure TLS settings match server

3. **Concurrency Conflicts**
   - Handle `OptimisticConcurrencyException`
   - Retry with updated expected version
   - Consider event versioning strategy

4. **Performance Issues**
   - Adjust keep-alive settings
   - Use appropriate load balancing
   - Monitor memory usage

## License

MIT