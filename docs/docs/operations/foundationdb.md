---
title: FoundationDB Operations
description: Production checklist and runbook for Orisun on FoundationDB.
---

FoundationDB mode is the clustered backend for high-write deployments that need parallel commits without PostgreSQL position locking. Build it with `-tags foundationdb` or use the `orisun-fdb` release binary.

## Required Runtime

Every Orisun node running the FoundationDB backend needs:

- `libfdb_c` installed on the host or baked into the image.
- A readable `fdb.cluster` file.
- The same `ORISUN_FDB_ROOT` across all nodes that share one Orisun deployment.
- The same `ORISUN_BOUNDARIES` and `ORISUN_ADMIN_BOUNDARY` as the rest of the cluster.

```bash
ORISUN_BACKEND=foundationdb
ORISUN_FDB_CLUSTER_FILE=/etc/foundationdb/fdb.cluster
ORISUN_FDB_ROOT=orisun
ORISUN_FDB_TRANSACTION_TIMEOUT_MS=10000
```

The Go binding API defaults to `730`, matching FoundationDB 7.3.x. Keep the installed client library compatible with the server major version. Release FDB binaries are Linux-only and still dynamically link the client library.

## Cluster File Handling

Treat `fdb.cluster` as live configuration. FoundationDB changes coordinator addresses during some maintenance operations, and clients need the updated file.

- On VMs or bare metal, distribute the cluster file with your config system and restart Orisun after coordinator changes.
- On Kubernetes, mount the operator-managed cluster-file ConfigMap into every Orisun pod.
- Do not bake environment-specific cluster files into immutable application images.

## Publisher Ownership

The FDB backend stores publisher locks in FoundationDB, not in NATS. Each lock acquisition has a unique token and a renewable lease. The publisher checks ownership before publishing to JetStream and before writing the checkpoint.

Expected behavior:

- Exactly one Orisun node owns a boundary publisher at a time.
- If the owner exits, pauses past the lease, or loses the lease to another node, protected publisher work is canceled.
- A stale owner cannot release a newer owner's lock token.
- A replacement publisher resumes from the durable FDB checkpoint.

The release gate for this behavior is the FDB lock failover test plus the gRPC ledger workload:

```bash
scripts/fdb_test_container.sh -run TestFDBLockExpiredOwnerFailoverFencesOldLease -v
TEST_PKGS=./cmd/ scripts/fdb_test_container.sh -run TestE2E_LedgerWorkload_FoundationDB -v
```

Run the extended soak before a production rollout:

```bash
ORISUN_FDB_SOAK=1 TEST_PKGS=./cmd/ \
  scripts/fdb_test_container.sh -run TestE2E_LedgerWorkload_FoundationDBSoak -v
```

## Indexes And Query Shape

FoundationDB criteria reads and consistency checks require ready covering indexes. Unindexed criteria fail with `FAILED_PRECONDITION` instead of scanning a whole boundary.

Before using a criterion in production traffic:

1. Create the boundary index through the admin API.
2. Wait for the index to report ready.
3. Deploy writers that use that criterion for CCC.

This keeps conflict ranges narrow: commands that touch different indexed subsets can commit concurrently.

## Transaction Limits

FoundationDB has a hard 10 MB transaction limit. Orisun rejects oversized `SaveEvents` batches before commit using an estimate that includes payloads and matching index entries, and maps any final FoundationDB `transaction_too_large` error to `INVALID_ARGUMENT`.

Operational guidance:

- Keep bulk imports chunked well below 9 MB per `SaveEvents` request.
- Remember that every matching index increases transaction size.
- Keep `ORISUN_GRPC_MAX_RECEIVE_MESSAGE_SIZE` above your expected request size, but do not use the gRPC cap as the storage transaction budget.

## Backup And Restore

Use FoundationDB-native backups, not filesystem snapshots of Orisun nodes.

- Run `fdbbackup` agents continuously to durable object storage.
- Practice point-in-time restore into a separate FoundationDB cluster.
- Restore Orisun by pointing nodes at the restored cluster file and the same `ORISUN_FDB_ROOT`.

NATS is not the durable source of truth. After restore or restart, subscribers and publishers catch up from FoundationDB.

## Monitoring

Export `fdbcli status json` or an equivalent FoundationDB exporter. Alert on:

- commit latency and proxy CPU saturation,
- transaction log queue growth,
- storage lag and data movement backlog,
- transaction conflict rate,
- unavailable or degraded cluster status,
- Orisun publish/checkpoint errors and publisher lock churn.

High conflict rate on a small set of criteria usually means application commands are contending on the same consistency context. That is expected for hot aggregates, but it should show up as retry pressure rather than invariant drift.

## Release Checklist

Before marking a FoundationDB-backed Orisun release production-ready:

- `go test ./...` or the project CI equivalent is green.
- `go test -tags foundationdb ./foundationdb` is green with local client libraries.
- `scripts/fdb_test_container.sh -run TestFoundationDB -v` is green.
- `TEST_PKGS=./cmd/ scripts/fdb_test_container.sh -run TestE2E_LedgerWorkload_FoundationDB -v` is green.
- The extended soak command above has passed for the target release candidate.
- Release workflow publishes the `orisun-fdb-linux-amd64` and `orisun-fdb-linux-arm64` artifacts.
- Operators have documented backup, restore, cluster-file update, and client-library upgrade procedures.
