#!/bin/bash

# This script tests the Docker setup locally

set -e

echo "Building Docker image locally..."
docker build \
  --build-arg VERSION=dev \
  --build-arg BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
  --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) \
  -t orisun:local .

echo "\nStarting PostgreSQL container..."
docker run -d \
  --name postgres-test \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=password@1 \
  -e POSTGRES_DB=orisun \
  -p 5434:5432 \
  postgres:17.5-alpine3.22

echo "Waiting for PostgreSQL to start..."
sleep 5

echo "\nStarting Orisun container..."
docker run -d \
  --name orisun-test \
  -p 8991:8991 \
  -p 5005:5005 \
  -e ORISUN_PG_USER=postgres \
  -e ORISUN_PG_NAME=orisun \
  -e ORISUN_PG_PASSWORD=password@1 \
  -e ORISUN_PG_HOST=host.docker.internal \
  -e ORISUN_PG_PORT=5434 \
  -e ORISUN_LOGGING_LEVEL=INFO \
  -e ORISUN_ADMIN_USERNAME=admin \
  -e ORISUN_ADMIN_PASSWORD=changeit \
  orisun:local

echo "\nContainers started. You can access Orisun at:"
echo "- Admin UI: http://localhost:8991"
echo "- gRPC: localhost:5005"

echo "\nTo stop and remove the containers, run:"
echo "docker stop orisun-test postgres-test"
echo "docker rm orisun-test postgres-test"