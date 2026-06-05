---
title: Configuration
description: Environment variables for Orisun binaries, containers, and embedded deployments.
---

Orisun reads environment variables with the `ORISUN_` prefix.

Configuration is shared across release binaries, Docker images, and embedded deployments that call `config.InitializeConfig()`. If you run the binary directly, set these variables in your shell, process supervisor, service manager, or platform secret store.

## Required settings

| Variable | Description |
| --- | --- |
| `ORISUN_BACKEND` | `postgres` or `sqlite`; defaults to `postgres`. |
| `ORISUN_BOUNDARIES` | JSON array of boundary definitions. |
| `ORISUN_ADMIN_BOUNDARY` | Boundary used for admin state. |

For PostgreSQL, also set:

| Variable | Description |
| --- | --- |
| `ORISUN_PG_HOST` | PostgreSQL host. |
| `ORISUN_PG_PORT` | PostgreSQL port. |
| `ORISUN_PG_USER` | PostgreSQL user. |
| `ORISUN_PG_PASSWORD` | PostgreSQL password. |
| `ORISUN_PG_NAME` | PostgreSQL database. |
| `ORISUN_PG_SCHEMAS` | Comma-separated `boundary:schema` mappings. |

For SQLite, set:

| Variable | Description |
| --- | --- |
| `ORISUN_SQLITE_DIR` | Directory for per-boundary SQLite database files. |
| `ORISUN_NATS_CLUSTER_ENABLED` | Must be `false` for SQLite. |

## Boundary configuration

`ORISUN_BOUNDARIES` is a JSON array:

```bash
ORISUN_BOUNDARIES='[{"name":"orders","description":"orders"},{"name":"orisun_admin","description":"admin"}]'
```

For PostgreSQL, every boundary should also appear in `ORISUN_PG_SCHEMAS`:

```bash
ORISUN_PG_SCHEMAS=orders:public,orisun_admin:admin
```

## Common settings

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_GRPC_PORT` | `5005` | gRPC API port. |
| `ORISUN_ADMIN_PORT` | `8991` | Admin HTTP port. |
| `ORISUN_ADMIN_USERNAME` | `admin` | Bootstrap admin username. |
| `ORISUN_ADMIN_PASSWORD` | `changeit` | Bootstrap admin password. |
| `ORISUN_LOGGING_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR`. |
| `ORISUN_PG_LISTEN_ENABLED` | `true` | Use PostgreSQL `LISTEN/NOTIFY` wake-ups. |
| `ORISUN_POLLING_PUBLISHER_BATCH_SIZE` | `1000` | Max events drained per publisher read batch. |

## PostgreSQL pool settings

| Variable | Default |
| --- | --- |
| `ORISUN_PG_WRITE_MAX_OPEN_CONNS` | `25` |
| `ORISUN_PG_WRITE_MAX_IDLE_CONNS` | `10` |
| `ORISUN_PG_WRITE_CONN_MAX_IDLE_TIME` | `5m` |
| `ORISUN_PG_WRITE_CONN_MAX_LIFETIME` | `30m` |
| `ORISUN_PG_READ_MAX_OPEN_CONNS` | `50` |
| `ORISUN_PG_READ_MAX_IDLE_CONNS` | `25` |
| `ORISUN_PG_READ_CONN_MAX_IDLE_TIME` | `5m` |
| `ORISUN_PG_READ_CONN_MAX_LIFETIME` | `30m` |
| `ORISUN_PG_ADMIN_MAX_OPEN_CONNS` | `5` |
| `ORISUN_PG_ADMIN_MAX_IDLE_CONNS` | `2` |

## NATS settings

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_NATS_URL` | empty | External JetStream-enabled NATS URL. When set, Orisun connects to it instead of starting embedded NATS. |
| `ORISUN_NATS_PORT` | `4224` | Embedded NATS client port. |
| `ORISUN_NATS_STORE_DIR` | `./data/orisun/nats` | NATS data directory. |
| `ORISUN_NATS_EVENT_STREAM_MAX_BYTES` | `536870912` | Per-boundary event stream memory cap. |
| `ORISUN_NATS_EVENT_STREAM_MAX_MSGS` | `-1` | Per-boundary event stream message cap. |
| `ORISUN_NATS_EVENT_STREAM_MAX_AGE` | `5m` | Retention overlap for catch-up subscribers. |

## TLS settings

| Variable | Default |
| --- | --- |
| `ORISUN_GRPC_TLS_ENABLED` | `false` |
| `ORISUN_GRPC_TLS_CERT_FILE` | `/etc/orisun/tls/server.crt` |
| `ORISUN_GRPC_TLS_KEY_FILE` | `/etc/orisun/tls/server.key` |
| `ORISUN_GRPC_TLS_CA_FILE` | `/etc/orisun/tls/ca.crt` |
| `ORISUN_GRPC_TLS_CLIENT_AUTH_REQUIRED` | `false` |

## Telemetry and profiling

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_OTEL_ENABLED` | `true` | Enable OpenTelemetry. |
| `ORISUN_OTEL_ENDPOINT` | `localhost:4317` | OTLP gRPC endpoint. |
| `ORISUN_PPROF_ENABLED` | `false` | Enable pprof. |
| `ORISUN_PPROF_PORT` | `6060` | pprof port. |

## Cluster settings

| Variable | Default |
| --- | --- |
| `ORISUN_NATS_CLUSTER_ENABLED` | `false` |
| `ORISUN_NATS_CLUSTER_NAME` | `orisun-nats-cluster` |
| `ORISUN_NATS_CLUSTER_HOST` | `0.0.0.0` |
| `ORISUN_NATS_CLUSTER_PORT` | `6222` |
| `ORISUN_NATS_CLUSTER_ROUTES` | `nats://0.0.0.0:6223,nats://0.0.0.0:6224` |
| `ORISUN_NATS_CLUSTER_USERNAME` | `nats` |
| `ORISUN_NATS_CLUSTER_PASSWORD` | `password@1` |
