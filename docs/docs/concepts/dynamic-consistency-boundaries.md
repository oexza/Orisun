---
title: Dynamic Consistency Boundaries
description: Use Orisun as a DCB event store with query-scoped append conditions.
---

Orisun is CCC-first, but it can also be used as a [Dynamic Consistency Boundary](https://dcb.events/) event store. DCB describes an event-store pattern where the application reads a dynamically selected set of events, records the observed position, and appends new events only if that same set has not changed.

Compatibility here is semantic, not vocabulary-level. Orisun does not expose a separate DCB API with DCB field names. Instead, Orisun's existing CCC API can express the same append condition: "append these events only if no event matching this query committed after the position I observed."

In Orisun, that operation is expressed with existing EventStore API fields:

1. Read the relevant events with `GetEvents` or `GetLatestByCriteria`.
2. Keep the returned position for that read context.
3. Append with `SaveEvents.query.expected_position`.
4. Put the dynamic set definition in `SaveEvents.query.subsetQuery`.
5. Treat `ALREADY_EXISTS` as the append-condition failure and re-read before retrying.

## What Compatible Means

Orisun is DCB-compatible in the sense that a DCB command loop can be implemented directly on top of the EventStore API:

| DCB requirement | Orisun support |
| --- | --- |
| Events have a type | `event_type` is supplied on save and stored as queryable `eventType`. |
| Events have tags | Tags are represented as JSON fields in event `data`. |
| Reads select events by type and tags | `Query.criteria` filters by JSON fields, including `eventType`. |
| The read returns an ordering position | `GetEvents` returns event positions; `GetLatestByCriteria` returns `context_position`. |
| Appends can include a condition over a dynamic query | `SaveEvents.query.subsetQuery` defines the query and `expected_position` anchors it. |
| The store rejects stale appends | Orisun returns `ALREADY_EXISTS` when matching events committed after `expected_position`. |

The core compatible operation is:

```text
append(events)
  if no event matching subsetQuery exists after expected_position
```

In Orisun that operation is `SaveEvents` with both `query.expected_position` and `query.subsetQuery`.

## Where Orisun Differs

Orisun is not a literal implementation of a DCB-specific API surface. The differences are intentional:

- **CCC is the primary model.** Orisun documentation, code, and API names are command-first: a command reads a context, decides, then saves with the same context. DCB describes the same consistency behavior from the event store's append-condition perspective.
- **Orisun boundaries are not DCB consistency boundaries.** An Orisun boundary is a configured storage partition. A DCB consistency boundary is the event subset matched by a query at write time.
- **Tags are JSON data fields.** Orisun does not have a separate tag store. Event type and tags are queryable fields in the stored JSON event data.
- **The append method is `SaveEvents`.** There is no separate `appendWithCondition` method. The condition is encoded by `expected_position` plus `subsetQuery`.
- **The conflict status is gRPC-shaped.** DCB literature may call this an append-condition failure; Orisun returns gRPC `ALREADY_EXISTS`.
- **Indexes are backend-specific.** PostgreSQL and SQLite can scan unindexed criteria, while FoundationDB requires ready covering indexes. DCB-compatible modeling still needs Orisun index planning.

The practical rule: use DCB terminology for modeling and discussion when it helps, but use Orisun's API names and operational rules when implementing.

## Term Mapping

| DCB term | Orisun term |
| --- | --- |
| Event store | Orisun server plus selected storage backend |
| Event log | Orisun boundary |
| Event type | `event_type` on save; canonical `eventType` key in stored event data |
| Tags | Queryable JSON fields in event `data` |
| Query | `GetEvents`, `GetLatestByCriteria`, and `Query.criteria` |
| Append condition | `SaveEvents.query.expected_position` plus `SaveEvents.query.subsetQuery` |
| Consistency boundary | The event subset matched by `subsetQuery` |
| Conflict | `ALREADY_EXISTS` |

The most important naming difference is boundary. An Orisun boundary is a configured physical/logical partition: PostgreSQL schema, SQLite file, FoundationDB key range prefix, or equivalent backend namespace. A DCB consistency boundary is the dynamic subset matched by the write's query. Most applications use one Orisun boundary for a product/domain area, then define many DCB consistency boundaries inside it.

## Example

A course-registration command might depend on:

- the course capacity event,
- existing registration events for that course,
- the student's own registration events.

Those are not necessarily one aggregate stream. In DCB terms, the command builds a dynamic consistency boundary from event type and tag predicates. In Orisun, model those predicates as JSON fields:

```json
{
  "criteria": [
    {
      "tags": [
        {"key": "eventType", "value": "CoursePublished"},
        {"key": "courseId", "value": "course-123"}
      ]
    },
    {
      "tags": [
        {"key": "eventType", "value": "StudentRegistered"},
        {"key": "courseId", "value": "course-123"}
      ]
    },
    {
      "tags": [
        {"key": "eventType", "value": "StudentRegistered"},
        {"key": "studentId", "value": "student-456"}
      ]
    }
  ]
}
```

Use that query to read the context. Then pass the same query as `SaveEvents.query.subsetQuery` with the observed `expected_position`. If another matching registration commits before your append, Orisun rejects the write with `ALREADY_EXISTS`.

## Event Types And Tags

Orisun stores the API `event_type` as the canonical `eventType` JSON field. DCB-style event type filters should query `eventType`.

Tags are ordinary JSON fields in Orisun. You can use top-level fields such as `courseId` and `studentId`, or a naming convention such as `scopes.courseId`. The key rule is that the fields used in DCB conditions must be present in the event `data` and indexed for high-volume paths.

## Reading The Context

Use `GetEvents` when a command needs to replay the matching history.

Use `GetLatestByCriteria` when the command uses carried state and needs the latest event for several criteria. `GetLatestByCriteria` reads all criteria from one consistent snapshot and returns a `context_position` for the next append condition.

Do not assemble a multi-part DCB context from independent reads. Separate reads can observe different snapshots. Use one combined query or `GetLatestByCriteria` so the expected position represents the entire context.

## Backend Notes

PostgreSQL and SQLite can evaluate unindexed criteria, but hot DCB paths should still be indexed. Without indexes, reads, CCC checks, and DCB append conditions may scan too much data.

FoundationDB requires ready covering indexes for criteria reads, CCC checks, and DCB append conditions. This is stricter, but it keeps DCB conflict ranges scoped to the indexed event subset and avoids boundary-wide scans.

## Design Guidance

- Prefer CCC terminology inside Orisun code and docs when discussing command handlers.
- Use DCB terminology when integrating with teams, papers, or tools that already use the DCB vocabulary.
- Keep each append condition as narrow as the business invariant allows.
- Index every event-data field used in high-volume DCB conditions.
- Treat `ALREADY_EXISTS` as an expected append-condition failure, not as infrastructure failure.
