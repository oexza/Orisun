---
title: Configuration
description: Environment variables for Orisun binaries, containers, and embedded deployments.
---

Orisun reads environment variables with the `ORISUN_` prefix.

Configuration is shared across release binaries, Docker images, and embedded deployments that call `config.InitializeConfig()`. If you run the binary directly, set these variables in your shell, process supervisor, service manager, or platform secret store. The compiled defaults come from [`config/config.yaml`](https://github.com/oexza/Orisun/blob/main/config/config.yaml).

## Minimum Required Settings

| Variable | Description |
| --- | --- |
| `ORISUN_BACKEND` | `postgres` or `sqlite`; defaults to `postgres`. |
| `ORISUN_BOUNDARIES` | JSON array of boundary definitions. |
| `ORISUN_ADMIN_BOUNDARY` | Boundary used for admin state. |
| `ORISUN_ADMIN_PASSWORD` | Bootstrap admin password. The default is for local development only. |

For PostgreSQL, also set:

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_PG_HOST` | `localhost` | PostgreSQL host. |
| `ORISUN_PG_PORT` | `5434` | PostgreSQL port. |
| `ORISUN_PG_USER` | `postgres` | PostgreSQL user. |
| `ORISUN_PG_PASSWORD` | `postgres` | PostgreSQL password. |
| `ORISUN_PG_NAME` | `orisun` | PostgreSQL database. |
| `ORISUN_PG_SCHEMAS` | `orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin` | Comma-separated `boundary:schema` mappings. |
| `ORISUN_PG_SSLMODE` | `disable` | PostgreSQL SSL mode passed to the driver. |

For SQLite, set:

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_SQLITE_DIR` | `./data/orisun/sqlite` | Directory for per-boundary SQLite database files. |
| `ORISUN_NATS_CLUSTER_ENABLED` | `false` | Must stay `false` for SQLite. |

## Boundary configuration

`ORISUN_BOUNDARIES` is a JSON array:

```bash
ORISUN_BOUNDARIES='[{"name":"orders","description":"orders"},{"name":"orisun_admin","description":"admin"}]'
```

For PostgreSQL, every boundary should also appear in `ORISUN_PG_SCHEMAS`:

```bash
ORISUN_PG_SCHEMAS=orders:public,orisun_admin:admin
```

Boundary names must be valid PostgreSQL identifiers even when using SQLite: 1-63 characters, starting with a letter or underscore, then letters, digits, or underscores. This keeps boundary names portable across backends.

## Server settings

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_GRPC_PORT` | `5005` | gRPC API port. |
| `ORISUN_GRPC_ENABLE_REFLECTION` | `true` | Enable gRPC reflection for tools such as `grpcurl`. |
| `ORISUN_GRPC_CONNECTION_TIMEOUT` | `60s` | Server connection timeout. |
| `ORISUN_GRPC_KEEP_ALIVE_TIME` | `30s` | Server keepalive ping interval. |
| `ORISUN_GRPC_KEEP_ALIVE_TIMEOUT` | `5s` | Keepalive ping timeout. |
| `ORISUN_GRPC_MAX_CONCURRENT_STREAMS` | `10000` | Maximum concurrent HTTP/2 streams per connection. |
| `ORISUN_GRPC_MAX_RECEIVE_MESSAGE_SIZE` | `67108864` | Maximum inbound gRPC message size, 64 MB by default. |
| `ORISUN_GRPC_MAX_SEND_MESSAGE_SIZE` | `67108864` | Maximum outbound gRPC message size, 64 MB by default. |
| `ORISUN_GRPC_INITIAL_WINDOW_SIZE` | `1048576` | HTTP/2 stream flow-control window, 1 MB by default. |
| `ORISUN_GRPC_INITIAL_CONN_WINDOW_SIZE` | `1048576` | HTTP/2 connection flow-control window, 1 MB by default. |
| `ORISUN_GRPC_WRITE_BUFFER_SIZE` | `65536` | gRPC write buffer size. |
| `ORISUN_GRPC_READ_BUFFER_SIZE` | `65536` | gRPC read buffer size. |
| `ORISUN_GRPC_KEEPALIVE_MIN_TIME` | `5s` | Minimum client keepalive interval accepted by the server. |
| `ORISUN_GRPC_KEEPALIVE_PERMIT_WITHOUT_STREAM` | `true` | Allow client keepalive pings without active streams. |
| `ORISUN_ADMIN_PORT` | `8991` | Admin HTTP port. |
| `ORISUN_ADMIN_USERNAME` | `admin` | Bootstrap admin username. |
| `ORISUN_ADMIN_PASSWORD` | `changeit` | Bootstrap admin password. |
| `ORISUN_LOGGING_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR`. |
| `ORISUN_POLLING_PUBLISHER_BATCH_SIZE` | `1000` | Max events drained per publisher read batch. |

## SQLite settings

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_SQLITE_DIR` | `./data/orisun/sqlite` | Directory containing one `{boundary}.db` file per boundary. |
| `ORISUN_SQLITE_SYNCHRONOUS` | `NORMAL` | SQLite synchronous mode. Use stronger durability only after measuring the write cost. |
| `ORISUN_SQLITE_BUSY_TIMEOUT_MS` | `5000` | Busy timeout for contended SQLite operations. |
| `ORISUN_SQLITE_READ_POOL_SIZE` | `0` | Read pool size. `0` lets Orisun choose a CPU-based default. |
| `ORISUN_SQLITE_CACHE_SIZE` | `0` | SQLite cache size override. |
| `ORISUN_SQLITE_MMAP_SIZE` | `0` | SQLite mmap size override. |
| `ORISUN_SQLITE_WAL_AUTO_CHECKPOINT` | `0` | SQLite WAL auto-checkpoint override. |
| `ORISUN_SQLITE_TEMP_STORE` | `MEMORY` | SQLite temp-store mode. |

## PostgreSQL pool settings

Orisun uses separate PostgreSQL pools for writes, reads, and admin work. Size their combined open connections below PostgreSQL `max_connections` or the PgBouncer pool size.

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_PG_LISTEN_ENABLED` | `true` | Use PostgreSQL `LISTEN/NOTIFY` wake-ups. Polling still protects correctness when notifications are delayed or missed. |
| `ORISUN_PG_WRITE_MAX_OPEN_CONNS` | `25` | Write pool open-connection cap. |
| `ORISUN_PG_WRITE_MAX_IDLE_CONNS` | `10` | Write pool idle-connection cap. |
| `ORISUN_PG_WRITE_CONN_MAX_IDLE_TIME` | `5m` | Write pool idle lifetime. |
| `ORISUN_PG_WRITE_CONN_MAX_LIFETIME` | `30m` | Write pool max connection lifetime. |
| `ORISUN_PG_READ_MAX_OPEN_CONNS` | `50` | Read pool open-connection cap. |
| `ORISUN_PG_READ_MAX_IDLE_CONNS` | `25` | Read pool idle-connection cap. |
| `ORISUN_PG_READ_CONN_MAX_IDLE_TIME` | `5m` | Read pool idle lifetime. |
| `ORISUN_PG_READ_CONN_MAX_LIFETIME` | `30m` | Read pool max connection lifetime. |
| `ORISUN_PG_ADMIN_MAX_OPEN_CONNS` | `5` | Admin pool open-connection cap. |
| `ORISUN_PG_ADMIN_MAX_IDLE_CONNS` | `2` | Admin pool idle-connection cap. |
| `ORISUN_PG_ADMIN_CONN_MAX_IDLE_TIME` | `5m` | Admin pool idle lifetime. |
| `ORISUN_PG_ADMIN_CONN_MAX_LIFETIME` | `30m` | Admin pool max connection lifetime. |

## NATS settings

| Variable | Default | Description |
| --- | --- | --- |
| `ORISUN_NATS_URL` | empty | External JetStream-enabled NATS URL. When set, Orisun connects to it instead of starting embedded NATS. |
| `ORISUN_NATS_SERVER_NAME` | `orisun-nats-2` | Embedded NATS server name. Use a unique value per clustered node. |
| `ORISUN_NATS_PORT` | `4224` | Embedded NATS client port. |
| `ORISUN_NATS_MAX_PAYLOAD` | `1048576` | NATS max payload. |
| `ORISUN_NATS_STORE_DIR` | `./data/orisun/nats` | NATS data directory. |
| `ORISUN_NATS_EVENT_STREAM_MAX_BYTES` | `536870912` | Per-boundary event stream memory cap. |
| `ORISUN_NATS_EVENT_STREAM_MAX_MSGS` | `-1` | Per-boundary event stream message cap. |
| `ORISUN_NATS_EVENT_STREAM_MAX_AGE` | `5m` | Retention overlap for catch-up subscribers. |
| `ORISUN_NATS_PUBLISH_ASYNC_MAX_PENDING` | `8192` | In-flight async publish acknowledgements. |

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
| `ORISUN_OTEL_ENABLED` | `true` | Enable OpenTelemetry tracing. |
| `ORISUN_OTEL_ENDPOINT` | `localhost:4317` | OTLP gRPC endpoint. |
| `ORISUN_OTEL_SERVICE_NAME` | `orisun` | Service name attached to exported traces. |
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
| `ORISUN_NATS_CLUSTER_TIMEOUT` | `1800s` |

Clustered Orisun deployments require the PostgreSQL backend. SQLite startup fails when `ORISUN_NATS_CLUSTER_ENABLED=true`.
