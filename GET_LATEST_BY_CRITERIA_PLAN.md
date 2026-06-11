# GetLatestByCriteria Plan

## Why We Are Doing This

The general-ledger workload needs to make decisions from the latest state of multiple accounts. The current safe implementation reads `account_id = A OR account_id = B` in one query, walks the result in descending position order, and stops after it has the latest event for each account.

That is correct because the command context comes from one backend read snapshot. It avoids the mixed-snapshot bug where account A is read before a concurrent transfer and account B is read after it, producing a decision context that never existed at one point in time.

However, the current approach can over-fetch. If one account has a deep history, the combined `OR` query may return many rows before the test finds the latest event for every requested account. The command does not need history replay; each event carries the resulting balance, so the command only needs the latest event per account plus a consistency token for the full context.

The goal is to add an API primitive that returns the latest event for each criterion from one consistent backend read. This preserves CCC correctness while avoiding large context reads.

## Correctness Rule

The server, not the client, must assemble the context from one read snapshot.

The response must include:

- the latest event matching each requested criterion,
- a `context_position`, which is the max position across the events observed in that same read snapshot.

The client should pass `context_position` as `SaveEvents.query.expected_position` with the same combined `subsetQuery`.

Computing a max position from multiple independent client queries is not equivalent. It can hide missed events that committed between those queries.

## API Shape

Add an EventStore RPC:

```proto
message GetLatestByCriteriaRequest {
  string boundary = 1;
  repeated Criterion criteria = 2;
}

message LatestCriterionResult {
  Criterion criterion = 1;
  Event event = 2;
}

message GetLatestByCriteriaResponse {
  repeated LatestCriterionResult results = 1;
  Position context_position = 2;
}

service EventStore {
  rpc GetLatestByCriteria(GetLatestByCriteriaRequest) returns (GetLatestByCriteriaResponse) {}
}
```

Semantics:

- Each criterion is an AND of its tags, matching existing `Query.criteria` semantics.
- The request criteria are OR'd for the purpose of the returned `context_position`.
- If a criterion has no matching event, its result is returned with no `event`.
- If no criteria match anything, `context_position` is the not-exists sentinel `(-1, -1)`.
- Empty `boundary` or empty `criteria` is `INVALID_ARGUMENT`.

## Implementation Plan

1. Add protobuf messages and RPC.
   - Update `proto/eventstore.proto`.
   - Regenerate Go protobuf and gRPC bindings.
   - Regenerate Node bindings only if the repo's Node generation path is available and expected for proto changes.

2. Extend backend interfaces.
   - Add `GetLatestByCriteria(ctx, req)` to the retrieval interface or introduce a narrower optional interface if keeping old retrievers isolated is cleaner.
   - Add the EventStore server method and authorization behavior matching `GetEvents`.

3. PostgreSQL implementation.
   - Execute inside one read-only transaction.
   - For each criterion, run an indexed descending lookup with `LIMIT 1`.
   - Use the existing `get_matching_events_v3` path where practical with `Direction_DESC` and `count = 1`, or a direct query if that keeps the code simpler and preserves criteria semantics.
   - Compute `context_position` as the max returned position from the same transaction.

4. SQLite implementation.
   - Take one read connection and run all criterion lookups within one transaction/snapshot.
   - Reuse `buildCriteriaSQLForBoundary` so criteria and index expression behavior stay consistent with `GetEvents` and CCC.
   - Query each criterion with `ORDER BY transaction_id DESC, global_id DESC LIMIT 1`.
   - Compute `context_position` from those rows.

5. FoundationDB follow-up.
   - Do not block the main-branch implementation on FDB.
   - On the FDB branch, implement the same API by opening one FDB read transaction and doing a reverse `Limit: 1` read per required index slice.
   - Keep mandatory-index behavior: missing covering index should be `FAILED_PRECONDITION`.

6. Port the GL test.
   - Move the shared ledger workload to main.
   - Replace the large combined `GetEvents` read with `GetLatestByCriteria([account_id=A, account_id=B])`.
   - Continue saving with a combined subset query for `A OR B`.
   - Use the response `context_position` as the expected position.
   - Keep the final full replay audit; that audit is validation, not command-path balance computation.

## Tests

Add or update tests for:

- SQLite `GetLatestByCriteria` returns the latest event per criterion.
- PostgreSQL `GetLatestByCriteria` returns the latest event per criterion.
- Missing criterion results are represented without an event and do not break `context_position`.
- `context_position` is the max position from the same response.
- GL workload passes on SQLite and PostgreSQL using `GetLatestByCriteria`.

Run:

```bash
go test ./sqlite ./postgres ./orisun
go test ./cmd -run TestE2E_Ledger -count=1 -v
go test ./cmd -run '^$'
```

Broaden to:

```bash
go test ./...
```

when practical.

## Non-Goals

- Do not merge the FDB backend into main as part of this change.
- Do not introduce long-lived client-visible snapshot tokens.
- Do not change existing `GetEvents` semantics.
- Do not remove the final ledger audit replay.

## Risks And Checks

- Proto changes can require generated code updates in multiple clients.
- PostgreSQL and SQLite criteria rendering must stay consistent with existing `GetEvents` and CCC behavior.
- The response must expose the server-computed `context_position`; clients should not infer it from independent calls.
- The GL test should still intentionally exercise optimistic conflicts under concurrency.
