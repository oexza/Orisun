# Docker Setup Guide

## Troubleshooting Docker Build Issues

If you encounter the following error when building the Docker image:

```
buildx failed with: ERROR: failed to build: failed to solve: process "/bin/sh -c CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags=\"...\" -o orisun ./" did not complete successfully: exit code: 1
```

This is typically caused by an incorrect import path in the build command. The module name in the Dockerfile must match the actual import path used in the code.

### Solution

The issue is that the `-ldflags` in the build command are using an incorrect import path. The module name in `go.mod` is `orisun`, but the build command is trying to use `github.com/oexza/orisun/common` as the import path.

Correct the import path in the Dockerfile:

```dockerfile
# Incorrect
-ldflags="-w -s -X 'github.com/oexza/orisun/common.Version=${VERSION}' -X 'github.com/oexza/orisun/common.BuildTime=${BUILD_TIME}' -X 'github.com/oexza/orisun/common.GitCommit=${GIT_COMMIT}'" \

# Correct
-ldflags="-w -s -X 'orisun/common.Version=${VERSION}' -X 'orisun/common.BuildTime=${BUILD_TIME}' -X 'orisun/common.GitCommit=${GIT_COMMIT}'" \
```

Also ensure that the `BuildTime` and `GitCommit` variables are defined in the `common/version.go` file.

If you're forking or renaming the repository, make sure to update all import paths accordingly in:
1. Dockerfile
2. build.sh
3. GitHub Actions workflows
4. Any other build scripts

## Setting Up Docker Hub Credentials in GitHub

To enable the GitHub Actions workflow to push Docker images to Docker Hub, you need to set up the following secrets in your GitHub repository:

1. Go to your GitHub repository
2. Click on **Settings** > **Secrets and variables** > **Actions**
3. Click on **New repository secret**
4. Add the following secrets:

   - **DOCKERHUB_USERNAME**: Your Docker Hub username
   - **DOCKERHUB_TOKEN**: Your Docker Hub access token (not your password)

## Creating a Docker Hub Access Token

1. Log in to [Docker Hub](https://hub.docker.com/)
2. Click on your username in the top-right corner and select **Account Settings**
3. Go to the **Security** tab
4. Click on **New Access Token**
5. Give your token a name (e.g., "GitHub Actions")
6. Select the appropriate permissions (at minimum, you need **Read & Write** access)
7. Click **Generate**
8. Copy the token immediately (you won't be able to see it again)
9. Use this token as the value for the **DOCKERHUB_TOKEN** secret in GitHub

## Testing Docker Locally

You can test the Docker setup locally using the provided script:

```bash
./scripts/test_docker.sh
```

This script will:
1. Build the Docker image locally
2. Start a PostgreSQL container
3. Start an Orisun container connected to PostgreSQL

## Manual Docker Commands

### Building the Image

```bash
docker build \
  --build-arg VERSION=dev \
  --build-arg BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
  --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) \
  -t orisun:local .
```

### Running with Docker Compose

```bash
docker-compose up -d
```

### Running Containers Manually

```bash
# Start PostgreSQL
docker run -d \
  --name postgres-test \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=password@1 \
  -e POSTGRES_DB=orisun \
  -p 5434:5432 \
  postgres:17.5-alpine3.22

# Start Orisun
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
```