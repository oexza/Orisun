#!/bin/sh

# Fix ownership of mounted volumes
# This is necessary because ECS mounts volumes with root ownership
# but our application runs as the 'orisun' user

echo "Fixing permissions for /var/lib/orisun..."

# Ensure the directory exists and has correct ownership
chown -R orisun:orisun /var/lib/orisun

echo "Permissions fixed. Starting application as orisun user..."

# Switch to orisun user and execute the main application
exec su-exec orisun "$@"