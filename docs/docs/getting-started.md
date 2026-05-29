---
id: getting-started
title: Getting Started
description: Choose a backend, run Orisun, and verify the gRPC API.
slug: /getting-started
---

Orisun is a batteries-included event store for systems that need durable event history, content-based consistency checks, and real-time delivery without running a separate broker.

The quickest path is Docker Compose: choose SQLite for a single-node deployment, or PostgreSQL when you want the clustered backend and database-managed storage from the start.

## Before you start

You need:

- Docker and Docker Compose
- `grpcurl` for the verification command
- a boundary name for your application events
- a separate boundary for Orisun admin state

The examples use:

| Boundary | Purpose |
| --- | --- |
| `orders` | application events |
| `orisun_admin` | users, credentials, and admin projections |

## Choose a backend

| Backend | Best for | Multi-node | Image |
| --- | --- | --- | --- |
| SQLite | Embedded apps, edge services, development, single-node production | No | `orexza/orisun:sqlite` |
| PostgreSQL | Clustered deployments, larger datasets, shared database platforms | Yes | `orexza/orisun:pg` |

Both backends expose the same EventStore and Admin gRPC APIs.

### When to choose SQLite

Choose SQLite when one active Orisun node is enough and operational simplicity matters more than clustering. SQLite mode is complete: it stores the event log, admin state, index metadata, publisher checkpoints, projector checkpoints, and JSON criteria indexes.

### When to choose PostgreSQL

Choose PostgreSQL when you need multiple Orisun nodes, want database-level operational tooling, or expect the event log to grow beyond a single-node operational profile.

## Start with SQLite

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

## Start with PostgreSQL

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

Docker images use one repository with backend flavor tags:

| Tag | Backend |
| --- | --- |
| `orexza/orisun:latest` | All backends |
| `orexza/orisun:pg` | PostgreSQL only |
| `orexza/orisun:sqlite` | SQLite only |
| `orexza/orisun:<version>` | All backends for a release |
| `orexza/orisun:<version>-pg` | PostgreSQL-only release |
| `orexza/orisun:<version>-sqlite` | SQLite-only release |

Backend-specific binaries can also be built locally:

```bash
./build.sh linux amd64 dev pg
./build/orisun-pg-linux-amd64

./build.sh linux amd64 dev sqlite
./build/orisun-sqlite-linux-amd64
```

## Next steps

- Learn the consistency model in [Command Context Consistency](./concepts/command-context-consistency).
- Read and subscribe with the [EventStore API](./api/eventstore).
- Review production settings in [Configuration](./operations/configuration).
