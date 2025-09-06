# Orisun - The batteries included event store.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/yourusername/Orisun/actions/workflows/ci.yml/badge.svg)](https://github.com/yourusername/Orisun/actions/workflows/ci.yml)
[![Release](https://github.com/yourusername/Orisun/actions/workflows/release.yml/badge.svg)](https://github.com/yourusername/Orisun/actions/workflows/release.yml)

## Overview

Orisun is a modern event store designed for building event-driven applications. It combines PostgreSQL's reliability with NATS JetStream's real-time capabilities to provide a complete event sourcing solution.

### Key Features

- **PostgreSQL Backend**: Reliable, transactional event storage with ACID guarantees
- **Embedded NATS**: Real-time event streaming without external dependencies
- **Multi-tenant Architecture**: Isolated boundaries with separate schemas
- **Optimistic Concurrency**: Stream-based versioning with expected version checks
- **Rich Querying**: Filter events by stream, tags, and global position
- **Real-time Subscriptions**: Subscribe to event changes as they happen
- **Admin Dashboard**: Built-in web interface for user management and system monitoring
- **Event Projections**: Built-in read models and projections for common use cases
- **User Management**: Create, view, and manage users through the admin interface
- **Clustered Deployment**: High availability with automatic failover and distributed locking
- **Horizontal Scaling**: Add nodes dynamically for increased throughput and resilience
- **Resilient Projections**: Automatic retry and failover for projection services across nodes
- **Load Balancing**: Event processing automatically distributed across healthy nodes
- **Zero-Downtime Failover**: Seamless takeover when nodes go down or become unavailable
```
## Releases and Versioning

Orisun follows semantic versioning (SemVer) for all releases.

- **Major versions** (X.0.0): Introduce breaking changes to APIs or core functionality
- **Minor versions** (0.X.0): Add new features in a backward-compatible manner
- **Patch versions** (0.0.X): Include backward-compatible bug fixes and performance improvements

Check the [Releases page](https://github.com/yourusername/Orisun/releases) for the latest versions and detailed release notes.

### Release Process

Releases are automated through GitHub Actions. To create a new release:

1. Ensure all changes are committed and pushed to the main branch
2. Run the release script with the new version number (without 'v' prefix):
   ```bash
   ./scripts/release.sh 1.2.3
   ```
3. The script will create and push a git tag (e.g., v1.2.3)
4. GitHub Actions will automatically:
   - Build binaries for multiple platforms (Linux, macOS, Windows)
   - Create a Docker image and push it to Docker Hub
   - Publish the Node.js client to npm
   - Generate release notes and create a GitHub release

### Installation Options

#### Binary Downloads
Download pre-built binaries from the [Releases page](https://github.com/yourusername/Orisun/releases).

#### Docker
```bash
docker pull orisun/orisun:latest
# or a specific version
docker pull orisun/orisun:v1.2.3
```

#### Node.js Client
```bash
npm install @orisun/client
```

## Getting Started

### Prerequisites

- PostgreSQL 13+
- Go 1.24+ (for building from source)

### Quick Start

1. **Configure environment variables**:

```bash
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"boundary1"},{"name":"orisun_test_2","description":"boundary2"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_PORT=8991 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
./orisun-darwin-arm64
```

## Key Concepts

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
3. Each boundary maintains its own:
   - Event sequences
   - Consistency guarantees
   - Event tables

For example:
- If `ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin`, then:
  - ✅ `boundary: "orisun_test_1"` - Request will succeed
  - ✅ `boundary: "orisun_test_2"` - Request will succeed
  - ✅ `boundary: "orisun_admin"` - Request will succeed
  - ❌ `boundary: "payments"` - Request will fail (schema not configured)

This boundary pre-configuration ensures:
- Security through explicit schema allow listing
- Clear separation of domains
- Controlled resource allocation

### Admin Dashboard
Orisun includes a built-in admin dashboard accessible at the configured admin port (default: 8991). The dashboard provides:

- **User Management**: Create, view, and delete users
- **System Monitoring**: View event counts and system statistics
- **Authentication**: Secure login system with session management
- **Real-time Updates**: Live updates of system metrics

**Access the Admin Dashboard:**
```bash
# Default admin credentials
Username: admin
Password: changeit

# Access URL (default)
http://localhost:8991
```

**Admin Dashboard Features:**
- **Dashboard**: Overview of system metrics and user counts
- **Users**: Manage system users (create, view, delete)
- **Login/Logout**: Secure authentication system

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
  "boundary": "orisun_test_1",
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
  "boundary": "orisun_test_2",
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
  "boundary": "orisun_test_2",
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

## Common Use Cases

### Multiple Bounded Contexts
```bash
# User domain events in orisun_test_1 schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents
{
  "boundary": "orisun_test_1",
  "events": [...]
}

# Order domain events in orisun_test_2 schema
grpcurl -d @ localhost:50051 eventstore.EventStore/SaveEvents
{
  "boundary": "orisun_test_2",
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
| `ORISUN_PG_PASSWORD` | PostgreSQL password | postgres | Yes |
| `ORISUN_PG_NAME` | PostgreSQL database name | orisun | Yes |
| `ORISUN_PG_SCHEMAS` | Comma-separated list of boundary:schema mappings | `orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin` | Yes |
| `ORISUN_BOUNDARIES` | JSON array of boundary definitions with names and descriptions | `[{"name":"orisun_test_1","description":"boundary1"},{"name":"orisun_test_2","description":"boundary2"},{"name":"orisun_admin","description":"boundary3"}]` | Yes |
| `ORISUN_ADMIN_BOUNDARY` | Name of the boundary used for admin operations | orisun_admin | Yes |
| `ORISUN_ADMIN_PORT` | Port for the admin dashboard | 8991 | No |
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
ORISUN_ADMIN_PORT=8991 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
./orisun-darwin-arm64
```

#### Clustered Mode
For high availability and horizontal scaling, Orisun supports clustered deployments with automatic failover and distributed locking. 

**Key Features:**
- **Distributed Locking**: JetStream-based locks prevent duplicate processing across nodes
- **Resilient Projections**: Automatic retry and failover for projection services
- **Load Balancing**: Event processing distributed across available nodes
- **High Availability**: Automatic failover when nodes go down
- **Consistent Event Processing**: Each boundary processed by exactly one node at a time

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
ORISUN_ADMIN_PORT=8991 \
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

#### Docker Deployment
Orisun can be deployed using Docker for easier container orchestration:

**Dockerfile Example:**
```dockerfile
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY orisun-linux-amd64 .
CMD ["./orisun-linux-amd64"]
```

**Docker Compose for Clustered Deployment:**
```yaml
version: '3.8'
services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: orisun
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  orisun-node-1:
    image: orisun:latest
    environment:
      ORISUN_PG_HOST: postgres
      ORISUN_PG_PORT: 5432
      ORISUN_PG_USER: postgres
      ORISUN_PG_PASSWORD: postgres
      ORISUN_PG_NAME: orisun
      ORISUN_PG_SCHEMAS: "orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin"
      ORISUN_BOUNDARIES: '[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_test_2","description":"test boundary 2"},{"name":"orisun_admin","description":"admin boundary"}]'
      ORISUN_ADMIN_BOUNDARY: orisun_admin
      ORISUN_ADMIN_PORT: 8991
      ORISUN_GRPC_PORT: 5005
      ORISUN_NATS_PORT: 4222
      ORISUN_NATS_CLUSTER_ENABLED: "true"
      ORISUN_NATS_CLUSTER_NAME: orisun-cluster
      ORISUN_NATS_CLUSTER_HOST: orisun-node-1
      ORISUN_NATS_CLUSTER_PORT: 6222
      ORISUN_NATS_CLUSTER_ROUTES: "nats://orisun-node-2:6222,nats://orisun-node-3:6222"
      ORISUN_NATS_SERVER_NAME: orisun-node-1
    ports:
      - "5005:5005"
      - "8991:8991"
      - "4222:4222"
    depends_on:
      - postgres
    volumes:
      - node1_data:/root/data

  orisun-node-2:
    image: orisun:latest
    environment:
      ORISUN_PG_HOST: postgres
      ORISUN_PG_PORT: 5432
      ORISUN_PG_USER: postgres
      ORISUN_PG_PASSWORD: postgres
      ORISUN_PG_NAME: orisun
      ORISUN_PG_SCHEMAS: "orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin"
      ORISUN_BOUNDARIES: '[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_test_2","description":"test boundary 2"},{"name":"orisun_admin","description":"admin boundary"}]'
      ORISUN_ADMIN_BOUNDARY: orisun_admin
      ORISUN_ADMIN_PORT: 8992
      ORISUN_GRPC_PORT: 5006
      ORISUN_NATS_PORT: 4223
      ORISUN_NATS_CLUSTER_ENABLED: "true"
      ORISUN_NATS_CLUSTER_NAME: orisun-cluster
      ORISUN_NATS_CLUSTER_HOST: orisun-node-2
      ORISUN_NATS_CLUSTER_PORT: 6222
      ORISUN_NATS_CLUSTER_ROUTES: "nats://orisun-node-1:6222,nats://orisun-node-3:6222"
      ORISUN_NATS_SERVER_NAME: orisun-node-2
    ports:
      - "5006:5006"
      - "8992:8992"
      - "4223:4223"
    depends_on:
      - postgres
    volumes:
      - node2_data:/root/data

  orisun-node-3:
    image: orisun:latest
    environment:
      ORISUN_PG_HOST: postgres
      ORISUN_PG_PORT: 5432
      ORISUN_PG_USER: postgres
      ORISUN_PG_PASSWORD: postgres
      ORISUN_PG_NAME: orisun
      ORISUN_PG_SCHEMAS: "orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin"
      ORISUN_BOUNDARIES: '[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_test_2","description":"test boundary 2"},{"name":"orisun_admin","description":"admin boundary"}]'
      ORISUN_ADMIN_BOUNDARY: orisun_admin
      ORISUN_ADMIN_PORT: 8993
      ORISUN_GRPC_PORT: 5007
      ORISUN_NATS_PORT: 4224
      ORISUN_NATS_CLUSTER_ENABLED: "true"
      ORISUN_NATS_CLUSTER_NAME: orisun-cluster
      ORISUN_NATS_CLUSTER_HOST: orisun-node-3
      ORISUN_NATS_CLUSTER_PORT: 6222
      ORISUN_NATS_CLUSTER_ROUTES: "nats://orisun-node-1:6222,nats://orisun-node-2:6222"
      ORISUN_NATS_SERVER_NAME: orisun-node-3
    ports:
      - "5007:5007"
      - "8993:8993"
      - "4224:4224"
    depends_on:
      - postgres
    volumes:
      - node3_data:/root/data

volumes:
  postgres_data:
  node1_data:
  node2_data:
  node3_data:
```

**Kubernetes Deployment:**
For production Kubernetes deployments, consider:
- Using StatefulSets for persistent NATS storage
- ConfigMaps for environment configuration
- Services for load balancing gRPC endpoints
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
ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_admin","description":"admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_PORT=8991 \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
./orisun-darwin-arm64
```

## Usage

### Starting the Server
```bash
cd ./orisun
go run main.go
```

### Client Libraries

While official client libraries are coming soon, you can generate clients for your favorite programming language using the Protocol Buffers definition file.

**Generate clients from the proto file:**

The gRPC service definition is available at `eventstore/eventstore.proto`. You can use the Protocol Buffers compiler (`protoc`) to generate client code for any supported language.

**Examples:**

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

**Java Client:**
```bash
# Using protoc with Java plugin
protoc --java_out=src/main/java \
       --grpc-java_out=src/main/java \
       --plugin=protoc-gen-grpc-java=/path/to/protoc-gen-grpc-java \
       eventstore/eventstore.proto
```

**Node.js/TypeScript Client:**
```bash
# Install dependencies
npm install grpc-tools @grpc/grpc-js @grpc/proto-loader

# Generate TypeScript definitions
protoc --plugin=protoc-gen-ts=./node_modules/.bin/protoc-gen-ts \
       --ts_out=grpc_js:. \
       --js_out=import_style=commonjs:. \
       --grpc_out=grpc_js:. \
       eventstore/eventstore.proto
```

**C# Client:**
```bash
# Install Grpc.Tools package
dotnet add package Grpc.Tools

# Generate C# client (add to .csproj)
<Protobuf Include="eventstore/eventstore.proto" GrpcServices="Client" />
```

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

**Authentication:**
All gRPC calls require basic authentication. Include the authorization header with base64-encoded credentials:
```
Authorization: Basic <base64(username:password)>
```

## Architecture
Orisun uses:
- **PostgreSQL**: For durable event storage and consistency guarantees
- **NATS JetStream**: For real-time event streaming and pub/sub
- **gRPC**: For client-server communication
- **Admin Dashboard**: Built-in web interface for system management
- **Event Projections**: Built-in read models and projections
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