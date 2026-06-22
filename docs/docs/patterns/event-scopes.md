---
title: Event Scopes
description: Model event membership with queryable scope keys.
---

Event scopes are a modeling convention for grouping related events without forcing every command into one stream. The event that creates something gets its own stable event id. Later events copy that id into queryable scope fields.

Orisun does not reserve a special `scope` field. Criteria and indexes match JSON keys in event `data`, so scope keys are normal event data keys. A common convention is to prefix them with `scope.`. The examples below show stored event data; when calling `SaveEvents`, pass the type through `event_type` and Orisun writes the canonical `eventType` key into `data`.

```json
{
  "eventType": "AccountOpened",
  "accountOpenedId": "account-opened-alice",
  "holder": "alice"
}
```

```json
{
  "eventType": "TransferRecorded",
  "transferRecordedId": "transfer-1",
  "from": "alice",
  "to": "bob",
  "amount": 25,
  "scope.fromAccountOpenedId": "account-opened-alice",
  "scope.toAccountOpenedId": "account-opened-bob"
}
```

In application code you can still model this as a nested object:

```typescript
{
  eventType: 'TransferRecorded',
  transferRecordedId: 'transfer-1',
  from: 'alice',
  to: 'bob',
  amount: 25,
  scope: {
    fromAccountOpenedId: 'account-opened-alice',
    toAccountOpenedId: 'account-opened-bob',
  },
}
```

Flatten it before saving if you want to query or index `scope.fromAccountOpenedId`, then unflatten it after reads. Orisun matches JSON keys exactly; `scope.fromAccountOpenedId` is a key name, not an implicit nested path.

## Why scope events

Scopes let each event declare the prior event identities it belongs to:

- an account event belongs to the `AccountOpened` event that started that account,
- a transfer event can belong to both affected account scopes,
- a workflow event can belong to the user, order, payment, or approval event that made it relevant.

That gives commands and projectors stable query handles without introducing aggregate-stream rules into the event store.

## Query a scope

To read account history for Alice, query the root event and transfer events where Alice's account is either side of the transfer:

```json
{
  "criteria": [
    {
      "tags": [
        {"key": "eventType", "value": "AccountOpened"},
        {"key": "accountOpenedId", "value": "account-opened-alice"}
      ]
    },
    {
      "tags": [
        {"key": "eventType", "value": "TransferRecorded"},
        {"key": "scope.fromAccountOpenedId", "value": "account-opened-alice"}
      ]
    },
    {
      "tags": [
        {"key": "eventType", "value": "TransferRecorded"},
        {"key": "scope.toAccountOpenedId", "value": "account-opened-alice"}
      ]
    }
  ]
}
```

Criteria entries are ORed together. Tags inside one criterion are ANDed together.

Including `eventType` in each criterion keeps query shapes specific and aligns with partial indexes. If you intentionally need every event with a scope key, you can query only `scope.fromAccountOpenedId` or `scope.toAccountOpenedId`, but that is usually a broader and less index-friendly shape.

## Use scopes with CCC

Scopes are a natural fit for [Command Context Consistency](../concepts/command-context-consistency). A transfer command can build its decision model from both affected account scopes, then save `TransferRecorded` only if that same scoped context is unchanged.

```json
{
  "boundary": "accounts",
  "query": {
    "expected_position": {
      "commit_position": 100,
      "prepare_position": 7
    },
    "subsetQuery": {
      "criteria": [
        {
          "tags": [
            {"key": "eventType", "value": "AccountOpened"},
            {"key": "accountOpenedId", "value": "account-opened-alice"}
          ]
        },
        {
          "tags": [
            {"key": "eventType", "value": "AccountOpened"},
            {"key": "accountOpenedId", "value": "account-opened-bob"}
          ]
        },
        {
          "tags": [
            {"key": "eventType", "value": "TransferRecorded"},
            {"key": "scope.fromAccountOpenedId", "value": "account-opened-alice"}
          ]
        },
        {
          "tags": [
            {"key": "eventType", "value": "TransferRecorded"},
            {"key": "scope.toAccountOpenedId", "value": "account-opened-alice"}
          ]
        },
        {
          "tags": [
            {"key": "eventType", "value": "TransferRecorded"},
            {"key": "scope.fromAccountOpenedId", "value": "account-opened-bob"}
          ]
        },
        {
          "tags": [
            {"key": "eventType", "value": "TransferRecorded"},
            {"key": "scope.toAccountOpenedId", "value": "account-opened-bob"}
          ]
        }
      ]
    }
  },
  "events": [
    {
      "event_id": "00000000-0000-4000-8000-000000000101",
      "event_type": "TransferRecorded",
      "data": "{\"transferRecordedId\":\"transfer-1\",\"from\":\"alice\",\"to\":\"bob\",\"amount\":25,\"scope.fromAccountOpenedId\":\"account-opened-alice\",\"scope.toAccountOpenedId\":\"account-opened-bob\"}",
      "metadata": "{}"
    }
  ]
}
```

Use all criteria that the command model actually read. A real transfer should include root account events and every event type and scope key that affects each account's balance.

## Index scope keys

Create partial indexes for scope keys used by command contexts or high-volume projections:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CreateIndex <<EOF
{
  "boundary": "accounts",
  "name": "transfer_from_account_scope",
  "fields": [
    {"json_key": "scope.fromAccountOpenedId", "value_type": "TEXT"}
  ],
  "conditions": [
    {"key": "eventType", "operator": "=", "value": "TransferRecorded"}
  ],
  "condition_combinator": "AND"
}
EOF
```

For several event types that share the same scope key, create one partial index with ORed `eventType` conditions, or separate indexes when your operational policy favors smaller per-type indexes.

## Guidelines

- Put the creating event's id on the creating event itself, such as `accountOpenedId`.
- Put backlinks to earlier events under queryable scope keys, such as `scope.fromAccountOpenedId`.
- Carry all scopes needed by downstream commands and projectors, not every id you know.
- Keep `metadata` for tracing, request source, and operational context; put domain scopes in `data`.
- Index scope keys that appear in CCC `subsetQuery` values or replay filters.
