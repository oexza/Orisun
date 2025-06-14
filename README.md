# Orisun - The batteries included event store.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Overview

Orisun is a modern event store designed for building event-driven applications. It combines PostgreSQL's reliability with NATS JetStream's real-time capabilities to provide a complete event sourcing solution.

### Key Features

- **PostgreSQL Backend**: Reliable, transactional event storage
- **Embedded NATS**: Real-time event streaming without external dependencies
- **Multi-tenant Architecture**: Isolated boundaries with separate schemas
- **Optimistic Concurrency**: Stream-based versioning with expected version checks
- **Rich Querying**: Filter events by stream, tags, and global position
- **Real-time Subscriptions**: Subscribe to event changes as they happen
- **Admin Dashboard**: Built-in web interface for monitoring and management

## Getting Started

### Prerequisites

- PostgreSQL 13+
- Go 1.20+ (for building from source)

### Quick Start

1. **Configure environment variables**:

```bash
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_ADMIN_BOUNDARY:orisun_admin \
ORISUN_PG_SCHEMAS=users:test2,orisun_admin:admin \
./orisun-darwin-arm64
```

## Key Concepts

### Boundaries and Schemas
In Orisun, a "boundary" directly corresponds to a PostgreSQL schema. Boundaries must be pre-configured at startup:

```bash
# Configure allowed boundaries (schemas)
ORISUN_PG_SCHEMAS=users:test2,orisun_admin:admin \
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
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin \
ORISUN_PG_HOST=localhost \
[... other config ...] \
orisun-darwin-arm64
```

## gRPC API Examples

All gRPC API calls require authentication using a basic auth header. You can provide this with grpcurl using the `-H` flag:

```bash
grpcurl -H "Authorization: Basic dXNlcm5hbWU6cGFzc3dvcmQ=" -d @ localhost:50051 eventstore.EventStore/SaveEvents
```

### SaveEvents
Save events to a specific schema/boundary. Here's an example of saving user registration events:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents
{
  "boundary": "users",
  "events": [
    {
      "event_id": "0191b93c-5f3c-75c8-92ce-5a3300709178",
      "event_type": "UserRegistered",
      "tags": [
        {"key": "tenant_id", "value": "tenant-456"},
        {"key": "source", "value": "web_signup"}
      ],
      "data": "{\"email\": \"john.doe@example.com\", \"username\": \"johndoe\", \"full_name\": \"John Doe\"}",
      "metadata": "{\"source\": \"web_signup\", \"ip_address\": \"192.168.1.1\"}"
    },
    {
      "event_id": "0191b93c-5f3c-75c8-92ce-5a3300709179",
      "event_type": "UserProfileCompleted",
      "tags": [
        {"key": "tenant_id", "value": "tenant-456"}
      ],
      "data": "{\"phone\": \"+1234567890\", \"address\": \"123 Main St, City, Country\"}",
      "metadata": "{\"completed_at\": \"2024-01-20T15:30:00Z\"}"
    }
  ],
  "stream": {
    "expected_version": -1,
    "name": "user-1234"
  }
}
```

### GetEvents
Query events with various criteria. Here's an example of retrieving order events:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/GetEvents <<
{
  "boundary": "orders",
  "stream": {
    "name": "order-789"
  },
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "event_type", "value": "OrderCreated"}
        ]
      },
      {
        "tags": [
          {"key": "event_type", "value": "PaymentProcessed"}
        ]
      },
      {
        "tags": [
          {"key": "event_type", "value": "OrderShipped"}
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
Subscribe to events with complex filtering. Here's an example of monitoring payment events:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/SubscribeToEvents <<EOF
{
  "subscriber_name": "payment-processor",
  "boundary": "payments",
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "event_type", "value": "PaymentInitiated"}
        ]
      },
      {
        "tags": [
          {"key": "event_type", "value": "PaymentAuthorized"}
        ]
      },
      {
        "tags": [
          {"key": "event_type", "value": "PaymentFailed"}
        ]
      }
    ]
  }
}
```

### PublishToPubSub
Publish a message to a pub/sub topic. Here's an example of publishing order notifications:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/PublishToPubSub <<
{
  "subject": "order.notifications",
  "data": "{\"order_id\": \"order-789\", \"status\": \"shipped\", \"customer_email\": \"john.doe@example.com\"}",
  "metadata": "{\"priority\": \"high\", \"notification_type\": \"shipping_update\"}"
}
```

### SubscribeToPubSub
Subscribe to messages from a pub/sub topic. Here's an example of processing inventory updates:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/SubscribeToPubSub <<
{
  "subject": "inventory.updates",
  "consumer_name": "inventory-processor"
}
```

## Common Use Cases

### Multiple Bounded Contexts
```bash
# User domain events in users schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents
{
  "boundary": "users",
  "events": [...]
}

# Order domain events in orders schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents
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

## Configuration

Orisun can be configured using environment variables:

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `ORISUN_PG_HOST` | PostgreSQL host | localhost | Yes |
| `ORISUN_PG_PORT` | PostgreSQL port | 5432 | Yes |
| `ORISUN_PG_USER` | PostgreSQL username | postgres | Yes |
| `ORISUN_PG_PASSWORD` | PostgreSQL password | - | Yes |
| `ORISUN_PG_NAME` | PostgreSQL database name | - | Yes |
| `ORISUN_PG_SCHEMAS` | Comma-separated list of boundary:schema mappings | - | Yes |
| `ORISUN_GRPC_PORT` | gRPC server port | 50051 | No |
| `ORISUN_NATS_PORT` | NATS server port | 4222 | No |
| `ORISUN_NATS_STORE_DIR` | NATS storage directory | /data/nats | No |
| `ORISUN_NATS_MAX_PAYLOAD` | Maximum payload size for NATS messages | 1048576 | No |
| `ORISUN_NATS_CLUSTER_NAME` | Name of the NATS cluster | orisun-nats-cluster | No |
| `ORISUN_NATS_CLUSTER_HOST` | Host for NATS cluster | localhost | No |
| `ORISUN_NATS_CLUSTER_PORT` | Port for NATS cluster | 6222 | No |
| `ORISUN_NATS_CLUSTER_USERNAME` | Username for NATS cluster | nats | No |
| `ORISUN_NATS_CLUSTER_PASSWORD` | Password for NATS cluster | password@1 | No |
| `ORISUN_NATS_CLUSTER_ENABLED` | Enable NATS clustering | false | No |
| `ORISUN_NATS_CLUSTER_TIMEOUT` | Timeout for NATS cluster operations | 60s | No |
| `ORISUN_NATS_CLUSTER_ROUTES` | Comma-separated list of cluster routes | nats://localhost:6333,nats://localhost:6334 | No |

### Running Modes

#### Standalone Mode
By default, Orisun runs in standalone mode. Here's an example configuration:

```bash
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=public \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
orisun-darwin-arm64
```

#### Clustered Mode
For high availability, you can run Orisun in clustered mode. Here's an example configuration:

```bash
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=public \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
ORISUN_NATS_CLUSTER_ENABLED=true \
ORISUN_NATS_CLUSTER_NAME=orisun-cluster \
ORISUN_NATS_CLUSTER_HOST=localhost \
ORISUN_NATS_CLUSTER_PORT=6222 \
ORISUN_NATS_CLUSTER_USERNAME=nats \
ORISUN_NATS_CLUSTER_PASSWORD=your_cluster_password \
ORISUN_NATS_CLUSTER_ROUTES='nats://localhost:6333,nats://localhost:6334' \
orisun-darwin-arm64
```

## Error Handling

### Common Error Responses
- `ALREADY_EXISTS`: Consistency condition violation (e.g., concurrent updates to the same stream)
- `INVALID_ARGUMENT`: Missing or invalid required fields
- `INTERNAL`: Database or system errors (check logs for details)
- `NOT_FOUND`: Requested stream or consumer doesn't exist

### Troubleshooting
1. **Connection Issues**
   - Verify PostgreSQL connection settings
   - Check if PostgreSQL is running and accessible
   - Ensure NATS ports are available

2. **Performance Issues**
   - Monitor PostgreSQL query performance
   - Check NATS message backlog
   - Verify system resources (CPU, memory, disk)

3. **Schema Issues**
   - Ensure schemas are properly configured
   - Check PostgreSQL user permissions

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
# Build for current system (default)
./build.sh

# Cross-compile for specific OS/architecture
./build.sh linux amd64     # For Linux x86_64
./build.sh darwin arm64    # For macOS Apple Silicon
./build.sh windows amd64   # For Windows x86_64
```

3. Run the built binary:
```bash
# Using environment variables
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=test:public \
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
- PostgreSQL for durable event storage and consistency guarantees (with plans to support other databases)
- NATS JetStream for real-time event streaming and pub/sub
- gRPC for client-server communication
- Modular plugin system for extending functionality and adding new storage implementations

## Performance
- Handles thousands of events per second
- Efficient querying with PostgreSQL indexes
- Load balanced message distribution
- Optimized for both write and read operations

## Contributing

### Development Setup
1. Fork the repository
2. Clone your fork: `git clone https://github.com/yourusername/orisun.git`
3. Create a feature branch: `git checkout -b feature/amazing-feature`
4. Install dependencies: `go mod download`
5. Make your changes
6. Run tests: `go test ./...`
7. Commit changes: `git commit -m 'Add some amazing feature'`
8. Push to your fork: `git push origin feature/amazing-feature`
9. Open a Pull Request

### Code Style
- Follow Go best practices and style guide
- Write meaningful commit messages
- Include tests for new features
- Update documentation as needed

### Community Guidelines
- Report bugs and security issues responsibly
- Participate in discussions and reviews constructively

## License
This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.