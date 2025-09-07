# Orisun - The batteries included event store.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/oexza/Orisun/actions/workflows/ci.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/ci.yml)
[![Release](https://github.com/oexza/Orisun/actions/workflows/release.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/release.yml)

## Introduction

Orisun is a modern event store designed for building event-driven applications. It combines PostgreSQL's reliability with NATS JetStream's real-time capabilities to provide a complete event sourcing solution.

### Key Features

- **PostgreSQL Backend**: Reliable, transactional event storage with ACID guarantees
- **Embedded NATS**: Real-time event streaming without external dependencies
- **Multi-tenant Architecture**: Isolated boundaries with separate schemas
- **Optimistic Concurrency**: Stream-based versioning with expected version checks
- **Rich Querying**: Filter events by stream, tags, and global position
- **Real-time Subscriptions**: Subscribe to event changes as they happen
- **Admin Dashboard**: Built-in web interface for user management and system monitoring
- **User Management**: Create, view, and manage users through the admin interface
- **Clustered Deployment**: High availability with automatic failover and distributed locking
- **Horizontal Scaling**: Add nodes dynamically for increased throughput and resilience
- **Zero-Downtime Failover**: Seamless takeover when nodes go down or become unavailable

## Quick Start (Run in One Minute)

### Option 1: Docker Compose (Recommended)

The fastest way to get Orisun running with PostgreSQL:

Create a `docker-compose.yml` file:

```yaml
version: '3.8'
services:
  postgres:
    image: postgres:17.5-alpine
    environment:
      POSTGRES_DB: orisun
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  orisun:
    image: orexza/orisun:latest
    environment:
      ORISUN_PG_HOST: postgres
      ORISUN_PG_PORT: 5432
      ORISUN_PG_USER: postgres
      ORISUN_PG_PASSWORD: postgres
      ORISUN_PG_NAME: orisun
      ORISUN_PG_SCHEMAS: "orisun_test_1:public,orisun_admin:admin"
      ORISUN_BOUNDARIES: '[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_admin","description":"admin boundary"}]'
      ORISUN_ADMIN_BOUNDARY: orisun_admin
      ORISUN_ADMIN_USERNAME: admin
      ORISUN_ADMIN_PASSWORD: changeit
    ports:
      - "8992:8992"
      - "5005:5005"
    depends_on:
      - postgres

volumes:
  postgres_data:
```

Then run:

```bash
# Start Orisun and PostgreSQL
docker-compose up -d

# Access the admin dashboard at http://localhost:8992
# Default credentials: admin/changeit
```

### Option 2: Download Binary

1. Download the appropriate binary for your platform from the [Releases page](https://github.com/oexza/Orisun/releases)
2. Run with basic configuration:

```bash
# Configure PostgreSQL connection
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS="orisun_test_1:public,orisun_admin:admin" \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
./orisun-[platform]-[arch]

# Access the admin dashboard at http://localhost:8992
# Default credentials: admin/changeit
```

Replace `[platform]-[arch]` with your platform (e.g., `darwin-arm64`, `linux-amd64`)

### Option 3: Docker

Run Orisun directly with Docker:

```bash
# Make sure you have PostgreSQL running or use host.docker.internal to connect to host machine's PostgreSQL
docker run -d \
  --name orisun \
  -p 8992:8992 \
  -p 5005:5005 \
  -e ORISUN_PG_USER=postgres \
  -e ORISUN_PG_NAME=orisun \
  -e ORISUN_PG_PASSWORD=password@1 \
  -e ORISUN_PG_HOST=host.docker.internal \
  -e ORISUN_PG_PORT=5432 \
  -e ORISUN_PG_SCHEMAS="orisun_test_1:public,orisun_admin:admin" \
  -e ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_admin","description":"admin boundary"}]' \
  -e ORISUN_ADMIN_BOUNDARY=orisun_admin \
  -e ORISUN_ADMIN_USERNAME=admin \
  -e ORISUN_ADMIN_PASSWORD=changeit \
  orexza/orisun:latest

# Access the admin dashboard at http://localhost:8992
# Default credentials: admin/changeit
```

## Clients

Orisun provides official client libraries for Node.js and Java, with support for generating clients for other languages using Protocol Buffers.

### Official Client Libraries

**Node.js Client:**
For Node.js applications, see the [Node.js Client README](clients/node/README.md) for installation and usage instructions.

**Java Client:**
For Java applications, see the [Java Client README](clients/java/README.md) for installation and usage instructions.

### Generate Custom Clients

For other programming languages, you can generate clients using the Protocol Buffers definition file available at `eventstore/eventstore.proto`.

**Go Client:**
```bash
# Install protoc-gen-go and protoc-gen-go-grpc
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate Go client
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       eventstore/eventstore.proto
```

**Python Client:**
```bash
# Install grpcio-tools
pip install grpcio-tools

# Generate Python client
python -m grpc_tools.protoc -I. \
       --python_out=. \
       --grpc_python_out=. \
       eventstore/eventstore.proto
```

**C# Client:**
```bash
# Install Grpc.Tools package
dotnet add package Grpc.Tools

# Generate C# client (add to .csproj)
<Protobuf Include="eventstore/eventstore.proto" GrpcServices="Client" />
```

## Advanced Configuration

### Boundaries and Schemas
In Orisun, a "boundary" directly corresponds to a PostgreSQL schema. Boundaries must be pre-configured at startup:

```bash
# Configure allowed boundaries (schemas) and their descriptions
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"boundary1"},{"name":"orisun_test_2","description":"boundary2"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_PG_HOST=localhost \
[... other config ...] \
orisun-darwin-arm64
```

When Orisun starts:
1. It validates and creates the specified schemas if they don't exist
2. Only requests to these pre-configured boundaries will be accepted
3. Each boundary maintains its own event sequences and consistency guarantees

For example:
- If `ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin`, then:
  - ✅ `boundary: "orisun_test_1"` - Request will succeed
  - ✅ `boundary: "orisun_test_2"` - Request will succeed
  - ✅ `boundary: "orisun_admin"` - Request will succeed
  - ❌ `boundary: "payments"` - Request will fail (schema not configured)

### Admin Dashboard
Orisun includes a built-in admin dashboard accessible at the configured admin port (default: 8992):

```bash
# Access URL (default)
http://localhost:8992

# Default admin credentials
Username: admin
Password: changeit
```

The dashboard provides:
- **User Management**: Create, view, and delete users
- **System Monitoring**: View event counts and system statistics
- **Authentication**: Secure login system with session management

## gRPC API Examples

All gRPC API calls require authentication using a basic auth header. You can provide this with grpcurl using the `-H` flag:

```bash
grpcurl -H "Authorization: Basic dXNlcm5hbWU6cGFzc3dvcmQ=" -d @ localhost:50051 eventstore.EventStore/SaveEvents
```

### SaveEvents
Save events to a specific schema/boundary. Here's an example of saving user registration events:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_1",
  "events": [
    {
      "event_id": "0191b93c-5f3c-75c8-92ce-5a3300709178",
      "event_type": "UserRegistered",
      "data": "{\"email\": \"john.doe@example.com\", \"username\": \"johndoe\", \"full_name\": \"John Doe\"}",
      "metadata": "{\"source\": \"web_signup\", \"ip_address\": \"192.168.1.1\"}"
    },
    {
      "event_id": "0191b93c-5f3c-75c8-92ce-5a3300709179",
      "event_type": "UserProfileCompleted",
      "data": "{\"phone\": \"+1234567890\", \"address\": \"123 Main St, City, Country\"}",
      "metadata": "{\"completed_at\": \"2024-01-20T15:30:00Z\"}"
    }
  ],
  "stream": {
    "expected_version": -1,
    "name": "user-1234"
  }
}
EOF
```

### GetEvents
Query events with various criteria. Here's an example of retrieving order events:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_2",
  "stream": {
    "name": "order-789"
  },
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "eventType", "value": "OrderCreated"}
        ]
      },
      {
        "tags": [
          {"key": "eventType", "value": "PaymentProcessed"}
        ]
      },
      {
        "tags": [
          {"key": "eventType", "value": "OrderShipped"}
        ]
      }
    ]
  },
  "count": 100,
  "direction": "ASC",
  "from_position": {
    "commit_position": 1000,
    "prepare_position": 999
  }
}
EOF
```

### CatchUpSubscribeToEvents
Subscribe to events with complex filtering. Here's an example of monitoring payment events:

```bash
grpcurl -d @ localhost:50051 eventstore.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "payment-processor",
  "boundary": "orisun_test_2",
  "afterPosition": {
    "commit_position": 0,
    "prepare_position": 0
  },
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "eventType", "value": "PaymentInitiated"}
        ]
      },
      {
        "tags": [
          {"key": "eventType", "value": "PaymentAuthorized"}
        ]
      },
      {
        "tags": [
          {"key": "eventType", "value": "PaymentFailed"}
        ]
      }
    ]
  }
}
EOF
```

## Common Use Cases

### Multiple Bounded Contexts
```bash
# User domain events in orisun_test_1 schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_1",
  "stream": {
    "name": "user-123",
    "expected_version": -1
  },
  "events": [
    {
      "event_id": "user-event-001",
      "event_type": "UserCreated",
      "data": "{\"username\": \"john_doe\", \"email\": \"john@example.com\"}",
      "metadata": "{\"source\": \"user_service\"}"
    }
  ]
}
EOF

# Order domain events in orisun_test_2 schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_2",
  "stream": {
    "name": "order-456",
    "expected_version": -1
  },
  "events": [
    {
      "event_id": "order-event-001",
      "event_type": "OrderCreated",
      "data": "{\"order_id\": \"456\", \"customer_id\": \"123\", \"total\": 99.99}",
      "metadata": "{\"source\": \"order_service\"}"
    }
  ]
}
EOF
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
| `ORISUN_PG_PASSWORD` | PostgreSQL password | postgres | Yes |
| `ORISUN_PG_NAME` | PostgreSQL database name | orisun | Yes |
| `ORISUN_PG_SCHEMAS` | Comma-separated list of boundary:schema mappings | `orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin` | Yes |
| `ORISUN_BOUNDARIES` | JSON array of boundary definitions with names and descriptions | `[{"name":"orisun_test_1","description":"boundary1"},{"name":"orisun_test_2","description":"boundary2"},{"name":"orisun_admin","description":"boundary3"}]` | Yes |
| `ORISUN_ADMIN_BOUNDARY` | Name of the boundary used for admin operations | orisun_admin | Yes |
| `ORISUN_ADMIN_PORT` | Port for the admin dashboard | 8992 | No |
| `ORISUN_ADMIN_USERNAME` | Admin dashboard username | admin | No |
| `ORISUN_ADMIN_PASSWORD` | Admin dashboard password | changeit | No |
| `ORISUN_GRPC_PORT` | gRPC server port | 5005 | No |
| `ORISUN_GRPC_ENABLE_REFLECTION` | Enable gRPC reflection | true | No |
| `ORISUN_NATS_SERVER_NAME` | NATS server name | orisun-nats-2 | No |
| `ORISUN_NATS_PORT` | NATS server port | 4224 | No |
| `ORISUN_NATS_MAX_PAYLOAD` | Maximum payload size for NATS messages | 1048576 | No |
| `ORISUN_NATS_STORE_DIR` | NATS storage directory | ./data/orisun/nats | No |
| `ORISUN_NATS_CLUSTER_NAME` | Name of the NATS cluster | orisun-nats-cluster | No |
| `ORISUN_NATS_CLUSTER_HOST` | Host for NATS cluster | 0.0.0.0 | No |
| `ORISUN_NATS_CLUSTER_PORT` | Port for NATS cluster | 6222 | No |
| `ORISUN_NATS_CLUSTER_USERNAME` | Username for NATS cluster | nats | No |
| `ORISUN_NATS_CLUSTER_PASSWORD` | Password for NATS cluster | password@1 | No |
| `ORISUN_NATS_CLUSTER_ENABLED` | Enable NATS clustering | false | No |
| `ORISUN_NATS_CLUSTER_TIMEOUT` | Timeout for NATS cluster operations | 1800s | No |
| `ORISUN_NATS_CLUSTER_ROUTES` | Comma-separated list of cluster routes | `nats://0.0.0.0:6223,nats://0.0.0.0:6224` | No |
| `ORISUN_POLLING_PUBLISHER_BATCH_SIZE` | Batch size for event polling | 1000 | No |
| `ORISUN_LOGGING_LEVEL` | Logging level (DEBUG, INFO, WARN, ERROR) | INFO | No |

### Running Modes

#### Standalone Mode
By default, Orisun runs in standalone mode. Here's an example configuration:

```bash
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_PORT=8992 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
./orisun-darwin-arm64
```

#### Clustered Mode
For high availability and horizontal scaling, Orisun supports clustered deployments with automatic failover and distributed locking. 


**Cluster Requirements:**
- Minimum 3 nodes for NATS JetStream quorum
- Shared PostgreSQL database accessible by all nodes
- Network connectivity between all cluster nodes
- Unique ports for each node (NATS, gRPC, Admin)

**Node 1 Configuration:**
```bash
ORISUN_PG_HOST=your-postgres-host \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_test_2","description":"test boundary 2"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_PORT=8992 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
ORISUN_NATS_CLUSTER_ENABLED=true \
ORISUN_NATS_CLUSTER_NAME=orisun-cluster \
ORISUN_NATS_CLUSTER_HOST=node1.example.com \
ORISUN_NATS_CLUSTER_PORT=6222 \
ORISUN_NATS_CLUSTER_USERNAME=nats \
ORISUN_NATS_CLUSTER_PASSWORD=secure_cluster_password \
ORISUN_NATS_CLUSTER_ROUTES='nats://node2.example.com:6222,nats://node3.example.com:6222' \
ORISUN_NATS_SERVER_NAME=orisun-node-1 \
ORISUN_NATS_STORE_DIR=./data/node1/nats \
./orisun-linux-amd64
```

**Node 2 Configuration:**
```bash
ORISUN_PG_HOST=your-postgres-host \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_test_2","description":"test boundary 2"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_PORT=8992 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_GRPC_PORT=5006 \
ORISUN_NATS_PORT=4223 \
ORISUN_NATS_CLUSTER_ENABLED=true \
ORISUN_NATS_CLUSTER_NAME=orisun-cluster \
ORISUN_NATS_CLUSTER_HOST=node2.example.com \
ORISUN_NATS_CLUSTER_PORT=6222 \
ORISUN_NATS_CLUSTER_USERNAME=nats \
ORISUN_NATS_CLUSTER_PASSWORD=secure_cluster_password \
ORISUN_NATS_CLUSTER_ROUTES='nats://node1.example.com:6222,nats://node3.example.com:6222' \
ORISUN_NATS_SERVER_NAME=orisun-node-2 \
ORISUN_NATS_STORE_DIR=./data/node2/nats \
./orisun-linux-amd64
```

**Node 3 Configuration:**
```bash
ORISUN_PG_HOST=your-postgres-host \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_test_2","description":"test boundary 2"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_PORT=8993 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_GRPC_PORT=5007 \
ORISUN_NATS_PORT=4224 \
ORISUN_NATS_CLUSTER_ENABLED=true \
ORISUN_NATS_CLUSTER_NAME=orisun-cluster \
ORISUN_NATS_CLUSTER_HOST=node3.example.com \
ORISUN_NATS_CLUSTER_PORT=6222 \
ORISUN_NATS_CLUSTER_USERNAME=nats \
ORISUN_NATS_CLUSTER_PASSWORD=secure_cluster_password \
ORISUN_NATS_CLUSTER_ROUTES='nats://node1.example.com:6222,nats://node2.example.com:6222' \
ORISUN_NATS_SERVER_NAME=orisun-node-3 \
ORISUN_NATS_STORE_DIR=./data/node3/nats \
./orisun-linux-amd64
```

**Cluster Behavior:**
- **Event Polling**: Each boundary is processed by exactly one node at a time using distributed locks
- **Projection Processing**: User, auth, and count projections automatically failover between nodes
- **Lock Acquisition**: Nodes continuously attempt to acquire locks for available boundaries
- **Automatic Failover**: When a node goes down, other nodes automatically pick up its workload
- **Load Distribution**: Work is automatically distributed across healthy nodes

**Monitoring Cluster Health:**
- Check logs for lock acquisition messages: `"Successfully acquired lock for boundary: <boundary_name>"`
- Monitor projection startup messages: `"User projector started"`, `"Auth user projector started"`
- Watch for failover events: `"Failed to acquire lock"` followed by retry attempts
- Admin dashboards on each node show local metrics

**Best Practices:**
- Use a load balancer for gRPC endpoints across nodes
- Monitor PostgreSQL connection pool usage
- Set up proper network security between cluster nodes
- Use persistent storage for NATS data directories
- Configure appropriate timeouts for your network latency
- Monitor NATS cluster status and JetStream health


**Kubernetes Deployment:**
For production Kubernetes deployments, consider:
- Using StatefulSets for persistent NATS storage
- ConfigMaps for environment configuration
- PersistentVolumes for NATS data directories
- Horizontal Pod Autoscaler for scaling based on load
- Network policies for security between pods

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
   - For clusters: Verify network connectivity between nodes
   - Check firewall rules for cluster ports (NATS cluster port 6222)

2. **Performance Issues**
   - Monitor PostgreSQL query performance
   - Check NATS message backlog
   - Verify system resources (CPU, memory, disk)
   - For clusters: Monitor lock contention in logs
   - Check projection processing delays

3. **Schema Issues**
   - Ensure schemas are properly configured
   - Check PostgreSQL user permissions
   - Verify boundary configurations match schema mappings
   - Validate JSON format in ORISUN_BOUNDARIES environment variable

4. **Admin Dashboard Issues**
   - Check admin boundary configuration
   - Verify admin credentials
   - Ensure admin port is accessible
   - For clusters: Each node has its own admin dashboard

5. **Cluster-Specific Issues**
   - **Lock Acquisition Failures**: Check logs for "Failed to acquire lock" messages - this is normal behavior when other nodes hold locks
   - **Projection Startup Issues**: Look for "Failed to start [projection] projection" followed by retry attempts
   - **NATS Cluster Issues**: Verify cluster routes configuration and ensure all nodes can reach each other
   - **Split Brain Prevention**: Ensure minimum 3 nodes for proper JetStream quorum
   - **Failover Delays**: Nodes may take 5-10 seconds to detect and take over from failed nodes
   - **Data Directory Conflicts**: Ensure each node has unique NATS storage directories

6. **Common Log Messages (Normal Behavior)**
   - `"Failed to acquire lock for boundary: <name>"` - Another node is processing this boundary
   - `"Successfully acquired lock for boundary: <name>"` - This node is now processing the boundary
   - `"User projector started"` - Projection service started successfully
   - `"Failed to start user projection (likely due to lock contention)"` - Will retry automatically

## Building from Source

### Prerequisites
- Go 1.24.2+
- Make

1. Clone the repository:
```bash
git clone https://github.com/oexza/orisun.git
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
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_PORT=8992 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
./orisun-darwin-arm64
```

## Architecture

Orisun uses:
- **PostgreSQL 17.5+**: For durable event storage and consistency guarantees
- **NATS JetStream 2.11.1+**: For real-time event streaming and pub/sub
- **gRPC**: For client-server communication
- **Go 1.24.2+**: For high-performance server implementation
- **Admin Dashboard**: Built-in web interface for system management
- **User Management**: Integrated user administration system
- **Modular Design**: Plugin system for extending functionality

## Performance

Orisun is designed for high-performance event processing with comprehensive benchmarking to ensure optimal throughput and latency.

### Benchmark Results

The following benchmarks were conducted on a local development environment:

| Benchmark | Events/Sec | Description |
|-----------|------------|-------------|
| **SaveEvents_Single** | ~213 | Single event saves with unique streams |
| **SaveEvents_Batch_100** | ~1,900 | Batch saves (100 events per batch, new streams) |
| **SaveEvents_Batch_1000** | ~15,200 | Batch saves (1,000 events per batch, new streams) |
| **SaveEvents_Batch_10000** | ~59,400 | Batch saves (10,000 events per batch, new streams) |
| **SaveEvents_SingleStream_100** | ~1,737 | Batch saves (100 events per batch, same stream) |
| **SaveEvents_SingleStream_1000** | ~14,884 | Batch saves (1,000 events per batch, same stream) |
| **SaveEvents_ConcurrentStreams_10** | ~221 | Concurrent saves (10 workers, unique streams) |
| **SaveEvents_ConcurrentStreams_100** | ~221 | Concurrent saves (100 workers, unique streams) |
| **GetEvents** | ~2,250-2,500 | Event retrieval (50 events per request) |
| **MemoryUsage** | ~1,250-1,500 | Memory-intensive operations |
| **HighThroughput** | Variable | Concurrent operations with multiple workers |

### Benchmark Scenarios

- **SaveEvents_Single**: Individual event saves to new streams (simulates basic event creation)
- **SaveEvents_Batch**: Batch saves to new streams (simulates bulk data import scenarios)
- **SaveEvents_SingleStream**: Sequential batch saves to the same stream (simulates event sourcing append scenarios)
- **SaveEvents_ConcurrentStreams**: Concurrent saves where each worker uses its own stream (simulates multi-user scenarios)

### Performance Insights

- **Enhanced Granular Locking**: The updated implementation with two-tier locking (criteria-based + stream-level fallback) delivers excellent performance scaling
- **Batch Size Optimization**: Performance scales dramatically with batch size:
  - **100 events**: 1,769 events/sec (56.5ms latency)
  - **1,000 events**: 15,131 events/sec (66.1ms latency)
  - **5,000 events**: 45,607 events/sec (109.6ms latency)
- **Memory Efficiency**: Stable allocation patterns (115-165 allocs/op) with linear memory scaling relative to batch size
- **Consistency Guarantees**: Zero data corruption with optimistic concurrency control through dual version checking
- **Deadlock Prevention**: Ordered lock acquisition prevents deadlocks in high-concurrency scenarios
- **CTE Performance**: Optimized Common Table Expression-based insertion maintains high throughput while ensuring data integrity

### Key Performance Features

- **High Throughput**: Handles thousands of events per second
- **Efficient Querying**: PostgreSQL indexes and GIN support for fast lookups
- **Load Balanced Distribution**: Automatic message distribution across nodes
- **Optimized Operations**: Both write and read operations are performance-tuned
- **Real-time Streaming**: Minimal latency event streaming with NATS JetStream
- **Concurrent Processing**: Parallel execution support for maximum throughput
- **Memory Efficient**: Optimized memory usage for large-scale deployments

### Running Benchmarks

To run the benchmark suite yourself:

```bash
# Run all benchmarks
go test -bench=. -benchmem ./benchmark_test.go

# Run specific benchmark
go test -bench=BenchmarkSaveEvents_Single -benchtime=5s ./benchmark_test.go

# Use the collection script
./collect_benchmarks.sh
```

*Note: Performance results may vary based on hardware, PostgreSQL configuration, and system load.*

## Development

### Development Setup
1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/orisun.git`
3. Create a feature branch: `git checkout -b feature/amazing-feature`
4. Install dependencies: `go mod download`
5. Make your changes
6. Run tests: `go test ./...`
7. Commit changes: `git commit -m 'Add some amazing feature'`
8. Push to your fork: `git push origin feature/amazing-feature`
9. Open a Pull Request

### Building the Docker Image Locally

```bash
docker build -t orisun:local .
```

With specific version information:

```bash
docker build \
  --build-arg VERSION=1.0.0 \
  --build-arg TARGET_OS=linux \
  --build-arg TARGET_ARCH=amd64 \
  -t orisun:local .
```

### Versioning

Orisun follows semantic versioning (SemVer) for all releases. Check the [Releases page](https://github.com/oexza/Orisun/releases) for the latest versions and detailed release notes.

### Usage

#### Starting the Server
```bash
# Run from source
go run main.go

# Or run the built binary
./orisun-[platform]-[arch]
```

#### Using Generated Clients

**Using the Generated Client:**

Once you've generated the client code, you can connect to Orisun and use the EventStore service:

```go
// Go example
conn, err := grpc.Dial("localhost:5005", grpc.WithInsecure())
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := eventstore.NewEventStoreClient(conn)

// Add basic auth header
ctx := metadata.AppendToOutgoingContext(context.Background(), 
    "authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("username:password")))

// Save events
response, err := client.SaveEvents(ctx, &eventstore.SaveEventsRequest{
    Boundary: "orisun_test_1",
    Events: []*eventstore.Event{
        {
            EventId: "unique-event-id",
            EventType: "UserRegistered",
            Data: `{"email": "user@example.com"}`,
        },
    },
})
```

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