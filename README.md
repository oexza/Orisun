# Orisun

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/oexza/Orisun/actions/workflows/ci.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/ci.yml)
[![Release](https://github.com/oexza/Orisun/actions/workflows/release.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/release.yml)
[![Latest Release](https://img.shields.io/github/v/release/oexza/Orisun?label=release)](https://github.com/oexza/Orisun/releases/latest)

Orisun is a batteries-included event store for systems that need durable event history, content-based consistency checks, and real-time delivery without running a separate broker.

**Full documentation:** [oexza.github.io/Orisun](https://oexza.github.io/Orisun/)

**Start here:** [choose the right docs path](https://oexza.github.io/Orisun/docs/start-here)

## What It Provides

- Transactional event storage on PostgreSQL, YugabyteDB, or SQLite.
- Command Context Consistency: save only if the queried event context has not changed.
- Server-side latest-by-criteria reads for carried-state command contexts.
- Embedded NATS JetStream for catch-up and live subscriptions, with optional external NATS via `ORISUN_NATS_URL`.
- Durable publisher checkpoints so committed events are not skipped.
- Sequential publishing per boundary in ascending event-log position.
- gRPC APIs, generated clients, Docker images, embedding packages, auth, TLS, telemetry, pprof, and index management.

## Quick Start

Download a release binary from [GitHub Releases](https://github.com/oexza/Orisun/releases), or build one locally:

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
  orexza/orisun:sqlite
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

See the [getting started guide](https://oexza.github.io/Orisun/docs/getting-started) for binary and Docker setup for all supported backends.

## Artifacts

| Artifact | Location |
| --- | --- |
| Documentation | [GitHub Pages](https://oexza.github.io/Orisun/) |
| Start here | [Choose the right docs path](https://oexza.github.io/Orisun/docs/start-here) |
| Setup guide | [SQLite, PostgreSQL, and YugabyteDB setup](https://oexza.github.io/Orisun/docs/getting-started) |
| API guide | [EventStore and Admin API](https://oexza.github.io/Orisun/docs/api/eventstore) |
| Releases | [github.com/oexza/Orisun/releases](https://github.com/oexza/Orisun/releases) |
| Docker images | [Docker Hub](https://hub.docker.com/r/orexza/orisun), [GHCR](https://github.com/oexza/Orisun/pkgs/container/orisun) |
| Go client | [orisun-client-go](https://github.com/oexza/orisun-client-go) |
| Node.js client | [orisun-node-client](https://github.com/oexza/orisun-node-client) |
| Java client | [orisun-client-java](https://github.com/oexza/orisun-client-java) |

Release binaries are attached to each GitHub release:

| Asset pattern | Backend |
| --- | --- |
| `orisun-<os>-<arch>` | All backends |
| `orisun-pg-<os>-<arch>` | PostgreSQL-compatible backends: PostgreSQL and YugabyteDB |
| `orisun-sqlite-<os>-<arch>` | SQLite only |
| `orisun-fdb-<os>-<arch>` | FoundationDB only, built with `-tags foundationdb` |

Docker tags are published to Docker Hub (`orexza/orisun`) and GitHub Container Registry (`ghcr.io/oexza/orisun`) with the same flavor tags:

| Tag | Backend |
| --- | --- |
| `orexza/orisun:latest` | All backends |
| `orexza/orisun:pg` | PostgreSQL-compatible backends: PostgreSQL and YugabyteDB |
| `orexza/orisun:sqlite` | SQLite only |
| `orexza/orisun:<version>` | All backends for a release |
| `orexza/orisun:<version>-pg` | PostgreSQL-compatible release |
| `orexza/orisun:<version>-sqlite` | SQLite-only release |

Use the same suffixes with `ghcr.io/oexza/orisun`, for example `ghcr.io/oexza/orisun:sqlite`.

## Development

```bash
go test ./...
go build ./...
task build
task build:pg
task build:sqlite
go build -tags foundationdb ./cmd/orisun-fdb
```

Release:

```bash
./scripts/release.sh 1.2.3 --notes release-notes.md
```

## License

[MIT](LICENSE)
