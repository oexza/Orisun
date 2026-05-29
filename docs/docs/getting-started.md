---
id: getting-started
title: Getting Started
description: Choose a backend, run Orisun, and verify the gRPC API.
slug: /getting-started
---

Orisun is a batteries-included event store for systems that need durable event history, content-based consistency checks, and real-time delivery without running a separate broker.

You can run Orisun as a release binary, a Docker image, or an embedded Go package. Docker Compose is convenient for trying the full stack, but production deployments can run the binary directly under systemd, Nomad, Kubernetes, Fly, Render, or any process supervisor.

## Before you start

You need:

- an Orisun release binary or Docker
- `grpcurl` for the verification command
- a boundary name for your application events
- a separate boundary for Orisun admin state

The examples use:

| Boundary | Purpose |
| --- | --- |
| `orders` | application events |
| `orisun_admin` | users, credentials, and admin projections |

## Choose a backend

| Backend | Best for | Multi-node | Binary | Image |
| --- | --- | --- | --- | --- |
| SQLite | Embedded apps, edge services, development, single-node production | No | `orisun-sqlite` | `orexza/orisun:sqlite` |
| PostgreSQL | Clustered deployments, larger datasets, shared database platforms | Yes | `orisun-pg` | `orexza/orisun:pg` |

Both backends expose the same EventStore and Admin gRPC APIs.

### When to choose SQLite

Choose SQLite when one active Orisun node is enough and operational simplicity matters more than clustering. SQLite mode is complete: it stores the event log, admin state, index metadata, publisher checkpoints, projector checkpoints, and JSON criteria indexes.

### When to choose PostgreSQL

Choose PostgreSQL when you need multiple Orisun nodes, want database-level operational tooling, or expect the event log to grow beyond a single-node operational profile.

## Install a binary

Download a release asset for your OS, architecture, and backend from [GitHub Releases](https://github.com/oexza/Orisun/releases). Asset names follow this pattern:

| Binary | Includes |
| --- | --- |
| `orisun-<os>-<arch>` | all backends |
| `orisun-pg-<os>-<arch>` | PostgreSQL only |
| `orisun-sqlite-<os>-<arch>` | SQLite only |

For example, on Linux amd64:

```bash
VERSION=x.y.z

curl -L \
  "https://github.com/oexza/Orisun/releases/download/v${VERSION}/orisun-sqlite-linux-amd64" \
  -o orisun-sqlite

chmod +x ./orisun-sqlite
```

You can also build locally:

```bash
./build.sh linux amd64 dev pg
./build/orisun-pg-linux-amd64

./build.sh linux amd64 dev sqlite
./build/orisun-sqlite-linux-amd64
```

## Run SQLite from a binary

SQLite is the fastest way to start a local or single-node Orisun server without a separate database.

```bash
mkdir -p ./data/orisun/sqlite ./data/orisun/nats

ORISUN_BACKEND=sqlite \
ORISUN_SQLITE_DIR=./data/orisun/sqlite \
ORISUN_NATS_STORE_DIR=./data/orisun/nats \
ORISUN_NATS_CLUSTER_ENABLED=false \
ORISUN_BOUNDARIES='[{"name":"orders"},{"name":"orisun_admin"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
./orisun-sqlite
```

## Run PostgreSQL from a binary

PostgreSQL mode expects an existing PostgreSQL database. Create the database and schemas with your normal database tooling, then start Orisun with connection settings:

```bash
ORISUN_BACKEND=postgres \
ORISUN_PG_HOST=localhost \
ORISUN_PG_PORT=5432 \
ORISUN_PG_USER=postgres \
ORISUN_PG_PASSWORD='password@1' \
ORISUN_PG_NAME=orisun \
ORISUN_PG_SCHEMAS=orders:public,orisun_admin:admin \
ORISUN_BOUNDARIES='[{"name":"orders"},{"name":"orisun_admin"}]' \
ORISUN_ADMIN_BOUNDARY=orisun_admin \
ORISUN_ADMIN_USERNAME=admin \
ORISUN_ADMIN_PASSWORD=changeit \
ORISUN_NATS_STORE_DIR=./data/orisun/nats \
./orisun-pg
```

## Run SQLite with Docker

Create `docker-compose.yml`:

```yaml
services:
  orisun:
    image: orexza/orisun:sqlite
    environment:
      ORISUN_BACKEND: sqlite
      ORISUN_SQLITE_DIR: /var/lib/orisun/sqlite
      ORISUN_NATS_CLUSTER_ENABLED: "false"
      ORISUN_BOUNDARIES: '[{"name":"orders"},{"name":"orisun_admin"}]'
      ORISUN_ADMIN_BOUNDARY: orisun_admin
      ORISUN_ADMIN_USERNAME: admin
      ORISUN_ADMIN_PASSWORD: changeit
    ports:
      - "5005:5005"
      - "8991:8991"
    volumes:
      - orisun-data:/var/lib/orisun
    restart: unless-stopped

volumes:
  orisun-data:
```

Start the server:

```bash
docker compose up -d
```

## Run PostgreSQL with Docker Compose

Create `docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:17.5-alpine3.22
    environment:
      POSTGRES_DB: orisun
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: password@1
    ports:
      - "5434:5432"
    volumes:
      - postgres-data:/var/lib/postgresql/data

  orisun:
    image: orexza/orisun:pg
    environment:
      ORISUN_PG_HOST: postgres
      ORISUN_PG_PORT: 5432
      ORISUN_PG_USER: postgres
      ORISUN_PG_PASSWORD: password@1
      ORISUN_PG_NAME: orisun
      ORISUN_PG_SCHEMAS: orders:public,orisun_admin:admin
      ORISUN_BOUNDARIES: '[{"name":"orders"},{"name":"orisun_admin"}]'
      ORISUN_ADMIN_BOUNDARY: orisun_admin
      ORISUN_ADMIN_USERNAME: admin
      ORISUN_ADMIN_PASSWORD: changeit
    ports:
      - "5005:5005"
      - "8991:8991"
    volumes:
      - orisun-data:/var/lib/orisun/data
    depends_on:
      - postgres
    restart: unless-stopped

volumes:
  postgres-data:
  orisun-data:
```

Start the stack:

```bash
docker compose up -d
```

## Verify the API

The examples use the default `admin:changeit` credentials.

```bash
AUTH='Authorization: Basic YWRtaW46Y2hhbmdlaXQ='
grpcurl -H "$AUTH" localhost:5005 list
```

Expected services include:

```text
orisun.Admin
orisun.EventStore
```

:::warning
Change `ORISUN_ADMIN_PASSWORD` before production use.
:::

## Save your first event

```bash
grpcurl -H "$AUTH" -d @ localhost:5005 orisun.EventStore/SaveEvents <<EOF
{
  "boundary": "orders",
  "query": {
    "expected_position": {
      "commit_position": -1,
      "prepare_position": -1
    }
  },
  "events": [
    {
      "event_id": "order-001",
      "event_type": "OrderPlaced",
      "data": "{\"customer_id\":\"c-1\",\"amount\":45}",
      "metadata": "{\"source\":\"checkout\"}"
    }
  ]
}
EOF
```

The response contains the committed log position:

```json
{
  "log_position": {
    "commit_position": 1,
    "prepare_position": 0
  }
}
```

## Release artifacts

Orisun publishes both release binaries and Docker images.

Binary assets are attached to each GitHub release:

| Asset | Backend |
| --- | --- |
| `orisun-linux-amd64`, `orisun-darwin-arm64`, ... | All backends |
| `orisun-pg-linux-amd64`, `orisun-pg-darwin-arm64`, ... | PostgreSQL only |
| `orisun-sqlite-linux-amd64`, `orisun-sqlite-darwin-arm64`, ... | SQLite only |

Docker images use one repository with backend flavor tags:

| Tag | Backend |
| --- | --- |
| `orexza/orisun:latest` | All backends |
| `orexza/orisun:pg` | PostgreSQL only |
| `orexza/orisun:sqlite` | SQLite only |
| `orexza/orisun:<version>` | All backends for a release |
| `orexza/orisun:<version>-pg` | PostgreSQL-only release |
| `orexza/orisun:<version>-sqlite` | SQLite-only release |

## Next steps

- Learn the consistency model in [Command Context Consistency](./concepts/command-context-consistency).
- Read and subscribe with the [EventStore API](./api/eventstore).
- Review production settings in [Configuration](./operations/configuration).
