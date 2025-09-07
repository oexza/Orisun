FROM golang:latest AS builder

# Install build dependencies
# RUN apt-get update && apt-get install -y --no-install-recommends \
#     git \
#     gcc \
#     libc6-dev \
#     && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build arguments for version information
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

# Build the application with optimizations and version information
RUN CGO_ENABLED=0 go build -a -installsuffix cgo \
    -ldflags="-w -s -X orisun/common.Version=${VERSION} -X orisun/common.BuildTime=${BUILD_TIME} -X orisun/common.GitCommit=${GIT_COMMIT}" \
    -o orisun ./

# Use a minimal debian slim image for the final container
FROM alpine:3.18

# Add CA certificates and timezone data
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Create a non-root user to run the application
RUN mkdir -p /app && \
    useradd -r -d /app -M orisun
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/orisun /app/

# Set ownership
RUN chown -R orisun:orisun /app

# Add this before switching to the non-root user
RUN mkdir -p /var/lib/orisun/data && \
    chown -R orisun:orisun /var/lib/orisun

# Switch to non-root user
USER orisun

# Expose necessary ports (admin UI, gRPC, NATS, and additional gRPC port)
EXPOSE 8991 5005 4222 50051

# Set environment variables
ENV GO_ENV=production

# Set default command
CMD ["/app/orisun"]