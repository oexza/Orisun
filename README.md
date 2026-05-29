# Orisun

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/oexza/Orisun/actions/workflows/ci.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/ci.yml)
[![Release](https://github.com/oexza/Orisun/actions/workflows/release.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/release.yml)

Orisun is a batteries-included event store for systems that need durable event history, content-based consistency checks, and real-time delivery without running a separate broker.

It stores events transactionally in PostgreSQL, publishes them through embedded NATS JetStream, and implements **Command Context Consistency (CCC)**: each command defines its own consistency context by querying event data, then saves only if that context has not changed.

## Why Orisun

- **Content-based consistency**: query events by JSON payload fields instead of pre-defining streams or aggregate roots.
- **Durable source of truth**: PostgreSQL stores the event log, positions, checkpoints, indexes, and admin state.
- **Real-time delivery included**: embedded NATS JetStream handles live subscriptions and catch-up delivery.
- **No-miss publishing**: publisher checkpoints and stable-prefix reads prevent skipped events even when notifications are missed.
- **Sequential per-boundary publishing**: events publish in ascending `(transaction_id, global_id)` order.
- **Production controls**: explicit JSONB indexes, gRPC auth/TLS options, OpenTelemetry, pprof, clustering, and PgBouncer guidance.

## Contents

- [Quick Start](#quick-start)
- [Core Model](#core-model)
- [Using The API](#using-the-api)
- [Delivery Guarantees](#delivery-guarantees)
- [Architecture](#architecture)
- [Boundaries And Schemas](#boundaries-and-schemas)
- [Indexing](#indexing)
- [Configuration](#configuration)
- [Operations](#operations)
- [Clients](#clients)
- [Development](#development)

## Storage Backends

Orisun supports two storage backends, chosen at startup via `ORISUN_BACKEND`:

| Backend | Use Case | Multi-Node | Driver |
| --- | --- | --- | --- |
| `postgres` (default) | Production, clustered deployments, large datasets | Yes — cluster nodes coordinate via PG advisory locks | `pgx` |
| `sqlite` | Embedded / single-node, dev, edge, low-ops | **No** — single-node only; rejected at startup if `ORISUN_NATS_CLUSTER_ENABLED=true` | `zombiezen.com/go/sqlite` (pure Go) |

For SQLite, set `ORISUN_SQLITE_DIR` to the directory holding the per-boundary `{boundary}.db` files. The directory is created on startup if missing. Example:

```bash
ORISUN_BACKEND=sqlite \
ORISUN_SQLITE_DIR=/var/lib/orisun/sqlite \
ORISUN_BOUNDARIES='[{"name":"orders"},{"name":"orisun_admin"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
./orisun
```

The SQLite path uses WAL mode + `synchronous=NORMAL`, with one writer + N readers per boundary. Multi-row INSERT is used so a single `SaveEvents` call commits as one transaction; concurrent calls serialise at the boundary's write pool.

## Quick Start

### Docker Compose

Create `docker-compose.yml`:

```yaml
version: "3.8"

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
      - "5005:5005"
      - "8991:8991"
    volumes:
      - orisun-data:/var/lib/orisun/data
    depends_on:
      - postgres
    restart: unless-stopped

volumes:
  postgres-data:
  orisun-data:
```

Start Orisun:

```bash
docker-compose up -d
```

Verify the gRPC API:

```bash
grpcurl -H "Authorization: Basic YWRtaW46Y2hhbmdlaXQ=" localhost:5005 list
```

Default credentials are `admin:changeit`.

### Binary

Download a release binary and run it against PostgreSQL:

```bash
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=your_database \
ORISUN_PG_SCHEMAS="orisun_test_1:public,orisun_admin:admin" \
ORISUN_BOUNDARIES='[{"name":"orisun_test_1","description":"test"},{"name":"orisun_admin","description":"admin"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
./orisun-darwin-arm64
```

## Core Model

### Command Context Consistency

Traditional event stores often ask you to choose an aggregate stream up front. Orisun takes a different path: a command defines the events it cares about by querying the event payload.

Example: a money transfer command can define its context as all events where `account_holder` is Alice or Bob:

```json
{
  "criteria": [
    {"tags": [{"key": "account_holder", "value": "alice"}]},
    {"tags": [{"key": "account_holder", "value": "bob"}]}
  ]
}
```

The command flow is:

1. **Check**: query matching events and build the command's context model.
2. **Decide**: validate business rules in your application.
3. **Record**: save events with the same context query and expected position.
4. **Retry on conflict**: if the context changed, Orisun returns `ALREADY_EXISTS`.

This gives commands the consistency scope they actually need without forcing unrelated events into the same aggregate.

### Positions

Every event has a durable position:

| Field | Meaning |
|---|---|
| `transaction_id` | PostgreSQL transaction ID for the batch |
| `global_id` | Monotonic per-boundary event position |

Use `{-1, -1}` as the "before first event" position.

## Using The API

All examples use the default basic auth header:

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
```

### Save An Event

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
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
      "data": "{\"email\":\"alice@example.com\",\"username\":\"alice\"}",
      "metadata": "{\"source\":\"signup\"}"
    }
  ]
}
EOF
```

Response:

```json
{
  "new_global_id": 0,
  "latest_transaction_id": 123456789,
  "latest_global_id": 0
}
```

### Save A Batch

Batches are atomic. Events in one batch share the same transaction position and receive increasing global IDs.

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orisun_test_1",
  "query": {
    "expected_position": {
      "transaction_id": 123456789,
      "global_id": 0
    }
  },
  "events": [
    {
      "event_id": "profile-001",
      "event_type": "UserProfileCompleted",
      "data": "{\"user_id\":\"user-001\",\"phone\":\"+1234567890\"}",
      "metadata": "{}"
    },
    {
      "event_id": "email-001",
      "event_type": "EmailVerified",
      "data": "{\"user_id\":\"user-001\",\"email\":\"alice@example.com\"}",
      "metadata": "{}"
    }
  ]
}
EOF
```

### Query Events

Read from the beginning:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_1",
  "count": 100,
  "direction": "ASC"
}
EOF
```

Page from a position:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_1",
  "from_position": {
    "transaction_id": 123456789,
    "global_id": 0
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

Query by JSON payload fields:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orisun_test_1",
  "query": {
    "criteria": [
      {"tags": [{"key": "username", "value": "alice"}]}
    ]
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

Criteria entries are combined with OR. Tags within one criterion are combined with AND.

### Subscribe

Catch-up subscriptions replay stored events, then switch to live NATS delivery:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "user-events",
  "boundary": "orisun_test_1",
  "after_position": {
    "transaction_id": -1,
    "global_id": -1
  }
}
EOF
```

Filtered subscription:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "registrations",
  "boundary": "orisun_test_1",
  "after_position": {
    "transaction_id": -1,
    "global_id": -1
  },
  "query": {
    "criteria": [
      {"tags": [{"key": "eventType", "value": "UserRegistered"}]}
    ]
  }
}
EOF
```

## Delivery Guarantees

PostgreSQL is the durable source of truth. NATS JetStream is the real-time delivery layer.

`LISTEN/NOTIFY` is only a wake-up signal. If a notification is missed, correctness is still preserved by the PostgreSQL checkpoint and periodic catch-up polling.

Per boundary:

| Guarantee | How Orisun enforces it |
|---|---|
| No skipped events | The publisher stores the last published `(transaction_id, global_id)` in PostgreSQL and always resumes from the next position. |
| Sequential publishing | The publisher drains events in ascending `(transaction_id, global_id)` order and rejects non-advancing batches before publishing. |
| Stable committed prefix | ASC reads only expose transactions older than the current snapshot `xmin`, so a younger committed transaction cannot jump ahead of an older open transaction. |
| Single active publisher | Clustered nodes acquire a distributed lock per boundary. Failover resumes from the persisted checkpoint. |

Publishing is **at-least-once** at the boundary between NATS publish and checkpoint update. If NATS accepts an event and the checkpoint write fails, the event can be republished. Consumers should deduplicate by `event_id` or NATS message ID.

## Architecture

```text
Client
  |
  | gRPC
  v
Orisun
  |
  | transactional write + CCC check
  v
PostgreSQL event log
  |
  | pg_notify wake-up, checkpointed drain
  v
Embedded NATS JetStream
  |
  | live + catch-up subscriptions
  v
Subscribers
```

Main packages:

| Path | Responsibility |
|---|---|
| `cmd/` | Server entry point, integration tests, benchmarks |
| `orisun/` | Core API, publisher loop, streams, generated protobuf bindings |
| `postgres/` | PostgreSQL event store, migrations, admin DB, listener |
| `nats/` | Embedded NATS and JetStream setup |
| `admin/` | Auth, admin gRPC service, projections |
| `config/` | Environment-backed configuration |
| `clients/node/` | TypeScript client package |
| `proto/` | Protobuf source |

Supported production backend today: PostgreSQL. SQLite code exists as a stub and is not production-ready.

## Boundaries And Schemas

A **boundary** is a logical domain. Boundaries are isolated even when they share the same PostgreSQL schema because each boundary uses prefixed tables.

Example:

```bash
ORISUN_PG_SCHEMAS=orders:public,payments:public,admin:admin
```

Creates tables like:

```text
public.orders_orisun_es_event
public.payments_orisun_es_event
admin.admin_orisun_es_event
```

Each boundary has:

| Table | Purpose |
|---|---|
| `{boundary}_orisun_es_event` | Event log |
| `{boundary}_orisun_es_event_global_id_seq` | Per-boundary global ID sequence |
| `{boundary}_orisun_last_published_event_position` | NATS publisher checkpoint |
| `{boundary}_projector_checkpoint` | Projector checkpoint state |

Configured boundaries must match `ORISUN_BOUNDARIES`; requests to unknown boundaries are rejected.

## Indexing

Criteria queries match JSONB payload values with expressions like:

```sql
data->>'account_holder' = 'alice'
```

Without an index, criteria reads and CCC consistency checks scan the full boundary event table. In production, create btree indexes for every JSON field used in `criteria.tags`.

Create a simple index:

```bash
grpcurl -H "$AUTH" \
  -d '{"boundary":"orders","name":"user_id","fields":[{"json_key":"user_id","value_type":"TEXT"}]}' \
  localhost:5005 orisun.Admin/CreateIndex
```

Create a composite index:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.Admin/CreateIndex <<EOF
{
  "boundary": "orders",
  "name": "category_priority",
  "fields": [
    {"json_key": "category", "value_type": "TEXT"},
    {"json_key": "priority", "value_type": "TEXT"}
  ]
}
EOF
```

Create a partial index:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.Admin/CreateIndex <<EOF
{
  "boundary": "orders",
  "name": "placed_amount",
  "fields": [{"json_key": "amount", "value_type": "NUMERIC"}],
  "conditions": [{"key": "eventType", "operator": "=", "value": "OrderPlaced"}],
  "condition_combinator": "AND"
}
EOF
```

Drop an index:

```bash
grpcurl -H "$AUTH" \
  -d '{"boundary":"orders","name":"user_id"}' \
  localhost:5005 orisun.Admin/DropIndex
```

See [ADMIN_API.md](ADMIN_API.md) for the full admin API.

## Configuration

Orisun reads environment variables with the `ORISUN_` prefix.

### Required Production Settings

| Variable | Description |
|---|---|
| `ORISUN_PG_HOST` | PostgreSQL host |
| `ORISUN_PG_PORT` | PostgreSQL port |
| `ORISUN_PG_USER` | PostgreSQL user |
| `ORISUN_PG_PASSWORD` | PostgreSQL password |
| `ORISUN_PG_NAME` | PostgreSQL database |
| `ORISUN_PG_SCHEMAS` | Comma-separated `boundary:schema` mappings |
| `ORISUN_BOUNDARIES` | JSON array of boundary definitions |
| `ORISUN_ADMIN_BOUNDARY` | Boundary used for admin state |

### Common Settings

| Variable | Default | Description |
|---|---:|---|
| `ORISUN_GRPC_PORT` | `5005` | gRPC API port |
| `ORISUN_ADMIN_PORT` | `8991` | Admin HTTP port |
| `ORISUN_ADMIN_USERNAME` | `admin` | Default admin username |
| `ORISUN_ADMIN_PASSWORD` | `changeit` | Default admin password |
| `ORISUN_LOGGING_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `ORISUN_PG_LISTEN_ENABLED` | `true` | Use PostgreSQL `LISTEN/NOTIFY` for publisher wake-ups |
| `ORISUN_POLLING_PUBLISHER_BATCH_SIZE` | `1000` | Max events drained per publisher read batch |
| `ORISUN_NATS_PORT` | `4224` | Embedded NATS client port |
| `ORISUN_NATS_STORE_DIR` | `./data/orisun/nats` | NATS data directory |
| `ORISUN_NATS_EVENT_STREAM_MAX_BYTES` | `536870912` | Per-boundary event stream memory cap |
| `ORISUN_NATS_EVENT_STREAM_MAX_MSGS` | `-1` | Per-boundary event stream message cap |
| `ORISUN_NATS_EVENT_STREAM_MAX_AGE` | `5m` | Retention overlap for catch-up subscribers |
| `ORISUN_OTEL_ENABLED` | `true` | Enable OpenTelemetry |
| `ORISUN_OTEL_ENDPOINT` | `localhost:4317` | OTLP gRPC endpoint |
| `ORISUN_PPROF_ENABLED` | `false` | Enable pprof |
| `ORISUN_PPROF_PORT` | `6060` | pprof port |

### PostgreSQL Pool Settings

| Variable | Default |
|---|---:|
| `ORISUN_PG_WRITE_MAX_OPEN_CONNS` | `25` |
| `ORISUN_PG_WRITE_MAX_IDLE_CONNS` | `10` |
| `ORISUN_PG_WRITE_CONN_MAX_IDLE_TIME` | `5m` |
| `ORISUN_PG_WRITE_CONN_MAX_LIFETIME` | `30m` |
| `ORISUN_PG_READ_MAX_OPEN_CONNS` | `50` |
| `ORISUN_PG_READ_MAX_IDLE_CONNS` | `25` |
| `ORISUN_PG_READ_CONN_MAX_IDLE_TIME` | `5m` |
| `ORISUN_PG_READ_CONN_MAX_LIFETIME` | `30m` |
| `ORISUN_PG_ADMIN_MAX_OPEN_CONNS` | `5` |
| `ORISUN_PG_ADMIN_MAX_IDLE_CONNS` | `2` |
| `ORISUN_PG_ADMIN_CONN_MAX_IDLE_TIME` | `5m` |
| `ORISUN_PG_ADMIN_CONN_MAX_LIFETIME` | `30m` |

### TLS Settings

| Variable | Default |
|---|---|
| `ORISUN_GRPC_TLS_ENABLED` | `false` |
| `ORISUN_GRPC_TLS_CERT_FILE` | `/etc/orisun/tls/server.crt` |
| `ORISUN_GRPC_TLS_KEY_FILE` | `/etc/orisun/tls/server.key` |
| `ORISUN_GRPC_TLS_CA_FILE` | `/etc/orisun/tls/ca.crt` |
| `ORISUN_GRPC_TLS_CLIENT_AUTH_REQUIRED` | `false` |

### Cluster Settings

| Variable | Default |
|---|---|
| `ORISUN_NATS_CLUSTER_ENABLED` | `false` |
| `ORISUN_NATS_CLUSTER_NAME` | `orisun-nats-cluster` |
| `ORISUN_NATS_CLUSTER_HOST` | `0.0.0.0` |
| `ORISUN_NATS_CLUSTER_PORT` | `6222` |
| `ORISUN_NATS_CLUSTER_ROUTES` | `nats://0.0.0.0:6223,nats://0.0.0.0:6224` |
| `ORISUN_NATS_CLUSTER_USERNAME` | `nats` |
| `ORISUN_NATS_CLUSTER_PASSWORD` | `password@1` |
| `ORISUN_NATS_CLUSTER_TIMEOUT` | `1800s` |

## Operations

### Standalone Mode

Use one Orisun node with PostgreSQL:

```bash
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD=your_password \
ORISUN_PG_NAME=orisun \
ORISUN_PG_SCHEMAS=orders:public,admin:admin \
ORISUN_BOUNDARIES='[{"name":"orders","description":"orders"},{"name":"admin","description":"admin"}]' \
ORISUN_ADMIN_BOUNDARY=admin \
./orisun-linux-amd64
```

### Clustered Mode

Clustered mode uses embedded NATS clustering and one active publisher per boundary.

Minimum recommendation: three nodes for JetStream quorum.

Each node should share:

- PostgreSQL database and schemas
- `ORISUN_BOUNDARIES`
- `ORISUN_PG_SCHEMAS`
- `ORISUN_NATS_CLUSTER_NAME`
- NATS cluster credentials

Each node should have unique:

- `ORISUN_GRPC_PORT`
- `ORISUN_NATS_PORT`
- `ORISUN_NATS_CLUSTER_HOST`
- `ORISUN_NATS_SERVER_NAME`
- `ORISUN_NATS_STORE_DIR`

Expected publisher behavior:

- A node logs successful lock acquisition when it owns a boundary.
- Other nodes may log lock contention. That is normal.
- If the owner exits, another node resumes from the PostgreSQL checkpoint.

### PgBouncer

Session mode works out of the box.

For transaction mode:

- SQL functions use schema-qualified table references.
- The Go-side pool uses multi-statement transactions normally.
- The `pgx` driver default statement cache requires PgBouncer 1.21+ with `max_prepared_statements > 0`.
- Older PgBouncer deployments should use `cache_describe` or simple protocol mode.

### Runtime Tuning

Orisun honors standard Go runtime settings:

| Variable | Recommendation |
|---|---|
| `GOMAXPROCS` | Auto-set from cgroup CPU quota through `automaxprocs` |
| `GOMEMLIMIT` | Set to about 80 percent of container memory |
| `GOGC` | Tune upward for lower GC frequency if memory allows |

Effective values are logged at startup.

### Troubleshooting

| Symptom | Check |
|---|---|
| Cannot connect | PostgreSQL host/port, gRPC port, firewall, Docker networking |
| `ALREADY_EXISTS` | Expected CCC conflict; re-query context and retry |
| Slow criteria queries | Missing JSONB btree indexes |
| Publisher lag | PostgreSQL listener health, NATS health, boundary lock ownership |
| Duplicate delivery | Expected after publish/checkpoint failure; deduplicate by `event_id` |
| Cluster instability | NATS quorum, routes, unique ports, persistent store dirs |

## Clients

Official client libraries are maintained separately:

| Language | Package | Repository |
|---|---|---|
| Go | `github.com/oexza/orisun-client-go` | [orisun-client-go](https://github.com/oexza/orisun-client-go) |
| Java | `com.orisunlabs:orisun-java-client:0.0.1` | [orisun-client-java](https://github.com/oexza/orisun-client-java) |
| Node.js | `@orisun/eventstore-client` | [orisun-node-client](https://github.com/oexza/orisun-node-client) |

Install:

```bash
go get github.com/oexza/orisun-client-go
npm install @orisun/eventstore-client
```

Generate custom clients from the proto files in `proto/` or from the shared proto repository.

## Development

### Requirements

- Go 1.26.3+
- Docker for integration tests
- `task` for optional development workflows

### Common Commands

```bash
go test ./...
go build ./...
go fmt ./...
go vet ./...
go run cmd/main.go
```

Taskfile helpers:

```bash
task tools
task build
task live
task debug
```

Build release binaries:

```bash
./build.sh
./build.sh linux amd64
./build.sh darwin arm64
./build.sh windows amd64
```

Run benchmarks:

```bash
go test -bench=. -benchtime=3s ./cmd/benchmark_test.go
go test -run='^$' -bench=BenchmarkConsistencyCheck -benchtime=5s ./postgres/...
./collect_benchmarks.sh
```

### Docker

Build locally:

```bash
docker build -t orisun:local .
```

Run the local Docker smoke test:

```bash
./scripts/test_docker.sh
```

### Releasing

Create a normal release from `main`:

```bash
./scripts/release.sh 1.2.3
```

Attach curated release notes to the GitHub release:

```bash
./scripts/release.sh 1.2.3 --notes release-notes.md
```

The release script stores the notes verbatim in the annotated git tag, including markdown headings. The GitHub release workflow uses notes in this order:

1. Manual `release_notes` input from `workflow_dispatch`
2. Annotated tag message from `scripts/release.sh --notes`
3. Generated commit log since the previous tag

## Security Notes

- Change `ORISUN_ADMIN_PASSWORD` before production use.
- Enable gRPC TLS in production-facing deployments.
- Protect PostgreSQL credentials and NATS cluster credentials.
- Use network policy/firewall rules for NATS cluster routes and PostgreSQL.

## License

[MIT](LICENSE)
