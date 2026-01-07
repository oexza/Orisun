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
    image: postgres:17.5-alpine
    environment:
      POSTGRES_DB: orisun
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    ports:
      - "5432:5432"

  orisun:
    image: orexza/orisun:latest
    environment:
      ORISUN_PG_HOST: postgres
      ORISUN_PG_USER: postgres
      ORISUN_PG_PASSWORD: postgres
      ORISUN_PG_NAME: orisun
      ORISUN_PG_SCHEMAS: "orisun_test_1:public,orisun_admin:admin"
      ORISUN_BOUNDARIES: '[{"name":"orisun_test_1","description":"test boundary"},{"name":"orisun_admin","description":"admin boundary"}]'
      ORISUN_ADMIN_BOUNDARY: orisun_admin
    ports:
      - "5005:5005"  # gRPC API
    depends_on:
      - postgres
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
  -e ORISUN_PG_HOST=host.docker.internal \
  -e ORISUN_PG_USER=postgres \
  -e ORISUN_PG_PASSWORD=your_password \
  -e ORISUN_PG_NAME=your_database \
  -e ORISUN_PG_SCHEMAS="orisun_test_1:public,orisun_admin:admin" \
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
- **boundary**: "orisun_test_1" - The bounded context/schema for this event
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

1. **Multi-Tenancy**: Boundaries (PostgreSQL schemas) provide isolated domains/bounded contexts
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

Orisun provides official client libraries for Go, Node.js, and Java, with support for generating clients for other languages using Protocol Buffers.

### Official Client Libraries

**Go Client:**
For Go applications, see the [Go Client README](clients/go/README.md) for installation and usage instructions.

**Node.js Client:**
For Node.js applications, see the [Node.js Client README](clients/node/README.md) for installation and usage instructions.

**Java Client:**
For Java applications, see the [Java Client README](clients/java/README.md) for installation and usage instructions.

### Generate Custom Clients

For other programming languages, you can generate clients using the Protocol Buffers definition file available at `eventstore/eventstore.proto`.

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

## Boundaries and Schemas
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


## gRPC API Examples

All gRPC API calls require authentication using a basic auth header. You can provide this with grpcurl using the `-H` flag:

```bash
# Default credentials: admin:changeit
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents
```

### SaveEvents
Save events to a specific schema/boundary. Here's an example of saving user registration events:

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
| `ORISUN_OTEL_ENABLED` | Enable OpenTelemetry tracing | false | No |
| `ORISUN_OTEL_ENDPOINT` | OTLP gRPC endpoint for tracing | localhost:4317 | No |
| `ORISUN_OTEL_SERVICE_NAME` | Service name for traces | orisun | No |
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
    image: orexza/orisun:latest
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


5. **Cluster-Specific Issues**
   - **Lock Acquisition Failures**: Check logs for "Failed to acquire lock" messages - this is normal behavior when other nodes hold locks
   - **Projection Startup Issues**: Look for "Failed to start [projection] projection" followed by retry attempts
   - **NATS Cluster Issues**: Verify cluster routes configuration and ensure all nodes can reach each other
   - **Split Brain Prevention**: Ensure minimum 3 nodes for proper JetStream quorum
   - **Failover Delays**: Nodes may take 5-10 seconds to detect and take over from failed nodes

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
ORISUN_GRPC_PORT=5005 \
ORISUN_NATS_PORT=4222 \
./orisun-darwin-arm64
```

## Architecture

Orisun uses:
- **PostgreSQL 13+**: Current production storage backend (pluggable architecture supports future backends like SQLite and FoundationDB)
- **NATS JetStream 2.11.1+**: For real-time event streaming and pub/sub
- **gRPC**: For client-server communication
- **Go 1.24.2+**: For high-performance server implementation
- **User Management**: Integrated user administration system
- **Modular Design**: Plugin system for extending functionality

## Performance

Orisun is designed for high-performance event processing with comprehensive benchmarking to ensure optimal throughput and latency.

### Benchmark Results

The following benchmarks were conducted on an Apple M1 Pro (darwin-arm64) with PostgreSQL running in a Docker container:

| Benchmark | Result | Description |
|-----------|---------|-------------|
| **SaveEvents_Burst10000** | 6,221 events/sec | Burst operations (10K events) |
| **MemoryUsage** | 2,006 iterations @ 1.60ms/op | Memory allocation patterns |
| **DirectDatabase10K** | 2,212 events/sec | Direct database writes (concurrent with granular locking) |
| **DirectDatabase10KBatch** | 88K-104K events/sec | Direct database batch writes |

### Benchmark Scenarios

- **SaveEvents_Single**: Individual event saves through the gRPC API with full authentication and authorization checks
- **SaveEvents_Batch**: Batch saves through the gRPC API with optimistic concurrency control
- **GetEvents**: Event retrieval queries with pagination and filtering
- **SubscribeToEvents**: Real-time event subscriptions via NATS JetStream
- **SaveEvents_Burst**: High-throughput burst operations simulating traffic spikes
- **DirectDatabase**: Direct database write operations bypassing the gRPC layer
- **DirectDatabaseBatch**: Batch database writes showing maximum achievable throughput

### Performance Insights

- **Batch Size Scaling**: Throughput improves significantly with batch size:
  - **10 events**: 5,065 events/sec (1.97ms latency)
  - **100 events**: 12,037 events/sec (8.31ms latency)
  - **1,000 events**: 14,528 events/sec (68.83ms latency)
  - **2,500 events**: 16,595 events/sec (150.65ms latency)
- **Direct Database Performance**: Batch writes achieve 78K-112K events/sec, demonstrating the underlying PostgreSQL performance
- **gRPC Overhead**: The gRPC API layer adds authentication, validation, and coordination overhead, providing 718-16,595 events/sec depending on batch size
- **Concurrent Operations**: Successfully handles truly simultaneous writes (10K concurrent attempts in 4.62 seconds)
- **Memory Efficiency**: Stable allocation patterns with predictable memory usage

### Key Performance Features

- **High Throughput**: Scales from 718 events/sec for single operations to 112K events/sec for batch operations
- **Efficient Querying**: PostgreSQL JSONB indexes and GIN support for fast event lookups
- **Optimistic Concurrency**: Command Context Consistency with dual-phase checking prevents data corruption
- **Real-time Streaming**: Low-latency event distribution via NATS JetStream
- **Concurrent Processing**: Handles simultaneous operations with proper locking and coordination
- **Memory Efficient**: Optimized memory allocation patterns for long-running operations

### Running Benchmarks

To run the benchmark suite yourself:

```bash
# Run all benchmarks
go test -bench=. -benchtime=3s -count=1 ./cmd/benchmark_test.go

# Run specific benchmark
go test -bench=BenchmarkSaveEvents_Single -benchtime=5s ./cmd/benchmark_test.go

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