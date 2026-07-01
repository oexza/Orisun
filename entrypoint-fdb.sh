#!/bin/sh
set -e

echo "Fixing permissions for /var/lib/orisun..."
mkdir -p /var/lib/orisun/nats
chown -R orisun:orisun /var/lib/orisun

echo "Permissions fixed. Starting Orisun FoundationDB as orisun user..."
exec runuser -u orisun -- "$@"
