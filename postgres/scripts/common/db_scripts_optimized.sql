-- Optimized version of db_scripts.sql with performance improvements
-- Key optimizations:
-- 1. Better index strategies
-- 2. Reduced function calls and improved query plans
-- 3. Enhanced error handling
-- 4. Prepared statement patterns
-- 5. Memory-efficient operations

CREATE EXTENSION IF NOT EXISTS btree_gin;

-- Table & Sequence (with optimizations)
CREATE TABLE IF NOT EXISTS orisun_es_event
(
    transaction_id BIGINT                    NOT NULL,
    global_id      BIGINT PRIMARY KEY,
    stream_name    TEXT                      NOT NULL,
    stream_version BIGINT                    NOT NULL,
    event_id       UUID                      NOT NULL,
    event_type     TEXT                      NOT NULL CHECK (event_type <> ''),
    data           JSONB                     NOT NULL,
    metadata       JSONB,
    date_created   TIMESTAMPTZ DEFAULT(NOW() AT TIME ZONE 'UTC') NOT NULL
);

CREATE SEQUENCE IF NOT EXISTS orisun_es_event_global_id_seq
    OWNED BY orisun_es_event.global_id;

-- Optimized Indexes
-- Primary stream lookup index (most common query pattern)
CREATE INDEX IF NOT EXISTS idx_stream_name_version ON orisun_es_event (stream_name, stream_version DESC);

-- Global ordering index for event publishing
CREATE INDEX IF NOT EXISTS idx_global_order ON orisun_es_event (transaction_id, global_id);

-- Optimized GIN index for JSONB queries with better configuration
CREATE INDEX IF NOT EXISTS idx_stream_data_gin ON orisun_es_event
    USING GIN (stream_name, data) 
    WITH (fastupdate = true, gin_pending_list_limit = 256);

-- Partial index for recent events (hot data)
CREATE INDEX IF NOT EXISTS idx_recent_events ON orisun_es_event (stream_name, stream_version DESC)
    WHERE date_created > (NOW() - INTERVAL '7 days');

-- Event type index for type-based queries
CREATE INDEX IF NOT EXISTS idx_event_type ON orisun_es_event (event_type, stream_name);

-- Optimized Insert Function
CREATE OR REPLACE FUNCTION insert_events_with_consistency(
    schema TEXT,
    stream_info JSONB,
    events JSONB
)
    RETURNS TABLE
            (
                new_stream_version    BIGINT,
                latest_transaction_id BIGINT,
                latest_global_id      BIGINT
            )
    LANGUAGE plpgsql
AS
$$
DECLARE
    stream                  TEXT   := stream_info ->> 'stream_name';
    expected_stream_version BIGINT := (stream_info ->> 'expected_version')::BIGINT;
    stream_criteria         JSONB  := stream_info -> 'criteria';
    current_tx_id           BIGINT := pg_current_xact_id()::TEXT::BIGINT;
    current_stream_version  BIGINT := -1;
    filtered_stream_version BIGINT := -1;  -- Added for criteria-based version checking
    events_count           INT    := jsonb_array_length(events);
    lock_key               BIGINT;
BEGIN
    -- Input validation
    IF events_count = 0 THEN
        RAISE EXCEPTION 'Events array cannot be empty';
    END IF;
    
    IF events_count > 1000 THEN
        RAISE EXCEPTION 'Too many events in single batch. Maximum allowed: 1000, provided: %', events_count;
    END IF;
    
    IF stream IS NULL OR stream = '' THEN
        RAISE EXCEPTION 'Stream name cannot be null or empty';
    END IF;

    -- Set schema once
    EXECUTE format('SET search_path TO %I', schema);
    
    -- Use consistent hash for lock key to avoid hash collisions
    lock_key := ('x' || substr(md5(stream), 1, 15))::bit(60)::bigint;
    PERFORM pg_advisory_xact_lock(lock_key);

    -- Optimized stream version check with single query
    IF stream_criteria IS NOT NULL THEN
        -- When criteria is specified, we need both filtered and unfiltered versions
        WITH version_check AS (
            SELECT 
                MAX(CASE WHEN data @> ANY (SELECT jsonb_array_elements(stream_criteria)) 
                         THEN stream_version ELSE NULL END) as filtered_version,
                MAX(stream_version) as actual_version
            FROM orisun_es_event 
            WHERE stream_name = stream
        )
        SELECT 
            COALESCE(filtered_version, -1),
            COALESCE(actual_version, -1)
        INTO filtered_stream_version, current_stream_version
        FROM version_check;
        
        -- Check if the filtered version matches expected
        IF filtered_stream_version <> expected_stream_version THEN
            RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
                expected_stream_version, filtered_stream_version;
        END IF;
    ELSE
        -- Simple case: no criteria filtering
        SELECT COALESCE(MAX(stream_version), -1)
        INTO current_stream_version
        FROM orisun_es_event
        WHERE stream_name = stream;
        
        IF current_stream_version <> expected_stream_version THEN
            RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
                expected_stream_version, current_stream_version;
        END IF;
    END IF;

    -- Optimized batch insert with pre-allocated sequence values
    WITH 
    sequence_values AS (
        SELECT generate_series(1, events_count) as seq_num,
               nextval('orisun_es_event_global_id_seq') as global_id
    ),
    inserted_events AS (
        INSERT INTO orisun_es_event (
            stream_name,
            stream_version,
            transaction_id,
            event_id,
            global_id,
            event_type,
            data,
            metadata
        )
        SELECT 
            stream,
            current_stream_version + sv.seq_num,
            current_tx_id,
            (e ->> 'event_id')::UUID,
            sv.global_id,
            e ->> 'event_type',
            COALESCE(e -> 'data', '{}'),
            COALESCE(e -> 'metadata', '{}')
        FROM jsonb_array_elements(events) WITH ORDINALITY AS e(event, seq_num)
        JOIN sequence_values sv ON sv.seq_num = e.seq_num
        RETURNING global_id
    )
    SELECT 
        current_stream_version + events_count,
        current_tx_id,
        MAX(global_id)
    INTO new_stream_version, latest_transaction_id, latest_global_id
    FROM inserted_events;

    RETURN QUERY SELECT new_stream_version, latest_transaction_id, latest_global_id;
END;
$$;

-- Optimized Query Function
CREATE OR REPLACE FUNCTION get_matching_events(
    schema TEXT,
    stream_name TEXT DEFAULT NULL,
    from_stream_version BIGINT DEFAULT NULL,
    criteria JSONB DEFAULT NULL,
    after_position JSONB DEFAULT NULL,
    sort_dir TEXT DEFAULT 'ASC',
    max_count INT DEFAULT 1000
) RETURNS SETOF orisun_es_event
    LANGUAGE plpgsql
    STABLE AS
$$
DECLARE
    op TEXT := CASE WHEN sort_dir = 'ASC' THEN '>=' ELSE '<=' END;
    schema_prefix TEXT;
    criteria_array JSONB := criteria -> 'criteria';
    tx_id BIGINT := (after_position ->> 'transaction_id')::BIGINT;
    global_id BIGINT := (after_position ->> 'global_id')::BIGINT;
    effective_limit INT := LEAST(GREATEST(max_count, 1), 10000);
BEGIN
    IF sort_dir NOT IN ('ASC', 'DESC') THEN
        RAISE EXCEPTION 'Invalid sort direction: "%"', sort_dir;
    END IF;
    
    schema_prefix := quote_ident(schema) || '.';

    -- Set work_mem for this query to improve sort performance
    SET LOCAL work_mem = '64MB';

    -- Optimized query paths based on common patterns
    IF stream_name IS NOT NULL AND criteria_array IS NULL AND after_position IS NULL THEN
        -- Fast path: stream-only queries (most common)
        RETURN QUERY EXECUTE format(
            $q$
            SELECT * FROM %4$sorisun_es_event
            WHERE stream_name = $1
            AND ($2 IS NULL OR stream_version %3$s $2)
            ORDER BY stream_version %5$s
            LIMIT $3
            $q$,
            op, sort_dir, schema_prefix
        ) USING stream_name, from_stream_version, effective_limit;
        
    ELSIF stream_name IS NOT NULL AND criteria_array IS NOT NULL AND after_position IS NULL THEN
        -- Criteria-based queries
        RETURN QUERY EXECUTE format(
            $q$
            SELECT * FROM %4$sorisun_es_event
            WHERE stream_name = $1
            AND ($2 IS NULL OR stream_version %3$s $2)
            AND data @> ANY (SELECT jsonb_array_elements($4))
            ORDER BY stream_version %5$s
            LIMIT $6
            $q$,
            op, sort_dir, schema_prefix
        ) USING stream_name, from_stream_version, effective_limit, criteria_array;
        
    ELSE
        -- General case with position-based pagination
        RETURN QUERY EXECUTE format(
            $q$
            SELECT * FROM %8$sorisun_es_event
            WHERE 
                ($1 IS NULL OR stream_name = $1) AND
                ($2 IS NULL OR stream_version %4$s $2) AND
                ($7 IS NULL OR data @> ANY (SELECT jsonb_array_elements($7))) AND
                ($3 IS NULL OR $4 IS NULL OR 
                    (transaction_id, global_id) %4$s ($5, $6))
            ORDER BY transaction_id %9$s, global_id %9$s
            LIMIT $10
            $q$,
            op, sort_dir, schema_prefix
        ) USING stream_name, from_stream_version, after_position, tx_id, global_id, 
                effective_limit, criteria_array;
    END IF;
END;
$$;

-- Optimized supporting tables
CREATE TABLE IF NOT EXISTS orisun_last_published_event_position
(
    boundary       TEXT PRIMARY KEY,
    transaction_id BIGINT NOT NULL DEFAULT 0,
    global_id      BIGINT NOT NULL DEFAULT 0,
    date_created   TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    date_updated   TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Add index for faster updates
CREATE INDEX IF NOT EXISTS idx_last_published_updated ON orisun_last_published_event_position (date_updated DESC);

CREATE TABLE IF NOT EXISTS events_count (
    id VARCHAR(255) PRIMARY KEY,
    event_count BIGINT NOT NULL, -- Changed from VARCHAR to BIGINT for better performance
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projector_checkpoint (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    commit_position BIGINT NOT NULL,
    prepare_position BIGINT NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Add index for checkpoint queries
CREATE INDEX IF NOT EXISTS idx_projector_checkpoint_name ON projector_checkpoint (name);
CREATE INDEX IF NOT EXISTS idx_projector_checkpoint_position ON projector_checkpoint (commit_position, prepare_position);

-- Performance monitoring view
CREATE OR REPLACE VIEW stream_stats AS
SELECT 
    stream_name,
    COUNT(*) as event_count,
    MAX(stream_version) as latest_version,
    MIN(date_created) as first_event_date,
    MAX(date_created) as latest_event_date,
    COUNT(DISTINCT event_type) as unique_event_types
FROM orisun_es_event
GROUP BY stream_name;

-- Utility function for stream health check
CREATE OR REPLACE FUNCTION check_stream_health(stream_name_param TEXT)
RETURNS TABLE(
    stream_name TEXT,
    is_healthy BOOLEAN,
    version_gaps TEXT[],
    duplicate_versions BIGINT[],
    latest_version BIGINT
)
LANGUAGE plpgsql
STABLE AS
$$
BEGIN
    RETURN QUERY
    WITH stream_versions AS (
        SELECT DISTINCT oe.stream_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream_name_param
        ORDER BY oe.stream_version
    ),
    version_analysis AS (
        SELECT 
            MAX(stream_version) as max_version,
            COUNT(*) as version_count,
            ARRAY_AGG(CASE WHEN lead(stream_version) OVER (ORDER BY stream_version) - stream_version > 1 
                          THEN stream_version + 1 || '-' || (lead(stream_version) OVER (ORDER BY stream_version) - 1)
                          ELSE NULL END) FILTER (WHERE lead(stream_version) OVER (ORDER BY stream_version) - stream_version > 1) as gaps
        FROM stream_versions
    ),
    duplicate_check AS (
        SELECT ARRAY_AGG(stream_version) as duplicates
        FROM (
            SELECT stream_version
            FROM orisun_es_event
            WHERE stream_name = stream_name_param
            GROUP BY stream_version
            HAVING COUNT(*) > 1
        ) dups
    )
    SELECT 
        stream_name_param,
        CASE WHEN va.max_version = va.version_count - 1 AND COALESCE(array_length(dc.duplicates, 1), 0) = 0 
             THEN true ELSE false END,
        COALESCE(va.gaps, ARRAY[]::TEXT[]),
        COALESCE(dc.duplicates, ARRAY[]::BIGINT[]),
        COALESCE(va.max_version, -1)
    FROM version_analysis va
    CROSS JOIN duplicate_check dc;
END;
$$;