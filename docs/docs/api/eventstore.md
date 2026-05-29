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

## CatchUpSubscribeToEvents

Catch-up subscriptions replay stored events, then switch to live JetStream delivery.

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "order-projector",
  "boundary": "orders",
  "after_position": {
    "commit_position": -1,
    "prepare_position": -1
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
    "commit_position": -1,
    "prepare_position": -1
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
