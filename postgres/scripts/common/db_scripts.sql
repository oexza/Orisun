CREATE EXTENSION IF NOT EXISTS btree_gin;

-- Table & Sequence
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
    date_created   TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    tags           JSONB                     NOT NULL
);

CREATE SEQUENCE IF NOT EXISTS orisun_es_event_global_id_seq
    OWNED BY orisun_es_event.global_id;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_stream ON orisun_es_event (stream_name);
CREATE INDEX IF NOT EXISTS idx_stream_version ON orisun_es_event (stream_name, stream_version);
CREATE INDEX IF NOT EXISTS idx_es_event_tags ON orisun_es_event USING GIN (tags jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_global_order ON orisun_es_event (transaction_id, global_id);
CREATE INDEX IF NOT EXISTS idx_stream_version_tags ON orisun_es_event
    USING GIN (stream_name, stream_version, tags jsonb_path_ops);

-- Insert Function (With Improved Locking)
CREATE OR REPLACE FUNCTION insert_events_with_consistency(
    schema TEXT,
    stream_info JSONB,
    global_condition JSONB,
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
    last_position           JSONB  := global_condition -> 'last_retrieved_position';
    global_criteria         JSONB  := global_condition -> 'criteria';
    current_tx_id           BIGINT := pg_current_xact_id()::TEXT::BIGINT;
    current_stream_version  BIGINT := -1;
    conflict_transaction_id BIGINT := NULL;
    conflict_global_id      BIGINT := NULL;
    global_keys             TEXT[];
    key_record              TEXT;
BEGIN
    IF jsonb_array_length(events) = 0 THEN
        RAISE EXCEPTION 'Events array cannot be empty';
    END IF;

    EXECUTE format('SET search_path TO %I', schema);
    PERFORM pg_advisory_xact_lock(hashtext(stream));

    -- Stream version check
    SELECT MAX(oe.stream_version)
    INTO current_stream_version
    FROM orisun_es_event oe
    WHERE oe.stream_name = stream
      AND (stream_criteria IS NULL OR tags @> ANY (SELECT jsonb_array_elements(stream_criteria)));

    IF current_stream_version IS NULL THEN
        current_stream_version := -1;
    END IF;

    IF current_stream_version <> expected_stream_version THEN
        RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
            expected_stream_version, current_stream_version;
    END IF;

    IF global_criteria IS NOT NULL THEN
        -- Extract all unique criteria key-value pairs
        SELECT ARRAY_AGG(DISTINCT format('%s:%s', key_value.key, key_value.value))
        INTO global_keys
        FROM jsonb_array_elements(global_criteria) AS criterion,
             jsonb_each_text(criterion) AS key_value;

        -- Lock key-value pairs in alphabetical order (deadlock prevention)
        IF global_keys IS NOT NULL THEN
            global_keys := ARRAY(
                    SELECT DISTINCT unnest(global_keys)
                    ORDER BY 1 -- Alphabetical sort
                           );

            FOREACH key_record IN ARRAY global_keys
                LOOP
                    PERFORM pg_advisory_xact_lock(hashtext(key_record));
                END LOOP;
        END IF;

        -- Global position check
        IF last_position IS NOT NULL THEN
            SELECT e.transaction_id, e.global_id
            INTO conflict_transaction_id, conflict_global_id
            FROM orisun_es_event e
            WHERE e.tags @> ANY (SELECT jsonb_array_elements(global_criteria))
            ORDER BY e.transaction_id DESC, e.global_id DESC
            LIMIT 1;

            -- Handle the case when no events are found
            IF conflict_transaction_id IS NULL THEN
                conflict_transaction_id := 0;
            END IF;

            IF conflict_global_id IS NULL THEN
                conflict_global_id := 0;
            END IF;

            -- Compare with expected position
            IF conflict_transaction_id != (last_position ->> 'transaction_id')::BIGINT OR
               conflict_global_id != (last_position ->> 'global_id')::BIGINT THEN
                RAISE EXCEPTION 'OptimisticConcurrencyException: Global Conflict: Expected Position %/% but found Position %/%',
                    (last_position ->> 'transaction_id'),
                    (last_position ->> 'global_id'), conflict_transaction_id, conflict_global_id;
            END IF;
        END IF;
    END IF;

    -- select the frontier of the stream if a subset criteria was specified to ensure the next set of events are properly versioned
    IF stream_criteria IS NOT NULL THEN
        SELECT MAX(oe.stream_version)
        INTO current_stream_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream;
    END IF;

    WITH inserted_events AS (
        INSERT INTO orisun_es_event (
                                     stream_name,
                                     stream_version,
                                     transaction_id,
                                     event_id,
                                     global_id,
                                     event_type,
                                     data,
                                     metadata,
                                     tags
            )
            SELECT stream,
                   current_stream_version + ROW_NUMBER() OVER (),
                   current_tx_id,
                   (e ->> 'event_id')::UUID,
                   nextval('orisun_es_event_global_id_seq'),
                   e ->> 'event_type',
                   COALESCE(e -> 'data', '{}'),
                   COALESCE(e -> 'metadata', '{}'),
                   COALESCE(e -> 'tags', '{}')
            FROM jsonb_array_elements(events) AS e
            RETURNING jsonb_array_length(events), global_id
    )
    SELECT current_stream_version + jsonb_array_length(events), current_tx_id, MAX(global_id)
    INTO new_stream_version, latest_transaction_id, latest_global_id
    FROM inserted_events;

    RETURN QUERY SELECT new_stream_version, latest_transaction_id, latest_global_id;
END;
$$;

-- Query Function
CREATE OR REPLACE FUNCTION get_matching_events(
    schema TEXT,
    stream_name TEXT DEFAULT NULL,
    from_stream_version INT DEFAULT NULL,
    criteria JSONB DEFAULT NULL,
    after_position JSONB DEFAULT NULL,
    sort_dir TEXT DEFAULT 'ASC',
    max_count INT DEFAULT 1000
) RETURNS SETOF orisun_es_event
    LANGUAGE plpgsql
    STABLE AS
$$
DECLARE
    op TEXT := CASE WHEN sort_dir = 'ASC' THEN '>' ELSE '<' END;
    schema_prefix TEXT;
BEGIN
    IF sort_dir NOT IN ('ASC', 'DESC') THEN
        RAISE EXCEPTION 'Invalid sort direction: "%"', sort_dir;
    END IF;
    
    -- Instead of SET search_path, use schema prefix in the query
    schema_prefix := quote_ident(schema) || '.';

    RETURN QUERY EXECUTE format(
            $q$
        SELECT * FROM %10$sorisun_es_event
        WHERE 
            (%1$L IS NULL OR stream_name = %1$L) AND
            (%2$L IS NULL OR stream_version %4$s %2$L) AND
            (%3$L IS NULL OR 
             (transaction_id, global_id) %4$s (
                %5$L::BIGINT, 
                %6$L::BIGINT
             )) AND
            (%7$L::JSONB IS NULL OR tags @> ANY (
                SELECT jsonb_array_elements(%7$L)
            ))
        ORDER BY transaction_id %8$s, global_id %8$s
        LIMIT %9$L
        $q$,
            stream_name,
            from_stream_version,
            after_position,
            op,
            (after_position ->> 'transaction_id')::text,
            (after_position ->> 'global_id')::text,
            (criteria -> 'criteria'),
            sort_dir,
            LEAST(GREATEST(max_count, 1), 10000),
            schema_prefix
    );
END;
$$;

CREATE TABLE IF NOT EXISTS orisun_last_published_event_position
(
    boundary       TEXT PRIMARY KEY,
    transaction_id BIGINT NOT NULL DEFAULT 0,
    global_id      BIGINT NOT NULL DEFAULT 0,
    date_created   TIMESTAMPTZ     DEFAULT NOW() NOT NULL,
    date_updated   TIMESTAMPTZ     DEFAULT NOW() NOT NULL
);