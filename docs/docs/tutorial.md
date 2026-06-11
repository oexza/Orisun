---
id: tutorial
title: Tutorial — A Consistent Ledger
description: Build an account ledger end to end with Command Context Consistency, indexes, and a live subscription.
---

This tutorial builds a small account ledger and walks through the core Orisun workflow: save events, scope a consistency check with a query, handle a concurrency conflict, index the query field, and subscribe a projector to live updates.

Every step uses `grpcurl` against a running server. Use [Getting Started](./getting-started) to start Orisun before continuing.

## The scenario

An account can be opened, credited, and debited. The invariant is simple: an account must not be debited below zero. If two debits are decided from the same balance, both must not commit. Command Context Consistency gives the application that protection.

The ledger uses an `accounts` boundary. Add it to the server configuration before startup:

```bash
ORISUN_BOUNDARIES='[{"name":"accounts"},{"name":"orisun_admin"}]'
ORISUN_ADMIN_BOUNDARY=orisun_admin
```

For PostgreSQL, also map it to a schema:

```bash
ORISUN_PG_SCHEMAS=accounts:public,orisun_admin:admin
```

All commands below use the default Basic auth header:

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
```

## 1. Open an account

The first event for a brand-new context uses the before-first-event position `{-1, -1}`.

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "accounts",
  "query": {
    "expected_position": {"commit_position": -1, "prepare_position": -1},
    "subsetQuery": {
      "criteria": [
        {"tags": [{"key": "account_id", "value": "acct-1"}]}
      ]
    }
  },
  "events": [
    {
      "event_id": "acct-1-opened",
      "event_type": "AccountOpened",
      "data": "{\"account_id\":\"acct-1\",\"balance\":0}",
      "metadata": "{}"
    }
  ]
}
EOF
```

`subsetQuery` declares the consistency context: all events tagged `account_id = acct-1`. The save succeeds only if no matching event exists after `{-1, -1}`. Note the committed `log_position` in the response. It is the expected position for the next command.

The stored event data also includes `eventType` from the caller-supplied `event_type`, so later queries and indexes can filter by event type without adding it to each `data` JSON payload.

## 2. Credit the account

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "accounts",
  "query": {
    "expected_position": {"commit_position": 1, "prepare_position": 0},
    "subsetQuery": {
      "criteria": [
        {"tags": [{"key": "account_id", "value": "acct-1"}]}
      ]
    }
  },
  "events": [
    {
      "event_id": "acct-1-credit-1",
      "event_type": "MoneyCredited",
      "data": "{\"account_id\":\"acct-1\",\"amount\":100,\"balance\":100}",
      "metadata": "{}"
    }
  ]
}
EOF
```

Use the `log_position` returned by step 1 as `expected_position`. If another command wrote to `acct-1` in between, this save is rejected.

## 3. Read the context and decide

This ledger carries the account balance on each event, so a debit command does not need to replay the whole account history. Read the latest matching event from a single server-side snapshot:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/GetLatestByCriteria <<EOF
{
  "boundary": "accounts",
  "criteria": [
    {"tags": [{"key": "account_id", "value": "acct-1"}]}
  ]
}
EOF
```

The application reads `results[0].event.data.balance`, decides whether the debit is valid, and remembers `context_position`. That position is the consistency anchor for the debit.

## 4. Debit with a consistency check

The debit decision, such as `balance >= amount`, lives in application code. Orisun guarantees that the selected context did not change between the read and the write.

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "accounts",
  "query": {
    "expected_position": {"commit_position": 2, "prepare_position": 1},
    "subsetQuery": {
      "criteria": [
        {"tags": [{"key": "account_id", "value": "acct-1"}]}
      ]
    }
  },
  "events": [
    {
      "event_id": "acct-1-debit-1",
      "event_type": "MoneyDebited",
      "data": "{\"account_id\":\"acct-1\",\"amount\":40,\"balance\":60}",
      "metadata": "{}"
    }
  ]
}
EOF
```

Set `expected_position` to the `context_position` observed in step 3. The save commits only if no newer `acct-1` event exists.

## 5. Handle the conflict

If a second debit was decided against the same balance, one of the two saves loses the race and returns:

```text
ERROR:
  Code: AlreadyExists
```

This is a concurrency signal. The losing command should:

1. Re-run the latest-by-criteria read in step 3.
2. Read the carried balance from the new latest event.
3. Re-check the invariant. The debit may no longer be valid.
4. Retry the save with the new `expected_position`.

Reusing the same `event_id` on retry keeps the command idempotent at the application boundary.

## 6. Index the query field

Steps 3 and 4 both filter on `account_id`. Without an index, each query scans the boundary table. Create a btree index once, during deployment:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CreateIndex <<EOF
{
  "boundary": "accounts",
  "name": "account_id",
  "fields": [
    {"json_key": "account_id", "value_type": "TEXT"}
  ]
}
EOF
```

Both the CCC consistency check and read queries can now use the index. See [Indexing](./concepts/indexing) for composite and partial index examples.

## 7. Project to a read model

A balance read model stays current by subscribing. Catch-up replay reads stored history first, then switches to live JetStream delivery:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "balance-projector",
  "boundary": "accounts",
  "after_position": {"commit_position": -1, "prepare_position": -1}
}
EOF
```

The stream emits each event with its `position` and `date_created`. The projector applies events in order and persists its own checkpoint after each side effect is durable, so a restart resumes from the last processed position. Delivery is at least once, so consumers should deduplicate by `event_id`. See [Delivery Guarantees](./concepts/delivery-guarantees).

## What you built

- A consistency boundary scoped by event content, not a fixed stream — [Command Context Consistency](./concepts/command-context-consistency).
- An optimistic write that rejects stale context with `ALREADY_EXISTS`.
- An index that keeps the context query and read model fast.
- A live, recoverable projection over the same event log.
