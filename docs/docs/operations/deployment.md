---
title: Deployment
description: Run Orisun as SQLite, PostgreSQL, or a PostgreSQL-backed cluster.
---

Orisun can run as a standalone release binary, a Docker image, or an embedded Go package. The same `ORISUN_` environment variables configure each mode; choose the packaging model that fits your platform.

## Binary deployments

Use the release binary when you want Orisun supervised like any other server process. This is a good fit for systemd, Nomad, Kubernetes containers built from your own base image, VM deployments, and PaaS platforms that run a command directly.

Release assets are backend-specific:

| Binary | Use when |
| --- | --- |
| `orisun-<os>-<arch>` | You want one binary with all backends compiled in. |
| `orisun-pg-<os>-<arch>` | The deployment only uses PostgreSQL. |
| `orisun-sqlite-<os>-<arch>` | The deployment only uses SQLite. |

For direct binary deployment:

- run the process under a supervisor that restarts it on failure
- persist `ORISUN_NATS_STORE_DIR`
- persist `ORISUN_SQLITE_DIR` when using SQLite
- inject secrets through the platform's secret manager
- expose `ORISUN_GRPC_PORT` only to trusted clients unless TLS and auth policy are configured

Example systemd unit:

```ini
[Unit]
Description=Orisun event store
After=network-online.target
Wants=network-online.target

[Service]
User=orisun
Group=orisun
EnvironmentFile=/etc/orisun/orisun.env
ExecStart=/usr/local/bin/orisun-sqlite
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

The environment file contains the same settings shown in [Getting Started](../getting-started).

## Container deployments

Use the published Docker images when your platform already standardizes on containers, or when you want the simplest way to run the official release artifact.

The image tags mirror the binary flavors:

| Image | Backend |
| --- | --- |
| `orexza/orisun:latest` | all backends |
| `orexza/orisun:pg` | PostgreSQL only |
| `orexza/orisun:sqlite` | SQLite only |

Persist the same directories you would persist for a binary deployment. Containers do not change the storage model.

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

## PostgreSQL Major Upgrades

Orisun `0.3.0` changes PostgreSQL event positions so public `commit_position` values are Orisun logical positions instead of PostgreSQL internal transaction IDs. This is a breaking storage migration for PostgreSQL-backed deployments.

Recommended upgrade sequence:

1. Stop all Orisun nodes cleanly.
2. Back up PostgreSQL and the NATS store directory.
3. Upgrade PostgreSQL using your platform's normal process. If you are still running an Orisun version before `0.3.0`, prefer PostgreSQL `pg_upgrade` over dump/restore because older Orisun versions exposed PostgreSQL internal transaction IDs as public positions.
4. Start one Orisun `0.3.0` node first.
5. Wait for database migrations to complete for every configured boundary.
6. Confirm publishers/projectors are healthy, then start the rest of the Orisun nodes.

During the first `0.3.0` startup, Orisun:

- adds internal `pg_xact_id` metadata to event tables,
- remaps legacy `transaction_id` values to logical Orisun commit positions,
- updates publisher checkpoints and projector checkpoints that point at stored events,
- clears stale current-cluster-only `pg_xact_id` values when detected.

After this migration, future PostgreSQL major upgrades no longer need to preserve PostgreSQL transaction IDs for Orisun correctness. PostgreSQL internal transaction IDs remain useful only for current-cluster stable-prefix reads.

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

## Limits and sizing

| Limit | Value | Notes |
| --- | --- | --- |
| gRPC request message size | 4 MB (gRPC default) | Caps a single `SaveEvents` request, so it bounds batch size × event payload. Split very large batches. |
| Event `data` / `metadata` | JSON string per field | No separate field cap; the whole request must fit the message-size limit above. |
| Publisher read batch | `ORISUN_POLLING_PUBLISHER_BATCH_SIZE` (default 1000) | Events drained per publisher read cycle. Raise for high write volume, lower to smooth memory. |
| `GetEvents` page | `count` per request, server-capped at 10000 | Page with `from_position`; see [Positions and Ordering](../concepts/positions#positions-and-paging). |
| Live retention (per boundary) | `ORISUN_NATS_EVENT_STREAM_MAX_BYTES` (512 MB), `_MAX_MSGS`, `_MAX_AGE` (5m) | In-memory live buffer only. Size for your slowest subscriber, not for durability. |

Sizing guidance:

- Keep batches comfortably under 4 MB. For bulk imports, chunk into many ordered `SaveEvents` calls.
- Set retention age above the slowest subscriber's expected lag and above the catch-up handover grace (~10s).
- Subscribers that routinely fall out of the live window are served from durable storage; this is correct but increases read load. Scale retention or subscriber throughput accordingly.

## Security Checklist

- Change `ORISUN_ADMIN_PASSWORD` before production use.
- Enable gRPC TLS in production-facing deployments.
- Protect PostgreSQL credentials and NATS cluster credentials.
- Use network policy or firewall rules for PostgreSQL, gRPC, and NATS cluster routes.
