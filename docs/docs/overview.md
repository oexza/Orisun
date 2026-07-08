---
id: overview
title: What is Orisun?
description: What Orisun is, the guarantees it makes, and where to start.
slug: /
---

Orisun is an open-source event store built for **Command Context Consistency**: commands query the exact events they depend on, and writes succeed only if that context is still current.

It stores the event log transactionally in PostgreSQL, YugabyteDB, or SQLite, and delivers committed events — catch-up replay plus live subscriptions — through embedded NATS JetStream. Storage, consistency checks, publishing, indexes, auth, and gRPC APIs ship as one deployable server.

## Guarantees

- **Consistency scoped to the command.** A write declares the event subset it depends on with JSON criteria and commits only if that subset is unchanged — no need to force every invariant into a single stream.
- **No skipped committed events.** A durable per-boundary checkpoint drives publishing. Wake-up signals can be missed; committed events still drain in order.
- **Per-boundary ordering.** Events publish in ascending log position within each boundary.
- **Same API on every backend.** SQLite, PostgreSQL, and YugabyteDB expose the identical gRPC surface, so deployments can grow without client changes.

## How it works

1. **Store** events transactionally in the selected backend.
2. **Check** command consistency by querying the event subset the command depends on.
3. **Publish** committed events sequentially per boundary from durable checkpoints.
4. **Subscribe** with catch-up replay from storage, then live JetStream delivery.

## Quick start

SQLite is the fastest local loop — event log, admin state, indexes, publisher checkpoints, and embedded JetStream run from one binary with no separate database.

1. Download `orisun-sqlite` from [GitHub Releases](https://github.com/oexza/Orisun/releases).
2. Start it with the [SQLite binary example](/docs/getting-started#run-sqlite-from-a-binary).
3. Verify gRPC with [Verify the API](/docs/getting-started#verify-the-api).
4. Save an event with [Save your first event](/docs/getting-started#save-your-first-event).

Move to PostgreSQL or YugabyteDB when you need multiple Orisun nodes or database-managed operations. SQLite is single-node only and requires NATS clustering disabled.

## Pick your path

| Goal | Read first | Then read |
| --- | --- | --- |
| Try Orisun locally | [Getting Started](/docs/getting-started) | [Tutorial](/docs/tutorial) |
| Embed Orisun in a Go service | [Go Embedding](/docs/embedding/go) | [Storage Backends](/docs/concepts/storage-backends) |
| Model a business invariant | [Command Context Consistency](/docs/concepts/command-context-consistency) | [Positions](/docs/concepts/positions) |
| Save, query, and subscribe | [EventStore API](/docs/api/eventstore) | [Clients](/docs/api/clients) |
| Prepare production settings | [Configuration](/docs/operations/configuration) | [Deployment](/docs/operations/deployment) |
| Debug a running node | [Troubleshooting](/docs/operations/troubleshooting) | [Observability](/docs/operations/observability) |

## When Orisun fits

Use Orisun when:

- commands need to read event history before deciding what to write,
- consistency depends on a subset of events, not always a fixed stream,
- projectors need to recover from downtime without relying only on broker retention,
- you want the event store, publisher, gRPC API, auth, indexes, and telemetry in one server.

Choose another tool when you only need transient messaging or a general-purpose queue with no durable event-log semantics. See [Comparing Orisun](/docs/comparison) for how Orisun differs from Kafka, EventStoreDB, PostgreSQL `LISTEN/NOTIFY`, and NATS JetStream.
