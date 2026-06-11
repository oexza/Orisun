---
title: EventStore API
description: Save, query, subscribe, ping, and manage indexes.
---

The EventStore service owns event operations:

- `SaveEvents`
- `GetEvents`
- `CatchUpSubscribeToEvents`
- `Ping`
- `CreateIndex`
- `DropIndex`

All examples use the default Basic auth header:

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
```

## Data model

Events have four caller-supplied fields:

| Field | Description |
| --- | --- |
| `event_id` | Stable event identifier. Use this for idempotency and deduplication. |
| `event_type` | Event type name, for example `OrderPlaced`. |
| `data` | JSON object encoded as a string. Criteria queries match this JSON object. |
| `metadata` | JSON object encoded as a string. Use for request source, tracing, or non-domain metadata. |

Orisun also stores a durable `position` and `date_created` on committed events.

:::note
`SaveEvents` adds `eventType` to the stored event data from `event_type`, so criteria and indexes can use the `eventType` JSON key.
:::

## SaveEvents

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orders",
  "query": {
    "expected_position": {
      "commit_position": -1,
      "prepare_position": -1
    }
  },
  "events": [
    {
      "event_id": "order-001",
      "event_type": "OrderPlaced",
      "data": "{\"customer_id\":\"c-1\",\"amount\":45}",
      "metadata": "{\"source\":\"checkout\"}"
    }
  ]
}
EOF
```

Batches are atomic. Events in one batch share the same commit position and receive increasing prepare positions.

### Save with a consistency subset

Use `query.subsetQuery` to enforce Command Context Consistency for a specific event subset:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orders",
  "query": {
    "expected_position": {
      "commit_position": 12,
      "prepare_position": 8
    },
    "subsetQuery": {
      "criteria": [
        {
          "tags": [
            {"key": "customer_id", "value": "c-1"}
          ]
        }
      ]
    }
  },
  "events": [
    {
      "event_id": "order-002",
      "event_type": "OrderConfirmed",
      "data": "{\"customer_id\":\"c-1\",\"amount\":45}",
      "metadata": "{}"
    }
  ]
}
EOF
```

If the subset changed after the expected position, Orisun returns `ALREADY_EXISTS`.

## GetEvents

Read from the beginning:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orders",
  "from_position": {
    "commit_position": 0,
    "prepare_position": 0
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

Read by criteria:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orders",
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "customer_id", "value": "c-1"}
        ]
      }
    ]
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

Page from a position:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetEvents <<EOF
{
  "boundary": "orders",
  "from_position": {
    "commit_position": 1000,
    "prepare_position": 42
  },
  "count": 100,
  "direction": "ASC"
}
EOF
```

`GetEvents` returns matching events with their committed position and creation time:

```json
{
  "events": [
    {
      "event_id": "order-001",
      "event_type": "OrderPlaced",
      "data": "{\"customer_id\":\"c-1\",\"amount\":45}",
      "metadata": "{\"source\":\"checkout\"}",
      "position": {"commit_position": 1, "prepare_position": 0},
      "date_created": "2026-05-30T12:00:00Z"
    }
  ]
}
```

`Event` adds `position` and `date_created` to the fields supplied at write time. `CatchUpSubscribeToEvents` delivers the same event shape.

### Paging through a boundary

`GetEvents` returns one bounded page (`count`, server-capped at 10000). To walk the whole log or a criteria set, page forward:

1. First call uses `from_position` `{0, 0}` to start at the beginning.
2. Process the page, then take the `position` of the last event.
3. Pass it as `from_position` on the next call.
4. Stop when a page returns fewer events than `count`.

Keep the consumer idempotent and deduplicate by `event_id` rather than assuming exactly-once paging. The position model behind `from_position` and `direction` is described in [Positions and Ordering](../concepts/positions).

## GetLatestByCriteria

`GetLatestByCriteria` returns the latest event matching each criterion, assembled by the server from **one consistent read snapshot**, plus a `context_position` to use as the `expected_position` of the next `SaveEvents` with the same combined criteria. It is the command-side read for the carried-state pattern: store the resulting state (for example an account balance) on each event, then a command needs only the latest event per entity, not a history replay.

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetLatestByCriteria <<EOF
{
  "boundary": "ledger",
  "criteria": [
    {"tags": [{"key": "account_id", "value": "acct-01"}]},
    {"tags": [{"key": "account_id", "value": "acct-02"}]}
  ]
}
EOF
```

The response carries one `result` per request criterion in order (`event` unset when nothing matches) and `context_position` — the max position observed in the same snapshot, or `{-1, -1}` when nothing matched.

Why a dedicated RPC instead of separate `GetEvents` calls: two calls are two snapshots. An event can commit between them with a position *below* the maximum you observed, and a scalar `expected_position` can only prove "nothing newer than X exists" — it cannot prove your reads saw everything up to X. `GetLatestByCriteria` closes that gap by sampling the whole context atomically. See [Command Context Consistency](../concepts/command-context-consistency).

## CatchUpSubscribeToEvents

Catch-up subscriptions replay stored events, then switch to live JetStream delivery.

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "order-projector",
  "boundary": "orders",
  "after_position": {
    "commit_position": 0,
    "prepare_position": 0
  }
}
EOF
```

Filtered subscription:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "placed-orders",
  "boundary": "orders",
  "after_position": {
    "commit_position": 0,
    "prepare_position": 0
  },
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "eventType", "value": "OrderPlaced"}
        ]
      }
    ]
  }
}
EOF
```

## Ping

`Ping` is an authenticated liveness check that takes no arguments:

```bash
grpcurl -H "$AUTH" -d '{}' localhost:5005 orisun.EventStore/Ping
```

## CreateIndex

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CreateIndex <<EOF
{
  "boundary": "orders",
  "name": "customer_id",
  "fields": [
    {"json_key": "customer_id", "value_type": "TEXT"}
  ]
}
EOF
```

`value_type` is `TEXT`, `NUMERIC`, `BOOLEAN`, or `TIMESTAMPTZ`. Add `conditions` for a partial index. Each condition `operator` must be one of `=`, `>`, `<`, `>=`, or `<=`. See [Indexing](../concepts/indexing) for composite and partial index examples.

## DropIndex

```bash
grpcurl -H "$AUTH" \
  -d '{"boundary":"orders","name":"customer_id"}' \
  localhost:5005 orisun.EventStore/DropIndex
```

## Proto source

The EventStore protobuf source lives at [`proto/eventstore.proto`](https://github.com/oexza/Orisun/blob/main/proto/eventstore.proto).

## Common status codes

| Status | Meaning |
| --- | --- |
| `INVALID_ARGUMENT` | The request is malformed, uses invalid JSON, or references invalid index fields. |
| `UNAUTHENTICATED` | Missing or invalid credentials. |
| `PERMISSION_DENIED` | Authenticated user does not have a required role. |
| `ALREADY_EXISTS` | Optimistic consistency conflict during `SaveEvents`; re-query and retry if still valid. |
| `INTERNAL` | Storage, publishing, or unexpected server failure. |
