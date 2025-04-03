FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o orisun ./

# Use a minimal scratch image for the final container
FROM alpine:3.18

# Add CA certificates and timezone data
RUN apk --no-cache add ca-certificates tzdata

# Create a non-root user to run the application
RUN adduser -D -H -h /app orisun
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

# Expose necessary ports
EXPOSE 8080
EXPOSE 50051

# Set environment variables
ENV GO_ENV=production

# Run the application
CMD ["/app/orisun"]