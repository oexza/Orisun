# AGENTS.md

Guidance for coding agents working in this repository.

## Project Overview

Orisun is a Go event store implementing Command Context Consistency (CCC). It exposes gRPC APIs, uses embedded NATS JetStream for real-time event publishing, and supports pluggable storage backends:

- `postgres/`: PostgreSQL backend for production and clustered deployments.
- `sqlite/`: embedded single-node backend.
- `orisun/`: core event store types, client/server-facing logic, streams, and generated protobuf bindings.
- `nats/`: embedded NATS/JetStream integration.
- `admin/`: admin service, auth, telemetry, and projections.
- `config/`: environment-driven configuration.
- `cmd/`: server entry point, integration tests, TLS tests, and benchmarks.
- `clients/node/`: TypeScript Node client and generated JS/TS protobuf bindings.
- `proto/`: protobuf source files.

CCC is content-query based: commands define their consistency context by querying event data, then writes re-check the context before saving. Do not introduce stream/aggregate assumptions unless the task explicitly calls for that.

## Common Commands

Use Go 1.26.3 or the version declared in `go.mod`.

```bash
go test -v ./...
go build -v ./...
go fmt ./...
go vet ./...
go run cmd/main.go
```

Taskfile helpers:

```bash
task build
task live
task debug
```

Project scripts:

```bash
./build.sh
./build.sh linux amd64
./collect_benchmarks.sh
```

Node client commands from `clients/node/`:

```bash
npm run build
npm test
npm run generate-proto
```

## Testing Notes

- Run focused tests for the package you modify, then broaden to `go test -v ./...` when feasible.
- PostgreSQL integration tests use testcontainers and require Docker availability.
- Cluster and TLS tests live under `cmd/` and can be run directly, for example:

```bash
go test -v ./cmd/e2e_integration_test.go
go test -v ./cmd/e2e_cluster_test.go
go test -v ./cmd/tls_test.go
go test -v ./postgres/postgres_eventstore_test.go
```

- Benchmark files are in `cmd/` and `postgres/`; do not treat benchmark output files as source of truth unless the task is explicitly about performance results.

## Architecture Conventions

- Storage backend selection is controlled by `ORISUN_BACKEND`; `postgres` is the default.
- Boundaries are logical domains. PostgreSQL maps boundaries to schemas using `ORISUN_PG_SCHEMAS`; SQLite maps each boundary to a database file.
- Requests to unconfigured boundaries should be rejected early.
- Consistency violations should use the gRPC `ALREADY_EXISTS` status.
- Writes must respect expected positions for optimistic concurrency.
- Content queries filter JSON event data. PostgreSQL production performance depends on explicit user-managed indexes via the admin API; do not add automatic broad GIN indexes casually.
- Server code uses `github.com/goccy/go-json`, commonly aliased as `json`. Keep `encoding/json` out of server code unless there is a specific reason.
- NATS JetStream is embedded. PostgreSQL remains the durable source of truth; NATS is the real-time delivery layer.
- `LISTEN/NOTIFY` is only a publisher wake-up. No-miss behavior depends on the PostgreSQL checkpoint and stable-prefix reads, so preserve the ASC visibility barrier in `get_matching_events_v3`.
- Per-boundary publishing must remain sequential by `(transaction_id, global_id)`. The publisher should reject non-advancing batches before publishing any partial prefix.
- Delivery is at-least-once around NATS publish plus checkpoint write. Consumers may see duplicates, but the publisher must never skip events or publish later events ahead of earlier events.
- SQLite is single-node only and must remain incompatible with NATS clustering.

## Generated Code

Generated protobuf files are present in both Go and Node client code:

- Go: `orisun/*.pb.go`
- Node: `clients/node/src/generated/*`

When changing `proto/*.proto`, regenerate the relevant bindings and include generated changes. Keep manual edits out of generated files unless the task explicitly requires an emergency patch.

## Admin And Config

- Admin defaults are documented in `README.md` and `CLAUDE.md`; avoid changing default credentials, ports, or environment names without updating docs and tests.
- Configuration is environment-based with the `ORISUN_` prefix.
- `config/config.yaml` is a local/default config reference, not a replacement for environment handling.

## Editing Guidelines

- Prefer small, package-local changes that match existing patterns.
- Preserve current public API behavior unless the task is explicitly a breaking change.
- Keep generated artifacts, benchmark outputs, logs, `tmp/`, `data/`, and build outputs out of unrelated changes.
- Use `go fmt` on Go files after edits.
- If a change touches storage or publisher semantics, add or update tests for expected-position behavior, boundary handling, restart/checkpoint behavior, and per-boundary ordering where practical.
- If a change touches gRPC/protobuf contracts, update docs or client bindings as appropriate.
