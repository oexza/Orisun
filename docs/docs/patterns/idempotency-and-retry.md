---
title: Idempotency & Retry
description: Make commands safe to retry and consumers safe to reprocess.
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

Orisun gives you two distinct idempotency problems to solve, and a mechanism for each:

- **Write side** — a command may need to be retried (network blip, contention, or a lost response). Retrying must not double-apply the business decision.
- **Read side** — delivery is [at least once](../concepts/delivery-guarantees#at-least-once-delivery), so a projector can see the same event more than once. Reprocessing must not double-apply a side effect.

## Write side: the CCC check is your idempotency guard

The primary idempotency mechanism is the Command Context Consistency `expected_position`, not the `event_id`:

1. Read the command context (`GetLatestByCriteria` or `GetEvents`) and note the position.
2. Save with that position as `expected_position` and a `subsetQuery` for the context.
3. If the save **committed** and you retry with the *same* `expected_position`, Orisun returns `ALREADY_EXISTS` — the context already advanced — rather than writing a duplicate.

So `ALREADY_EXISTS` after a retry means *"something already moved this context"* — very often your own first attempt.

:::note
The store does **not** deduplicate by `event_id`. There is no unique constraint on `event_id` (the primary key is the per-boundary `global_id`). A stable `event_id` is for *detection* and *consumer dedup*; the CCC check is what prevents duplicate writes.
:::

### Use a deterministic event_id

Generate the `event_id` from the command intent, not with a fresh random UUID on every attempt. A retried command then carries the same `event_id` as the original, which lets you (and your projectors) recognize it.

### Retry loop

On `ALREADY_EXISTS`, re-read the context and re-decide — the invariant may no longer hold. Loop until the save commits or the decision is no longer valid:

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
// Deterministic per command intent — NOT uuid.NewString() on each attempt.
eventID := "00000000-0000-4000-8000-0000000000a1"

for {
	latest, err := client.GetLatestByCriteria(ctx, &eventstore.GetLatestByCriteriaRequest{
		Boundary: "accounts",
		Criteria: []*eventstore.Criterion{{
			Tags: []*eventstore.Tag{{Key: "account_id", Value: "acct-1"}},
		}},
	})
	if err != nil {
		return err
	}

	balance := readBalance(latest) // application reads carried state
	if balance < amount {
		return ErrInsufficientFunds // no longer valid — stop
	}

	_, err = client.SaveEvents(ctx, &eventstore.SaveEventsRequest{
		Boundary: "accounts",
		Query: &eventstore.SaveQuery{
			ExpectedPosition: latest.ContextPosition,
			SubsetQuery: &eventstore.Query{
				Criteria: []*eventstore.Criterion{{
					Tags: []*eventstore.Tag{{Key: "account_id", Value: "acct-1"}},
				}},
			},
		},
		Events: []*eventstore.EventToSave{{
			EventId:   eventID,
			EventType: "MoneyDebited",
			Data:      `{"account_id":"acct-1","amount":40,"balance":` + bal(balance-amount) + `}`,
		}},
	})
	if err == nil {
		return nil
	}

	var conflict *orisun.OptimisticConcurrencyException
	if !errors.As(err, &conflict) {
		return err // a real failure, not a concurrency signal
	}
	// Context changed between read and write — loop re-reads and re-decides.
}
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
const eventId = '00000000-0000-4000-8000-0000000000a1'; // deterministic per command

for (;;) {
  const latest = await client.getLatestByCriteria({
    boundary: 'accounts',
    criteria: [{ tags: [{ key: 'account_id', value: 'acct-1' }] }],
  });

  const balance = latest.results[0].event?.data.balance ?? 0;
  if (balance < amount) throw new Error('insufficient funds');

  try {
    await client.saveEvents({
      boundary: 'accounts',
      query: {
        expectedPosition: latest.contextPosition,
        subsetQuery: { criteria: [{ tags: [{ key: 'account_id', value: 'acct-1' }] }] },
      },
      events: [{
        eventId,
        eventType: 'MoneyDebited',
        data: { account_id: 'acct-1', amount, balance: balance - amount },
      }],
    });
    return;
  } catch (error) {
    if (!error.message.includes('AlreadyExists')) throw error;
    // Context changed between read and write — loop re-reads and re-decides.
  }
}
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
String eventId = "00000000-0000-4000-8000-0000000000a1"; // deterministic per command

while (true) {
    Eventstore.GetLatestByCriteriaResponse latest = client.getLatestByCriteria(
        Eventstore.GetLatestByCriteriaRequest.newBuilder()
            .setBoundary("accounts")
            .addCriteria(Eventstore.Criterion.newBuilder()
                .addTags(Eventstore.Tag.newBuilder()
                    .setKey("account_id").setValue("acct-1").build())
                .build())
            .build());

    long balance = readBalance(latest); // application reads carried state
    if (balance < amount) throw new IllegalStateException("insufficient funds");

    try {
        client.saveEvents(Eventstore.SaveEventsRequest.newBuilder()
            .setBoundary("accounts")
            .setQuery(Eventstore.SaveQuery.newBuilder()
                .setExpectedPosition(latest.getContextPosition())
                .setSubsetQuery(Eventstore.Query.newBuilder()
                    .addCriteria(Eventstore.Criterion.newBuilder()
                        .addTags(Eventstore.Tag.newBuilder()
                            .setKey("account_id").setValue("acct-1").build())
                        .build())
                    .build())
                .build())
            .addEvents(Eventstore.EventToSave.newBuilder()
                .setEventId(eventId)
                .setEventType("MoneyDebited")
                .setData(debitJson("acct-1", amount, balance - amount))
                .build())
            .build());
        return;
    } catch (OptimisticConcurrencyException conflict) {
        // Context changed between read and write — loop re-reads and re-decides.
    }
}
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

`grpcurl` is not suited to retry loops — it makes one call. Use it to reproduce a single conflict:

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "accounts",
  "query": {
    "expected_position": {"commit_position": 2, "prepare_position": 1},
    "subsetQuery": {"criteria": [{"tags": [{"key": "account_id", "value": "acct-1"}]}]}
  },
  "events": [{
    "event_id": "00000000-0000-4000-8000-0000000000a1",
    "event_type": "MoneyDebited",
    "data": "{\"account_id\":\"acct-1\",\"amount\":40,\"balance\":60}"
  }]
}
EOF
```

  </TabItem>
</Tabs>

### Ambiguous failures: "maybe it committed"

A timeout *after* the server received the save but *before* you got the response is ambiguous — the command may have committed. Treat it like a conflict: re-read the context. If the carried state already reflects your decision, your first attempt committed; do not apply it again. A deterministic `event_id` makes this check recognizable downstream.

## Read side: deduplicate by event_id in the projector

Because delivery is at least once, a projector must treat `apply(event)` as idempotent. Two common approaches:

- **Idempotent writes** — make the side effect a keyed upsert keyed by `event_id`, so applying the same event twice converges. Simplest.
- **Processed-event table** — record each processed `event_id`; skip events already seen. Needed when the side effect is not naturally idempotent (e.g. appending to an external ledger).

Persist the projector checkpoint **after** the side effect is durable, so a restart resumes from the last fully-applied event rather than re-emitting it. See [Delivery Guarantees](../concepts/delivery-guarantees).

## Summary

| Concern | Mechanism |
| --- | --- |
| Don't write a duplicate on retry | CCC `expected_position` → `ALREADY_EXISTS` |
| Recognize a retried command | Deterministic `event_id` |
| Don't double-apply on redelivery | Consumer dedup by `event_id` |
| Recover from a lost response | Re-read the context before retrying |
