---
title: Command Context Consistency
description: Scope optimistic consistency with event-content queries.
---

Command Context Consistency, or CCC, is Orisun's optimistic consistency model. A command defines the event subset it depends on with a query. Orisun saves the command's new events only if that queried subset has not changed since the expected position.

Traditional event stores often ask applications to choose an aggregate stream up front. Orisun lets the command choose the consistency context it actually needs.

## Why CCC Exists

Many business commands do not fit neatly into one stream. A transfer can depend on two accounts. A limit check can depend on a customer, product, and risk class. A workflow step can depend on multiple prior decisions.

With CCC, the application asks for the event subset that matters to the command, builds a model from those events, and records the next events only if that subset is unchanged.

## Command flow

1. **Check**: query matching events and build the command context model.
2. **Decide**: validate business rules in application code.
3. **Record**: save events with the same context query and expected position.
4. **Retry**: if Orisun returns `ALREADY_EXISTS`, re-query the context and decide again.

## Example context

A transfer command can depend on all events where `account_holder` is either Alice or Bob:

```json
{
  "criteria": [
    {
      "tags": [
        {"key": "account_holder", "value": "alice"}
      ]
    },
    {
      "tags": [
        {"key": "account_holder", "value": "bob"}
      ]
    }
  ]
}
```

Criteria entries are combined with OR. Tags inside one criterion are combined with AND.

## Expected position

The save request includes an expected position for the queried context:

```json
{
  "expected_position": {
    "commit_position": -1,
    "prepare_position": -1
  }
}
```

Use `{-1, -1}` as the before-first-event position.

## Reading a command context

For a single paged history read, use `GetEvents` and take the position of the last event returned as the expected position for the next write.

For carried-state contexts that need the latest event for multiple criteria, use `GetLatestByCriteria`. The server reads all criteria from one consistent snapshot and returns:

- the latest event matching each criterion, in request order,
- `context_position`, the max position observed in that same snapshot.

Pass `context_position` to `SaveEvents.query.expected_position` with the same combined criteria.

Do not assemble a multi-criterion command context from independent `GetEvents` calls. Those calls can observe different snapshots; a write can commit between them with a position below the maximum position you observed, and a scalar expected position cannot prove the earlier call saw it.

## Save with a subset query

In `SaveEvents`, the consistency query is passed as `query.subsetQuery`:

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
            {"key": "account_holder", "value": "alice"}
          ]
        },
        {
          "tags": [
            {"key": "account_holder", "value": "bob"}
          ]
        }
      ]
    }
  },
  "events": [
    {
      "event_id": "transfer-001",
      "event_type": "TransferRecorded",
      "data": "{\"from\":\"alice\",\"to\":\"bob\",\"amount\":25}",
      "metadata": "{}"
    }
  ]
}
```

The field name is `subsetQuery` because it is the protobuf JSON name for `SaveQuery.subsetQuery`.

## Conflict behavior

If any event matching the command's context appears after the expected position, Orisun rejects the write with an `ALREADY_EXISTS` gRPC status. This is an expected concurrency signal, not a server failure.

The application should:

1. Query the context again.
2. Rebuild the decision model.
3. Decide whether the command is still valid.
4. Retry the save with the new expected position.

## Design guidance

- Keep criteria as narrow as the command's invariants allow.
- Use `GetLatestByCriteria` for multi-criterion carried-state decisions.
- Create indexes for fields used in high-volume command contexts.
- Treat `ALREADY_EXISTS` as a retryable business conflict.
- Use stable event IDs so retried commands remain idempotent at the application boundary.
