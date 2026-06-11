---
title: Storage Backends
description: Choose between PostgreSQL, SQLite, and FoundationDB deployment profiles.
---

Orisun supports PostgreSQL, SQLite, and FoundationDB. The backend is selected with `ORISUN_BACKEND` or by using a backend-specific binary or Docker image.

## Backend Matrix

| Backend | Use case | Multi-node | Driver |
| --- | --- | --- | --- |
| `postgres` | Production clusters, larger datasets, shared database platforms | Yes | `pgx` |
| `sqlite` | Embedded apps, edge, development, low-ops single-node production | No | `zombiezen.com/go/sqlite` |
| `foundationdb` | Distributed transactional key-value deployments | Yes | FoundationDB Go binding |

## PostgreSQL

PostgreSQL is the clustered backend. Multiple Orisun nodes can share one database. Publishers coordinate with PostgreSQL advisory locks so only one active publisher owns a boundary at a time. Writes also take a short per-boundary advisory lock while drawing public positions and committing, which preserves commit-ordered positions across concurrent writers.

PostgreSQL stores:

- event log tables
- per-boundary position state
- publisher checkpoints
- projector checkpoints
- index metadata
- admin state

Choose PostgreSQL when you need horizontal Orisun nodes, database-managed backup/restore, mature operational tooling, or PgBouncer integration.

The write lock is per boundary, not global. It is intentional: Command Context Consistency relies on public positions being a stable upper bound for committed events in that boundary. Split unrelated high-write domains into separate boundaries when they do not share invariants.

### PostgreSQL position metadata

PostgreSQL mode stores two ordering-related values:

- `transaction_id`: Orisun's logical commit position, durable across PostgreSQL major upgrades and restore workflows.
- `pg_xact_id`: PostgreSQL's internal transaction ID, used only as a current-cluster visibility marker so publishers and catch-up reads do not skip older open transactions.

Do not use PostgreSQL internal transaction IDs as application cursors. From Orisun `0.3.1`, startup migrations remap older Orisun databases that exposed PostgreSQL transaction IDs as public commit positions. See [Positions and Ordering](./positions#postgresql-transaction-ids) and [Deployment](../operations/deployment#postgresql-major-upgrades).

## FoundationDB

FoundationDB is a clustered backend built on ordered key-value transactions. Build binaries that include it with the `foundationdb` build tag and install the native FoundationDB client libraries on every host.

FoundationDB stores:

- event records in ordered per-boundary key ranges
- publisher checkpoints
- projector checkpoints
- admin state
- index metadata and secondary index keys
- watch signal keys for publisher wake-ups

```bash
go build -tags foundationdb ./cmd/orisun-fdb

ORISUN_BACKEND=foundationdb
ORISUN_FDB_CLUSTER_FILE=/etc/foundationdb/fdb.cluster
ORISUN_FDB_ROOT=orisun
```

Criteria queries keep the same public API, but FoundationDB requires ready covering boundary indexes for criteria reads and consistency checks. Unindexed criteria fail with `FAILED_PRECONDITION`; this avoids boundary-wide scans and keeps write conflict ranges scoped to the indexed event subset.

FoundationDB assigns event positions with commit versionstamps instead of a per-boundary counter. Plain appends can commit in parallel; writes with a consistency context conflict only on the covered index range for that context, so commands on different aggregates in one boundary commit concurrently. As in the other backends, an `expected_position` takes effect only together with consistency criteria.

## SQLite

SQLite is a complete single-node implementation, not a development-only fallback. It includes:

- event log tables
- admin state
- index metadata
- publisher checkpoints
- projector checkpoints
- JSON criteria queries
- the same CCC save semantics as PostgreSQL

SQLite creates one database file per boundary in `ORISUN_SQLITE_DIR`.

```bash
ORISUN_BACKEND=sqlite
ORISUN_SQLITE_DIR=/var/lib/orisun/sqlite
ORISUN_NATS_CLUSTER_ENABLED=false
```

SQLite is rejected at startup when NATS clustering is enabled. There must be exactly one active Orisun writer node.

Choose SQLite when a single active node is acceptable and simplicity matters. It is a production single-node backend, not a reduced local-development mode. For throughput, durability, and failover options — boundary sharding, Litestream, LiteFS — see [Scaling SQLite](../operations/deployment#scaling-sqlite).

## Boundary State

A boundary is a logical domain. Boundaries isolate event logs, indexes, publisher checkpoints, and projector checkpoints.

PostgreSQL maps boundaries to schemas with `ORISUN_PG_SCHEMAS`:

```bash
ORISUN_PG_SCHEMAS=orders:public,payments:public,orisun_admin:admin
```

SQLite maps each boundary to a file:

```text
/var/lib/orisun/sqlite/orders.db
/var/lib/orisun/sqlite/orisun_admin.db
```

FoundationDB maps each boundary to tuple-encoded key ranges under `ORISUN_FDB_ROOT`.

## Migrating between backends

The public API is the same across backends, but storage files, keyspaces, and database schemas are backend-specific. Treat backend migration as a data migration: export from one backend, replay or import into the other, then move traffic after validation.
