# Orisun - A Batteries Included Event Store

## Description
Orisun is a batteries-included event store, with an embedded NATS JetStream server and PostgreSQL support. It provides a reliable, scalable event store with built-in pub/sub capabilities, making it ideal for event-driven architectures and CQRS applications.

### Key Features
- **Embedded NATS JetStream**: No separate NATS installation required
- **Auto Database Setup**: Automatically creates and manages its schema
- **Stream-based Event Sourcing**: Traditional stream-based event sourcing with optimistic concurrency
- **Global Ordering**: Built-in global ordering guarantee for events
- **Dynamic Consistency Boundaries**: Lock across multiple streams using tag-based queries
- **Real-time Event Streaming**: Subscribe to event changes in real-time
- **Flexible Event Querying**: Query events by various criteria including custom tags
- **High Performance**: Efficient PostgreSQL-based storage with embedded NATS JetStream for streaming

## Prerequisites
- PostgreSQL 13+ database

## Quick Start (Using Pre-built Binary)

1. Download the latest release for your platform from the [releases page](https://github.com/yourusername/orisun/releases)

2. Run the binary with environment variables:
```bash
# Minimal configuration
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=your_schema \
orisun-[platform]-[arch]

# Example with all options
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=your_schema \
ORISUN_GRPC_PORT=50051 \
ORISUN_NATS_PORT=4222 \
ORISUN_NATS_STORE_DIR=/var/opt/nats \
orisun-darwin-arm64
```

Orisun will automatically:
- Set up its database schema
- Start an embedded NATS JetStream server
- Start the gRPC server


## Key Concepts

### Boundaries and Schemas
In Orisun, a "boundary" directly corresponds to a PostgreSQL schema. Boundaries must be pre-configured at startup:

```bash
# Configure allowed boundaries (schemas)
ORISUN_PG_SCHEMAS=users,orders,payments \
ORISUN_PG_HOST=localhost \
[... other config ...] \
orisun-darwin-arm64
```

When Orisun starts:
1. It validates and creates the specified schemas if they don't exist
2. Only requests to these pre-configured boundaries will be accepted
3. Each boundary maintains its own:
   - Event sequences
   - Consistency guarantees
   - Event tables

For example:
- If `ORISUN_DB_SCHEMAS=users,orders`, then:
  - ✅ `boundary: "users"` - Request will succeed
  - ✅ `boundary: "orders"` - Request will succeed
  - ❌ `boundary: "payments"` - Request will fail (schema not configured)

This boundary pre-configuration ensures:
- Security through explicit schema allow listing
- Clear separation of domains
- Controlled resource allocation

### Environment Setup
```bash
# Multiple schemas can be pre-configured
ORISUN_PG_SCHEMAS=users,orders,payments \
ORISUN_PG_HOST=localhost \
[... other config ...] \
orisun-darwin-arm64
```

## gRPC API Examples

### SaveEvents
Save events to a specific schema/boundary:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents
{
  "events": [
    {
      "event_id": "evt-123",
      "event_type": "UserCreated",
      "tags": [
        {"key": "aggregate_id", "value": "user-123"},
        {"key": "version", "value": "1"},
        {"key": "tenant_id", "value": "tenant-456"}
      ],
      "data": "{\"username\": \"john_doe\", \"email\": \"john@example.com\"}",
      "metadata": "{\"source\": \"user_service\", \"correlation_id\": \"corr-789\"}",
      "stream_id": "user-123",
      "version": 1
    }
  ],
  "boundary": "users",
  "consistency_condition": {
    "query": {
      "criteria": [
        {
          "tags": [
            {"key": "tenant_id", "value": "tenant-456"},
            {"key": "aggregate_id", "value": "user-123"}
          ]
        }
      ]
    },
    "consistency_marker": {
      "commit_position": "1000",
      "prepare_position": "999"
    }
  }
}
```

### GetEvents
Query events with various criteria:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/GetEvents <<
{
  "boundary": "users",
  "stream": {
    "stream": "user-123"
  },
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "tenant_id", "value": "tenant-456"},
          {"key": "event_type", "value": "UserCreated"}
        ]
      },
      {
        "tags": [
          {"key": "event_type", "value": "UserUpdated"}
        ]
      }
    ]
  },
  "count": 100,
  "direction": "ASC",
  "last_retrieved_position": {
    "commit_position": "1000",
    "prepare_position": "999"
  }
}
```

### SubscribeToEvents
Subscribe to events with complex filtering:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/SubscribeToEvents <<EOF
{
  "subscriber_name": "user-activity-monitor",
  "boundary": "users",
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "tenant_id", "value": "tenant-456"},
          {"key": "event_type", "value": "UserCreated"}
        ]
      },
      {
        "tags": [
          {"key": "tenant_id", "value": "tenant-456"},
          {"key": "event_type", "value": "UserDeleted"}
        ]
      }
    ]
  }
}
```

### PublishToPubSub
Publish a message to a pub/sub topic:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/PublishToPubSub <<
{
  "subject": "notifications",
  "data": "{\"message\": \"Hello World\"}",
  "metadata": "{\"priority\": \"high\"}"
}
```

### SubscribeToPubSub
Subscribe to messages from a pub/sub topic:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/SubscribeToPubSub <<
{
  "subject": "notifications",
  "consumer_name": "notification-processor"
}
```

## Common Use Cases

### Multiple Bounded Contexts
```bash
# User domain events in users schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents <<EOF
{
  "boundary": "users",
  "events": [...]
}

# Order domain events in orders schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents <<EOF
{
  "boundary": "orders",
  "events": [...]
}
```

### Schema Management
- Each boundary (schema) maintains its own:
  - Event sequences
  - Consistency boundaries
  - Indexes
  - Event tables

This separation ensures:
- Domain isolation
- Independent scaling
- Separate consistency guarantees
- Clear bounded context boundaries

## Error Handling
Common error responses:
- `ALREADY_EXISTS`: Consistency condition violation
- `INVALID_ARGUMENT`: Missing required fields
- `INTERNAL`: Database or system errors
- `NOT_FOUND`: Stream or consumer not found

## Building from Source

### Prerequisites
- Go 1.20+
- Make

1. Clone the repository:
```bash
git clone https://github.com/yourusername/orisun.git
cd orisun
```

2. Build the binary:
```bash
./build.sh
```

3. Run the built binary:
```bash
# Using environment variables
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=public \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
./orisun-darwin-arm64

## Usage

### Starting the Server
```bash
cd ./orisun/src/main/orisun
go run .
```

### Client Libraries
- coming soon...

## Architecture
Orisun uses:
- PostgreSQL for durable event storage and consistency guarantees
- NATS JetStream for real-time event streaming and pub/sub
- gRPC for client-server communication

## Performance
- Handles thousands of events per second
- Efficient querying with PostgreSQL indexes
- Load balanced message distribution
- Optimized for both write and read operations

## Contributing
1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License
This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.