-- Hybrid CTE + Delayed Advisory Lock Approach
-- This combines the performance benefits of CTE with the consistency guarantees of advisory locks
-- The lock is only acquired when a conflict is detected, reducing lock contention

CREATE EXTENSION IF NOT EXISTS btree_gin;

-- Table & Sequence (same as original)
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

-- Optimized indexes for the delayed lock approach
CREATE INDEX IF NOT EXISTS idx_stream_name_version ON orisun_es_event (stream_name, stream_version DESC);
CREATE INDEX IF NOT EXISTS idx_global_order ON orisun_es_event (transaction_id, global_id);
CREATE INDEX IF NOT EXISTS idx_stream_data_gin ON orisun_es_event
    USING GIN (stream_name, data) 
    WITH (fastupdate = true, gin_pending_list_limit = 256);

-- Delayed Lock CTE Insert Function
CREATE OR REPLACE FUNCTION insert_events_with_consistency_delayed_lock(
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
    events_count           INT    := jsonb_array_length(events);
    lock_key               BIGINT;
    retry_count            INT    := 0;
    max_retries            INT    := 3;
    conflict_detected      BOOLEAN := FALSE;
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

    EXECUTE format('SET search_path TO %I', schema);
    
    -- Prepare lock key for potential use
    lock_key := ('x' || substr(md5(stream), 1, 15))::bit(60)::bigint;

    -- Retry loop for conflict resolution
    WHILE retry_count <= max_retries LOOP
        BEGIN
            -- Try CTE approach first (optimistic path)
            RETURN QUERY
            WITH 
            -- Step 1: Calculate current frontier at insert time
            current_frontier AS (
                SELECT 
                    CASE 
                        WHEN stream_criteria IS NOT NULL THEN
                            -- For criteria-based streams, check both filtered and actual versions
                            COALESCE(
                                MAX(CASE WHEN data @> ANY (SELECT jsonb_array_elements(stream_criteria)) 
                                         THEN stream_version ELSE NULL END), 
                                -1
                            )
                        ELSE
                            -- For regular streams, just get max version
                            COALESCE(MAX(stream_version), -1)
                    END as current_version,
                    -- Always get the actual frontier for insertion
                    COALESCE(MAX(stream_version), -1) as insertion_frontier
                FROM orisun_es_event 
                WHERE stream_name = stream
            ),
            -- Step 2: Validate expected version
            version_validation AS (
                SELECT 
                    cf.current_version,
                    cf.insertion_frontier,
                    CASE 
                        WHEN cf.current_version = expected_stream_version THEN TRUE
                        ELSE FALSE
                    END as version_valid
                FROM current_frontier cf
            ),
            -- Step 3: Generate sequence values for events
            sequence_values AS (
                SELECT 
                    generate_series(1, events_count) as seq_num,
                    nextval('orisun_es_event_global_id_seq') as global_id
            ),
            -- Step 4: Insert events (only if version is valid)
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
                    vv.insertion_frontier + sv.seq_num,
                    current_tx_id,
                    (e ->> 'event_id')::UUID,
                    sv.global_id,
                    e ->> 'event_type',
                    COALESCE(e -> 'data', '{}'),
                    COALESCE(e -> 'metadata', '{}')
                FROM version_validation vv
                CROSS JOIN jsonb_array_elements(events) WITH ORDINALITY AS e(event, seq_num)
                JOIN sequence_values sv ON sv.seq_num = e.seq_num
                WHERE vv.version_valid = TRUE  -- Only insert if version check passes
                RETURNING global_id
            )
            -- Step 5: Return results
            SELECT 
                CASE 
                    WHEN EXISTS (SELECT 1 FROM inserted_events) THEN
                        expected_stream_version + events_count
                    ELSE
                        -- This should trigger the exception below
                        -1
                END,
                current_tx_id,
                COALESCE((SELECT MAX(global_id) FROM inserted_events), -1)
            FROM version_validation vv
            WHERE vv.version_valid = TRUE;
            
            -- If we get here, the CTE approach succeeded
            RETURN;
            
        EXCEPTION
            WHEN others THEN
                -- Check if this is a version conflict or other error
                IF SQLSTATE = '23505' OR  -- unique_violation (potential race condition)
                   NOT EXISTS (SELECT 1 FROM (
                       WITH current_check AS (
                           SELECT 
                               CASE 
                                   WHEN stream_criteria IS NOT NULL THEN
                                       COALESCE(
                                           MAX(CASE WHEN data @> ANY (SELECT jsonb_array_elements(stream_criteria)) 
                                                    THEN stream_version ELSE NULL END), 
                                           -1
                                       )
                                   ELSE
                                       COALESCE(MAX(stream_version), -1)
                               END as current_version
                           FROM orisun_es_event 
                           WHERE stream_name = stream
                       )
                       SELECT 1 FROM current_check WHERE current_version = expected_stream_version
                   ) sub) THEN
                    
                    conflict_detected := TRUE;
                    retry_count := retry_count + 1;
                    
                    -- If we've detected a conflict, fall back to advisory lock approach
                    IF retry_count <= max_retries THEN
                        -- Acquire advisory lock for conflict resolution
                        PERFORM pg_advisory_xact_lock(lock_key);
                        
                        -- Re-check version under lock
                        DECLARE
                            locked_current_version BIGINT;
                        BEGIN
                            SELECT 
                                CASE 
                                    WHEN stream_criteria IS NOT NULL THEN
                                        COALESCE(
                                            MAX(CASE WHEN data @> ANY (SELECT jsonb_array_elements(stream_criteria)) 
                                                     THEN stream_version ELSE NULL END), 
                                            -1
                                        )
                                    ELSE
                                        COALESCE(MAX(stream_version), -1)
                                END
                            INTO locked_current_version
                            FROM orisun_es_event 
                            WHERE stream_name = stream;
                            
                            IF locked_current_version <> expected_stream_version THEN
                                RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
                                    expected_stream_version, locked_current_version;
                            END IF;
                            
                            -- Continue with the retry under lock protection
                        END;
                    ELSE
                        -- Max retries exceeded
                        RAISE EXCEPTION 'OptimisticConcurrencyException:MaxRetriesExceeded: Unable to resolve version conflict after % attempts', max_retries;
                    END IF;
                ELSE
                    -- Re-raise non-conflict errors
                    RAISE;
                END IF;
        END;
    END LOOP;
    
    -- This should never be reached
    RAISE EXCEPTION 'Unexpected end of retry loop';
END;
$$;

-- Performance monitoring function for the delayed lock approach
CREATE OR REPLACE FUNCTION get_delayed_lock_stats()
RETURNS TABLE(
    metric_name TEXT,
    metric_value BIGINT
)
LANGUAGE plpgsql
STABLE AS
$$
BEGIN
    RETURN QUERY
    SELECT 'total_streams'::TEXT, COUNT(DISTINCT stream_name)::BIGINT FROM orisun_es_event
    UNION ALL
    SELECT 'total_events'::TEXT, COUNT(*)::BIGINT FROM orisun_es_event
    UNION ALL
    SELECT 'avg_events_per_stream'::TEXT, 
           CASE WHEN COUNT(DISTINCT stream_name) > 0 
                THEN (COUNT(*) / COUNT(DISTINCT stream_name))::BIGINT 
                ELSE 0 END 
    FROM orisun_es_event
    UNION ALL
    SELECT 'streams_with_criteria'::TEXT, 
           COUNT(DISTINCT stream_name)::BIGINT 
    FROM orisun_es_event 
    WHERE data ? 'criteria' OR jsonb_typeof(data) = 'object';
END;
$$;

-- Query function (same as optimized version)
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
    SET LOCAL work_mem = '64MB';

    -- Optimized query paths
    IF stream_name IS NOT NULL AND criteria_array IS NULL AND after_position IS NULL THEN
        RETURN QUERY EXECUTE format(
            $q$
            SELECT * FROM %4$sorisun_es_event
            WHERE stream_name = $1
            AND ($2 IS NULL OR stream_version %3$s $2)
            ORDER BY stream_version %5$s
            LIMIT $6
            $q$,
            op, sort_dir, schema_prefix
        ) USING stream_name, from_stream_version, effective_limit;
        
    ELSE
        RETURN QUERY EXECUTE format(
            $q$
            SELECT * FROM %8$sorisun_es_event
            WHERE 
                ($1 IS NULL OR stream_name = $1) AND
                ($2 IS NULL OR stream_version %4$s $2) AND
                ($7 IS NULL OR data @> ANY (SELECT jsonb_array_elements($7))) AND
                ($3 IS NULL OR $5 IS NULL OR 
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

-- Supporting tables (same as original)
CREATE TABLE IF NOT EXISTS orisun_last_published_event_position
(
    boundary       TEXT PRIMARY KEY,
    transaction_id BIGINT NOT NULL DEFAULT 0,
    global_id      BIGINT NOT NULL DEFAULT 0,
    date_created   TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    date_updated   TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

CREATE TABLE IF NOT EXISTS events_count (
    id VARCHAR(255) PRIMARY KEY,
    event_count BIGINT NOT NULL,
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