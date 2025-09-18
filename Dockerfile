# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:latest AS builder

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Platform arguments for cross-compilation
ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

# Build arguments for version information
ARG VERSION=dev
ARG TARGET_OS=linux
ARG TARGET_ARCH=amd64

# Use target platform if available, otherwise use build args
RUN echo "Building for platform: $TARGETPLATFORM (OS: $TARGETOS, ARCH: $TARGETARCH)"

# Make build script executable and create build directory
RUN chmod +x ./build.sh && mkdir -p ./build

# Use build.sh to build the application with cross-compilation
# Use TARGETOS/TARGETARCH if available, otherwise fall back to TARGET_OS/TARGET_ARCH
RUN FINAL_OS=${TARGETOS:-${TARGET_OS}} && \
    FINAL_ARCH=${TARGETARCH:-${TARGET_ARCH}} && \
    echo "Building with OS: $FINAL_OS, ARCH: $FINAL_ARCH" && \
    ./build.sh $FINAL_OS $FINAL_ARCH ${VERSION}

# Verify the binary was created and list all files in build directory
RUN FINAL_OS=${TARGETOS:-${TARGET_OS}} && \
    FINAL_ARCH=${TARGETARCH:-${TARGET_ARCH}} && \
    ls -la ./build/ && \
    test -f ./build/orisun-$FINAL_OS-$FINAL_ARCH && \
    echo "Binary orisun-$FINAL_OS-$FINAL_ARCH exists and is executable"

# Use a minimal Alpine image for the final container
FROM alpine:3.18

# Add CA certificates and timezone data
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user to run the application
RUN mkdir -p /app && adduser -D -h /app -s /sbin/nologin orisun
WORKDIR /app

# Copy the binary from the builder stage with dynamic architecture detection
# First try the specific architecture, then fallback to any available binary
COPY --from=builder /app/build/ /tmp/build/
RUN ls -la /tmp/build/ && \
    if [ -f "/tmp/build/orisun-linux-amd64" ]; then \
        cp /tmp/build/orisun-linux-amd64 /app/orisun; \
    elif [ -f "/tmp/build/orisun-linux-arm64" ]; then \
        cp /tmp/build/orisun-linux-arm64 /app/orisun; \
    else \
        echo "No suitable binary found!" && ls -la /tmp/build/ && exit 1; \
    fi && \
    chmod +x /app/orisun && \
    ls -la /app/orisun && \
    echo "Binary /app/orisun is ready"

# Set ownership and ensure data directory exists
RUN mkdir -p /var/lib/orisun/data && chown -R orisun:orisun /var/lib/orisun

# Switch to non-root user
USER orisun

# Expose necessary ports (admin UI, gRPC, NATS, and additional gRPC port)
EXPOSE 8991 5005 4222 50051

# Set environment variables
ENV GO_ENV=production

# Set default command
CMD ["/app/orisun"]