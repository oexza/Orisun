-- Alternative implementation of insert_events_with_consistency using CTEs instead of advisory locks
-- This approach uses atomic CTE operations to ensure consistency without explicit locking

CREATE OR REPLACE FUNCTION insert_events_with_consistency_cte(
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
    result_record           RECORD;
BEGIN
    IF jsonb_array_length(events) = 0 THEN
        RAISE EXCEPTION 'Events array cannot be empty';
    END IF;

    EXECUTE format('SET search_path TO %I', schema);

    -- Use a single CTE-based query to atomically check version and insert events
    WITH 
    -- Step 1: Get current stream version with criteria filtering
    current_version AS (
        SELECT COALESCE(MAX(oe.stream_version), -1) as current_stream_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream
          AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria)))
    ),
    -- Step 2: Get frontier version if criteria is specified
    frontier_version AS (
        SELECT CASE 
            WHEN stream_criteria IS NOT NULL THEN 
                COALESCE(MAX(oe.stream_version), -1)
            ELSE 
                (SELECT current_stream_version FROM current_version)
        END as frontier_stream_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream
    ),
    -- Step 3: Validate expected version matches current version
    version_check AS (
        SELECT 
            cv.current_stream_version,
            fv.frontier_stream_version,
            CASE 
                WHEN cv.current_stream_version = expected_stream_version THEN true
                ELSE false
            END as version_valid
        FROM current_version cv, frontier_version fv
    ),
    -- Step 4: Insert events only if version check passes
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
            vc.frontier_stream_version + ROW_NUMBER() OVER (),
            current_tx_id,
            (e ->> 'event_id')::UUID,
            nextval('orisun_es_event_global_id_seq'),
            e ->> 'event_type',
            COALESCE(e -> 'data', '{}'),
            COALESCE(e -> 'metadata', '{}')
        FROM jsonb_array_elements(events) AS e, version_check vc
        WHERE vc.version_valid = true
        RETURNING stream_version, global_id
    ),
    -- Step 5: Calculate final results
    final_results AS (
        SELECT 
            COALESCE(MAX(ie.stream_version), vc.current_stream_version) as final_stream_version,
            current_tx_id as final_transaction_id,
            COALESCE(MAX(ie.global_id), 0) as final_global_id,
            COUNT(ie.stream_version) as inserted_count,
            vc.version_valid
        FROM version_check vc
        LEFT JOIN inserted_events ie ON true
        GROUP BY vc.current_stream_version, vc.version_valid
    )
    SELECT 
        fr.final_stream_version,
        fr.final_transaction_id,
        fr.final_global_id,
        fr.version_valid,
        fr.inserted_count
    INTO result_record
    FROM final_results fr;

    -- Check if version validation failed and raise exception
    IF NOT result_record.version_valid THEN
        RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
            expected_stream_version, 
            (SELECT COALESCE(MAX(oe.stream_version), -1) 
             FROM orisun_es_event oe 
             WHERE oe.stream_name = stream 
               AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria))));
    END IF;

    -- Check if no events were inserted (shouldn't happen if version check passed)
    IF result_record.inserted_count = 0 THEN
        RAISE EXCEPTION 'No events were inserted despite valid version check';
    END IF;

    RETURN QUERY SELECT 
        result_record.final_stream_version, 
        result_record.final_transaction_id, 
        result_record.final_global_id;
END;
$$;

-- Alternative simpler approach using SERIALIZABLE isolation level
-- This relies on PostgreSQL's SERIALIZABLE isolation to handle conflicts

CREATE OR REPLACE FUNCTION insert_events_with_consistency_serializable(
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
    final_stream_version    BIGINT;
    final_global_id         BIGINT;
BEGIN
    IF jsonb_array_length(events) = 0 THEN
        RAISE EXCEPTION 'Events array cannot be empty';
    END IF;

    EXECUTE format('SET search_path TO %I', schema);
    
    -- Set transaction isolation level to SERIALIZABLE for automatic conflict detection
    SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;

    -- Get current stream version
    SELECT COALESCE(MAX(oe.stream_version), -1)
    INTO current_stream_version
    FROM orisun_es_event oe
    WHERE oe.stream_name = stream
      AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria)));

    -- Check expected version
    IF current_stream_version <> expected_stream_version THEN
        RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
            expected_stream_version, current_stream_version;
    END IF;

    -- Get frontier version if criteria specified
    IF stream_criteria IS NOT NULL THEN
        SELECT COALESCE(MAX(oe.stream_version), -1)
        INTO current_stream_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream;
    END IF;

    -- Insert events atomically
    WITH inserted_events AS (
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
            current_stream_version + ROW_NUMBER() OVER (),
            current_tx_id,
            (e ->> 'event_id')::UUID,
            nextval('orisun_es_event_global_id_seq'),
            e ->> 'event_type',
            COALESCE(e -> 'data', '{}'),
            COALESCE(e -> 'metadata', '{}')
        FROM jsonb_array_elements(events) AS e
        RETURNING stream_version, global_id
    )
    SELECT 
        MAX(stream_version),
        MAX(global_id)
    INTO final_stream_version, final_global_id
    FROM inserted_events;

    RETURN QUERY SELECT final_stream_version, current_tx_id, final_global_id;
END;
$$;

/*
Key Differences from Advisory Lock Approach:

1. CTE-based approach:
   - Uses a single complex CTE query to atomically check version and insert
   - Relies on PostgreSQL's MVCC and transaction isolation
   - May have higher chance of serialization failures under high concurrency
   - More complex logic but avoids explicit locking

2. SERIALIZABLE isolation approach:
   - Uses PostgreSQL's SERIALIZABLE isolation level
   - Automatically detects and prevents serialization anomalies
   - Simpler code structure
   - May cause transaction retries under high concurrency
   - Better for scenarios where retry logic is acceptable

Trade-offs:
- Advisory locks provide explicit control but can cause blocking
- CTE approach is more atomic but complex
- SERIALIZABLE approach is simpler but may require retry logic in application
- Performance characteristics vary based on concurrency patterns

Recommendation:
- Use SERIALIZABLE approach for simpler code with application-level retry
- Use CTE approach for more control without blocking
- Keep advisory lock approach for maximum consistency guarantees
*/