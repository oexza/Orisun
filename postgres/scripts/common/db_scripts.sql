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
    date_created   TIMESTAMPTZ DEFAULT NOW() NOT NULL
    -- ,
    -- tags           JSONB                     NOT NULL
);

CREATE SEQUENCE IF NOT EXISTS orisun_es_event_global_id_seq
    OWNED BY orisun_es_event.global_id;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_stream ON orisun_es_event (stream_name);
CREATE INDEX IF NOT EXISTS idx_stream_version ON orisun_es_event (stream_name, stream_version);
-- CREATE INDEX IF NOT EXISTS idx_es_event_tags ON orisun_es_event USING GIN (tags jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_global_order ON orisun_es_event (transaction_id, global_id);
CREATE INDEX IF NOT EXISTS idx_stream_version_tags ON orisun_es_event
    USING GIN (stream_name, stream_version, data);

-- Insert Function
CREATE OR REPLACE FUNCTION insert_events_with_consistency(
    schema TEXT,
    stream_info JSONB,
    events JSONB
    -- global_condition JSONB
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
    -- last_position           JSONB  := global_condition -> 'last_retrieved_position';
    -- global_criteria         JSONB  := global_condition -> 'criteria';
    current_tx_id           BIGINT := pg_current_xact_id()::TEXT::BIGINT;
    current_stream_version  BIGINT := -1;
    -- conflict_transaction_id BIGINT := NULL;
    -- conflict_global_id      BIGINT := NULL;
    -- global_keys             TEXT[];
    -- key_record              TEXT;
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
      AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria)));

    IF current_stream_version IS NULL THEN
        current_stream_version := -1;
    END IF;

    IF current_stream_version <> expected_stream_version THEN
        RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected %, Actual %',
            expected_stream_version, current_stream_version;
    END IF;

    -- select the frontier of the stream if a subset criteria was specified to ensure that the events being inserted are properly versioned.
    IF stream_criteria IS NOT NULL THEN
        SELECT MAX(oe.stream_version)
        INTO current_stream_version
        FROM orisun_es_event oe
        WHERE oe.stream_name = stream;
    END IF;

    IF current_stream_version IS NULL THEN
        current_stream_version := -1;
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
                                     metadata
            )
            SELECT stream,
                   current_stream_version + ROW_NUMBER() OVER (),
                   current_tx_id,
                   (e ->> 'event_id')::UUID,
                   nextval('orisun_es_event_global_id_seq'),
                   e ->> 'event_type',
                   COALESCE(e -> 'data', '{}'),
                   COALESCE(e -> 'metadata', '{}')
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
    criteria_array JSONB := criteria -> 'criteria';
    tx_id TEXT := (after_position ->> 'transaction_id')::text;
    global_id TEXT := (after_position ->> 'global_id')::text;
BEGIN
    IF sort_dir NOT IN ('ASC', 'DESC') THEN
        RAISE EXCEPTION 'Invalid sort direction: "%"', sort_dir;
    END IF;
    
    -- Instead of SET search_path, use schema prefix in the query
    schema_prefix := quote_ident(schema) || '.';

    -- Optimize the query based on which parameters are provided
    IF stream_name IS NOT NULL AND criteria_array IS NULL AND after_position IS NULL THEN
        -- Optimized path for stream-only queries (common case)
        RETURN QUERY EXECUTE format(
            $q$
            SELECT * FROM %5$sorisun_es_event
            WHERE stream_name = %1$L
            AND (%2$L IS NULL OR stream_version %3$s= %2$L)
            ORDER BY transaction_id %4$s, global_id %4$s
            LIMIT %6$L
            $q$,
            stream_name,
            from_stream_version,
            op,
            sort_dir,
            schema_prefix,
            LEAST(GREATEST(max_count, 1), 10000)
        );
    ELSE
        -- General case with all possible filters
        RETURN QUERY EXECUTE format(
            $q$
            SELECT * FROM %11$sorisun_es_event
            WHERE 
                (%1$L IS NULL OR stream_name = %1$L) AND
                (%2$L IS NULL OR stream_version %4$s= %2$L) AND
                (%8$L::JSONB IS NULL OR data @> ANY (
                    SELECT jsonb_array_elements(%8$L)
                )) AND
                (%3$L IS NULL OR (
                        (transaction_id, global_id) %4$s (
                            %5$L::BIGINT, 
                            %6$L::BIGINT
                        )%7$s
                    )
                )
            ORDER BY transaction_id %9$s, global_id %9$s
            LIMIT %10$L
            $q$,
            stream_name,
            from_stream_version,
            after_position,
            op,
            tx_id,
            global_id,
            format(' AND %L::xid8 < (pg_snapshot_xmin(pg_current_snapshot()))', 
                    tx_id
            ),
            criteria_array,
            sort_dir,
            LEAST(GREATEST(max_count, 1), 10000),
            schema_prefix
        );
    END IF;
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