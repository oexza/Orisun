# Event Streaming Configuration

Orisun supports two different approaches for streaming events from the database to NATS:

1. **WAL (Write-Ahead Log) Streaming** - Uses PostgreSQL logical replication for real-time event streaming
2. **Polling** - Periodically polls the database for new events

You can configure which approach to use, or even use both simultaneously.

## Configuration Options

### Basic Configuration

In your `config.yaml` file, configure the event streaming method:

```yaml
eventStreaming:
  method: "wal"  # Options: "wal", "polling", or "both"
  wal:
    enabled: true
    slotName: "orisun_events_slot"
    publicationName: "orisun_events_pub"
  polling:
    enabled: false
    batchSize: 1000
    pollInterval: "5s"
```

### Environment Variables

You can also configure using environment variables:

```bash
# Event streaming method
export ORISUN_EVENT_STREAMING_METHOD=wal

# WAL configuration
export ORISUN_WAL_ENABLED=true
export ORISUN_WAL_SLOT_NAME=orisun_events_slot
export ORISUN_WAL_PUBLICATION_NAME=orisun_events_pub

# Polling configuration
export ORISUN_POLLING_ENABLED=false
export ORISUN_POLLING_BATCH_SIZE=1000
export ORISUN_POLLING_INTERVAL=5s
```

## Streaming Methods

### 1. WAL (Logical Replication) - Recommended

**Advantages:**
- Real-time event streaming with minimal latency
- Lower database overhead
- Automatic schema detection
- No polling overhead

**Use when:**
- You need real-time event processing
- You want to minimize database load
- You have PostgreSQL 10+ with logical replication enabled

**Configuration:**
```yaml
eventStreaming:
  method: "wal"
  wal:
    enabled: true
    slotName: "orisun_events_slot"
    publicationName: "orisun_events_pub"
```

**Prerequisites:**
- PostgreSQL must be configured for logical replication
- Run the setup script: `postgres/scripts/common/setup_logical_replication.sql`

### 2. Polling

**Advantages:**
- Works with any PostgreSQL version
- Simple and reliable
- No special PostgreSQL configuration required

**Use when:**
- You're using an older PostgreSQL version
- Logical replication is not available
- You prefer a simpler setup

**Configuration:**
```yaml
eventStreaming:
  method: "polling"
  polling:
    enabled: true
    batchSize: 1000
    pollInterval: "5s"
```

### 3. Both (Hybrid)

You can run both approaches simultaneously for redundancy or migration purposes:

```yaml
eventStreaming:
  method: "both"
  wal:
    enabled: true
    slotName: "orisun_events_slot"
    publicationName: "orisun_events_pub"
  polling:
    enabled: true
    batchSize: 1000
    pollInterval: "10s"  # Longer interval when used with WAL
```

## PostgreSQL Setup for WAL

If you choose WAL streaming, you need to configure PostgreSQL:

### 1. Update postgresql.conf

```ini
wal_level = logical
max_replication_slots = 10
max_wal_senders = 10
```

### 2. Run Setup Script

Execute the provided setup script:

```sql
-- Run postgres/scripts/common/setup_logical_replication.sql
```

This script will:
- Create a replication user
- Set up the publication for all event tables
- Configure necessary permissions

### 3. Restart PostgreSQL

After updating the configuration, restart PostgreSQL to apply changes.

## Performance Considerations

### WAL Streaming
- **Latency**: Near real-time (milliseconds)
- **Database Load**: Very low
- **Network**: Continuous connection
- **Scalability**: Excellent

### Polling
- **Latency**: Depends on poll interval (default 5s)
- **Database Load**: Moderate (depends on poll frequency)
- **Network**: Periodic queries
- **Scalability**: Good

## Monitoring

Both approaches provide logging for monitoring:

```bash
# WAL streaming logs
INFO Starting WAL-based event streaming
INFO Published event UserCreated from boundary orisun_test_1

# Polling logs  
INFO Starting polling-based event streaming
INFO Successfully acquired polling lock for boundary orisun_test_1
```

## Migration Between Methods

You can safely switch between methods by:

1. Updating the configuration
2. Restarting the application
3. The system will automatically use the new method

For zero-downtime migration, use the "both" method temporarily:

1. Set method to "both"
2. Verify both are working
3. Switch to the desired single method
4. Restart the application

## Troubleshooting

### WAL Issues
- Check PostgreSQL logs for replication errors
- Verify logical replication is enabled
- Ensure the replication user has proper permissions

### Polling Issues
- Check database connectivity
- Verify event tables exist
- Monitor polling frequency vs. database performance

### Both Methods
- Monitor for duplicate events (should be handled by the system)
- Check NATS for proper event delivery