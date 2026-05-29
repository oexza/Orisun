---
title: Deployment
description: Run Orisun as SQLite, PostgreSQL, or a PostgreSQL-backed cluster.
---

## Standalone SQLite

SQLite is the simplest production-capable single-node setup. It does not need a separate database container.

Use this profile when:

- one active Orisun node is enough
- deployment simplicity matters
- the event store should be embedded or edge-friendly

SQLite must run with `ORISUN_NATS_CLUSTER_ENABLED=false`.

Operational notes:

- Persist `/var/lib/orisun` or the configured `ORISUN_SQLITE_DIR`.
- Run exactly one active Orisun writer node.
- Back up both SQLite files and the NATS store directory if live delivery retention matters during restore.

## Standalone PostgreSQL

Use one Orisun node with PostgreSQL when you want the event log in PostgreSQL but do not need Orisun clustering.

This profile is useful when:

- PostgreSQL is already part of the platform
- the database needs independent backup and operational controls
- you may later add more Orisun nodes

Persist the NATS store directory for durable JetStream state. PostgreSQL remains the event source of truth.

## Clustered PostgreSQL

Clustered mode uses PostgreSQL, embedded NATS clustering, and one active publisher per boundary.

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

Minimum recommendation: three nodes for JetStream quorum.

Expected publisher behavior:

- The node that owns a boundary logs successful lock acquisition.
- Other nodes may log lock contention for that boundary.
- If the owner exits, another node resumes from the PostgreSQL checkpoint.

## PgBouncer

Session mode works out of the box.

For transaction mode:

- SQL functions use schema-qualified table references.
- The Go-side pool uses multi-statement transactions normally.
- PgBouncer 1.21+ should be configured with compatible prepared-statement handling.
- Older PgBouncer deployments should use simple protocol mode or compatible describe-cache settings.

## Runtime Tuning

| Variable | Recommendation |
| --- | --- |
| `GOMAXPROCS` | Auto-set from cgroup CPU quota through `automaxprocs`. |
| `GOMEMLIMIT` | Set to about 80 percent of container memory. |
| `GOGC` | Tune upward for lower GC frequency if memory allows. |

Effective values are logged at startup.

## Security Checklist

- Change `ORISUN_ADMIN_PASSWORD` before production use.
- Enable gRPC TLS in production-facing deployments.
- Protect PostgreSQL credentials and NATS cluster credentials.
- Use network policy or firewall rules for PostgreSQL, gRPC, and NATS cluster routes.
