<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/static/img/logo-dark.svg">
    <img src="docs/static/img/logo.svg" alt="Orisun" width="96" height="96">
  </picture>
</p>

# Orisun

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/OrisunLabs/Orisun/actions/workflows/ci.yml/badge.svg)](https://github.com/OrisunLabs/Orisun/actions/workflows/ci.yml)
[![Release](https://github.com/OrisunLabs/Orisun/actions/workflows/release.yml/badge.svg)](https://github.com/OrisunLabs/Orisun/actions/workflows/release.yml)
[![Latest Release](https://img.shields.io/github/v/release/OrisunLabs/Orisun?label=release)](https://github.com/OrisunLabs/Orisun/releases/latest)

Orisun is an open-source event database for decisions that must stay correct as facts change.

State-based databases show what is true now, but not how it became true. Orisun preserves the complete event history. When a command, whether it is a service, a workflow, or an AI agent, declares the events its decision depends on, Orisun commits its new events only if that context is still current. Committed events are then published sequentially within each boundary through catch-up plus live JetStream delivery.

Orisun is an MIT-licensed server with backend-specific binaries and images. Run SQLite on a laptop or a single node, then use PostgreSQL, YugabyteDB, or FoundationDB as the deployment grows. The gRPC API stays the same, with storage, ordered publishing, auth, and index management built into the server.

The mechanism behind this is **Command Context Consistency (CCC)**: commands declare the event subset they depend on with content-based queries, and writes succeed only if that subset is unchanged. Orisun also supports **Dynamic Consistency Boundary (DCB)** style append conditions over event types and queryable JSON tags.

**Full documentation:** [orisunlabs.github.io/Orisun](https://orisunlabs.github.io/Orisun/)

**Start here:** [What is Orisun?](https://orisunlabs.github.io/Orisun/docs/)

**Current release:** [`v0.6.1`](https://github.com/OrisunLabs/Orisun/releases/tag/v0.6.1)

## What It Provides

- Transactional event storage on PostgreSQL, YugabyteDB, SQLite, or FoundationDB beta.
- Context-correct command writes: save only if the queried event context has not changed.
- Dynamic Consistency Boundary style append conditions over event types and queryable JSON tags.
- Server-side latest-by-criteria reads for carried-state command contexts.
- Embedded NATS JetStream for catch-up and live subscriptions, with optional external NATS via `ORISUN_NATS_URL`.
- Durable publisher checkpoints so committed events are not skipped.
- Sequential publishing per boundary in ascending event-log position.
- gRPC APIs, generated clients, Docker images, embedding packages, auth, TLS, telemetry, pprof, and index management.
- NATS-free embedded SQLite runtime and a `gomobile` binding surface for Android and iOS apps.

## Quick Start

Download a release binary from [GitHub Releases](https://github.com/OrisunLabs/Orisun/releases), or build one locally:

```bash
./build.sh linux amd64 dev sqlite
```

SQLite single-node with a binary:

```bash
mkdir -p ./data/orisun/sqlite ./data/orisun/nats

ORISUN_BACKEND=sqlite \
ORISUN_SQLITE_DIR=./data/orisun/sqlite \
ORISUN_NATS_STORE_DIR=./data/orisun/nats \
ORISUN_NATS_CLUSTER_ENABLED=false \
ORISUN_BOUNDARIES='[{"name":"orders"},{"name":"orisun_admin"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
./build/orisun-sqlite-linux-amd64
```

For SQLite production durability, use `ORISUN_SQLITE_SYNCHRONOUS=FULL`
(recommended). `NORMAL` is a throughput opt-out that can lose already
acknowledged commits after an OS crash or power loss before checkpointing.
SQLite stores event logs in one `{boundary}.db` file per boundary and stores
publisher checkpoints, projector checkpoints, admin users, and count caches in
one `{boundary}_metadata.db` file per boundary under `ORISUN_SQLITE_DIR`.

For an on-device store with no server, network listener, or NATS runtime, use
the [embedded mobile package](embedded/sqlite/mobile/README.md). It provides
Android/iOS bindings for local saves, CCC reads, indexes, and notifier-driven
subscriptions directly over SQLite.

Build the platform libraries on macOS with Xcode and the Android SDK/NDK:

```bash
go tool gomobile init
task build:mobile
```

This creates `build/mobile/orisun-mobile.aar` and
`build/mobile/OrisunMobile.xcframework.zip`. Release builds are stripped and
path-trimmed. The default Android AAR contains ARM64 devices and x86-64
emulators; set `ORISUN_MOBILE_ANDROID_TARGETS=android` for all four gomobile
ABIs. Artifact sizes include multiple architectures and are larger than the
native slice delivered to one device. See the
[mobile build and integration guide](embedded/sqlite/mobile/README.md) for the
runtime differences, size controls, and platform examples.

The same server can also run from Docker:

```bash
docker run --rm \
  -p 5005:5005 \
  -p 8991:8991 \
  -e ORISUN_BACKEND=sqlite \
  -e ORISUN_SQLITE_DIR=/var/lib/orisun/sqlite \
  -e ORISUN_NATS_CLUSTER_ENABLED=false \
  -e ORISUN_BOUNDARIES='[{"name":"orders"},{"name":"orisun_admin"}]' \
  -e ORISUN_ADMIN_BOUNDARY=orisun_admin \
  -v orisun-data:/var/lib/orisun \
  orisunlabs/orisun:0.6.1-sqlite
```

PostgreSQL with a binary:

```bash
./build.sh linux amd64 dev pg

ORISUN_BACKEND=postgres \
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD='password@1' \
ORISUN_PG_NAME=orisun \
ORISUN_PG_SCHEMAS=orders:public,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orders"},{"name":"orisun_admin"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
./build/orisun-pg-linux-amd64
```

YugabyteDB uses the PostgreSQL-compatible binary with `ORISUN_PG_DIALECT=yugabyte`.
Use YugabyteDB `v2025.2.3+` and enable `LISTEN/NOTIFY` on Masters and TServers
with `ysql_yb_enable_listen_notify=true`.

See the [getting started guide](https://orisunlabs.github.io/Orisun/docs/getting-started) for binary and Docker setup for all supported backends.

## Artifacts

| Artifact | Location |
| --- | --- |
| Documentation | [GitHub Pages](https://orisunlabs.github.io/Orisun/) |
| Start here | [What is Orisun?](https://orisunlabs.github.io/Orisun/docs/) |
| Setup guide | [SQLite, PostgreSQL, and YugabyteDB setup](https://orisunlabs.github.io/Orisun/docs/getting-started) |
| API guide | [EventStore and Admin API](https://orisunlabs.github.io/Orisun/docs/api/eventstore) |
| Releases | [github.com/OrisunLabs/Orisun/releases](https://github.com/OrisunLabs/Orisun/releases) |
| Docker images | [Docker Hub](https://hub.docker.com/repository/docker/orisunlabs/orisun), [GHCR](https://github.com/OrisunLabs/Orisun/pkgs/container/orisun) |
| Go client | [orisun-client-go](https://github.com/OrisunLabs/orisun-client-go) |
| Node.js client | [orisun-node-client](https://github.com/OrisunLabs/orisun-node-client) |
| Java client | [orisun-client-java](https://github.com/OrisunLabs/orisun-client-java) |
| Embedded Android/iOS SQLite | [Mobile binding package](embedded/sqlite/mobile/README.md) |

Release binaries are attached to each GitHub release:

| Asset pattern | Backend |
| --- | --- |
| `orisun-pg-<os>-<arch>` | PostgreSQL-compatible backends: PostgreSQL and YugabyteDB |
| `orisun-sqlite-<os>-<arch>` | SQLite only |
| `orisun-fdb-<os>-<arch>` | FoundationDB only, built with `-tags foundationdb` (beta) |

Docker tags are published to Docker Hub (`orisunlabs/orisun`) and GitHub Container Registry (`ghcr.io/orisunlabs/orisun`) with the same flavor tags:

| Tag | Backend |
| --- | --- |
| `orisunlabs/orisun:pg` | PostgreSQL-compatible backends: PostgreSQL and YugabyteDB |
| `orisunlabs/orisun:sqlite` | SQLite only |
| `orisunlabs/orisun:fdb` | FoundationDB only, beta, includes the FDB client library |
| `orisunlabs/orisun:<version>-pg` | PostgreSQL-compatible release |
| `orisunlabs/orisun:<version>-sqlite` | SQLite-only release |
| `orisunlabs/orisun:<version>-fdb` | FoundationDB-only release |

Use the same suffixes with `ghcr.io/orisunlabs/orisun`, for example `ghcr.io/orisunlabs/orisun:0.6.1-fdb`.

## Development

```bash
go test ./...
go build ./...
task build
task build:pg
task build:sqlite
task build:mobile
go build -tags foundationdb ./cmd/orisun-fdb
```

FoundationDB support is beta. It is covered by backend, failover, and ledger workload tests, but storage layout or operational behavior may still change in a future release if production feedback exposes a better contract.

Release:

```bash
./scripts/release.sh 1.2.3 --notes release-notes.md
```

## License

[MIT](LICENSE)
