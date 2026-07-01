#!/usr/bin/env bash
# Start a persistent single-node FoundationDB for local Orisun development.
#
# Usage:
#   scripts/fdb_local_docker.sh start
#   scripts/fdb_local_docker.sh status
#   scripts/fdb_local_docker.sh stop
set -euo pipefail

# 7.3.27 is used by the amd64 CI image, but it does not publish aarch64 Debian
# packages. 7.3.77 keeps the same 7.3 API family and works on Apple Silicon.
FDB_VERSION="${FDB_VERSION:-7.3.77}"
IMAGE_NAME="${IMAGE_NAME:-orisun-foundationdb-local:${FDB_VERSION}}"
CONTAINER_NAME="${CONTAINER_NAME:-orisun-fdb-local}"
DATA_VOLUME="${DATA_VOLUME:-orisun-fdb-data}"
LOG_VOLUME="${LOG_VOLUME:-orisun-fdb-logs}"
FDB_PORT="${FDB_PORT:-4500}"
CLUSTER_FILE="${CLUSTER_FILE:-build/fdb/fdb.cluster}"

usage() {
  echo "Usage: $0 {start|status|stop|restart|cluster-file|logs}" >&2
}

dockerfile() {
  cat <<'DOCKERFILE'
FROM debian:bookworm-slim

ARG FDB_VERSION=7.3.77

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl procps; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
      arm64) deb_arch=aarch64 ;; \
      amd64) deb_arch=amd64 ;; \
      *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    base="https://github.com/apple/foundationdb/releases/download/${FDB_VERSION}"; \
    curl -fsSL -o /tmp/foundationdb-clients.deb "${base}/foundationdb-clients_${FDB_VERSION}-1_${deb_arch}.deb"; \
    curl -fsSL -o /tmp/foundationdb-server.deb "${base}/foundationdb-server_${FDB_VERSION}-1_${deb_arch}.deb"; \
    dpkg -i /tmp/foundationdb-clients.deb; \
    dpkg --unpack /tmp/foundationdb-server.deb >/dev/null 2>&1 || true; \
    rm -f /var/lib/dpkg/info/foundationdb-server.postinst; \
    dpkg --configure foundationdb-server >/dev/null 2>&1 || true; \
    rm -rf /var/lib/apt/lists/* /tmp/foundationdb-*.deb

COPY run-fdb-local.sh /usr/local/bin/run-fdb-local
RUN chmod +x /usr/local/bin/run-fdb-local

EXPOSE 4500
CMD ["/usr/local/bin/run-fdb-local"]
DOCKERFILE
}

entrypoint() {
  cat <<'ENTRYPOINT'
#!/usr/bin/env bash
set -euo pipefail

FDB_PORT="${FDB_PORT:-4500}"
PUBLIC_ADDRESS="${PUBLIC_ADDRESS:-127.0.0.1:${FDB_PORT}}"
CLUSTER_FILE="/etc/foundationdb/fdb.cluster"

mkdir -p /var/fdb/data /var/fdb/logs /etc/foundationdb
echo "docker:docker@${PUBLIC_ADDRESS}" > "$CLUSTER_FILE"

/usr/sbin/fdbserver \
  --listen-address "0.0.0.0:${FDB_PORT}" \
  --public-address "$PUBLIC_ADDRESS" \
  --datadir /var/fdb/data \
  --logdir /var/fdb/logs \
  --locality-zoneid local \
  --locality-machineid local &
server_pid=$!

for _ in $(seq 1 60); do
  output="$(fdbcli -C "$CLUSTER_FILE" --exec "configure new single memory" 2>&1)" && break
  if echo "$output" | grep -q "Database already exists"; then
    break
  fi
  sleep 1
done

fdbcli -C "$CLUSTER_FILE" --exec "status minimal" || true
wait "$server_pid"
ENTRYPOINT
}

build_image() {
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' RETURN
  dockerfile > "$tmpdir/Dockerfile"
  entrypoint > "$tmpdir/run-fdb-local.sh"
  docker build \
    --build-arg "FDB_VERSION=${FDB_VERSION}" \
    -t "$IMAGE_NAME" \
    "$tmpdir"
}

write_cluster_file() {
  mkdir -p "$(dirname "$CLUSTER_FILE")"
  printf 'docker:docker@127.0.0.1:%s\n' "$FDB_PORT" > "$CLUSTER_FILE"
  chmod 600 "$CLUSTER_FILE"
}

start() {
  build_image
  write_cluster_file

  if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
    docker start "$CONTAINER_NAME" >/dev/null
  else
    docker run -d \
      --name "$CONTAINER_NAME" \
      --restart unless-stopped \
      -p "127.0.0.1:${FDB_PORT}:${FDB_PORT}" \
      -e "FDB_PORT=${FDB_PORT}" \
      -e "PUBLIC_ADDRESS=127.0.0.1:${FDB_PORT}" \
      -v "${DATA_VOLUME}:/var/fdb/data" \
      -v "${LOG_VOLUME}:/var/fdb/logs" \
      "$IMAGE_NAME" >/dev/null
  fi

  for _ in $(seq 1 60); do
    if docker exec "$CONTAINER_NAME" fdbcli -C /etc/foundationdb/fdb.cluster --exec "status minimal" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  docker exec "$CONTAINER_NAME" fdbcli -C /etc/foundationdb/fdb.cluster --exec "status minimal"
  echo "Cluster file: $CLUSTER_FILE"
}

case "${1:-start}" in
  start)
    start
    ;;
  restart)
    "$0" stop
    "$0" start
    ;;
  stop)
    docker stop "$CONTAINER_NAME" >/dev/null
    ;;
  status)
    docker exec "$CONTAINER_NAME" fdbcli -C /etc/foundationdb/fdb.cluster --exec "status minimal"
    ;;
  cluster-file)
    write_cluster_file
    echo "$CLUSTER_FILE"
    ;;
  logs)
    docker logs "$CONTAINER_NAME"
    ;;
  *)
    usage
    exit 2
    ;;
esac
