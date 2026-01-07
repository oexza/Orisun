CREATE EXTENSION IF NOT EXISTS btree_gin;

-- Table & Sequence
CREATE TABLE IF NOT EXISTS orisun_es_event
(
    transaction_id BIGINT                                         NOT NULL,
    global_id      BIGINT PRIMARY KEY,
    event_id       UUID                                           NOT NULL,
    event_type     TEXT                                           NOT NULL CHECK (event_type <> ''),
    data           JSONB                                          NOT NULL,
    metadata       JSONB,
    date_created   TIMESTAMPTZ DEFAULT (NOW() AT TIME ZONE 'UTC') NOT NULL
);

ALTER TABLE orisun_es_event
    DROP COLUMN IF EXISTS stream_version,
    DROP COLUMN IF EXISTS stream_name;
DROP INDEX IF EXISTS idx_stream_global_id;

CREATE SEQUENCE IF NOT EXISTS orisun_es_event_global_id_seq
    START WITH 0
    MINVALUE 0
    OWNED BY orisun_es_event.global_id;

-- Indexes
DROP INDEX IF EXISTS idx_stream;
DROP INDEX IF EXISTS idx_stream_tran_idglobal_id;
DROP INDEX IF EXISTS idx_stream_tags;
DROP INDEX IF EXISTS idx_stream_data_gin;
DROP INDEX IF EXISTS idx_global_order;
CREATE INDEX IF NOT EXISTS idx_global_order_covering ON orisun_es_event (transaction_id DESC, global_id DESC) INCLUDE (data);
CREATE INDEX IF NOT EXISTS idx_tags ON orisun_es_event
    USING GIN (data) WITH (fastupdate = true, gin_pending_list_limit = '128');

-- Insert Function
--
-- This function inserts a batch of events into a stream in the event store while enforcing
-- stream consistency. It locks the stream or specific criteria keys for the duration
-- of the transaction to prevent concurrent modifications.
--
-- Parameters:
--   schema (TEXT): The schema to use for the event store table.
--   query (JSONB): A JSON object containing query metadata.
--     - criteria (JSONB): Optional JSON object for granular locking.
--   events (JSONB): A JSON array of events to insert. Each event is a JSON object
--     with the following properties:
--     - event_id (UUID): A unique identifier for the event.
--     - event_type (TEXT): The type of the event.
--     - data (JSONB): The event data.
--     - metadata (JSONB): Optional event metadata.
--
-- Returns:
--   TABLE: A table with the following columns:
-- example input to this function:
-- Parameters: $1 = 'test2', $2 = '{"criteria": [{"username": "iskaba"}], 
-- "expected_version": 17}', $3 = '[{"data": {"username": "iskaba", "eventType": "UNAA"}, "event_id": "0191b93c-5f3c-75c8-92ce-5a3300709178", "metadata": {"id": "1234"}, "event_type": "UNAA"}, {"data": {"username": "iskaba", "eventType": "UNAA"}, "event_id": "0191b93c-5f3c-75c8-92ce-5a3300709178", "metadata": {"id": "1234"}, "event_type": "UNAA"}]'

CREATE OR REPLACE FUNCTION insert_events_with_consistency_v3(
    schema TEXT,
    query JSONB,
    events JSONB
)
    RETURNS TABLE
            (
                new_global_id         BIGINT,
                latest_transaction_id BIGINT,
                latest_global_id      BIGINT
            )
    LANGUAGE plpgsql
AS
$$
DECLARE
    criteria              JSONB;
    expected_tx_id        BIGINT;
    expected_gid          BIGINT;
    current_tx_id         BIGINT;
    latest_tx_id          BIGINT;
    latest_gid            BIGINT;
    key_record            TEXT;
    criteria_tags         TEXT[];
    new_global_id         BIGINT;
    latest_transaction_id BIGINT;
    latest_global_id      BIGINT;
BEGIN
    criteria := query -> 'criteria';
    expected_tx_id := (query -> 'expected_position' ->> 'transaction_id')::BIGINT;
    expected_gid := (query -> 'expected_position' ->> 'global_id')::BIGINT;
    current_tx_id := pg_current_xact_id()::TEXT::BIGINT;

    IF jsonb_array_length(events) = 0 THEN
        RAISE EXCEPTION 'Events array cannot be empty';
    END IF;

    EXECUTE format('SET search_path TO %I', schema);

    -- If criteria is present then we acquire granular locks for each criterion.
    -- This is to ensure that we don't block other insert operations
    -- having non-overlapping criteria.
    -- Each criterion object is locked as a unit (not individual fields within it).
    IF criteria IS NOT NULL THEN
        -- Extract all unique criteria (each criterion is one lock)
        SELECT ARRAY_AGG(DISTINCT criterion::text)
        INTO criteria_tags
        FROM jsonb_array_elements(criteria) AS criterion;

        -- Lock key-value pairs in alphabetical order (deadlock prevention)
        IF criteria_tags IS NOT NULL THEN
            criteria_tags := ARRAY(
                    SELECT DISTINCT unnest(criteria_tags)
                    ORDER BY 1 -- Alphabetical sort to ensure consistent lock order and deadlock prevention.
                             );

            FOREACH key_record IN ARRAY criteria_tags
                LOOP
                    PERFORM pg_advisory_xact_lock(('x' || substr(md5(key_record), 1, 15))::bit(60)::bigint);
                END LOOP;
        END IF;

        -- version check
        SELECT DISTINCT oe.transaction_id, oe.global_id
        INTO latest_tx_id, latest_gid
        FROM orisun_es_event oe
                 CROSS JOIN LATERAL jsonb_array_elements(criteria) AS crit(elem)
        WHERE oe.data @> crit.elem
        ORDER BY oe.transaction_id DESC, oe.global_id DESC
        LIMIT 1;

        IF latest_tx_id IS NULL THEN
            latest_tx_id := -1;
            latest_gid := -1;
        END IF;

        -- If expected_position is not provided, we set the default.
        IF expected_tx_id IS NULL OR expected_gid IS NULL THEN
            expected_tx_id := -1;
            expected_gid := -1;
        END IF;

        IF latest_tx_id <> expected_tx_id OR latest_gid <> expected_gid THEN
            RAISE EXCEPTION 'OptimisticConcurrencyException:StreamVersionConflict: Expected (%, %), Actual (%, %)',
                expected_tx_id, expected_gid, latest_tx_id, latest_gid;
        END IF;
    END IF;

    -- CTE-based insert pattern
    WITH inserted_events AS (
        INSERT INTO orisun_es_event (
                                     transaction_id,
                                     event_id,
                                     global_id,
                                     event_type,
                                     data,
                                     metadata
            )
            SELECT current_tx_id,
                   (e ->> 'event_id')::UUID,
                   nextval('orisun_es_event_global_id_seq'),
                   e ->> 'event_type',
                   CASE
                       WHEN jsonb_typeof(e -> 'data') = 'string' THEN (e ->> 'data')::jsonb
                       ELSE COALESCE(e -> 'data', '{}')
                       END,
                   CASE
                       WHEN jsonb_typeof(e -> 'metadata') = 'string' THEN (e ->> 'metadata')::jsonb
                       ELSE COALESCE(e -> 'metadata', '{}')
                       END
            FROM jsonb_array_elements(events) AS e
            RETURNING global_id),
         max_global_id AS (SELECT MAX(global_id) as max_seq_overall, COUNT(*) as inserted_count
                           FROM inserted_events)
    SELECT max_seq_overall, current_tx_id, max_seq_overall
    INTO new_global_id, latest_transaction_id, latest_global_id
    FROM max_global_id;

    RETURN QUERY SELECT new_global_id, latest_transaction_id, latest_global_id;
END;
$$;

-- Query Function
CREATE OR REPLACE FUNCTION get_matching_events_v3(
    schema TEXT,
    criteria JSONB DEFAULT NULL,
    after_position JSONB DEFAULT NULL,
    sort_dir TEXT DEFAULT 'ASC',
    max_count INT DEFAULT 1000
) RETURNS SETOF orisun_es_event
    LANGUAGE plpgsql
    STABLE AS
$$
DECLARE
    op             TEXT  := CASE WHEN sort_dir = 'ASC' THEN '>' ELSE '<' END;
    schema_prefix  TEXT;
    criteria_array JSONB := criteria -> 'criteria';
    tx_id          TEXT  := (after_position ->> 'transaction_id')::text;
    global_id      TEXT  := (after_position ->> 'global_id')::text;
BEGIN
    IF sort_dir NOT IN ('ASC', 'DESC') THEN
        RAISE EXCEPTION 'Invalid sort direction: "%"', sort_dir;
    END IF;

    schema_prefix := quote_ident(schema) || '.';

    RETURN QUERY EXECUTE format(
            $q$
        SELECT * FROM %10$sorisun_es_event
        WHERE
            (%2$L::JSONB IS NULL OR data @> ANY (
                SELECT jsonb_array_elements(%2$L)
            )) AND
            (%3$L IS NULL OR (
                    (transaction_id, global_id) %4$s= (
                        %5$L::BIGINT, 
                        %6$L::BIGINT
                    )%7$s
                )
            )
        ORDER BY transaction_id %8$s, global_id %8$s
        LIMIT %9$L
        $q$,
            '',
            criteria_array,
            after_position,
            op,
            tx_id,
            global_id,
            format(' AND %L::xid8 < (pg_snapshot_xmin(pg_current_snapshot()))',
                   tx_id
            ),
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

CREATE TABLE IF NOT EXISTS events_count
(
    id          VARCHAR(255) PRIMARY KEY,
    event_count VARCHAR(255) NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projector_checkpoint
(
    id               VARCHAR(255) PRIMARY KEY,
    name             VARCHAR(255) UNIQUE NOT NULL,
    commit_position  BIGINT              NOT NULL,
    prepare_position BIGINT              NOT NULL
);