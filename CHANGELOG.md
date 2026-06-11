# Changelog

## 0.4.0 - 2026-06-11

### Added

- `EventStore/GetLatestByCriteria` RPC: returns the latest event per criterion assembled from one consistent server-side read snapshot, plus a `context_position` for use as the next `expected_position`. Closes the mixed-snapshot gap where a context assembled from independent `GetEvents` calls can miss an event that committed between them below the observed max position. Implemented for PostgreSQL (single-statement `UNION ALL` of `LIMIT 1` lookups) and SQLite (one deferred read transaction).
- General-ledger workload e2e test (`TestE2E_LedgerWorkload_*`) driving concurrent double-entry transfers with carried balances through the public gRPC API, auditing carried-balance consistency, no-overdraft, money conservation, and debit/credit pairing.
- FoundationDB storage backend (`ORISUN_BACKEND=foundationdb`, built with `-tags foundationdb`). Event positions come from FoundationDB commit versionstamps, so plain appends and commands on different aggregates commit in parallel — no per-boundary sequence counter on the write path. Criteria reads and consistency conditions both require a ready covering index and fail closed (`FAILED_PRECONDITION`) otherwise; conflict ranges are scoped to the matching index slice. System indexes covering Orisun's own admin queries are created on the admin boundary at startup.
- `ORISUN_FDB_TRANSACTION_TIMEOUT_MS` (default `10000`) and `ORISUN_FDB_TRANSACTION_RETRY_LIMIT` (default unlimited) bound FoundationDB transactions.
- `scripts/fdb_test_container.sh` runs the FoundationDB backend tests against a throwaway single-node Docker cluster.

### Fixed

- PostgreSQL positions are now assigned in commit order per boundary: `insert_events_with_consistency_v3` takes a per-boundary advisory lock from position draw until commit. Previously a concurrent batch could draw lower `global_id`s, stay in flight while a later-drawn batch committed, and then commit below a context max another writer had already observed — invisible to a scalar expected-position check. The lock closes that window; the cost is that writes within one boundary serialise from draw to commit (the SQLite and FoundationDB backends already had commit-ordered positions). The function is replaced automatically at startup; no data migration.

## 0.3.1 - 2026-06-06

### Fixed

- Preserved pre-`0.3.0` read and subscription cursor semantics for PostgreSQL-backed stores. Clients can continue using `{0, 0}` as the beginning cursor for `GetEvents` and `CatchUpSubscribeToEvents`.
- PostgreSQL logical `commit_position` values are now derived as `MAX(global_id) + 1` for each committed batch, so no event is emitted at the exact `{0, 0}` position while existing zero-based `prepare_position` values remain unchanged.
- PostgreSQL startup migration now remaps legacy publisher and projector checkpoints to the corrected logical commit positions without changing stored `global_id` / `prepare_position` values.

## 0.3.0 - 2026-06-06

### Breaking Changes

- PostgreSQL event positions no longer expose PostgreSQL internal transaction IDs as `commit_position`.
  Existing PostgreSQL event rows are migrated on startup so `transaction_id` becomes a durable Orisun logical commit position derived from event `global_id` ordering. PostgreSQL's `pg_current_xact_id()` is still stored internally in `pg_xact_id` for stable-prefix visibility checks, but it is no longer part of the public EventStore position.
- Existing PostgreSQL projector checkpoints and publisher checkpoints are remapped to the new logical positions during startup migration. Consumers that persisted Orisun positions outside the Orisun database should rebuild or remap those external checkpoints before resuming from them.

### Migration Notes

- Stop Orisun cleanly before upgrading so publisher and projector checkpoints are current.
- Back up PostgreSQL before first starting Orisun `0.3.0`.
- Start one Orisun `0.3.0` node first and allow migrations to complete for all configured boundaries before starting the rest of a cluster.
- PostgreSQL major upgrades no longer need to preserve PostgreSQL transaction IDs for Orisun correctness after this migration. `pg_xact_id` is treated as current-cluster-only metadata and stale values are cleared when detected.

### Fixed

- PostgreSQL subscriptions and publishers no longer depend on PostgreSQL transaction IDs remaining monotonic across major upgrades, dump/restore migrations, logical replication moves, or fresh cluster restores.
- Missing projector checkpoints now resume from the before-first position instead of `(0, 0)`, preventing first logical event skips.
- Added Postgres live-subscription E2E coverage for write-after-subscribe delivery.
