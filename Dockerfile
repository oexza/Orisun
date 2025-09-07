FROM golang:latest AS builder

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build arguments for version information
ARG VERSION=dev
ARG TARGET_OS=linux
ARG TARGET_ARCH=amd64

# Make build script executable and create build directory
RUN chmod +x ./build.sh && mkdir -p ./build

# Use build.sh to build the application with cross-compilation
RUN ./build.sh ${TARGET_OS} ${TARGET_ARCH} ${VERSION}

# Use a minimal Alpine image for the final container
FROM alpine:3.18

# Add CA certificates and timezone data
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user to run the application
RUN mkdir -p /app && adduser -D -h /app -s /sbin/nologin orisun
WORKDIR /app

# Copy the binary from the builder stage (using the build.sh output path)
COPY --from=builder /app/build/orisun-linux-amd64 /app/orisun

# Set ownership
RUN chown -R orisun:orisun /app

# Add this before switching to the non-root user
RUN mkdir -p /var/lib/orisun/data && chown -R orisun:orisun /var/lib/orisun

# Switch to non-root user
USER orisun

# Expose necessary ports (admin UI, gRPC, NATS, and additional gRPC port)
EXPOSE 8991 5005 4222 50051

# Set environment variables
ENV GO_ENV=production

# Set default command
CMD ["/app/orisun"]