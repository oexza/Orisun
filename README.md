# Orisun

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![CI](https://github.com/oexza/Orisun/actions/workflows/ci.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/ci.yml)
[![Release](https://github.com/oexza/Orisun/actions/workflows/release.yml/badge.svg)](https://github.com/oexza/Orisun/actions/workflows/release.yml)

Orisun is a batteries-included event store for systems that need durable event history, content-based consistency checks, and real-time delivery without running a separate broker.

**Full documentation:** [oexza.github.io/Orisun](https://oexza.github.io/Orisun/)

## What It Provides

- Transactional event storage on PostgreSQL or SQLite.
- Command Context Consistency: save only if the queried event context has not changed.
- Embedded NATS JetStream for catch-up and live subscriptions.
- Durable publisher checkpoints so committed events are not skipped.
- Sequential publishing per boundary in ascending event-log position.
- gRPC APIs, generated clients, Docker images, embedding packages, auth, TLS, telemetry, pprof, and index management.

## Quick Start

SQLite single-node:

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

PostgreSQL:

```bash
docker compose up -d
```

See the [getting started guide](https://oexza.github.io/Orisun/docs/getting-started) for complete Docker Compose files for both backends.

## Artifacts

| Artifact | Location |
| --- | --- |
| Documentation | [GitHub Pages](https://oexza.github.io/Orisun/) |
| Setup guide | [SQLite and PostgreSQL setup](https://oexza.github.io/Orisun/docs/getting-started) |
| API guide | [EventStore and Admin API](https://oexza.github.io/Orisun/docs/api/eventstore) |
| Releases | [github.com/oexza/Orisun/releases](https://github.com/oexza/Orisun/releases) |
| Docker images | [orexza/orisun](https://hub.docker.com/r/orexza/orisun) |
| Go client | [orisun-client-go](https://github.com/oexza/orisun-client-go) |
| Node.js client | [orisun-node-client](https://github.com/oexza/orisun-node-client) |
| Java client | [orisun-client-java](https://github.com/oexza/orisun-client-java) |

Docker tags use one repository with flavor tags:

| Tag | Backend |
| --- | --- |
| `orexza/orisun:latest` | All backends |
| `orexza/orisun:pg` | PostgreSQL only |
| `orexza/orisun:sqlite` | SQLite only |
| `orexza/orisun:<version>` | All backends for a release |
| `orexza/orisun:<version>-pg` | PostgreSQL-only release |
| `orexza/orisun:<version>-sqlite` | SQLite-only release |

## Development

```bash
go test ./...
go build ./...
task build
task build:pg
task build:sqlite
```

Release:

```bash
./scripts/release.sh 1.2.3 --notes release-notes.md
```

## License

[MIT](LICENSE)
