---
title: Troubleshooting
description: Diagnose startup, API, consistency, and publishing issues.
---

Start with the symptom table, then use the focused sections below.

| Symptom | Check |
| --- | --- |
| Cannot connect | PostgreSQL/YugabyteDB host and port, gRPC port, firewall, Docker networking. |
| `ALREADY_EXISTS` | Expected CCC conflict; re-query context and retry. |
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

Requests to unknown boundaries are rejected. Make sure the requested boundary exists in `ORISUN_BOUNDARIES`.

For PostgreSQL-compatible backends, including YugabyteDB, also make sure `ORISUN_PG_SCHEMAS` includes a matching `boundary:schema` entry.

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
