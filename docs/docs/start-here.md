---
id: start-here
title: Start Here
description: Pick the right Orisun path before diving into concepts, APIs, or operations.
slug: /start-here
---

Use this page as the shortest route through the docs. Orisun has a small API surface, but the right starting point depends on whether you are trying to run a server, embed the store, model consistency, or operate a deployment.

## Pick Your Path

| Goal | Read first | Then read |
| --- | --- | --- |
| Try Orisun locally | [Getting Started](./getting-started) | [Tutorial](./tutorial) |
| Embed Orisun in a Go service | [Go Embedding](./embedding/go) | [Storage Backends](./concepts/storage-backends) |
| Model a business invariant | [Command Context Consistency](./concepts/command-context-consistency) | [Positions](./concepts/positions) |
| Save, query, and subscribe | [EventStore API](./api/eventstore) | [Clients](./api/clients) |
| Prepare production settings | [Configuration](./operations/configuration) | [Deployment](./operations/deployment) |
| Debug a running node | [Troubleshooting](./operations/troubleshooting) | [Observability](./operations/observability) |

## Fastest Local Loop

For a local server, use SQLite first. It runs the event log, admin state, indexes, publisher checkpoints, and embedded JetStream state without a separate database.

1. Download `orisun-sqlite` from [GitHub Releases](https://github.com/oexza/Orisun/releases).
2. Start it with the [SQLite binary example](./getting-started#run-sqlite-from-a-binary).
3. Verify gRPC with [Verify the API](./getting-started#verify-the-api).
4. Save an event with [Save your first event](./getting-started#save-your-first-event).

Move to PostgreSQL when you need multiple Orisun nodes or database-managed operations.

## Mental Model

Orisun applications usually follow this loop:

1. Define a boundary, such as `orders` or `ledger`.
2. Query the event data that matters to the command.
3. Save new events only if that queried context is still at the expected position.
4. Subscribe with catch-up replay from storage, then continue from live JetStream messages.

`event_type` is the API field for the event's type. Orisun stores it in event `data` as the canonical `eventType` JSON key, derives returned event types from that key, and lets content criteria and JSON indexes use `eventType` without duplicating it in every payload.

## NATS Choices

Standalone and embedded Orisun start embedded NATS JetStream by default.

Use embedded NATS when you want the simplest deployment. Set `ORISUN_NATS_URL` or use the Go embedding NATS options when you already operate a JetStream-enabled NATS server. Embedded Go callers can also use Orisun's in-process NATS handles directly instead of connecting to a URL.

SQLite remains single-node only and must run with NATS clustering disabled.
