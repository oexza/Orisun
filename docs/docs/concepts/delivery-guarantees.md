---
title: Delivery Guarantees
description: Understand publishing order, catch-up, and duplicate delivery.
---

PostgreSQL or SQLite is the durable source of truth. Embedded NATS JetStream is the real-time delivery layer.

Wake-up signals are hints. Correctness comes from durable checkpoints and ordered catch-up reads.

## Per-Boundary Guarantees

| Guarantee | How Orisun enforces it |
| --- | --- |
| No skipped committed events | The publisher stores the last published position in the selected backend and resumes from the next position. |
| Sequential publishing | The publisher drains events in ascending event-log position and rejects non-advancing batches. |
| Stable committed prefix | PostgreSQL avoids exposing in-flight transactions; SQLite serializes commits through one writer per boundary. |
| Single active publisher | PostgreSQL nodes acquire a boundary lock; SQLite runs exactly one active node. |

## Notifications are not the guarantee

PostgreSQL `LISTEN/NOTIFY`, SQLite wake-ups, and polling only tell the publisher that there may be work to do. If a signal is missed, periodic polling still drains the committed event log from the persisted checkpoint.

This design is why no committed event is skipped even when a wake-up signal is delayed or lost.

## At-least-once delivery

Publishing is at least once at the boundary between NATS publish and checkpoint update.

If NATS accepts an event and the checkpoint write fails, the event can be republished. Consumers should deduplicate by:

- `event_id`
- NATS message ID
- idempotent projector writes

## Ordering Scope

Sequential publishing is per boundary. Different boundaries can publish independently.

Within a boundary, consumers should process positions monotonically and persist their own projector checkpoint after side effects are durable.

## Subscriber catch-up

`CatchUpSubscribeToEvents` has two phases:

1. Read committed events after the requested position from the durable store.
2. Subscribe to JetStream for live delivery.

The catch-up phase is what lets a subscriber recover from downtime without depending on JetStream retention alone.
