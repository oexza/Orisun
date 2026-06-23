---
id: internals
title: Internals
description: How Orisun's write path, positions, publisher, and delivery actually work under the hood.
slug: /internals
---

This page is for advanced operators and contributors who want to understand *how* Orisun works, not just how to use it. It complements the concept pages, which describe the model; here we describe the mechanism. For the why behind each decision, read [Command Context Consistency](./concepts/command-context-consistency), [Positions](./concepts/positions), and [Delivery Guarantees](./concepts/delivery-guarantees) first.

## The write path

A `SaveEvents` call runs Command Context Consistency as a single database transaction. The work happens in the SQL function `insert_events_with_consistency_v3` (PostgreSQL) and its SQLite equivalent, inside one transaction:

1. **Re-check the context.** Within the same transaction that will insert, Orisun re-runs the command's `subsetQuery` against the committed log and compares the latest position it finds against the request's `expected_position`.
2. **Reject or insert.** If the context moved past `expected_position`, the transaction returns a consistency-conflict signal and Orisun surfaces `ALREADY_EXISTS` — no row is written. Otherwise the events are inserted and the transaction commits atomically.

The re-check and the insert share one transaction on purpose. Splitting them would reopen the race the model exists to close: a concurrent writer could land a conflicting event between the check and the insert. Keeping them in one transaction makes the check authoritative at commit time.

### Commit-ordered positions

To make an observed position a valid *upper bound* for a later check, positions must be assigned in commit order. Orisun serializes position assignment per boundary:

- **PostgreSQL** takes a transaction-scoped advisory lock (`pg_advisory_xact_lock` keyed per boundary) from position draw through commit, so concurrent writers for one boundary cannot interleave commits and produce out-of-order positions.
- **SQLite** has a single writer per boundary file (`BEGIN IMMEDIATE`), so serialization is natural.

The lock is per boundary, not global. Unrelated high-write domains in separate boundaries do not contend. See [Storage Backends](./concepts/storage-backends).

## Positions

Every committed event gets a two-part position. See [Positions and Ordering](./concepts/positions) for the usage contract; the internals are:

| Field | Backed by | Role |
| --- | --- | --- |
| `commit_position` | `transaction_id` — Orisun's logical commit position | Groups events committed together. Stable across PostgreSQL major upgrades and restore workflows. |
| `prepare_position` | `global_id` — per-boundary monotonic counter | The per-event sequence within a boundary. |

`global_id` comes from a per-boundary counter: PostgreSQL `transaction_id`/sequence state, SQLite the `orisun_es_seq` table.

### The visibility barrier for reads

Reads and the publisher must never return or publish an event that a later, lower-positioned event should precede — i.e. they must read a *stable committed prefix*. PostgreSQL ASC reads enforce this with a snapshot visibility predicate:

```sql
transaction_id::TEXT::xid8 < pg_snapshot_xmin(pg_current_snapshot())
```

This excludes events whose commit is not yet visible to the read snapshot. It is what lets the publisher drain ascending by position without ever skipping a committed event that was in flight. Do not remove this barrier — wake-up signals are not a substitute for it.

## The publisher and no-miss ordering

One active publisher per boundary drains the committed log and publishes to embedded NATS JetStream:

1. Read committed events after the persisted checkpoint, ordered ascending by `(transaction_id, global_id)`.
2. Publish each event to the boundary's JetStream subject.
3. Record the new checkpoint in the backend (`{boundary}_orisun_last_published_event_position`).
4. Repeat until the log is drained, then wait for the next wake-up.

### Wake-ups are hints, not the guarantee

PostgreSQL `LISTEN/NOTIFY` and SQLite wake-ups only tell the publisher there *may* be work. If a signal is lost or delayed, periodic polling still drains the log from the persisted checkpoint. This is why no committed event is skipped even when a wake-up goes missing — and why the visibility barrier above matters.

For YugabyteDB (`ORISUN_PG_DIALECT=yugabyte`), Orisun uses an application-managed committed-position watermark instead of PostgreSQL XID snapshot functions. Writers update the watermark in the same transaction as event inserts while holding the per-boundary position lock; ASC reads only return events at or below that watermark.

### At-least-once around publish + checkpoint

Publishing is at-least-once across the publish→checkpoint boundary: if JetStream accepts an event and the checkpoint write then fails, the event is republished on the next cycle. Events are never skipped and never published out of per-boundary order. Consumers deduplicate by `event_id`. See [Delivery Guarantees](./concepts/delivery-guarantees).

## Clustering and ownership

In a PostgreSQL cluster, a distributed advisory lock ensures exactly one node owns each boundary's publisher at a time. If the owner exits, another node acquires the lock and resumes from the persisted checkpoint. SQLite has no clustering — exactly one active node, enforced at startup.

Cluster coordination is scoped per boundary, so a boundary failing over does not affect other boundaries' publishers.

## Delivery: catch-up then live

`CatchUpSubscribeToEvents` has two phases:

1. **Catch-up** — read committed events after `after_position` from the durable store, ordered by position.
2. **Live** — switch to the JetStream stream for new events.

The JetStream stream uses in-memory retention (bounded by `ORISUN_NATS_EVENT_STREAM_MAX_BYTES`, `_MAX_MSGS`, and `_MAX_AGE`). It is a live-delivery buffer, not the source of truth — the durable log is. A subscriber that falls behind the retention window does not lose events; it is simply served from the durable store by the catch-up phase. The age window must exceed the catch-up→live handover grace (about 10 seconds) so a transitioning subscriber does not land in a gap.

## Backends

| | PostgreSQL | SQLite |
| --- | --- | --- |
| Layout | Schema-per-boundary, tables prefixed `{boundary}_orisun_es_event` | One `.db` file per boundary, unprefixed `orisun_es_event` |
| Writers | Concurrent, serialized by per-boundary advisory lock | Single writer per file (`BEGIN IMMEDIATE`) |
| Cluster | Yes — shared database, advisory-lock publisher ownership | No — single node, rejected at startup if clustering is enabled |
| Driver | `pgx` | `zombiezen.com/go/sqlite` (pure Go, no CGO) |

Both expose the same EventStore and Admin gRPC surface. See [Storage Backends](./concepts/storage-backends) and [Deployment](./operations/deployment).

## Runtime

- **`GOMAXPROCS`** is auto-set from the cgroup CPU quota through `automaxprocs` at init.
- **`GOMEMLIMIT`** and **`GOGC`** honor the standard Go environment variables; effective values are logged at startup.
- **Logging** is structured and leveled (`ORISUN_LOGGING_LEVEL`); use `DEBUG` to trace auth, subscription handover, and per-call detail. See [Observability](./operations/observability).

Hot-path SQL strings (`PostgresSaveEvents`, `PostgresGetEvents`, etc.) are precomputed per boundary at construction — there is no per-call string formatting on the request path, and boundary validation is a map presence check.
