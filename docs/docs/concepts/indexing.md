---
title: Indexing
description: Create JSON indexes for criteria queries and CCC checks.
---

Criteria queries match JSON payload fields. Without indexes, reads and CCC checks may scan the full boundary event table.

Create indexes for fields used in:

- command context criteria
- projector catch-up filters
- common read models
- high-volume event categories

## Index API

Index management is exposed on the EventStore gRPC service, not the Admin service. This matters for embedded deployments: applications can manage indexes without exposing Admin.

All examples assume:

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
```

## Simple Index

```bash
grpcurl -H "$AUTH" \
  -d '{"boundary":"orders","name":"customer_id","fields":[{"json_key":"customer_id","value_type":"TEXT"}]}' \
  localhost:5005 orisun.EventStore/CreateIndex
```

## Composite Index

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CreateIndex <<EOF
{
  "boundary": "orders",
  "name": "category_priority",
  "fields": [
    {"json_key": "category", "value_type": "TEXT"},
    {"json_key": "priority", "value_type": "TEXT"}
  ]
}
EOF
```

## Partial Index

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CreateIndex <<EOF
{
  "boundary": "orders",
  "name": "placed_amount",
  "fields": [
    {"json_key": "amount", "value_type": "NUMERIC"}
  ],
  "conditions": [
    {"key": "eventType", "operator": "=", "value": "OrderPlaced"}
  ],
  "condition_combinator": "AND"
}
EOF
```

## Drop An Index

```bash
grpcurl -H "$AUTH" \
  -d '{"boundary":"orders","name":"customer_id"}' \
  localhost:5005 orisun.EventStore/DropIndex
```

## Backend Behavior

PostgreSQL uses JSONB expression indexes. SQLite uses JSON expression indexes.

## Naming and safety

Index names are boundary-local logical names. Orisun validates names before creating backend objects.

Use migrations or a controlled startup task for production index creation. Creating indexes during high-traffic command paths can add avoidable latency.
