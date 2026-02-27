# Orisun - The batteries included event store.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/oexza/Orisun/actions/workflows/ci.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/ci.yml)
[![Release](https://github.com/oexza/Orisun/actions/workflows/release.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/release.yml)

## Table of Contents

- [Introduction](#introduction)
  - [What is Command Context Consistency?](#what-is-command-context-consistency)
  - [Key Features](#key-features)
- [Quick Start](#quick-start)
  - [Docker Compose](#option-1-docker-compose-recommended---60-seconds)
  - [Download Binary](#option-2-download-binary)
  - [Docker Standalone](#option-3-docker-standalone)
- [Getting Started Guide](#getting-started-guide)
  - [Your First Event](#your-first-event)
  - [Querying Events](#querying-events)
  - [Subscribing to Events](#subscribing-to-events)
  - [Command Context Consistency in Practice](#command-context-consistency-in-practice)
- [Architecture Overview](#architecture-overview)
- [Clients](#clients)
- [Configuration](#configuration)
- [Advanced Usage](#advanced-usage)
  - [Multiple Bounded Contexts](#multiple-bounded-contexts)
  - [Schema Management](#schema-management)
- [gRPC API Reference](#grpc-api-reference)
- [Performance](#performance)
- [Development](#development)
- [License](#license)

## Introduction

Orisun is a batteries-included event store designed for modern event-driven applications. It combines pluggable storage backends (currently PostgreSQL, with SQLite and FoundationDB planned) with NATS JetStream's real-time streaming capabilities to deliver a complete event sourcing solution that's both powerful and easy to use.

**Command Context Consistency (CCC)**

Orisun implements **Command Context Consistency**—a conceptually simple approach to ensuring data consistency in event-sourced systems without the complexity of streams, aggregates, or predefined tags.

**What is Command Context Consistency?**

In CCC, each command defines its **context** as the set of events relevant to checking its business rules. The context is determined by querying events based on their **data content**, not by stream ID or aggregate root.

**Example: Money Transfer**
```
Command: transferMoney("Peter", "Janine", 10)
Context: All events where payload references Peter or Janine
  - accountOpened, moneyDeposited, moneyWithdrawn, moneyTransferred, etc.
Context Model: { accountBalances: [{holder: "Peter", balance: 100}, {holder: "Janine", balance: 50}] }
Business Rules: Both accounts exist, Peter has sufficient funds
```

**Two-Phase Consistency:**
1. **Check**: Build context model from queried events, validate business rules
2. **Record**: Before saving, re-run query to ensure context hasn't changed (optimistic locking)

**Key Advantages:**
- **No Aggregate/Stream Lock-in**: Query events by payload content, not pre-defined streams
- **Command-Specific Contexts**: Each command sees only the events relevant to its rules
- **Flexible Querying**: Filter by data content using JSONB containment queries
- **Simple Mental Model**: Like RDBMS queries + optimistic locking, but for events

Built for developers who need enterprise-grade event sourcing without the complexity, Orisun provides:
- **Zero-Configuration Setup**: Get started in minutes with sensible defaults
- **Production-Ready**: Built-in clustering, failover, and monitoring
- **Developer Experience**: Clean APIs, comprehensive documentation, and intuitive tooling
- **Batteries Included**: Everything you need in a single binary - no external dependencies required
- **Pluggable Storage**: PostgreSQL backend with pluggable architecture for future storage options

### Key Features

#### Core Event Sourcing
- **Reliable Event Storage**: PostgreSQL backend with full ACID compliance and transaction guarantees
- **Zero Message Loss**: Guaranteed event delivery with immediate error propagation on subscription failures
- **Optimistic Concurrency**: Boundary-based versioning with expected position checks
- **Rich Event Querying**: Filter by boundary, data content, and global position
- **Real-time Subscriptions**: Subscribe to event changes as they happen with catch-up subscriptions

#### Built-in Infrastructure
- **Embedded NATS JetStream**: Real-time event streaming without external dependencies
- **Multi-tenant Architecture**: Isolated boundaries with separate database schemas
- **Pluggable Storage**: PostgreSQL backend with production-ready reliability
- **Admin gRPC Service**: User management and system administration via gRPC
- **User Management**: Create and manage users with role-based access control
- **OpenTelemetry Tracing**: Built-in distributed tracing for observability

#### Production Ready
- **Clustered Deployment**: High availability with automatic failover and distributed locking
- **Horizontal Scaling**: Add nodes dynamically for increased throughput and resilience
- **Zero-Downtime Failover**: Seamless takeover when nodes go down or become unavailable
- **Comprehensive Monitoring**: Built-in metrics, health checks, and performance tracking

## Quick Start

### Option 1: Docker Compose (Recommended - 60 Seconds)
The fastest way to get started with Orisun:

1. **Create `docker-compose.yml`:**
```yaml
version: '3.8'
services:
  postgres:
    image: postgres:17.5-alpine3.22
    environment:
      POSTGRES_DB: orisun
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: password@1
    ports:
      - "5434:5432"
    volumes:
      - postgres-data:/var/lib/postgresql/data

  orisun:
    image: orexza/orisun:latest
    environment:
      ORISUN_PG_HOST: postgres
      ORISUN_PG_USER: postgres
      ORISUN_PG_PASSWORD: password@1
      ORISUN_PG_NAME: orisun
      ORISUN_ADMIN_USERNAME: admin
      ORISUN_ADMIN_PASSWORD: changeit
    ports:
      - "5005:5005"  # gRPC API
      - "8991:8991"  # Admin HTTP API
    volumes:
      - orisun-data:/var/lib/orisun/data
    depends_on:
      - postgres
    restart: unless-stopped

volumes:
  postgres-data:
  orisun-data:
```

2. **Start everything:**
```bash
docker-compose up -d
```

3. **Verify Orisun is running:**
```bash
# List services (default admin credentials: admin/changeit)
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" localhost:5005 list
```

### Option 2: Download Binary

1. **Download** from [Releases](https://github.com/oexza/Orisun/releases)
2. **Run with PostgreSQL** (replace placeholders):

```bash
# Start Orisun with PostgreSQL
ORISUN_PG_HOST=localhost \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS="orisun_test_1:public,orisun_admin:admin" \
./orisun-darwin-arm64

# gRPC API: localhost:5005 (default credentials: admin/changeit)
# gRPC API: localhost:5005
```

Platforms available: `darwin-arm64`, `linux-amd64`, `linux-arm64`

### Option 3: Docker Standalone

Run Orisun with Docker (requires external PostgreSQL):

```bash
# Run Orisun container
docker run -d \
  --name orisun \
  -p 5005:5005 \
  -p 8991:8991 \
  -e ORISUN_PG_HOST=host.docker.internal \
  -e ORISUN_PG_USER=postgres \
  -e ORISUN_PG_PASSWORD=your_password \
  -e ORISUN_PG_NAME=your_database \
  -e ORISUN_ADMIN_USERNAME=admin \
  -e ORISUN_ADMIN_PASSWORD=changeit \
  orexza/orisun:latest

# gRPC API: localhost:5005 (default credentials: admin/changeit)
```

## Getting Started Guide

This guide will walk you through the core operations of Orisun: storing events, querying them, and subscribing to updates.

### Your First Event

Let's save your first event to Orisun. We'll use a simple user registration scenario:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_1",
  "query": {
    "expected_position": {
      "transaction_id": -1,
      "global_id": -1
    }
  },
  "events": [
    {
      "event_id": "user-001",
      "event_type": "UserRegistered",
      "data": "{\"email\": \"alice@example.com\", \"username\": \"alice\", \"full_name\": \"Alice Smith\"}",
      "metadata": "{\"source\": \"web_signup\", \"ip\": \"192.168.1.100\"}"
    }
  ]
}
EOF
```

**What's happening:**
- **boundary**: "orisun_test_1" - The bounded context for this event
- **expected_position**: {-1, -1} - We're starting fresh, no previous events expected
- **event_id**: Unique identifier for this event (use UUIDs in production)
- **event_type**: Type of event (e.g., "UserRegistered", "OrderCreated")
- **data**: Event payload as JSON (the actual domain data)
- **metadata**: Additional information about the event (source, timestamps, etc.)

**Response:**
```json
{
  "new_global_id": 0,
  "latest_transaction_id": 1234567890123456,
  "latest_global_id": 0
}
```

### Saving Multiple Events

You can save multiple events in a single transaction (they all share the same position):

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_1",
  "query": {
    "expected_position": {
      "transaction_id": 0,
      "global_id": 0
    }
  },
  "events": [
    {
      "event_id": "profile-001",
      "event_type": "UserProfileCompleted",
      "data": "{\"user_id\": \"user-001\", \"phone\": \"+1234567890\", \"address\": \"123 Main St\"}",
      "metadata": "{\"completed_at\": \"2024-01-20T10:30:00Z\"}"
    },
    {
      "event_id": "email-001",
      "event_type": "EmailVerified",
      "data": "{\"user_id\": \"user-001\", \"email\": \"alice@example.com\"}",
      "metadata": "{\"verified_at\": \"2024-01-20T10:31:00Z\"}"
    }
  ]
}
EOF
```

**Note**: The `expected_position` now points to the previous event (transaction_id: 0, global_id: 0). This ensures optimistic concurrency - if someone else saved events, we'll detect the conflict.

### Querying Events

Query all events from the beginning:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_1",
  "count": 100,
  "direction": "ASC"
}
EOF
```

Query events after a specific position (pagination):

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_1",
  "after_position": {
    "transaction_id": 0,
    "global_id": 1
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

Query events by content using JSONB containment:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_1",
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "username", "value": "alice"}
        ]
      }
    ]
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

This query finds all events where the data contains `"username": "alice"`.

### Subscribing to Events

Subscribe to all events in real-time:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "my-subscriber",
  "boundary": "orisun_test_1",
  "after_position": {
    "transaction_id": -1,
    "global_id": -1
  }
}
EOF
```

This will:
1. First, replay all historical events from the beginning
2. Then, stream new events as they arrive in real-time

Subscribe to filtered events:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "user-events-subscriber",
  "boundary": "orisun_test_1",
  "after_position": {
    "transaction_id": -1,
    "global_id": -1
  },
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "event_type", "value": "UserRegistered"}
        ]
      },
      {
        "tags": [
          {"key": "event_type", "value": "UserProfileCompleted"}
        ]
      }
    ]
  }
}
EOF
```

This subscription only receives `UserRegistered` and `UserProfileCompleted` events.

### Command Context Consistency in Practice

Let's see how to use CCC for a real banking scenario. Imagine a command to transfer money between accounts:

```bash
# Step 1: Query the current state (Check Phase)
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "banking",
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "account_holder", "value": "alice"}
        ]
      },
      {
        "tags": [
          {"key": "account_holder", "value": "bob"}
        ]
      }
    ]
  },
  "count": 1000,
  "direction": "DESC"
}
EOF
```

From the results, build your context model:
```json
[
  {"account_holder": "alice", "balance": 1000},
  {"account_holder": "bob", "balance": 500}
]
```

**Check business rules**: Both accounts exist, Alice has sufficient funds ✓

```bash
# Step 2: Record the transfer (Record Phase)
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "banking",
  "query": {
    "expected_position": {
      "transaction_id": 123,  // Latest position from Step 1
      "global_id": 456
    },
    "criteria": [  // This ensures no new events for alice/bob were added
      {
        "tags": [
          {"key": "account_holder", "value": "alice"}
        ]
      },
      {
        "tags": [
          {"key": "account_holder", "value": "bob"}
        ]
      }
    ]
  },
  "events": [
    {
      "event_id": "transfer-001",
      "event_type": "MoneyTransferred",
      "data": "{\"from\": \"alice\", \"to\": \"bob\", \"amount\": 100, \"reference\": \"rent\"}",
      "metadata": "{\"timestamp\": \"2024-01-20T11:00:00Z\"}"
    }
  ]
}
EOF
```

**What happens:**
1. Orisun re-runs the query to check for new alice/bob events
2. If the position matches (no new events), the transfer is saved
3. If there are new events (concurrent modification), you'll get an `ALREADY_EXISTS` error

**Error handling:**
```bash
# If you get an error, re-run Step 1 and Step 2 with the new position
# This ensures you always work with the latest state
```

### Real-World Example: E-Commerce Order

Here's a complete e-commerce order flow:

```bash
# 1. Create the order
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "ecommerce",
  "query": {
    "expected_position": {
      "transaction_id": -1,
      "global_id": -1
    },
    "criteria": [{"tags": [{"key": "order_id", "value": "ORDER-123"}]}]
  },
  "events": [{
    "event_id": "order-created-001",
    "event_type": "OrderCreated",
    "data": "{\"order_id\": \"ORDER-123\", \"customer_id\": \"CUST-456\", \"total\": 99.99, \"items\": [{\"product_id\": \"P-789\", \"quantity\": 2}]}",
    "metadata": "{\"timestamp\": \"2024-01-20T12:00:00Z\"}"
  }]
}
EOF

# 2. Payment processed
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "ecommerce",
  "query": {
    "expected_position": {"transaction_id": 100, "global_id": 0},
    "criteria": [{"tags": [{"key": "order_id", "value": "ORDER-123"}]}]
  },
  "events": [{
    "event_id": "payment-001",
    "event_type": "PaymentProcessed",
    "data": "{\"order_id\": \"ORDER-123\", \"amount\": 99.99, \"payment_method\": \"credit_card\"}",
    "metadata": "{\"timestamp\": \"2024-01-20T12:05:00Z\"}"
  }]
}
EOF

# 3. Order shipped
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "ecommerce",
  "query": {
    "expected_position": {"transaction_id": 101, "global_id": 1},
    "criteria": [{"tags": [{"key": "order_id", "value": "ORDER-123"}]}]
  },
  "events": [{
    "event_id": "shipped-001",
    "event_type": "OrderShipped",
    "data": "{\"order_id\": \"ORDER-123\", \"tracking_number\": \"TN-987654\", \"shipping_address\": \"123 Main St\"}",
    "metadata": "{\"timestamp\": \"2024-01-20T14:00:00Z\"}"
  }]
}
EOF
```

**Query the order history:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "ecommerce",
  "query": {
    "criteria": [{"tags": [{"key": "order_id", "value": "ORDER-123"}]}]
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

This returns all events for ORDER-123 in chronological order, perfect for rebuilding the order state.

## Architecture Overview

Orisun combines a pluggable storage layer with the real-time capabilities of NATS JetStream to deliver a complete event sourcing solution:

```
┌─────────────────────────────────────────────────────────────┐
│                    Orisun Event Store                       │
├──────────────────────┬──────────────────────────────────────┤
│   Storage Layer      │         NATS JetStream                │
│   (Pluggable)        │         (Streaming)                  │
├──────────────────────┼──────────────────────────────────────┤
│ • PostgreSQL          │ • Real-time Pub/Sub                  │
│ • ACID Transactions   │ • Catch-up Subscriptions            │
│ • Event Storage      │ • Durable Streams                   │
│ • Rich Querying      │ • Clustering Support                │
└──────────────────────┴──────────────────────────────────────┘
│          Admin & Observability (gRPC)                       │
│ • User Management  • OpenTelemetry Tracing                 │
└─────────────────────────────────────────────────────────────┘
```

### How It Works

**Command Context Consistency: A Simple Mental Model**

Command Context Consistency brings the simplicity of RDBMS queries + optimistic locking to event sourcing.

**Traditional Event Sourcing (Complex)**
- Organize events by streams/aggregates (User-123, Order-456)
- Manage aggregate boundaries and consistency per stream
- Complex when commands span multiple aggregates

**Command Context Consistency (Simple)**
- Query events by **type** and **payload content**
- Each command defines its own context via a query
- Two-phase consistency: Check rules → Check context stability

**Example: Banking System**

Command: `transferMoney("Peter", "Janine", 10)`

```
Context Query: All events where payload references "Peter" or "Janine"
  Event types: accountOpened, moneyDeposited, moneyWithdrawn, moneyTransferred
  Payload filter: holder IN ("Peter", "Janine")

Context Model (projection):
  { accountBalances: [{holder: "Peter", balance: 100}, {holder: "Janine", balance: 50}] }

Phase #1 - Check: Both accounts exist, Peter has sufficient funds ✓
Phase #2 - Record: Re-run query, ensure no new events for Peter/Janine, then record
```

**What Makes CCC Different:**
- **No Aggregate Lock-in**: Query by payload, not pre-defined streams
- **Command-Specific Contexts**: Each command sees only relevant events
- **Flexible**: Context defined by query criteria on data content
- **Simple**: Like `SELECT WHERE` + optimistic locking, but for events

**Orisun's Implementation:**

1. **Multi-Tenancy**: Boundaries provide isolated domains/bounded contexts — multiple boundaries can share a PostgreSQL schema, with isolation achieved via boundary-prefixed tables
2. **Event Storage**: Events stored with full queryability on data content
3. **CCC Support**: Query events by payload content to build command-specific contexts
4. **Real-time Streaming**: NATS JetStream delivers events immediately to subscribers
5. **Clustering**: Multiple nodes coordinate via distributed locks for high availability

### Data Flow

1. **Write Path**:
   - Client sends events via gRPC
   - Events are stored transactionally in PostgreSQL
   - Events are published to NATS JetStream
   - Subscribers receive events in real-time

2. **Read Path**:
   - Clients can query events by boundary, data content, or global position
   - Real-time subscriptions receive new events as they occur
   - Catch-up subscriptions can replay historical events

3. **Clustering**:
   - Multiple nodes coordinate via distributed locks
   - Automatic failover ensures continuous operation
   - Each node can handle read/write operations independently

## Clients

Orisun provides official client libraries for Go, Node.js, and Java. These clients are maintained as separate repositories with their own release cycles.

### Official Client Libraries

All clients are independent repositories that use the shared proto definitions:
- **Go Client**: [github.com/oexza/orisun-client-go](https://github.com/oexza/orisun-client-go)
- **Java Client**: [github.com/oexza/orisun-client-java](https://github.com/oexza/orisun-client-java)
- **Node.js Client**: [github.com/oexza/orisun-node-client](https://github.com/oexza/orisun-node-client)

### Protocol Buffer Definitions

The shared Protocol Buffer definitions are maintained in a separate repository:
- **Proto Definitions**: [github.com/oexza/orisun-proto](https://github.com/oexza/orisun-proto)

Both the main Orisun server and all clients reference this proto repository to ensure consistent API definitions across the ecosystem.

### Quick Links

**Go Client:**
```bash
go get github.com/oexza/orisun-client-go
```
See [orisun-client-go](https://github.com/oexza/orisun-client-go) for documentation.

**Java Client:**
```groovy
implementation 'com.orisunlabs:orisun-java-client:0.0.1'
```
See [orisun-client-java](https://github.com/oexza/orisun-client-java) for documentation.

**Node.js Client:**
```bash
npm install @orisun/eventstore-client
```
See [orisun-node-client](https://github.com/oexza/orisun-node-client) for documentation.

### Generate Custom Clients

For other programming languages, you can generate clients using the Protocol Buffer definitions from the [orisun-proto](https://github.com/oexza/orisun-proto) repository.

**Python Client:**
```bash
# Clone the proto repository
git clone https://github.com/oexza/orisun-proto.git
cd orisun-proto

# Install grpcio-tools
pip install grpcio-tools

# Generate Python client
python -m grpc_tools.protoc -I. \
       --python_out=. \
       --grpc_python_out=. \
       eventstore.proto
```

**C# Client:**
```bash
# Clone the proto repository
git clone https://github.com/oexza/orisun-proto.git
cd orisun-proto

# Install Grpc.Tools package
dotnet add package Grpc.Tools

# Generate C# client (add to .csproj)
<Protobuf Include="eventstore.proto" GrpcServices="Client" />
```

## Boundaries and Schemas

In Orisun, a boundary is a logical domain — **it does not map one-to-one with a PostgreSQL schema**. Multiple boundaries can share the same schema. Isolation between boundaries is maintained at the table level: each boundary gets its own set of prefixed tables (e.g., `orders_orisun_es_event`, `payments_orisun_es_event`) within whichever schema they are assigned to.

`ORISUN_PG_SCHEMAS` maps each boundary name to a schema:

```
ORISUN_PG_SCHEMAS=orders:public,payments:public,admin:admin
```

Here, `orders` and `payments` both live in the `public` schema but are fully isolated by their table prefixes.

Boundaries must be pre-configured at startup:

```bash
ORISUN_PG_SCHEMAS=orders:public,payments:public,admin:admin \
ORISUN_BOUNDARIES='[{"name":"orders","description":"Order domain"},{"name":"payments","description":"Payment domain"},{"name":"admin","description":"Admin boundary"}]' \
ORISUN_ADMIN_BOUNDARY=admin \
ORISUN_PG_HOST=localhost \
[... other config ...] \
orisun-darwin-arm64
```

When Orisun starts:
1. It validates and creates the configured schemas if they don't exist
2. For each boundary it calls `initialize_boundary_tables(boundary, schema)`, which creates the boundary's prefixed tables inside the target schema
3. Only requests to pre-configured boundaries are accepted; all others are rejected

For example, with `ORISUN_PG_SCHEMAS=orders:public,payments:public,admin:admin`:
  - ✅ `boundary: "orders"` - Request will succeed
  - ✅ `boundary: "payments"` - Request will succeed
  - ✅ `boundary: "admin"` - Request will succeed
  - ❌ `boundary: "shipping"` - Request will fail (boundary not configured)


## gRPC API Examples

All gRPC API calls require authentication using a basic auth header. You can provide this with grpcurl using the `-H` flag:

```bash
# Default credentials: admin:changeit
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents
```

### SaveEvents
Save events to a specific boundary. Here's an example of saving user registration events:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_1",
  "query": {
    "expected_position": {
      "commit_position": -1,
      "prepare_position": -1
    }
  },
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
  ]
}
EOF
```

### GetEvents
Query events with various criteria. Here's an example of retrieving order events:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_2",
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
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "payment-processor",
  "boundary": "orisun_test_2",
  "after_position": {
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

### Admin gRPC Service

Orisun provides a comprehensive Admin gRPC service for user management and system administration. All Admin operations require authentication using the same basic auth header.

**Quick Examples:**

```bash
# Create a new user
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"name":"Jane Doe","username":"janedoe","password":"securePass123","roles":["user"]}' \
  localhost:5005 orisun.Admin/CreateUser

# List all users
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/ListUsers

# Validate credentials
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"username":"janedoe","password":"securePass123"}' \
  localhost:5005 orisun.Admin/ValidateCredentials

# Change password
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"user_id":"user-id-here","current_password":"oldPass","new_password":"newPass"}' \
  localhost:5005 orisun.Admin/ChangePassword

# Delete user
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"user_id":"user-id-here"}' \
  localhost:5005 orisun.Admin/DeleteUser

# Get system statistics
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 orisun.Admin/GetUserCount

grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orisun_test_1"}' \
  localhost:5005 orisun.Admin/GetEventCount
```

**Available Admin Operations:**
- `CreateUser` - Create new users with roles
- `ListUsers` - List all users in the system
- `ValidateCredentials` - Validate username and password
- `ChangePassword` - Change user passwords
- `DeleteUser` - Delete users by ID
- `GetUserCount` - Get total user count
- `GetEventCount` - Get event count for a boundary

For complete documentation of all Admin gRPC endpoints, see [ADMIN_API.md](ADMIN_API.md).

## Common Use Cases

### Multiple Bounded Contexts
```bash
# User domain events in orisun_test_1 schema
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_1",
  "query": {
    "expected_position": {
      "commit_position": -1,
      "prepare_position": -1
    }
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
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_2",
  "query": {
    "expected_position": {
      "commit_position": -1,
      "prepare_position": -1
    }
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

Each boundary maintains its own set of tables within its assigned PostgreSQL schema, prefixed with the boundary name:

- `{boundary}_orisun_es_event` — event storage
- `{boundary}_orisun_es_event_global_id_seq` — global ID sequence
- `{boundary}_orisun_last_published_event_position` — publisher tracking
- `{boundary}_projector_checkpoint` — projector state

Multiple boundaries can share a schema; they are fully isolated by these prefixed tables. This means:
- Domain isolation without requiring a schema per boundary
- Independent event sequences and consistency guarantees per boundary
- Flexible deployment: group related boundaries in one schema or spread them across many

### Index Management

Orisun allows you to create custom btree indexes on JSONB data fields for improved query performance. Orisun lets you create surgical, efficient indexes on only the fields you actually query. (Note this is an abstraction over PostgreSQL's indexing capabilities, not a custom index implementation.)

**Creating an Index:**

```bash
# Simple index on user_id
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orders","name":"user_id","fields":[{"json_key":"user_id","value_type":"TEXT"}]}' \
  localhost:5005 orisun.Admin/CreateIndex

# Composite index on multiple fields
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{
    "boundary": "orders",
    "name": "cat_prio",
    "fields": [
      {"json_key": "category", "value_type": "TEXT"},
      {"json_key": "priority", "value_type": "TEXT"}
    ]
  }' \
  localhost:5005 orisun.Admin/CreateIndex

# Partial index (only index specific event types)
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{
    "boundary": "orders",
    "name": "placed_amount",
    "fields": [{"json_key": "amount", "value_type": "NUMERIC"}],
    "conditions": [{"key": "eventType", "operator": "=", "value": "OrderPlaced"}],
    "condition_combinator": "AND"
  }' \
  localhost:5005 orisun.Admin/CreateIndex
```

**Dropping an Index:**

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary": "orders", "name": "user_id"}' \
  localhost:5005 orisun.Admin/DropIndex
```

**Why Custom Indexes?**

- **Reduced Write Overhead**: Only index the fields you query, not every JSONB key
- **Smaller Index Size**: Partial indexes exclude irrelevant events
- **Better Query Performance**: Btree indexes are faster for equality lookups than GIN
- **Flexible Design**: Create composite indexes for multi-field queries

For more details, see the [Admin API Documentation](ADMIN_API.md).

## Configuration

Orisun can be configured using environment variables:

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `ORISUN_PG_HOST` | PostgreSQL host | localhost | Yes |
| `ORISUN_PG_PORT` | PostgreSQL port | 5434 | Yes |
| `ORISUN_PG_USER` | PostgreSQL username | postgres | Yes |
| `ORISUN_PG_PASSWORD` | PostgreSQL password | postgres | Yes |
| `ORISUN_PG_NAME` | PostgreSQL database name | orisun | Yes |
| `ORISUN_PG_SSLMODE` | PostgreSQL SSL mode | disable | No |
| `ORISUN_PG_SCHEMAS` | Comma-separated list of boundary:schema mappings | `orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin` | Yes |
| `ORISUN_BOUNDARIES` | JSON array of boundary definitions with names and descriptions | `[{"name":"orisun_test_1","description":"boundary1"},{"name":"orisun_test_2","description":"boundary2"},{"name":"orisun_admin","description":"boundary3"}]` | Yes |
| `ORISUN_ADMIN_BOUNDARY` | Name of the boundary used for admin operations | orisun_admin | Yes |
| `ORISUN_ADMIN_USERNAME` | Default admin username | admin | No |
| `ORISUN_ADMIN_PASSWORD` | Default admin password | changeit | No |
| `ORISUN_ADMIN_PORT` | Admin HTTP service port | 8991 | No |
| `ORISUN_GRPC_PORT` | gRPC server port | 5005 | No |
| `ORISUN_GRPC_ENABLE_REFLECTION` | Enable gRPC reflection | true | No |
| `ORISUN_GRPC_CONNECTION_TIMEOUT` | gRPC connection timeout | 60s | No |
| `ORISUN_GRPC_KEEP_ALIVE_TIME` | gRPC keep-alive interval | 30s | No |
| `ORISUN_GRPC_KEEP_ALIVE_TIMEOUT` | gRPC keep-alive timeout | 5s | No |
| `ORISUN_GRPC_MAX_CONCURRENT_STREAMS` | Max concurrent gRPC streams | 10000 | No |
| `ORISUN_GRPC_MAX_RECEIVE_MESSAGE_SIZE` | Max receive message size (bytes) | 67108864 (64MB) | No |
| `ORISUN_GRPC_MAX_SEND_MESSAGE_SIZE` | Max send message size (bytes) | 67108864 (64MB) | No |
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
| `ORISUN_OTEL_ENABLED` | Enable OpenTelemetry tracing | true | No |
| `ORISUN_OTEL_ENDPOINT` | OTLP gRPC endpoint for tracing | localhost:4317 | No |
| `ORISUN_OTEL_SERVICE_NAME` | Service name for traces | orisun | No |
| `ORISUN_PPROF_ENABLED` | Enable pprof profiling endpoint | false | No |
| `ORISUN_PPROF_PORT` | pprof HTTP port | 6060 | No |
| `ORISUN_PG_WRITE_MAX_OPEN_CONNS` | Write pool max open connections | 10 | No |
| `ORISUN_PG_WRITE_MAX_IDLE_CONNS` | Write pool max idle connections | 3 | No |
| `ORISUN_PG_READ_MAX_OPEN_CONNS` | Read pool max open connections | 10 | No |
| `ORISUN_PG_READ_MAX_IDLE_CONNS` | Read pool max idle connections | 5 | No |
| `ORISUN_PG_ADMIN_MAX_OPEN_CONNS` | Admin pool max open connections | 5 | No |
| `ORISUN_PG_ADMIN_MAX_IDLE_CONNS` | Admin pool max idle connections | 1 | No |
| `ORISUN_GRPC_TLS_ENABLED` | Enable TLS for gRPC server | false | No |
| `ORISUN_GRPC_TLS_CERT_FILE` | Path to TLS certificate file | /etc/orisun/tls/server.crt | No |
| `ORISUN_GRPC_TLS_KEY_FILE` | Path to TLS private key file | /etc/orisun/tls/server.key | No |
| `ORISUN_GRPC_TLS_CA_FILE` | Path to CA certificate (for client auth) | /etc/orisun/tls/ca.crt | No |
| `ORISUN_GRPC_TLS_CLIENT_AUTH_REQUIRED` | Require client certificates | false | No |

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

**Best Practices:**
- Use client-side load balancing (all official clients support multi-server and DNS-based load balancing)
- Monitor PostgreSQL connection pool usage
- Set up proper network security between cluster nodes
- Configure appropriate timeouts for your network latency
- Monitor NATS cluster status and JetStream health
- Note: NATS JetStream uses in-memory storage (data is not persisted)


**Kubernetes Deployment:**
For production Kubernetes deployments, consider:
- ConfigMaps for environment configuration
- Horizontal Pod Autoscaler for scaling based on load
- Network policies for security between pods
- Resource limits and requests for pods

## TLS Configuration

Orisun supports TLS for secure gRPC connections. By default, TLS is disabled, but it's recommended for production deployments.

### Enabling TLS

To enable TLS, set the following environment variables:

```bash
ORISUN_GRPC_TLS_ENABLED=true \
ORISUN_GRPC_TLS_CERT_FILE=/path/to/server.crt \
ORISUN_GRPC_TLS_KEY_FILE=/path/to/server.key \
./orisun-darwin-arm64
```

### Generating Self-Signed Certificates

For development and testing, you can generate self-signed certificates:

```bash
# Generate a private key
openssl genrsa -out server.key 2048

# Generate a certificate signing request
openssl req -new -key server.key -out server.csr \
  -subj "/CN=orisun.local/O=Orisun/C=US"

# Generate a self-signed certificate
openssl x509 -req -days 365 -in server.csr -signkey server.key -out server.crt
```

### TLS with Client Authentication

For enhanced security, you can require clients to present certificates:

```bash
# Generate CA certificate
openssl genrsa -out ca.key 2048
openssl req -new -x509 -days 365 -key ca.key -out ca.crt \
  -subj "/CN=OrisunCA/O=Orisun/C=US"

# Generate server certificate signed by CA
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr \
  -subj "/CN=orisun.local/O=Orisun/C=US"
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt

# Generate client certificate
openssl genrsa -out client.key 2048
openssl req -new -key client.key -out client.csr \
  -subj "/CN=client/O=OrisunClient/C=US"
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt

# Start Orisun with client authentication
ORISUN_GRPC_TLS_ENABLED=true \
ORISUN_GRPC_TLS_CERT_FILE=/path/to/server.crt \
ORISUN_GRPC_TLS_KEY_FILE=/path/to/server.key \
ORISUN_GRPC_TLS_CA_FILE=/path/to/ca.crt \
ORISUN_GRPC_TLS_CLIENT_AUTH_REQUIRED=true \
./orisun-darwin-arm64
```

### Connecting with TLS

When TLS is enabled, clients must connect using secure credentials:

**Using grpcurl with TLS (skip server verification for testing):**
```bash
grpcurl -insecure \
  -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 list
```

**Using grpcurl with TLS and CA verification:**
```bash
grpcurl \
  -cacert /path/to/ca.crt \
  -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 list
```

**Using grpcurl with TLS and client certificate:**
```bash
grpcurl \
  -cacert /path/to/ca.crt \
  -cert /path/to/client.crt \
  -key /path/to/client.key \
  -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  localhost:5005 list
```

**Docker Compose with TLS:**
```yaml
version: '3.8'
services:
  orisun:
    image: oexza/orisun:latest
    environment:
      ORISUN_GRPC_TLS_ENABLED: "true"
      ORISUN_GRPC_TLS_CERT_FILE: /etc/orisun/tls/server.crt
      ORISUN_GRPC_TLS_KEY_FILE: /etc/orisun/tls/server.key
    volumes:
      - ./tls:/etc/orisun/tls:ro
    ports:
      - "5005:5005"
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

4. **Cluster-Specific Issues**
   - **Lock Acquisition Failures**: Check logs for "Failed to acquire lock" messages - this is normal behavior when other nodes hold locks
   - **Projection Startup Issues**: Look for "Failed to start [projection] projection" followed by retry attempts
   - **NATS Cluster Issues**: Verify cluster routes configuration and ensure all nodes can reach each other
   - **Split Brain Prevention**: Ensure minimum 3 nodes for proper JetStream quorum
   - **Failover Delays**: Nodes may take 5-10 seconds to detect and take over from failed nodes

5. **Common Log Messages (Normal Behavior)**
   - `"Failed to acquire lock for boundary: <name>"` - Another node is processing this boundary
   - `"Successfully acquired lock for boundary: <name>"` - This node is now processing the boundary
   - `"User projector started"` - Projection service started successfully
   - `"Failed to start user projection (likely due to lock contention)"` - Will retry automatically

## Building from Source

### Prerequisites
- Go 1.26.0+
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
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
./orisun-darwin-arm64
```

## Architecture

Orisun uses:
- **PostgreSQL 13+**: Current production storage backend (pluggable architecture supports future backends like SQLite and FoundationDB)
- **NATS JetStream 2.12.4+**: For real-time event streaming and pub/sub
- **gRPC**: For client-server communication
- **Go 1.26.0+**: For high-performance server implementation
- **User Management**: Integrated user administration system
- **Modular Design**: Plugin system for extending functionality

## Performance

Orisun is designed for high-performance event processing with comprehensive benchmarking to ensure optimal throughput and latency.

### Benchmark Results

The following benchmarks were conducted on an Apple M1 Pro (darwin-arm64) with PostgreSQL running in a Docker container:

| Benchmark | Result | Description |
|-----------|--------|-------------|
| **SaveEvents_Batch (10 events)** | ~5,960 events/sec | Batched writes via gRPC |
| **DirectDatabase10K** | ~1,013 events/sec | Direct database writes (concurrent with granular locking) |
| **DirectDatabase10KBatch** | high throughput | Direct database batch writes (bypasses gRPC layer) |
| **ConsistencyCheck_NoIndex** | ~699 saves/sec | Version check with sequential scan (10K rows) |
| **ConsistencyCheck_WithIndex** | ~745 saves/sec | Version check with btree index (10K rows) |

### Benchmark Scenarios

- **SaveEvents_Burst**: High-throughput burst operations simulating traffic spikes
- **DirectDatabase**: Direct database write operations bypassing the gRPC layer
- **DirectDatabaseBatch**: Batch database writes showing maximum achievable throughput
- **ConsistencyCheck**: Measures the impact of btree indexes on CCC version check queries. The benchmark pre-populates 10,000 events across 500 streams, then performs save operations with stream-based consistency checks. Results show indexed lookups are ~6% faster than sequential scans.

### Running Benchmarks

To run the benchmark suite yourself:

```bash
# Run all benchmarks
go test -bench=. -benchtime=3s -count=1 ./cmd/benchmark_test.go

# Run specific benchmark
go test -bench=BenchmarkSaveEvents_Single -benchtime=5s ./cmd/benchmark_test.go

# Run index performance benchmarks
go test -run='^$' -bench=BenchmarkConsistencyCheck -benchtime=5s ./postgres/...

# Use the collection script
./collect_benchmarks.sh
```

*Note: Performance results may vary based on hardware, PostgreSQL configuration, and system load. These benchmarks were run on Apple M1 Pro with PostgreSQL in Docker.*

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