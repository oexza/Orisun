---
id: tutorial
title: Tutorial — A Consistent Ledger
description: Build an account ledger end to end with Command Context Consistency, indexes, and a live subscription.
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

This tutorial builds a small account ledger and walks through the core Orisun workflow: save events, scope a consistency check with a query, handle a concurrency conflict, index the query field, and subscribe a projector to live updates.

Start a server with [Getting Started](./getting-started) before continuing. The examples below use the default `admin:changeit` credentials and the `accounts` boundary.

## The scenario

An account can be opened, credited, and debited. The invariant is simple: an account must not be debited below zero. If two debits are decided from the same balance, both must not commit. Command Context Consistency gives the application that protection.

The ledger uses an `accounts` boundary. Add it to the server configuration before startup:

```bash
ORISUN_BOUNDARIES='[{"name":"accounts"},{"name":"orisun_admin"}]'
ORISUN_ADMIN_BOUNDARY=orisun_admin
```

For PostgreSQL-compatible backends, including YugabyteDB, also map it to a schema:

```bash
ORISUN_PG_SCHEMAS=accounts:public,orisun_admin:admin
```

## Connect

Pick your client once; every step below follows that choice. See [EventStore API](./api/eventstore#connect-and-authenticate) for full connection options.

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
import (
	"context"
	"log"

	orisun "github.com/oexza/orisun-client-go"
	eventstore "github.com/oexza/orisun-client-go/eventstore"
)

client, err := orisun.New(
	"localhost:5005",
	orisun.WithCredentials("admin", "changeit"),
	orisun.WithInsecure(),
)
if err != nil {
	log.Fatal(err)
}
defer client.Close()

ctx := context.Background()
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
import { EventStoreClient } from '@orisun/eventstore-client';

const client = new EventStoreClient({
  host: 'localhost',
  port: 5005,
  username: 'admin',
  password: 'changeit',
});
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
import com.orisunlabs.orisun.client.OrisunClient;
import com.orisunlabs.orisun.client.EventSubscription;
import com.orisun.eventstore.Eventstore;

try (OrisunClient client = OrisunClient.newBuilder()
    .withServer("localhost", 5005)
    .withBasicAuth("admin", "changeit")
    .build()) {
  // steps below run inside this block
}
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
```

  </TabItem>
</Tabs>

## 1. Open an account

The first event for a brand-new context uses the before-first-event position `{-1, -1}`.

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
result, err := client.SaveEvents(ctx, &eventstore.SaveEventsRequest{
	Boundary: "accounts",
	Query: &eventstore.SaveQuery{
		ExpectedPosition: &eventstore.Position{CommitPosition: -1, PreparePosition: -1},
		SubsetQuery: &eventstore.Query{
			Criteria: []*eventstore.Criterion{{
				Tags: []*eventstore.Tag{{Key: "account_id", Value: "acct-1"}},
			}},
		},
	},
	Events: []*eventstore.EventToSave{{
		EventId:   "018f2d5e-0001-7000-8000-000000000001",
		EventType: "AccountOpened",
		Data:      `{"account_id":"acct-1","balance":0}`,
		Metadata:  `{}`,
	}},
})

// result.LogPosition is the expected position for the next command
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
const result = await client.saveEvents({
  boundary: 'accounts',
  query: {
    expectedPosition: { commitPosition: -1, preparePosition: -1 },
    subsetQuery: {
      criteria: [{ tags: [{ key: 'account_id', value: 'acct-1' }] }],
    },
  },
  events: [
    {
      eventId: '018f2d5e-0001-7000-8000-000000000001',
      eventType: 'AccountOpened',
      data: { account_id: 'acct-1', balance: 0 },
    },
  ],
});

// result.logPosition is the expected position for the next command
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
Eventstore.WriteResult result = client.saveEvents(Eventstore.SaveEventsRequest.newBuilder()
    .setBoundary("accounts")
    .setQuery(Eventstore.SaveQuery.newBuilder()
        .setExpectedPosition(Eventstore.Position.newBuilder()
            .setCommitPosition(-1).setPreparePosition(-1).build())
        .setSubsetQuery(Eventstore.Query.newBuilder()
            .addCriteria(Eventstore.Criterion.newBuilder()
                .addTags(Eventstore.Tag.newBuilder()
                    .setKey("account_id").setValue("acct-1").build())
                .build())
            .build())
        .build())
    .addEvents(Eventstore.EventToSave.newBuilder()
        .setEventId("018f2d5e-0001-7000-8000-000000000001")
        .setEventType("AccountOpened")
        .setData("{\"account_id\":\"acct-1\",\"balance\":0}")
        .build())
    .build());

// result.getLogPosition() is the expected position for the next command
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

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
      "event_id": "018f2d5e-0001-7000-8000-000000000001",
      "event_type": "AccountOpened",
      "data": "{\"account_id\":\"acct-1\",\"balance\":0}",
      "metadata": "{}"
    }
  ]
}
EOF
```

  </TabItem>
</Tabs>

`subsetQuery` declares the consistency context: all events tagged `account_id = acct-1`. The save succeeds only if no matching event exists after `{-1, -1}`. Note the committed `log_position` in the response. It is the expected position for the next command.

The stored event data includes canonical `eventType` from the caller-supplied API `event_type`, and Orisun derives returned event types from that JSON key. Later queries and indexes can filter by event type without adding it to each `data` JSON payload.

## 2. Credit the account

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
_, err := client.SaveEvents(ctx, &eventstore.SaveEventsRequest{
	Boundary: "accounts",
	Query: &eventstore.SaveQuery{
		ExpectedPosition: &eventstore.Position{CommitPosition: 1, PreparePosition: 0},
		SubsetQuery: &eventstore.Query{
			Criteria: []*eventstore.Criterion{{
				Tags: []*eventstore.Tag{{Key: "account_id", Value: "acct-1"}},
			}},
		},
	},
	Events: []*eventstore.EventToSave{{
		EventId:   "018f2d5e-0002-7000-8000-000000000002",
		EventType: "MoneyCredited",
		Data:      `{"account_id":"acct-1","amount":100,"balance":100}`,
		Metadata:  `{}`,
	}},
})
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
await client.saveEvents({
  boundary: 'accounts',
  query: {
    expectedPosition: { commitPosition: 1, preparePosition: 0 },
    subsetQuery: {
      criteria: [{ tags: [{ key: 'account_id', value: 'acct-1' }] }],
    },
  },
  events: [
    {
      eventId: '018f2d5e-0002-7000-8000-000000000002',
      eventType: 'MoneyCredited',
      data: { account_id: 'acct-1', amount: 100, balance: 100 },
    },
  ],
});
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
client.saveEvents(Eventstore.SaveEventsRequest.newBuilder()
    .setBoundary("accounts")
    .setQuery(Eventstore.SaveQuery.newBuilder()
        .setExpectedPosition(Eventstore.Position.newBuilder()
            .setCommitPosition(1).setPreparePosition(0).build())
        .setSubsetQuery(Eventstore.Query.newBuilder()
            .addCriteria(Eventstore.Criterion.newBuilder()
                .addTags(Eventstore.Tag.newBuilder()
                    .setKey("account_id").setValue("acct-1").build())
                .build())
            .build())
        .build())
    .addEvents(Eventstore.EventToSave.newBuilder()
        .setEventId("018f2d5e-0002-7000-8000-000000000002")
        .setEventType("MoneyCredited")
        .setData("{\"account_id\":\"acct-1\",\"amount\":100,\"balance\":100}")
        .build())
    .build());
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

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
      "event_id": "018f2d5e-0002-7000-8000-000000000002",
      "event_type": "MoneyCredited",
      "data": "{\"account_id\":\"acct-1\",\"amount\":100,\"balance\":100}",
      "metadata": "{}"
    }
  ]
}
EOF
```

  </TabItem>
</Tabs>

Use the `log_position` returned by step 1 as `expected_position`. If another command wrote to `acct-1` in between, this save is rejected.

## 3. Read the context and decide

This ledger carries the account balance on each event, so a debit command does not need to replay the whole account history. Read the latest matching event from a single server-side snapshot:

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
resp, err := client.GetLatestByCriteria(ctx, &eventstore.GetLatestByCriteriaRequest{
	Boundary: "accounts",
	Criteria: []*eventstore.Criterion{{
		Tags: []*eventstore.Tag{{Key: "account_id", Value: "acct-1"}},
	}},
})

// Unmarshal resp.Results[0].Event.Data to read balance.
// resp.ContextPosition is the consistency anchor for the debit.
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
const latest = await client.getLatestByCriteria({
  boundary: 'accounts',
  criteria: [{ tags: [{ key: 'account_id', value: 'acct-1' }] }],
});

const balance = latest.results[0].event?.data.balance ?? 0;
const expectedPosition = latest.contextPosition; // consistency anchor for the debit
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
Eventstore.GetLatestByCriteriaResponse latest = client.getLatestByCriteria(
    Eventstore.GetLatestByCriteriaRequest.newBuilder()
        .setBoundary("accounts")
        .addCriteria(Eventstore.Criterion.newBuilder()
            .addTags(Eventstore.Tag.newBuilder()
                .setKey("account_id").setValue("acct-1").build())
            .build())
        .build());

// Parse latest.getResults(0).getEvent().getData() to read balance.
// latest.getContextPosition() is the consistency anchor for the debit.
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

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

  </TabItem>
</Tabs>

The application reads the carried balance from the latest event, decides whether the debit is valid, and remembers `context_position`. That position is the consistency anchor for the debit.

## 4. Debit with a consistency check

The debit decision, such as `balance >= amount`, lives in application code. Orisun guarantees that the selected context did not change between the read and the write.

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
_, err := client.SaveEvents(ctx, &eventstore.SaveEventsRequest{
	Boundary: "accounts",
	Query: &eventstore.SaveQuery{
		ExpectedPosition: &eventstore.Position{CommitPosition: 2, PreparePosition: 1},
		SubsetQuery: &eventstore.Query{
			Criteria: []*eventstore.Criterion{{
				Tags: []*eventstore.Tag{{Key: "account_id", Value: "acct-1"}},
			}},
		},
	},
	Events: []*eventstore.EventToSave{{
		EventId:   "018f2d5e-0003-7000-8000-000000000003",
		EventType: "MoneyDebited",
		Data:      `{"account_id":"acct-1","amount":40,"balance":60}`,
		Metadata:  `{}`,
	}},
})
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
await client.saveEvents({
  boundary: 'accounts',
  query: {
    expectedPosition: { commitPosition: 2, preparePosition: 1 },
    subsetQuery: {
      criteria: [{ tags: [{ key: 'account_id', value: 'acct-1' }] }],
    },
  },
  events: [
    {
      eventId: '018f2d5e-0003-7000-8000-000000000003',
      eventType: 'MoneyDebited',
      data: { account_id: 'acct-1', amount: 40, balance: 60 },
    },
  ],
});
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
client.saveEvents(Eventstore.SaveEventsRequest.newBuilder()
    .setBoundary("accounts")
    .setQuery(Eventstore.SaveQuery.newBuilder()
        .setExpectedPosition(Eventstore.Position.newBuilder()
            .setCommitPosition(2).setPreparePosition(1).build())
        .setSubsetQuery(Eventstore.Query.newBuilder()
            .addCriteria(Eventstore.Criterion.newBuilder()
                .addTags(Eventstore.Tag.newBuilder()
                    .setKey("account_id").setValue("acct-1").build())
                .build())
            .build())
        .build())
    .addEvents(Eventstore.EventToSave.newBuilder()
        .setEventId("018f2d5e-0003-7000-8000-000000000003")
        .setEventType("MoneyDebited")
        .setData("{\"account_id\":\"acct-1\",\"amount\":40,\"balance\":60}")
        .build())
    .build());
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

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
      "event_id": "018f2d5e-0003-7000-8000-000000000003",
      "event_type": "MoneyDebited",
      "data": "{\"account_id\":\"acct-1\",\"amount\":40,\"balance\":60}",
      "metadata": "{}"
    }
  ]
}
EOF
```

  </TabItem>
</Tabs>

Set `expected_position` to the `context_position` observed in step 3. The save commits only if no newer `acct-1` event exists.

## 5. Handle the conflict

If a second debit was decided against the same balance, one of the two saves loses the race. Detect it and retry:

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
var conflict *orisun.OptimisticConcurrencyException
if errors.As(err, &conflict) {
	// Concurrency signal. Re-run step 3, re-decide, retry the save.
}
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
try {
  await client.saveEvents({ /* debit request */ });
} catch (error) {
  if (error.message.includes('AlreadyExists')) {
    // Concurrency signal. Re-run step 3, re-decide, retry the save.
  } else {
    throw error;
  }
}
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
try {
    client.saveEvents(debitRequest);
} catch (OptimisticConcurrencyException conflict) {
    // Concurrency signal. Re-run step 3, re-decide, retry the save.
}
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

```text
ERROR:
  Code: AlreadyExists
```

  </TabItem>
</Tabs>

This is a concurrency signal. The losing command should:

1. Re-run the latest-by-criteria read in step 3.
2. Read the carried balance from the new latest event.
3. Re-check the invariant. The debit may no longer be valid.
4. Retry the save with the new `expected_position`.

Reusing the same `event_id` on retry keeps the command idempotent at the application boundary. See [Idempotency & Retry](./patterns/idempotency-and-retry) for the full pattern, including the retry loop and consumer-side deduplication.

## 6. Index the query field

Steps 3 and 4 both filter on `account_id`. Without an index, each query scans the boundary table. Create a btree index once, during deployment:

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
_, err := client.CreateIndex(ctx, &eventstore.CreateIndexRequest{
	Boundary: "accounts",
	Name:     "account_id",
	Fields: []*eventstore.IndexField{{
		JsonKey:   "account_id",
		ValueType: eventstore.ValueType_TEXT,
	}},
})
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
await client.createIndex({
  boundary: 'accounts',
  name: 'account_id',
  fields: [{ jsonKey: 'account_id', valueType: 'TEXT' }],
});
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
client.createIndex(Eventstore.CreateIndexRequest.newBuilder()
    .setBoundary("accounts")
    .setName("account_id")
    .addFields(Eventstore.IndexField.newBuilder()
        .setJsonKey("account_id")
        .setValueType(Eventstore.ValueType.TEXT)
        .build())
    .build());
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

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

  </TabItem>
</Tabs>

Both the CCC consistency check and read queries can now use the index. See [Indexing](./concepts/indexing) for composite and partial index examples.

## 7. Project to a read model

A balance read model stays current by subscribing. Catch-up replay reads stored history first, then switches to live JetStream delivery:

<Tabs groupId="client-lang">
  <TabItem value="go" label="Go" default>

```go
handler := orisun.NewSimpleEventHandler().
	WithOnEvent(func(event *eventstore.Event) error {
		// apply the event, persist side effects, then checkpoint event.Position
		return nil
	}).
	WithOnError(func(err error) {
		log.Printf("subscription stopped: %v", err)
	})

sub, err := client.SubscribeToEvents(ctx, &eventstore.CatchUpSubscribeToEventStoreRequest{
	Boundary:       "accounts",
	SubscriberName: "balance-projector",
	AfterPosition:  &eventstore.Position{CommitPosition: -1, PreparePosition: -1},
}, handler)
if err != nil {
	return err
}
defer sub.Close()
```

  </TabItem>
  <TabItem value="node" label="Node.js">

```typescript
const subscription = client.subscribeToEvents(
  {
    subscriberName: 'balance-projector',
    boundary: 'accounts',
    afterPosition: { commitPosition: -1, preparePosition: -1 },
  },
  (event) => {
    // apply the event, persist side effects, then checkpoint event.position
  },
  (error) => console.error('subscription error:', error),
);

// subscription.cancel() to stop
```

  </TabItem>
  <TabItem value="java" label="Java">

```java
EventSubscription sub = client.subscribeToEvents(
    Eventstore.CatchUpSubscribeToEventStoreRequest.newBuilder()
        .setBoundary("accounts")
        .setSubscriberName("balance-projector")
        .setAfterPosition(Eventstore.Position.newBuilder()
            .setCommitPosition(-1).setPreparePosition(-1).build())
        .build(),
    new EventSubscription.EventHandler() {
        public void onEvent(Eventstore.Event event) { /* apply + checkpoint */ }
        public void onError(Throwable error) { error.printStackTrace(); }
        public void onCompleted() {}
    });

// sub.close() to stop
```

  </TabItem>
  <TabItem value="grpcurl" label="grpcurl">

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/CatchUpSubscribeToEvents <<EOF
{
  "subscriber_name": "balance-projector",
  "boundary": "accounts",
  "after_position": {"commit_position": -1, "prepare_position": -1}
}
EOF
```

  </TabItem>
</Tabs>

The stream emits each event with its `position` and `date_created`. The projector applies events in order and persists its own checkpoint after each side effect is durable, so a restart resumes from the last processed position. Delivery is at least once, so consumers should deduplicate by `event_id`. See [Delivery Guarantees](./concepts/delivery-guarantees).

## What you built

- A consistency boundary scoped by event content, not a fixed stream — [Command Context Consistency](./concepts/command-context-consistency).
- An optimistic write that rejects stale context with `ALREADY_EXISTS`.
- An index that keeps the context query and read model fast.
- A live, recoverable projection over the same event log.
