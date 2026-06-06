# Changelog

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
