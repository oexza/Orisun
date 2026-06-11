---
title: Storage Backends
description: Choose between PostgreSQL and SQLite deployment profiles.
---

Orisun supports PostgreSQL and SQLite. The backend is selected with `ORISUN_BACKEND` or by using a backend-specific binary or Docker image.

## Backend Matrix

| Backend | Use case | Multi-node | Driver |
| --- | --- | --- | --- |
| `postgres` | Production clusters, larger datasets, shared database platforms | Yes | `pgx` |
| `sqlite` | Embedded apps, edge, development, low-ops single-node production | No | `zombiezen.com/go/sqlite` |

## PostgreSQL

PostgreSQL is the clustered backend. Multiple Orisun nodes can share one database. Publishers coordinate with PostgreSQL advisory locks so only one active publisher owns a boundary at a time.

PostgreSQL stores:

- event log tables
- per-boundary position state
- publisher checkpoints
- projector checkpoints
- index metadata
- admin state

Choose PostgreSQL when you need horizontal Orisun nodes, database-managed backup/restore, mature operational tooling, or PgBouncer integration.

### PostgreSQL position metadata

PostgreSQL mode stores two ordering-related values:

- `transaction_id`: Orisun's logical commit position, durable across PostgreSQL major upgrades and restore workflows.
- `pg_xact_id`: PostgreSQL's internal transaction ID, used only as a current-cluster visibility marker so publishers and catch-up reads do not skip older open transactions.

Do not use PostgreSQL internal transaction IDs as application cursors. From Orisun `0.3.1`, startup migrations remap older Orisun databases that exposed PostgreSQL transaction IDs as public commit positions. See [Positions and Ordering](./positions#postgresql-transaction-ids) and [Deployment](../operations/deployment#postgresql-major-upgrades).

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

## Migrating between backends

The public API is the same across both backends, but storage files and database schemas are backend-specific. Treat backend migration as a data migration: export from one backend, replay or import into the other, then move traffic after validation.
