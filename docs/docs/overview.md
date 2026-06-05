---
id: overview
title: Overview
description: What Orisun is, when to use it, and where to start.
slug: /
---

Orisun is an event store for applications that need durable history, content-based consistency checks, and live delivery from the same system.

It stores the event log in PostgreSQL or SQLite, uses Command Context Consistency to protect writes against stale decisions, and publishes committed events through embedded NATS JetStream for catch-up and live subscriptions.

## When Orisun fits

Use Orisun when:

- commands need to read event history before deciding what to write,
- consistency depends on a subset of events, not always a fixed stream,
- projectors need to recover from downtime without relying only on broker retention,
- you want the event store, publisher, gRPC API, auth, indexes, and telemetry in one deployable server.

Choose another tool when you only need transient messaging or a general-purpose queue with no durable event-log semantics.

## How it works

1. **Store** events transactionally in PostgreSQL or SQLite.
2. **Check** command consistency by querying the event subset the command depends on.
3. **Publish** committed events sequentially per boundary from durable publisher checkpoints.
4. **Subscribe** with catch-up replay from storage, then switch to live JetStream delivery.

## Deployment choices

| Choice | Best for |
| --- | --- |
| Release binary | Direct server deployment under systemd, Nomad, VM process managers, or PaaS commands. |
| Docker image | Container platforms and quick full-stack local setup. |
| Embedded Go package | Services that want Orisun inside the same Go process. |

| Backend | Best for |
| --- | --- |
| SQLite | Single-node production, embedded services, local development, and edge deployments. |
| PostgreSQL | Multi-node Orisun deployments, larger datasets, and database-managed operations. |

## Start here

If you are new to the project, start with [Start Here](/docs/start-here). It routes you by task: local setup, Go embedding, consistency modeling, API integration, production configuration, or troubleshooting.

Common next steps:

- Run a server with [Getting Started](/docs/getting-started).
- Build the end-to-end ledger in the [Tutorial](/docs/tutorial).
- Understand write consistency in [Command Context Consistency](/docs/concepts/command-context-consistency).
- Review operational setup in [Deployment](/docs/operations/deployment).
- Embed Orisun in a Go service with [Go Embedding](/docs/embedding/go).
