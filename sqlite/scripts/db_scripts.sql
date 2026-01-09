-- SQLite Event Store Schema
-- This schema replicates PostgreSQL's GIN index behavior using FTS5

-- 1. Main Event Table
CREATE TABLE IF NOT EXISTS orisun_es_event
(
    global_id      INTEGER PRIMARY KEY AUTOINCREMENT,
    transaction_id INTEGER NOT NULL,                       -- Logical grouping ID (e.g., Timestamp or Batch ID)
    event_id       TEXT    NOT NULL UNIQUE,                -- UUID stored as TEXT
    event_type     TEXT    NOT NULL CHECK (event_type <> ''),
    data           TEXT    NOT NULL,                       -- JSON stored as TEXT
    metadata       TEXT,                                   -- JSON stored as TEXT
    date_created   INTEGER DEFAULT (strftime('%s', 'now')) -- Unix Timestamp
);

-- 2. Indexes for Ordering and Lookup
CREATE INDEX IF NOT EXISTS idx_global_order_covering
    ON orisun_es_event (transaction_id DESC, global_id DESC);

-- 3. FTS5 Sidecar (The GIN Replacement)
-- This virtual table provides full-text search capabilities similar to PostgreSQL's GIN index
CREATE VIRTUAL TABLE IF NOT EXISTS events_search_idx USING fts5
(
    all_text,
    content='orisun_es_event',
    content_rowid='global_id'
);

-- 4. Trigger to automatically populate FTS index
-- This ensures the FTS index stays in sync with the main table
CREATE TRIGGER IF NOT EXISTS events_search_idx_insert
    AFTER INSERT
    ON orisun_es_event
BEGIN
    INSERT INTO events_search_idx(rowid, all_text)
    VALUES (NEW.global_id, (SELECT group_concat(key || ':' || value, ' ')
                            FROM json_each(NEW.data)));
END;

CREATE TRIGGER IF NOT EXISTS events_search_idx_update
    AFTER UPDATE
    ON orisun_es_event
BEGIN
    UPDATE events_search_idx
    SET all_text = (SELECT group_concat(key || ':' || value, ' ')
                    FROM json_each(NEW.data))
    WHERE rowid = NEW.global_id;
END;

CREATE TRIGGER IF NOT EXISTS events_search_idx_delete
    AFTER DELETE
    ON orisun_es_event
BEGIN
    DELETE FROM events_search_idx WHERE rowid = OLD.global_id;
END;

-- 5. Users Table (for admin functionality)
CREATE TABLE IF NOT EXISTS users
(
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    roles         TEXT NOT NULL, -- JSON array stored as TEXT
    created_at    INTEGER DEFAULT (strftime('%s', 'now')),
    updated_at    INTEGER DEFAULT (strftime('%s', 'now'))
);

-- 6. Users Count Table
CREATE TABLE IF NOT EXISTS users_count
(
    id         TEXT PRIMARY KEY,
    user_count TEXT NOT NULL,
    created_at INTEGER DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER DEFAULT (strftime('%s', 'now'))
);

-- 7. Events Count Table
CREATE TABLE IF NOT EXISTS events_count
(
    id          TEXT PRIMARY KEY,
    event_count TEXT NOT NULL,
    created_at  INTEGER DEFAULT (strftime('%s', 'now')),
    updated_at  INTEGER DEFAULT (strftime('%s', 'now'))
);

-- 8. Projector Checkpoint Table
CREATE TABLE IF NOT EXISTS projector_checkpoint
(
    name             TEXT PRIMARY KEY,
    commit_position  INTEGER DEFAULT 0,
    prepare_position INTEGER DEFAULT 0,
    updated_at       INTEGER DEFAULT (strftime('%s', 'now'))
);

-- 9. Event Publishing Tracking Table
CREATE TABLE IF NOT EXISTS event_publishing_tracker
(
    boundary              TEXT PRIMARY KEY,
    last_commit_position  INTEGER NOT NULL DEFAULT 0,
    last_prepare_position INTEGER NOT NULL DEFAULT 0,
    updated_at            INTEGER          DEFAULT (strftime('%s', 'now'))
);

-- 10. Indexes for performance
CREATE INDEX IF NOT EXISTS idx_event_id ON orisun_es_event (event_id);
CREATE INDEX IF NOT EXISTS idx_event_type ON orisun_es_event (event_type);
CREATE INDEX IF NOT EXISTS idx_transaction_id ON orisun_es_event (transaction_id);

-- 11. Locks Table (for distributed locking)
CREATE TABLE IF NOT EXISTS locks
(
    lock_name TEXT PRIMARY KEY,
    locked_at INTEGER NOT NULL,
    locked_by TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_locks_locked_at ON locks(locked_at);