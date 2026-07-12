# SQLite Backend-Local Group-Commit Writes Plan

This is an implementation plan, not user documentation.

**Status: implemented (2026-07-09).** Code in `sqlite/group_commit.go` +
`sqlite/sqlite_eventstore.go`; tests in `sqlite/group_commit_test.go`;
benchmarks `BenchmarkSqlite_GroupCommitVsDirect*`. The `synchronous=FULL`
default shipped with it (`sqlite/pool.go`).

The design target is SQLite write throughput: coalesce concurrent calls to
`sqlite.SqliteSaveEvents.Save` for a boundary into one SQLite transaction per
flush, while keeping the existing per-request CCC check inside that transaction.

The change should be contained inside the `sqlite` package. Nothing above it or
beside it should change: no `orisun.EventsSaver` interface changes, no
server-layer wrapper, no PostgreSQL changes, no shared batching abstraction, and
no config-package plumbing.

## Objective

Amortize SQLite's per-request write overhead across concurrent writers:

- one `BEGIN IMMEDIATE ... COMMIT` instead of one transaction per request,
- one WAL fsync per flush instead of one per request,
- one write-pool checkout per flush,
- one event-publisher signal per flush,
- less goroutine scheduling around the single SQLite writer.

### Durability: switch the default to `synchronous=FULL`

Today's default is `synchronous=NORMAL` (`pool.go`), under which WAL commits
do not fsync — only checkpoints do. A power loss or OS crash can therefore
lose commits that were already acknowledged to callers. That is wrong for an
event store whose contract is "ack after commit means persisted", and it is
weaker than the PostgreSQL backend (default `synchronous_commit=on`, fsync per
commit).

Group commit is exactly what makes `FULL` affordable: the per-commit fsync is
amortized across every request in the flush, which is the classic group-commit
motivation. This plan therefore includes flipping the default to `FULL` as
part of the rollout:

- default `synchronous=FULL`; `ORISUN_SQLITE_SYNCHRONOUS=NORMAL` remains an
  explicit opt-out for users who accept the power-loss window in exchange for
  throughput,
- benchmarks below must run under `FULL`, since that is the configuration the
  batching is meant to pay for — `NORMAL` numbers flatter the direct path,
- note the default change in operational docs (it is a behavior change for
  existing SQLite deployments).

Target write path:

```text
existing caller
  -> SqliteSaveEvents.Save(ctx, events, boundary, expectedPosition, subset)
  -> backend-local per-boundary queue
  -> sqlite worker drains queued requests
  -> ONE SQLite transaction:
       for each request, in queue order:
         SAVEPOINT
         existing CCC check + id allocation + insert
         on conflict/error: ROLLBACK TO SAVEPOINT, mark request error
       COMMIT
  -> notify publisher once if anything committed
  -> deliver one result per request
```

## Why SQLite Only

SQLite is the natural fit:

- SQLite already serializes writes. The repository's write pool is size 1, so
  concurrent writers are already queued in Go before reaching SQLite.
- Small writes pay transaction overhead repeatedly. Group commit turns N
  `BEGIN IMMEDIATE ... COMMIT` cycles into one.
- The implementation can reuse the existing `Save` internals with a savepoint
  loop on the single write connection.
- A rolled-back savepoint also rolls back `orisun_es_seq` updates, so rejected
  requests do not create SQLite position gaps.
- Boundaries map to separate SQLite database files, so batching should stay
  per-boundary.

Non-SQLite backends are explicitly out of scope. Do not touch them for this
change.

## Required Invariants

1. **Unchanged CCC semantics**: every request runs the same consistency check,
   against the same SQLite query semantics, inside the SQLite transaction.
2. **Queue order is authoritative within a flush**: if two batched requests
   conflict, the earlier one wins and the later one gets `ALREADY_EXISTS`.
3. **Per-request results**: each request gets its own position result or its
   own error; results are never merged.
4. **Rejected requests do not poison the flush**: every request runs under a
   savepoint.
5. **Atomic accepted prefix**: all accepted requests in a flush commit together.
   If commit fails, every provisionally accepted request reports an error.
6. **Ack after commit only**: a caller gets success only after the transaction
   containing its events commits.
7. **Publisher ordering preserved**: global IDs are allocated in queue order,
   and the existing polling publisher sees committed rows in `(transaction_id,
   global_id)` order.
8. **Bounded queueing**: the per-boundary queue is bounded; a full queue makes
   callers wait under their own contexts, never grows unbounded.

## Architecture

```text
SqliteSaveEvents (still implements the existing orisun.EventsSaver interface)
  Save(ctx, events, boundary, expectedPosition, subset)
    validate basic request shape
    enqueue request to that boundary's worker
    wait for this request's result or ctx cancellation

  per-boundary worker goroutine:
    take first request
    greedily drain more up to package-local batch limits
    one SQLite transaction; savepoint per request when the batch has more
    than one (a batch of one skips the savepoint — the transaction itself
    is the rollback boundary)
    deliver per-request results
```

Post-implementation revision: the flush path is the only production write
path. The original single-request "direct fast path" was removed by decision
(2026-07-09) — the pre-group-commit per-request transaction code survives
only as a test/benchmark comparison baseline (`saveBypassingQueue`).

### Flush Context

A flush spans many caller contexts, so it must not run under any one of them —
one caller's cancellation would poison the whole batch. The worker owns the
flush context: `context.WithTimeout(context.Background(),
sqliteGroupCommitFlushTimeout)`, used for the `pool.Write.Take` and every
statement in the flush. Caller contexts are consulted only at enqueue time
(waiting for queue space), at flush-assembly time (already-cancelled items are
dropped), and while the caller waits for its result.

### Result Delivery

Each queued request carries its own result channel, buffered with capacity 1.
The worker sends results non-blocking-safe: a caller that abandoned its `Save`
call after inclusion in a flush (context cancelled mid-flight) must never
stall the worker. The caller side selects on its result channel and
`ctx.Done()`.

### Worker Lifecycle

`NewSqliteSaveEvents` keeps its existing signature, so lifecycle is internal:

- workers are started lazily per boundary on first `Save` (or eagerly in the
  constructor — implementer's choice, but ownership stays inside
  `SqliteSaveEvents`),
- `SqliteSaveEvents` gains an unexported `close()` hooked into
  `InitializeSqliteDatabase`'s existing `ctx.Done()` shutdown goroutine,
  invoked before `closeAll(pools)` so workers never flush against a closed
  pool,
- on close, workers stop accepting new requests and fail queued-but-unflushed
  requests with an unavailability error; an in-progress flush is allowed to
  finish (it holds the write connection already),
- tests that construct `SqliteSaveEvents` directly must be able to trigger the
  same shutdown to avoid goroutine leaks; the unexported `close()` (reachable
  from package tests) covers this.

If a worker's `pool.Write.Take` fails because the pool is closed, the worker
fails the batch with that error and exits cleanly rather than spinning.

Scope decisions:

- **Per-boundary only.** SQLite uses one database file per boundary; there is no
  cross-boundary transaction target.
- **In-process only.** No durable command queue is needed because success is
  acknowledged only after commit. A crash before commit is equivalent to the
  request never completing.
- **Backend-local only.** Do not add `BatchEventsSaver`, do not wrap savers in
  `orisun` or `server`, and do not change PostgreSQL.
- **Existing public surface only.** `NewSqliteSaveEvents` still returns
  `*SqliteSaveEvents`, and that type still satisfies the existing
  `orisun.EventsSaver` interface.

## Package-Local Tuning

Keep first-cut tuning inside `sqlite`, as unexported constants or unexported
fields initialized by `NewSqliteSaveEvents`:

```go
const (
	sqliteGroupCommitMaxBatchRequests = 128
	sqliteGroupCommitMaxBatchEvents   = 1024
	sqliteGroupCommitMaxDelay         = 0
	sqliteGroupCommitMaxPending       = 4096
	sqliteGroupCommitFlushTimeout     = 30 * time.Second
)
```

`sqliteGroupCommitMaxDelay = 0` means opportunistic batching only: the worker
never waits just to fill a batch. Under low concurrency, batches are simply
size one (no savepoint, same flush code path).

Do not add new environment variables or config structs in the first cut. If
benchmarking later proves runtime tuning is necessary, add it deliberately as a
separate change.

Post-implementation revision (2026-07-09): runtime tuning was added as that
separate change — `sqlite.groupCommit` in config.yaml (`ORISUN_SQLITE_GC_*`
env vars) feeds `NewSqliteSaveEventsWithConfig`; zero values fall back to the
package constants, negatives are rejected at startup. Delay-sweep benchmark
(`BenchmarkSqlite_GroupCommitDelay`): `maxDelay > 0` lost at every measured
concurrency for closed-loop callers (10ms: 87 events/sec at 1 worker, 7.3k at
100, vs 10k/47.5k at 0s) — batches already fill opportunistically, the wait
only adds latency.

## Batching Policy

The worker flushes when any limit is hit:

- request count (`sqliteGroupCommitMaxBatchRequests`) bounds savepoint-loop
  length,
- event count (`sqliteGroupCommitMaxBatchEvents`) bounds transaction size,
- delay (`sqliteGroupCommitMaxDelay`, if nonzero) bounds added latency,
- queue empty in opportunistic mode.

Requests whose caller context is already cancelled at flush time are dropped
from the batch and answered with the context error.

## SQLite Flush Plan

The flush path lives inside `SqliteSaveEvents`; it does not need a public
`SaveBatch` method.

1. Validate non-empty requests and known boundary.
2. Normalize every item's events up front. Per-item normalization failures are
   recorded for that item and are not fatal to the batch.
3. Take the boundary's single write connection under the flush context.
4. `BEGIN IMMEDIATE` once.
5. For each valid item in queue order:
   - open a savepoint,
   - run the existing CCC check logic,
   - allocate IDs with `orisun_es_seq`,
   - insert rows with `insertEventBatch`,
   - release the savepoint on success and record `(transaction_id, global_id)`,
   - roll back the savepoint on conflict or per-item error and record that
     item's error.
6. `COMMIT` once.
7. If commit succeeds and at least one item was accepted, call
   `notifier.Notify(boundary)` once — after the commit, matching today's
   direct-path ordering (notify runs after `endFn` in `Save`'s defer).
8. If commit fails, every provisionally accepted item reports the commit error.

The existing `Save` method should be refactored into shared helpers rather than
duplicating CCC check, ID allocation, and insert logic.

### Connection Hygiene

The direct path gets rollback-on-error for free from
`sqlitex.ImmediateTransaction`'s end function. The manual
`BEGIN IMMEDIATE ... COMMIT` flush path must guarantee the connection returns
to the pool with no open transaction:

- if `COMMIT` fails, issue an explicit `ROLLBACK` before `pool.Write.Put`,
- a deferred recovery path rolls back any open transaction before the
  connection is returned, covering panics mid-flush,
- if rollback itself fails, do not return the connection to the pool in a
  dirty state — surface the error and let the pool replace the connection.

Without this, the next pool user (admin writes, publisher checkpoints)
inherits an open transaction.

## Failure Modes

- **Process crash before commit**: transaction rolls back; callers see errors
  or timeouts. This matches a crash during today's direct write.
- **Commit fails**: all provisionally accepted items report the commit error;
  nothing is persisted.
- **One item conflicts**: that item gets `ALREADY_EXISTS`; unrelated accepted
  items still commit.
- **Caller timeout while queued/in-flight**: the batch may still commit if the
  item was already included. This is the same ambiguity as a direct-mode timeout
  mid-transaction.
- **Backend panic during flush**: recover in the worker; the batch's callers get
  `INTERNAL`; the connection is rolled back before returning to the pool (see
  Connection Hygiene); the worker keeps serving.
- **Shutdown / pool closed**: queued-but-unflushed requests fail with an
  unavailability error; an in-progress flush finishes; a failed
  `pool.Write.Take` on a closed pool fails the batch and stops the worker.

## Metrics / Logs

Follow-up metrics:

- batch size distribution by requests and events,
- flush duration and commit duration (long flushes stall other write-pool
  users: admin writes, publisher checkpoints, index DDL),
- in-batch conflict count,
- queue depth and wait time,
- single-item fast-path ratio.

Log per flush at debug: boundary, drained, accepted, rejected, duration.

## Test Plan

### Unit: SQLite Queue/Worker

- concurrent `SqliteSaveEvents.Save` calls coalesce into one flush,
- results route to the right callers,
- single queued request flushes as a batch of one (no savepoint),
- unknown boundary returns the existing SQLite unknown-boundary error,
- caller context cancelled while queued returns the context error,
- context cancellation after inclusion in a flush preserves the existing
  direct-write ambiguity,
- backend panic returns `INTERNAL` for the batch and the worker survives,
- batch limits are respected,
- shutdown fails queued-but-unflushed requests and leaves no worker goroutine
  running,
- a closed write pool fails the batch cleanly (worker exits, no spin).

### SQLite Backend

- independent batched requests all commit,
- positions are per-request, gap-free, and ascending,
- conflicting batched requests: earlier wins, later gets `ALREADY_EXISTS`,
- a rejected request does not affect unrelated requests in the same flush,
- commit failure persists nothing,
- results match direct mode for the same request sequence,
- notifier fires once per flush with at least one accepted item, and only
  after the commit,
- after a mixed accept/reject flush, `orisun_es_seq` equals the number of
  committed events (rejected savepoints leave no gaps),
- cancelled items skipped before flush do not allocate IDs.

### Performance

Compare direct SQLite writes vs SQLite group commit, under `synchronous=FULL`
(the new default; see Durability above). Run one `NORMAL` pass for reference:

- 1 client, to confirm low-concurrency overhead,
- 100 concurrent clients with independent contexts,
- 100 concurrent clients on the same hot context,
- mixed 90/10 independent/hot contexts,
- burst benchmark using the existing `BenchmarkSqlite_Burst10000` shape,
- `sqliteGroupCommitMaxDelay = 0` vs small delays such as 1-5ms.

## Rollout

1. Refactor SQLite `Save` internals into helpers usable by both direct and
   flush paths.
2. Add backend-local per-boundary workers inside `SqliteSaveEvents`, with the
   shutdown hook described under Worker Lifecycle.
3. Add the multi-request SQLite flush path with savepoints and the connection
   hygiene rules above.
4. Flip the default `synchronous` from `NORMAL` to `FULL` (one-line change in
   `normalizeSqlitePoolConfig`; `ORISUN_SQLITE_SYNCHRONOUS` already exists as
   the override).
5. Add focused SQLite tests.
6. Add benchmarks (under `FULL`) and tune package-local defaults.
7. Document the SQLite-only behavior and the durability default change in
   operational notes.
