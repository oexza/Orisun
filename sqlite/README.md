# SQLite Event Store Implementation

This is a production-grade SQLite implementation of the Orisun event store, designed to replicate PostgreSQL's behavior using SQLite-specific features.

## Architecture

### Key Design Decisions

1. **FTS5 for GIN Index Replacement**: Uses SQLite's FTS5 (Full-Text Search) virtual table to replicate PostgreSQL's GIN index functionality for JSON containment queries.

2. **WAL Mode**: Uses Write-Ahead Logging (WAL) journal mode for better concurrent read performance.

3. **Connection Pooling**: Implements separate connection pools for write and read operations to optimize performance while respecting SQLite's single-writer limitation.

4. **Application-Level Locking**: Implements distributed locking using a dedicated locks table to replace PostgreSQL's advisory locks.

5. **Trigger-Based Index Sync**: Uses SQLite triggers to automatically keep the FTS5 index synchronized with the main event table.

## Schema Overview

### Main Tables

1. **orisun_es_event**: Primary event storage table
   - `global_id`: AUTOINCREMENT primary key (replaces PostgreSQL sequence)
   - `transaction_id`: Logical grouping identifier
   - `event_id`: UUID stored as TEXT
   - `event_type`: Event type
   - `data`: Event data as JSON TEXT
   - `metadata`: Event metadata as JSON TEXT
   - `date_created`: Unix timestamp

2. **events_search_idx**: FTS5 virtual table for JSON search
   - Automatically populated via triggers
   - Supports MATCH queries for efficient filtering

3. **users**: Admin user management
4. **projector_checkpoint**: Projector position tracking
5. **event_publishing_tracker**: Event publishing state
6. **users_count**, **events_count**: Cached counts

### Indexes

- `idx_global_order_covering`: Covering index for ordering queries
- `idx_event_id`, `idx_event_type`, `idx_transaction_id`: Performance indexes

## API Reference

### Initialization

```go
import sqlite "github.com/oexza/Orisun/sqlite"

// Simple initialization (single connection pool)
saveEvents, getEvents, lockProvider, adminDB, eventPublishingTracker :=
    sqlite.InitializeSQLiteDatabase(ctx, dbPath, js, logger)

// Advanced initialization (separate read/write pools)
saveEvents, getEvents, lockProvider, adminDB, eventPublishingTracker :=
    sqlite.InitializeSQLiteDatabaseWithPools(ctx, dbPath, js, logger)
```

### EventsSaver Interface

```go
type SQLiteSaveEvents struct {
    db     *sql.DB
    logger logging.Logger
}

func (s *SQLiteSaveEvents) Save(
    ctx context.Context,
    events []orisun.EventWithMapTags,
    boundary string,
    expectedPosition *orisun.Position,
    streamConsistencyCondition *orisun.Query,
) (transactionID string, globalID int64, err error)
```

**Features:**
- Optimistic concurrency control via FTS5 queries
- Transactional event insertion
- Automatic FTS index population via triggers
- Proper error handling with detailed logging

### EventsRetriever Interface

```go
type SQLiteGetEvents struct {
    db     *sql.DB
    logger logging.Logger
}

func (s *SQLiteGetEvents) Get(
    ctx context.Context,
    req *orisun.GetEventsRequest,
) (*orisun.GetEventsResponse, error)
```

**Features:**
- FTS5-based criteria filtering
- Position-based pagination
- Bidirectional traversal (ASC/DESC)
- Read-only transactions for consistency

### LockProvider Interface

```go
type SQLiteLockProvider struct {
    db     *sql.DB
    logger logging.Logger
}

func (p *SQLiteLockProvider) Lock(ctx context.Context, lockName string) error
func (p *SQLiteLockProvider) Unlock(ctx context.Context, lockName string) error
```

**Features:**
- Application-level distributed locking
- 30-second lock timeout
- Deadlock prevention via consistent ordering
- Automatic cleanup on context cancellation

### Admin Database

```go
type SQLiteAdminDB struct {
    db     *sql.DB
    logger logging.Logger
}

// User management
func (s *SQLiteAdminDB) ListAdminUsers() ([]*orisun.User, error)
func (s *SQLiteAdminDB) GetUserByUsername(username string) (orisun.User, error)
func (s *SQLiteAdminDB) GetUserById(id string) (orisun.User, error)
func (s *SQLiteAdminDB) UpsertUser(user orisun.User) error
func (s *SQLiteAdminDB) DeleteUser(id string) error

// Statistics
func (s *SQLiteAdminDB) GetUsersCount() (uint32, error)
func (s *SQLiteAdminDB) SaveUsersCount(usersCount uint32) error
func (s *SQLiteAdminDB) GetEventsCount(boundary string) (int, error)
func (s *SQLiteAdminDB) SaveEventCount(eventCount int, boundary string) error

// Projector management
func (s *SQLiteAdminDB) GetProjectorLastPosition(projectorName string) (*orisun.Position, error)
func (s *SQLiteAdminDB) UpdateProjectorPosition(name string, position *orisun.Position) error
```

### Event Publishing Tracker

```go
type SQLiteEventPublishingTracker struct {
    db     *sql.DB
    logger logging.Logger
}

func (t *SQLiteEventPublishingTracker) GetLastPublishedEventPosition(
    ctx context.Context,
    boundary string,
) (orisun.Position, error)

func (t *SQLiteEventPublishingTracker) InsertLastPublishedEvent(
    ctx context.Context,
    boundary string,
    transactionId int64,
    globalId int64,
) error
```

## FTS5 Query Building

The implementation builds FTS5 queries that replicate PostgreSQL's `@>` containment operator:

```go
// Input: [{"username": "iskaba", "status": "active"}]
// Output: "$.username:iskaba AND $.status:active"

func buildFTSQuery(criteria []*orisun.CriteriaGroup) string
```

**Query Format:**
- Within a criteria group: AND logic (all tags must match)
- Between criteria groups: OR logic (any group can match)
- Path notation: `$.key:value` for JSON paths
- Special characters are escaped for FTS5 safety

## Performance Considerations

### Write Performance
- **Single Writer**: SQLite allows only one concurrent writer
- **WAL Mode**: Enables concurrent reads during writes
- **Batch Inserts**: Multiple events inserted in a single transaction
- **Connection Pool**: Write pool limited to 1 connection

### Read Performance
- **FTS5 Index**: Full-text search with sub-millisecond response
- **Read Pool**: Multiple connections for concurrent reads
- **Covering Index**: Reduces disk I/O for ordering queries
- **Read-Only Transactions**: Prevents blocking writes

### Lock Performance
- **Application-Level**: No external dependencies
- **Timeout**: 30-second lock expiration prevents deadlocks
- **In-Memory**: Fast lock acquisition/release

## Limitations vs PostgreSQL

1. **Write Throughput**: Single writer limits concurrent writes (PostgreSQL: unlimited)
2. **JSON Operations**: No native JSONB, uses TEXT with JSON functions
3. **Index Types**: Only FTS5 available (PostgreSQL: B-tree, GIN, GiST, etc.)
4. **Advisory Locks**: Emulated at application level (PostgreSQL: native)
5. **Schema Flexibility**: Limited ALTER TABLE support (PostgreSQL: full DDL support)

## Best Practices

1. **Use WAL Mode**: Already enabled by default for better concurrency
2. **Regular VACUUM**: Schedule periodic VACUUM to reclaim space
3. **Connection Pooling**: Use separate pools for read/write operations
4. **Transaction Size**: Keep transaction batches reasonable (< 1000 events)
5. **Lock Timeout**: Adjust based on your operation duration
6. **Backup Strategy**: Use SQLite backup API or file-level backups

## Configuration

### Database Connection String

```go
// Write connection
dbPath + "?_foreign_keys=on&_journal_mode=WAL&_timeout=5000"

// Read connection (read-only mode)
dbPath + "?_foreign_keys=on&_journal_mode=WAL&_timeout=5000&_mode=ro"
```

**Parameters:**
- `_foreign_keys=on`: Enforce foreign key constraints
- `_journal_mode=WAL`: Enable write-ahead logging
- `_timeout=5000`: 5-second query timeout
- `_mode=ro`: Read-only mode for read connections

### Connection Pool Settings

```go
db.SetMaxOpenConns(25)   // Maximum open connections
db.SetMaxIdleConns(5)    // Maximum idle connections
db.SetConnMaxIdleTime(5 * time.Minute)   // Idle connection lifetime
db.SetConnMaxLifetime(30 * time.Minute)  // Maximum connection lifetime
```

## Testing

The implementation includes comprehensive error handling and logging for production use:

```go
// All errors are wrapped with context
if err != nil {
    s.logger.Errorf("Failed to save events: %v", err)
    return "", 0, status.Errorf(codes.Internal, "failed to save events: %w", err)
}

// Transaction rollback on error
defer func() {
    if err != nil {
        tx.Rollback()
    }
}()
```

## Migration from PostgreSQL

To migrate from PostgreSQL to SQLite:

1. **Export Data**: Use `pg_dump` to export PostgreSQL data
2. **Transform Data**: Convert PostgreSQL UUIDs to TEXT, JSONB to JSON
3. **Import Data**: Load transformed data into SQLite
4. **Update Configuration**: Change database configuration to use SQLite
5. **Verify**: Test all functionality with the new backend

## Troubleshooting

### Database Locked Errors

**Symptom**: "database is locked" errors during writes

**Solution:**
- Ensure WAL mode is enabled
- Reduce write transaction size
- Check for long-running read transactions
- Increase busy timeout: `?_timeout=10000`

### FTS5 Query Failures

**Symptom**: FTS5 MATCH queries return no results

**Solution:**
- Check that triggers are populating the FTS index
- Verify query format: `$.key:value`
- Escape special characters in values
- Rebuild FTS index if needed

### Performance Degradation

**Symptom**: Slow query performance over time

**Solution:**
- Run `VACUUM` to reclaim space
- Run `ANALYZE` to update statistics
- Check FTS index size: `SELECT count(*) FROM events_search_idx`
- Rebuild FTS index: `INSERT INTO events_search_idx(events_search_idx) VALUES('rebuild')`

## License

This implementation is part of the Orisun project and follows the same license terms.