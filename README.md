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
- [Boundaries and Schemas](#boundaries-and-schemas)
- [Index Management](#index-management)
- [Clients](#clients)
- [Admin API](#admin-api)
- [Configuration](#configuration)
- [Running Modes](#running-modes)
  - [Standalone Mode](#standalone-mode)
  - [Clustered Mode](#clustered-mode)
- [TLS Configuration](#tls-configuration)
- [Error Handling](#error-handling)
- [Performance](#performance)
- [Building from Source](#building-from-source)
- [Development](#development)
- [License](#license)

## Introduction

Orisun is a batteries-included event store designed for modern event-driven applications. It combines pluggable storage backends (currently PostgreSQL, with SQLite and FoundationDB planned) with NATS JetStream's real-time streaming capabilities to deliver a complete event sourcing solution that's both powerful and easy to use.

### What is Command Context Consistency?

Orisun implements **Command Context Consistency (CCC)**—a conceptually simple approach to ensuring data consistency in event-sourced systems without the complexity of streams, aggregates, or predefined tags.

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
- **Simple Mental Model**: Like RDBMS queries + optimistic locking, but for events

### Key Features

#### Core Event Sourcing
- **Reliable Event Storage**: PostgreSQL backend with full ACID compliance and transaction guarantees
- **Zero Message Loss**: Guaranteed event delivery with immediate error propagation on subscription failures
- **Optimistic Concurrency**: Boundary-based versioning with expected position checks
- **Content-Based Querying**: Filter events by JSONB data payload — no pre-defined streams required
- **Real-time Subscriptions**: Subscribe to event changes as they happen with catch-up subscriptions

#### Built-in Infrastructure
- **Embedded NATS JetStream**: Real-time event streaming without external dependencies
- **Multi-tenant Architecture**: Isolated boundaries with separate database schemas
- **Explicit Index Management**: Create targeted btree indexes on the JSONB fields you query — required for production performance
- **Admin gRPC Service**: User management, system administration, and index management via gRPC
- **OpenTelemetry Tracing**: Built-in distributed tracing for observability

#### Production Ready
- **Clustered Deployment**: High availability with automatic failover and distributed locking
- **Horizontal Scaling**: Add nodes dynamically for increased throughput and resilience
- **Zero-Downtime Failover**: Seamless takeover when nodes go down or become unavailable

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
ORISUN_PG_HOST=localhost \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS="orisun_test_1:public,orisun_admin:admin" \
./orisun-darwin-arm64
# gRPC API: localhost:5005 (default credentials: admin/changeit)
```

Platforms available: `darwin-arm64`, `linux-amd64`, `linux-arm64`

### Option 3: Docker Standalone

Run Orisun with Docker (requires external PostgreSQL):

```bash
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

This guide walks you through the core operations of Orisun: storing events, querying them, and subscribing to updates.

### Your First Event

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

**Fields:**
- **boundary**: The bounded context for this event
- **expected_position**: `{-1, -1}` means starting fresh with no previous events
- **event_id**: Unique identifier (use UUIDs in production)
- **event_type**: Event classification string
- **data**: Event payload as JSON (the actual domain data)
- **metadata**: Auxiliary information (source, timestamps, etc.)

**Response:**
```json
{
  "new_global_id": 0,
  "latest_transaction_id": 1234567890123456,
  "latest_global_id": 0
}
```

### Saving Multiple Events

You can save multiple events atomically. Set `expected_position` to the position returned from the previous write:

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
      "data": "{\"user_id\": \"user-001\", \"phone\": \"+1234567890\"}",
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

Query events by content (JSONB containment):

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

> **Performance note:** Criteria queries match events by JSONB key/value. Without an index on the queried field, this performs a full table scan. See [Index Management](#index-management).

### Subscribing to Events

Subscribe from the beginning and stream new events in real-time:

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

Subscribe to filtered events (e.g., specific event types):

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
        "tags": [{"key": "event_type", "value": "UserRegistered"}]
      },
      {
        "tags": [{"key": "event_type", "value": "UserProfileCompleted"}]
      }
    ]
  }
}
EOF
```

Multiple criteria entries are combined with OR — this subscription receives `UserRegistered` OR `UserProfileCompleted` events.

### Command Context Consistency in Practice

Here's a banking money transfer using CCC's two-phase approach:

```bash
# Phase 1 — Check: query the context (all events involving alice or bob)
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "banking",
  "query": {
    "criteria": [
      {"tags": [{"key": "account_holder", "value": "alice"}]},
      {"tags": [{"key": "account_holder", "value": "bob"}]}
    ]
  },
  "count": 1000,
  "direction": "DESC"
}
EOF
```

Build your context model from the results:
```json
[
  {"account_holder": "alice", "balance": 1000},
  {"account_holder": "bob", "balance": 500}
]
```

Check business rules: both accounts exist, Alice has sufficient funds ✓

```bash
# Phase 2 — Record: save with the query criteria to detect concurrent changes
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "banking",
  "query": {
    "expected_position": {
      "transaction_id": 123,
      "global_id": 456
    },
    "criteria": [
      {"tags": [{"key": "account_holder", "value": "alice"}]},
      {"tags": [{"key": "account_holder", "value": "bob"}]}
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
1. Orisun re-runs the criteria query to check for new alice/bob events
2. If the position matches (no concurrent changes), the transfer is saved
3. If the context changed, you get `ALREADY_EXISTS` — re-run Phase 1 and retry

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Orisun Event Store                       │
├──────────────────────┬──────────────────────────────────────┤
│   Storage Layer      │         NATS JetStream               │
│   (Pluggable)        │         (Streaming)                  │
├──────────────────────┼──────────────────────────────────────┤
│ • PostgreSQL         │ • Real-time Pub/Sub                  │
│ • ACID Transactions  │ • Catch-up Subscriptions             │
│ • Event Storage      │ • Durable Streams                    │
│ • Rich Querying      │ • Clustering Support                 │
└──────────────────────┴──────────────────────────────────────┘
│          Admin & Observability (gRPC)                       │
│ • User Management  • Index Management  • OpenTelemetry      │
└─────────────────────────────────────────────────────────────┘
```

**Stack:**
- **PostgreSQL 13+**: Current production storage backend (pluggable architecture supports future backends like SQLite and FoundationDB)
- **NATS JetStream**: Embedded real-time event streaming, no external broker required
- **gRPC**: Client-server communication with reflection enabled by default
- **Go 1.26.0+**: High-performance server implementation

### Data Flow

**Write Path:**
1. Client sends events via gRPC
2. Events are stored transactionally in PostgreSQL (CCC consistency check runs here)
3. Events are published to NATS JetStream
4. Subscribers receive events in real-time

**Read Path:**
1. Clients query events by boundary, data content, or global position
2. Real-time subscriptions receive new events as they arrive
3. Catch-up subscriptions replay historical events then switch to live

**Clustering:**
- Multiple nodes coordinate via PostgreSQL distributed locks
- Each boundary is processed by exactly one node at a time
- Automatic failover when nodes go down

## Boundaries and Schemas

In Orisun, a **boundary** is a logical domain — it does not map one-to-one with a PostgreSQL schema. Multiple boundaries can share the same schema, with isolation achieved via boundary-prefixed tables (e.g., `orders_orisun_es_event`, `payments_orisun_es_event`).

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

At startup, Orisun:
1. Validates and creates configured schemas if they don't exist
2. Calls `initialize_boundary_tables(boundary, schema)` for each boundary, creating its prefixed tables
3. Rejects all requests to unconfigured boundaries

Each boundary maintains these tables within its schema:
- `{boundary}_orisun_es_event` — event storage
- `{boundary}_orisun_es_event_global_id_seq` — global ID sequence
- `{boundary}_orisun_last_published_event_position` — publisher tracking
- `{boundary}_projector_checkpoint` — projector state

## Index Management

> **This is critical for production deployments.**
>
> Orisun's CCC criteria queries match events by JSONB key/value content. Without explicit indexes on the fields you query, every criteria-based read or write performs a **full table scan**. As your event log grows, unindexed queries will become the primary bottleneck — query latency will scale linearly with table size.
>
> **Rule of thumb: create a btree index for every distinct JSON key you use in `criteria.tags`.**

Orisun provides `CreateIndex` and `DropIndex` Admin RPCs that create PostgreSQL btree partial indexes on JSONB event data fields. This is an abstraction over PostgreSQL's indexing capabilities, not a custom implementation.

### Creating Indexes

**Simple index on a single field:**
```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orders","name":"user_id","fields":[{"json_key":"user_id","value_type":"TEXT"}]}' \
  localhost:5005 orisun.Admin/CreateIndex
```

**Composite index on multiple fields:**
```bash
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
```

**Partial index (only index a specific event type):**
```bash
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

Partial indexes are smaller and faster — use them when you only query a field in the context of a specific event type.

### Dropping an Index

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary": "orders", "name": "user_id"}' \
  localhost:5005 orisun.Admin/DropIndex
```

### Index Naming

Indexes are named `{boundary}_{name}_idx`. Use `DropIndex` with the same `boundary` + `name` you used at creation.

### When to Index

| You query by | Create index on |
|---|---|
| `{"key": "account_holder", "value": "..."}` | `account_holder` |
| `{"key": "event_type", "value": "..."}` | `event_type` |
| `{"key": "order_id", "value": "..."}` | `order_id` |
| Multiple fields together | Composite index |
| A field only within one event type | Partial index with condition |

For complete index API documentation, see [ADMIN_API.md](ADMIN_API.md).

## Clients

Orisun provides official client libraries maintained as separate repositories:

| Language | Package | Repository |
|----------|---------|------------|
| Go | `github.com/oexza/orisun-client-go` | [orisun-client-go](https://github.com/oexza/orisun-client-go) |
| Java | `com.orisunlabs:orisun-java-client:0.0.1` | [orisun-client-java](https://github.com/oexza/orisun-client-java) |
| Node.js | `@orisun/eventstore-client` | [orisun-node-client](https://github.com/oexza/orisun-node-client) |

All clients reference the shared proto definitions from [orisun-proto](https://github.com/oexza/orisun-proto).

**Installation:**
```bash
# Go
go get github.com/oexza/orisun-client-go

# Node.js
npm install @orisun/eventstore-client
```
```groovy
// Java (Gradle)
implementation 'com.orisunlabs:orisun-java-client:0.0.1'
```

### Generating Custom Clients

For other languages, generate clients from the [orisun-proto](https://github.com/oexza/orisun-proto) repository:

```bash
# Python
pip install grpcio-tools
python -m grpc_tools.protoc -I. --python_out=. --grpc_python_out=. eventstore.proto

# C# — add to .csproj:
# <Protobuf Include="eventstore.proto" GrpcServices="Client" />
```

## Admin API

All Admin calls require the same basic auth header. See [ADMIN_API.md](ADMIN_API.md) for the full reference.

**Quick examples:**

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

# Get event count for a boundary
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" \
  -d '{"boundary":"orisun_test_1"}' \
  localhost:5005 orisun.Admin/GetEventCount
```

**Available Admin operations:**
- `CreateUser`, `ListUsers`, `ValidateCredentials`, `ChangePassword`, `DeleteUser`, `GetUserCount`, `GetEventCount`
- `CreateIndex`, `DropIndex` — see [Index Management](#index-management)

## Configuration

Orisun is configured via environment variables with the `ORISUN_` prefix:

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `ORISUN_PG_HOST` | PostgreSQL host | localhost | Yes |
| `ORISUN_PG_PORT` | PostgreSQL port | 5434 | Yes |
| `ORISUN_PG_USER` | PostgreSQL username | postgres | Yes |
| `ORISUN_PG_PASSWORD` | PostgreSQL password | postgres | Yes |
| `ORISUN_PG_NAME` | PostgreSQL database name | orisun | Yes |
| `ORISUN_PG_SSLMODE` | PostgreSQL SSL mode | disable | No |
| `ORISUN_PG_SCHEMAS` | Comma-separated `boundary:schema` mappings | `orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin` | Yes |
| `ORISUN_BOUNDARIES` | JSON array of boundary definitions | `[{"name":"orisun_test_1","description":"boundary1"},...]` | Yes |
| `ORISUN_ADMIN_BOUNDARY` | Boundary used for admin operations | orisun_admin | Yes |
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
| `ORISUN_NATS_MAX_PAYLOAD` | Maximum NATS message payload size | 1048576 | No |
| `ORISUN_NATS_STORE_DIR` | NATS storage directory | ./data/orisun/nats | No |
| `ORISUN_NATS_CLUSTER_NAME` | NATS cluster name | orisun-nats-cluster | No |
| `ORISUN_NATS_CLUSTER_HOST` | NATS cluster host | 0.0.0.0 | No |
| `ORISUN_NATS_CLUSTER_PORT` | NATS cluster port | 6222 | No |
| `ORISUN_NATS_CLUSTER_USERNAME` | NATS cluster username | nats | No |
| `ORISUN_NATS_CLUSTER_PASSWORD` | NATS cluster password | password@1 | No |
| `ORISUN_NATS_CLUSTER_ENABLED` | Enable NATS clustering | false | No |
| `ORISUN_NATS_CLUSTER_TIMEOUT` | NATS cluster operation timeout | 1800s | No |
| `ORISUN_NATS_CLUSTER_ROUTES` | Comma-separated cluster routes | `nats://0.0.0.0:6223,nats://0.0.0.0:6224` | No |
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
| `ORISUN_GRPC_TLS_ENABLED` | Enable TLS for gRPC | false | No |
| `ORISUN_GRPC_TLS_CERT_FILE` | TLS certificate file path | /etc/orisun/tls/server.crt | No |
| `ORISUN_GRPC_TLS_KEY_FILE` | TLS private key file path | /etc/orisun/tls/server.key | No |
| `ORISUN_GRPC_TLS_CA_FILE` | CA certificate (for client auth) | /etc/orisun/tls/ca.crt | No |
| `ORISUN_GRPC_TLS_CLIENT_AUTH_REQUIRED` | Require client certificates | false | No |

## Running Modes

### Standalone Mode

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

### Clustered Mode

For high availability and horizontal scaling. Requires minimum 3 nodes for NATS JetStream quorum.

**Node 1:**
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

**Nodes 2 and 3** use the same configuration with these values changed:

| Variable | Node 2 | Node 3 |
|---|---|---|
| `ORISUN_GRPC_PORT` | 5006 | 5007 |
| `ORISUN_NATS_PORT` | 4223 | 4224 |
| `ORISUN_NATS_CLUSTER_HOST` | node2.example.com | node3.example.com |
| `ORISUN_NATS_CLUSTER_ROUTES` | node1,node3 | node1,node2 |
| `ORISUN_NATS_SERVER_NAME` | orisun-node-2 | orisun-node-3 |
| `ORISUN_NATS_STORE_DIR` | ./data/node2/nats | ./data/node3/nats |

**Cluster behavior:**
- Each boundary is processed by exactly one node at a time (distributed lock)
- When a node goes down, others automatically pick up its boundaries within 5–10 seconds
- All nodes can handle read/write operations independently
- Use client-side load balancing — all official clients support multi-server and DNS-based load balancing

**Expected log messages:**
- `"Successfully acquired lock for boundary: <name>"` — this node is processing the boundary
- `"Failed to acquire lock for boundary: <name>"` — another node holds it (normal)
- `"Failed to start user projection (likely due to lock contention)"` — will retry automatically

**Kubernetes:**
Use ConfigMaps for environment configuration, HPA for scaling, network policies for inter-pod security, and resource limits/requests per pod.

## TLS Configuration

TLS is disabled by default. Enable for production:

```bash
ORISUN_GRPC_TLS_ENABLED=true \
ORISUN_GRPC_TLS_CERT_FILE=/path/to/server.crt \
ORISUN_GRPC_TLS_KEY_FILE=/path/to/server.key \
./orisun-darwin-arm64
```

### Generating Self-Signed Certificates

```bash
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -subj "/CN=orisun.local/O=Orisun/C=US"
openssl x509 -req -days 365 -in server.csr -signkey server.key -out server.crt
```

### TLS with Client Authentication

```bash
# Generate CA
openssl genrsa -out ca.key 2048
openssl req -new -x509 -days 365 -key ca.key -out ca.crt -subj "/CN=OrisunCA/O=Orisun/C=US"

# Generate server certificate signed by CA
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -subj "/CN=orisun.local/O=Orisun/C=US"
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt

# Generate client certificate
openssl genrsa -out client.key 2048
openssl req -new -key client.key -out client.csr -subj "/CN=client/O=OrisunClient/C=US"
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt

# Start with mutual TLS
ORISUN_GRPC_TLS_ENABLED=true \
ORISUN_GRPC_TLS_CERT_FILE=/path/to/server.crt \
ORISUN_GRPC_TLS_KEY_FILE=/path/to/server.key \
ORISUN_GRPC_TLS_CA_FILE=/path/to/ca.crt \
ORISUN_GRPC_TLS_CLIENT_AUTH_REQUIRED=true \
./orisun-darwin-arm64
```

### Connecting with TLS

```bash
# Skip server verification (testing only)
grpcurl -insecure -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" localhost:5005 list

# With CA verification
grpcurl -cacert /path/to/ca.crt -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" localhost:5005 list

# With client certificate
grpcurl -cacert /path/to/ca.crt -cert /path/to/client.crt -key /path/to/client.key \
  -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" localhost:5005 list
```

**Docker Compose with TLS:**
```yaml
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
- `ALREADY_EXISTS`: CCC consistency violation — concurrent modification detected; re-fetch context and retry
- `INVALID_ARGUMENT`: Missing or invalid required fields
- `INTERNAL`: Database or system error — check server logs
- `NOT_FOUND`: Requested stream or consumer doesn't exist

### Troubleshooting

1. **Connection issues** — verify PostgreSQL settings, check NATS port availability, confirm firewall rules for cluster port 6222
2. **Slow queries / performance degradation** — the most common cause is missing indexes; see [Index Management](#index-management)
3. **Schema issues** — ensure schemas are configured, check PostgreSQL user permissions, validate `ORISUN_BOUNDARIES` JSON
4. **Cluster lock failures** — `"Failed to acquire lock"` is normal when another node holds the lock; watch for it persisting without any node acquiring
5. **Split brain** — ensure minimum 3 nodes for JetStream quorum

## Performance

Orisun is designed for high-performance event processing. The following benchmarks were run on Apple M1 Pro (darwin-arm64) with PostgreSQL in Docker:

| Benchmark | Result | Description |
|-----------|--------|-------------|
| **SaveEvents_Batch (10 events)** | ~5,960 events/sec | Batched writes via gRPC |
| **DirectDatabase10K** | ~1,013 events/sec | Direct database writes (concurrent with granular locking) |
| **DirectDatabase10KBatch** | high throughput | Direct database batch writes (bypasses gRPC layer) |
| **ConsistencyCheck_NoIndex** | ~699 saves/sec | CCC version check with sequential scan (10K rows) |
| **ConsistencyCheck_WithIndex** | ~745 saves/sec | CCC version check with btree index (10K rows) |

The `ConsistencyCheck` benchmarks pre-populate 10,000 events across 500 streams and measure save throughput. These numbers reflect a small dataset — at larger scale the gap between indexed and unindexed queries grows significantly. **Always define indexes on the fields you query in production.**

### Running Benchmarks

```bash
# All benchmarks
go test -bench=. -benchtime=3s -count=1 ./cmd/benchmark_test.go

# Specific benchmark
go test -bench=BenchmarkSaveEvents_Single -benchtime=5s ./cmd/benchmark_test.go

# Index performance benchmarks
go test -run='^$' -bench=BenchmarkConsistencyCheck -benchtime=5s ./postgres/...

# Use the collection script
./collect_benchmarks.sh
```

## Building from Source

### Prerequisites
- Go 1.26.0+
- Make

```bash
git clone https://github.com/oexza/orisun.git
cd orisun

# Build for current platform
./build.sh

# Cross-compile
./build.sh linux amd64     # Linux x86_64
./build.sh darwin arm64    # macOS Apple Silicon
./build.sh windows amd64   # Windows x86_64
```

## Development

### Setup
1. Fork and clone: `git clone https://github.com/YOUR_USERNAME/orisun.git`
2. Install dependencies: `go mod download`
3. Create a feature branch: `git checkout -b feature/amazing-feature`
4. Make your changes and run tests: `go test ./...`
5. Commit and push, then open a Pull Request

### Building the Docker Image

```bash
docker build -t orisun:local .

# With version info
docker build \
  --build-arg VERSION=1.0.0 \
  --build-arg TARGET_OS=linux \
  --build-arg TARGET_ARCH=amd64 \
  -t orisun:local .
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

[MIT](LICENSE)