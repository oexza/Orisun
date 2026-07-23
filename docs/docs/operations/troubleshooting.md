---
title: Troubleshooting
description: Diagnose startup, API, consistency, and publishing issues.
---

Start with the symptom table, then use the focused sections below.

| Symptom | Check |
| --- | --- |
| Cannot connect | PostgreSQL/YugabyteDB host and port, gRPC port, firewall, Docker networking. |
| `ALREADY_EXISTS` | Expected CCC conflict or DCB append-condition failure; re-query context and retry only if the command is still valid. |
| Boundary stays `PROVISIONING` or becomes `FAILED` | Inspect `Admin/GetBoundary.last_error`, placement, backend connectivity, and provisioning retry logs. |
| Slow criteria queries | Missing JSON field indexes for the selected backend. |
| Publisher lag | PostgreSQL/YugabyteDB listener health, SQLite signal/polling health, NATS health, boundary lock ownership. |
| Duplicate delivery | Expected after publish/checkpoint failure; deduplicate by `event_id`. |
| Cluster instability | NATS quorum, routes, unique ports, persistent store directories. |

## gRPC Reflection

If `grpcurl localhost:5005 list` fails, check:

- `ORISUN_GRPC_PORT`
- container port mappings
- `ORISUN_GRPC_ENABLE_REFLECTION`
- authentication headers

Use the default Basic auth header for examples:

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
grpcurl -H "$AUTH" localhost:5005 list
```

## Boundary Not Found

Requests to unknown or not-yet-installed boundaries are rejected. Query the
catalog first:

```bash
grpcurl -H "$AUTH" localhost:5005 orisun.Admin/ListBoundaries
grpcurl -H "$AUTH" \
  -d '{"name":"orders"}' \
  localhost:5005 orisun.Admin/GetBoundary
```

- If the definition is absent, call `CreateBoundary` for new storage or
  `ImportBoundary` for storage that already exists.
- If it is `PROVISIONING`, wait; the definition event committed but the local
  runtime is not ready yet.
- If it is `FAILED`, inspect `last_error`. Verify that backend and namespace
  match the running backend: PostgreSQL uses a schema, SQLite requires the
  boundary name, and FoundationDB requires `ORISUN_FDB_ROOT`.
- Do not call create/import again for a failed definition. Its immutable name
  already exists and the server is retrying it independently.

For a legacy PostgreSQL-compatible boundary, including YugabyteDB, make sure
`ORISUN_PG_SCHEMAS` includes its `boundary:schema` mapping on the first startup
that migrates it into the catalog. A mismatch between this mapping and an
existing catalog placement fails startup rather than remapping stored data.

For SQLite, verify both `{boundary}.db` and `{boundary}_metadata.db` are in
`ORISUN_SQLITE_DIR` before importing. For FoundationDB, keep
`ORISUN_FDB_ROOT` unchanged while discovering legacy key ranges.

## Publisher Lag

Publisher wake-up signals are only hints. If lag persists:

1. Check the selected storage backend is reachable.
2. Check NATS is healthy.
3. Check publisher checkpoint writes are succeeding.
4. In PostgreSQL-compatible clusters, check which node owns each boundary lock.
5. Check polling batch size and event volume.

## Duplicate Events

Orisun delivery is at least once. Consumers should be idempotent and deduplicate using stable event identifiers.

## Slow criteria queries

Criteria queries read JSON fields from event `data`. Create indexes for every high-volume key used in command contexts, `GetLatestByCriteria`, or projector filters. See [Indexing](../concepts/indexing).
