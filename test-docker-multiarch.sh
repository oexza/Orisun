#!/bin/bash

# Test Docker images for multiple architectures
# This script builds and tests both AMD64 and ARM64 images

set -e

echo "🔧 Testing Orisun Docker Multi-Arch Support"
echo "=========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
POSTGRES_PORT=15432
TIMEOUT=20

# Function to test an architecture
test_arch() {
    local ARCH_DISPLAY=$1
    local ARCH_PLATFORM=$2
    local TAG=$3
    local PORT=$4

    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${YELLOW}Testing $ARCH_DISPLAY image...${NC}"
    echo -e "${BLUE}========================================${NC}"

    # Start PostgreSQL
    echo "→ Starting PostgreSQL container..."
    docker run -d --name orisun-test-pg-${ARCH_PLATFORM} \
        -e POSTGRES_DB=orisun \
        -e POSTGRES_USER=postgres \
        -e POSTGRES_PASSWORD=postgres \
        -p ${POSTGRES_PORT}:5432 \
        postgres:17.5-alpine > /dev/null 2>&1

    # Wait for PostgreSQL to be ready
    echo "→ Waiting for PostgreSQL to be ready..."
    local pg_ready=false
    for i in $(seq 1 10); do
        if docker exec orisun-test-pg-${ARCH_PLATFORM} pg_isready -U postgres > /dev/null 2>&1; then
            pg_ready=true
            break
        fi
        sleep 1
    done

    if [ "$pg_ready" = false ]; then
        echo -e "${RED}✗ PostgreSQL failed to start${NC}"
        docker logs orisun-test-pg-${ARCH_PLATFORM} 2>&1 | tail -20
        docker stop orisun-test-pg-${ARCH_PLATFORM} > /dev/null 2>&1
        docker rm orisun-test-pg-${ARCH_PLATFORM} > /dev/null 2>&1
        return 1
    fi

    echo -e "${GREEN}✓ PostgreSQL is ready${NC}"

    # Test Orisun container
    echo "→ Starting Orisun $ARCH_DISPLAY container..."
    local log_file="/tmp/orisun-${ARCH_PLATFORM}-test.log"

    docker run --rm --name orisun-test-${ARCH_PLATFORM} \
        --link orisun-test-pg-${ARCH_PLATFORM}:postgres \
        -p ${PORT}:5005 \
        -e ORISUN_PG_HOST=postgres \
        -e ORISUN_PG_PORT=5432 \
        -e ORISUN_PG_USER=postgres \
        -e ORISUN_PG_PASSWORD=postgres \
        -e ORISUN_PG_NAME=orisun \
        -e ORISUN_PG_SCHEMAS="test:public,test_admin:admin" \
        -e ORISUN_BOUNDARIES='[{"name":"test","description":"test boundary"},{"name":"test_admin","description":"admin boundary"}]' \
        -e ORISUN_ADMIN_BOUNDARY=test_admin \
        --platform $ARCH_PLATFORM \
        ${TAG} > "$log_file" 2>&1 &
    local ORISUN_PID=$!

    # Wait for Orisun to start
    echo "→ Waiting for Orisun to start (${TIMEOUT}s timeout)..."
    local orisun_ready=false
    for i in $(seq 1 $TIMEOUT); do
        if grep -q "gRPC server listening on port 5005" "$log_file" 2>/dev/null; then
            orisun_ready=true
            break
        fi
        if ! kill -0 $ORISUN_PID 2>/dev/null; then
            break
        fi
        sleep 1
        echo -n "."
    done
    echo ""

    if [ "$orisun_ready" = true ]; then
        echo -e "${GREEN}✓ $ARCH_DISPLAY image started successfully!${NC}"

        # Show container info
        echo -e "\n${YELLOW}Container status:${NC}"
        docker ps --filter "name=orisun-test-${ARCH_PLATFORM}" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

        # Show last few log lines
        echo -e "\n${YELLOW}Recent log entries:${NC}"
        tail -10 "$log_file" | grep -E "INFO|WARN|ERROR" || tail -5 "$log_file"

        # Stop the container gracefully
        echo -e "\n→ Stopping container..."
        docker stop orisun-test-${ARCH_PLATFORM} > /dev/null 2>&1 || true
        SUCCESS=true
    else
        echo -e "${RED}✗ $ARCH_DISPLAY image failed to start${NC}"

        # Check if process is still running
        if ! kill -0 $ORISUN_PID 2>/dev/null; then
            echo -e "${RED}Container process has exited${NC}"
        fi

        # Show logs for debugging
        echo -e "\n${YELLOW}Full logs:${NC}"
        cat "$log_file"

        docker stop orisun-test-${ARCH_PLATFORM} > /dev/null 2>&1 || true
        SUCCESS=false
    fi

    # Cleanup
    echo "→ Cleaning up..."
    docker stop orisun-test-pg-${ARCH_PLATFORM} > /dev/null 2>&1
    docker rm orisun-test-pg-${ARCH_PLATFORM} > /dev/null 2>&1
    rm -f "$log_file"

    return 0
}

# Main testing logic
echo -e "${BLUE}This script will test both AMD64 and ARM64 Docker images${NC}"
echo -e "${YELLOW}AMD64 will use emulation (slower), ARM64 will run natively (fast)${NC}\n"

# Ask if user wants to build first
read -p "Build images before testing? (y/n) " -n 1 -r
echo ""
BUILD_IMAGES=false
if [[ $REPLY =~ ^[Yy]$ ]]; then
    BUILD_IMAGES=true
fi

if [ "$BUILD_IMAGES" = true ]; then
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${YELLOW}Building Docker images...${NC}"
    echo -e "${BLUE}========================================${NC}"

    echo "→ Building AMD64 image..."
    if docker buildx build --platform linux/amd64 --load -t orisun:amd64-test .; then
        echo -e "${GREEN}✓ AMD64 image built${NC}"
    else
        echo -e "${RED}✗ AMD64 build failed${NC}"
        exit 1
    fi

    echo "→ Building ARM64 image..."
    if docker buildx build --platform linux/arm64 --load -t orisun:arm64-test .; then
        echo -e "${GREEN}✓ ARM64 image built${NC}"
    else
        echo -e "${RED}✗ ARM64 build failed${NC}"
        exit 1
    fi
fi

echo -e "\n${BLUE}========================================${NC}"
echo -e "${YELLOW}Starting tests...${NC}"
echo -e "${BLUE}========================================${NC}"

# Test AMD64 (emulated on M1)
test_arch "AMD64 (emulated)" "linux/amd64" "orisun:amd64-test" "15006"

# Test ARM64 (native on M1)
test_arch "ARM64 (native)" "linux/arm64" "orisun:arm64-test" "15007"

echo -e "\n${GREEN}==========================================${NC}"
echo -e "${GREEN}✓ Testing complete!${NC}"
echo -e "${GREEN}==========================================${NC}"
echo ""
echo "If all tests passed, your Docker images are ready for release!"