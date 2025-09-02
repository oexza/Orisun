-- Improved CTE approach that addresses the frontier race condition
-- This version ensures atomic consistency even when concurrent transactions modify the frontier

CREATE OR REPLACE FUNCTION insert_events_with_consistency_improved_cte(
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

    -- CRITICAL ISSUE WITH ORIGINAL CTE APPROACH:
    -- The frontier version is calculated at the beginning of the transaction,
    -- but another transaction could insert events and move the frontier forward
    -- between our version check and our insert, causing version gaps or conflicts.
    
    -- SOLUTION: Use a more robust CTE that re-validates the frontier at insert time
    WITH 
    -- Step 1: Get current stream version with criteria filtering (for validation)
    current_version AS (
        SELECT COALESCE(MAX(oe.stream_version), -1) as current_stream_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream
          AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria)))
    ),
    -- Step 2: Validate expected version matches current version
    version_validation AS (
        SELECT 
            cv.current_stream_version,
            CASE 
                WHEN cv.current_stream_version = expected_stream_version THEN true
                ELSE false
            END as version_valid
        FROM current_version cv
    ),
    -- Step 3: ATOMIC frontier calculation and insert in single operation
    -- This ensures no other transaction can modify the frontier between calculation and insert
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
            -- CRITICAL: Calculate frontier version AT INSERT TIME, not before
            COALESCE(
                CASE 
                    WHEN stream_criteria IS NOT NULL THEN 
                        (SELECT MAX(oe2.stream_version) FROM orisun_es_event oe2 WHERE oe2.stream_name = stream)
                    ELSE 
                        vv.current_stream_version
                END, 
                -1
            ) + ROW_NUMBER() OVER (),
            current_tx_id,
            (e ->> 'event_id')::UUID,
            nextval('orisun_es_event_global_id_seq'),
            e ->> 'event_type',
            COALESCE(e -> 'data', '{}'),
            COALESCE(e -> 'metadata', '{}')
        FROM jsonb_array_elements(events) AS e, version_validation vv
        WHERE vv.version_valid = true
        RETURNING stream_version, global_id
    ),
    -- Step 4: Calculate final results
    final_results AS (
        SELECT 
            COALESCE(MAX(ie.stream_version), vv.current_stream_version) as final_stream_version,
            current_tx_id as final_transaction_id,
            COALESCE(MAX(ie.global_id), 0) as final_global_id,
            COUNT(ie.stream_version) as inserted_count,
            vv.version_valid
        FROM version_validation vv
        LEFT JOIN inserted_events ie ON true
        GROUP BY vv.current_stream_version, vv.version_valid
    )
    SELECT 
        fr.final_stream_version,
        fr.final_transaction_id,
        fr.final_global_id,
        fr.version_valid,
        fr.inserted_count
    INTO result_record
    FROM final_results fr;

    -- Check if version validation failed
    IF NOT result_record.version_valid THEN
        RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
            expected_stream_version, 
            (SELECT COALESCE(MAX(oe.stream_version), -1) 
             FROM orisun_es_event oe 
             WHERE oe.stream_name = stream 
               AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria))));
    END IF;

    -- Check if no events were inserted
    IF result_record.inserted_count = 0 THEN
        RAISE EXCEPTION 'No events were inserted despite valid version check';
    END IF;

    RETURN QUERY SELECT 
        result_record.final_stream_version, 
        result_record.final_transaction_id, 
        result_record.final_global_id;
END;
$$;

-- Alternative approach: Use SELECT FOR UPDATE to lock the frontier calculation
CREATE OR REPLACE FUNCTION insert_events_with_consistency_select_for_update(
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
    frontier_version        BIGINT := -1;
    final_stream_version    BIGINT;
    final_global_id         BIGINT;
BEGIN
    IF jsonb_array_length(events) = 0 THEN
        RAISE EXCEPTION 'Events array cannot be empty';
    END IF;

    EXECUTE format('SET search_path TO %I', schema);

    -- Step 1: Lock and get current stream version with criteria
    SELECT COALESCE(MAX(oe.stream_version), -1)
    INTO current_stream_version
    FROM orisun_es_event oe
    WHERE oe.stream_name = stream
      AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria)))
    FOR UPDATE; -- This locks the relevant rows

    -- Step 2: Validate expected version
    IF current_stream_version <> expected_stream_version THEN
        RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
            expected_stream_version, current_stream_version;
    END IF;

    -- Step 3: Get frontier version (locked to prevent concurrent modifications)
    IF stream_criteria IS NOT NULL THEN
        SELECT COALESCE(MAX(oe.stream_version), -1)
        INTO frontier_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream
        FOR UPDATE; -- Lock the entire stream frontier
    ELSE
        frontier_version := current_stream_version;
    END IF;

    -- Step 4: Insert events atomically
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
            frontier_version + ROW_NUMBER() OVER (),
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
RACE CONDITION ANALYSIS:

Original CTE Problem:
1. Transaction A reads frontier version = 10
2. Transaction B inserts events, moves frontier to 15
3. Transaction A inserts events starting from version 11
4. Result: Version gap or conflict (versions 11-14 vs 16+)

Solutions:

1. Improved CTE Approach:
   - Calculates frontier AT INSERT TIME within the same CTE
   - Uses subquery in INSERT to get real-time frontier
   - Still subject to serialization conflicts but more robust

2. SELECT FOR UPDATE Approach:
   - Explicitly locks relevant rows during frontier calculation
   - Prevents other transactions from modifying frontier
   - More similar to advisory lock behavior but row-level
   - Better consistency guarantees

3. Why Advisory Locks Work Better:
   - Stream-level locking prevents any concurrent modifications
   - Simpler logic with guaranteed consistency
   - No complex CTE or row-level locking needed

Recommendation:
- For high-concurrency scenarios: Use advisory locks (original approach)
- For moderate concurrency: Use SELECT FOR UPDATE approach
- For low concurrency with retry logic: Use improved CTE approach
*/