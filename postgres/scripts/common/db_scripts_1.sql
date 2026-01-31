CREATE EXTENSION IF NOT EXISTS btree_gin;

-- Initialize Boundary Tables Function
--
-- This function creates all boundary-prefixed tables, indexes, and sequences
-- for a given boundary in a specified schema.
--
-- Parameters:
--   boundary_name (TEXT): The boundary name to use as a prefix for all tables
--   schema_name (TEXT): The PostgreSQL schema where tables will be created
--
-- Example:
--   SELECT initialize_boundary_tables('orders', 'public');
--
-- Creates tables like: public.orders_orisun_es_event, public.orders_events_count, etc.

CREATE OR REPLACE FUNCTION initialize_boundary_tables(
    boundary_name TEXT,
    schema_name TEXT
) RETURNS VOID AS
$$
BEGIN
    -- Validate boundary_name is a valid PostgreSQL identifier
    -- - Must start with letter or underscore
    -- - Can contain letters, digits, underscores
    -- - Max length 63 characters
    IF boundary_name ~ '^[^a-zA-Z_]' OR boundary_name ~ '[^a-zA-Z0-9_]' OR length(boundary_name) > 63 THEN
        RAISE EXCEPTION 'Invalid boundary name: %. Must start with letter or underscore, contain only letters/digits/underscores, and be 63 chars or less', boundary_name;
    END IF;

    -- Create event table
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.%I (
        transaction_id BIGINT NOT NULL,
        global_id      BIGINT PRIMARY KEY,
        event_id       UUID NOT NULL,
        event_type     TEXT NOT NULL CHECK (event_type <> ''''),
        data           JSONB NOT NULL,
        metadata       JSONB,
        date_created   TIMESTAMPTZ DEFAULT (NOW() AT TIME ZONE ''UTC'') NOT NULL
    )', schema_name, boundary_name || '_orisun_es_event');

    -- Create sequence
    EXECUTE format('CREATE SEQUENCE IF NOT EXISTS %I.%I
        START WITH 0
        MINVALUE 0
        OWNED BY %I.%I.%I',
                   schema_name, boundary_name || '_orisun_es_event_global_id_seq',
                   schema_name, boundary_name || '_orisun_es_event', 'global_id');

    -- Create indexes
    EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I.%I (transaction_id DESC, global_id DESC) INCLUDE (data)',
                   boundary_name || '_idx_global_order_covering', schema_name, boundary_name || '_orisun_es_event');

    EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I.%I
        USING GIN (data) WITH (fastupdate = true, gin_pending_list_limit = ''128'')',
                   boundary_name || '_idx_tags', schema_name, boundary_name || '_orisun_es_event');

    -- Create last published event position table
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.%I (
        boundary       TEXT PRIMARY KEY,
        transaction_id BIGINT NOT NULL DEFAULT 0,
        global_id      BIGINT NOT NULL DEFAULT 0,
        date_created   TIMESTAMPTZ DEFAULT NOW() NOT NULL,
        date_updated   TIMESTAMPTZ DEFAULT NOW() NOT NULL
    )', schema_name, boundary_name || '_orisun_last_published_event_position');

    -- Create events count table
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.%I (
        id          VARCHAR(255) PRIMARY KEY,
        event_count VARCHAR(255) NOT NULL,
        created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        updated_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
    )', schema_name, boundary_name || '_events_count');

    -- Create projector checkpoint table
    EXECUTE format('CREATE TABLE IF NOT EXISTS %I.%I (
        id               VARCHAR(255) PRIMARY KEY,
        name             VARCHAR(255) UNIQUE NOT NULL,
        commit_position  BIGINT NOT NULL,
        prepare_position BIGINT NOT NULL
    )', schema_name, boundary_name || '_projector_checkpoint');
END;
$$ LANGUAGE plpgsql;


-- Insert Events with Consistency Function
--
-- This function inserts a batch of events into a boundary-prefixed event store table
-- while enforcing stream consistency via optimistic locking.
--
-- Parameters:
--   boundary_name (TEXT): The boundary name (used to find the prefixed table)
--   schema (TEXT): The schema to use for the event store table
--   query (JSONB): A JSON object containing query metadata
--     - criteria (JSONB): Optional JSON object for granular locking
--   events (JSONB): A JSON array of events to insert
--
-- Returns:
--   TABLE: A table with the new_global_id, latest_transaction_id, latest_global_id

CREATE OR REPLACE FUNCTION insert_events_with_consistency_v3(
    boundary_name TEXT,
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
    prefixed_seq_name     TEXT;
BEGIN
    criteria := query -> 'criteria';
    expected_tx_id := (query -> 'expected_position' ->> 'transaction_id')::BIGINT;
    expected_gid := (query -> 'expected_position' ->> 'global_id')::BIGINT;
    current_tx_id := pg_current_xact_id()::TEXT::BIGINT;

    -- Build prefixed sequence name (plain name, will be escaped when used)
    prefixed_seq_name := boundary_name || '_orisun_es_event_global_id_seq';

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
                    PERFORM pg_advisory_xact_lock(hashtext(key_record));
                END LOOP;
        END IF;

        -- version check - use dynamic SQL to query the prefixed table
        EXECUTE format('
            SELECT DISTINCT oe.transaction_id, oe.global_id
            FROM %I_orisun_es_event oe
                     CROSS JOIN LATERAL jsonb_array_elements($1) AS crit(elem)
            WHERE oe.data @> crit.elem
            ORDER BY oe.transaction_id DESC, oe.global_id DESC
            LIMIT 1',
                       boundary_name
                ) USING criteria INTO latest_tx_id, latest_gid;

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

    -- CTE-based insert pattern - use dynamic SQL for prefixed table and sequence
    EXECUTE format('
        WITH inserted_events AS (
            INSERT INTO %I_orisun_es_event (
                                         transaction_id,
                                         event_id,
                                         global_id,
                                         event_type,
                                         data,
                                         metadata
            )
            SELECT $1,
                   (e ->> ''event_id'')::UUID,
                   nextval(%L),
                   e ->> ''event_type'',
                   CASE
                       WHEN jsonb_typeof(e -> ''data'') = ''string'' THEN (e ->> ''data'')::jsonb
                       ELSE COALESCE(e -> ''data'', ''{}'')
                       END,
                   CASE
                       WHEN jsonb_typeof(e -> ''metadata'') = ''string'' THEN (e ->> ''metadata'')::jsonb
                       ELSE COALESCE(e -> ''metadata'', ''{}'')
                       END
            FROM jsonb_array_elements($2) AS e
            RETURNING global_id
        ),
         max_global_id AS (SELECT MAX(global_id) as max_seq_overall, COUNT(*) as inserted_count
                           FROM inserted_events)
        SELECT max_seq_overall, $1, max_seq_overall
        FROM max_global_id',
                   boundary_name,
                   prefixed_seq_name
            ) USING current_tx_id, events INTO new_global_id, latest_transaction_id, latest_global_id;

    RETURN QUERY SELECT new_global_id, latest_transaction_id, latest_global_id;
END;
$$;


-- Get Matching Events Function
--
-- This function queries events from a boundary-prefixed event store table
-- based on criteria and position.
--
-- Parameters:
--   boundary_name (TEXT): The boundary name (used to find the prefixed table)
--   schema (TEXT): The schema to use
--   criteria (JSONB): Optional JSON object for filtering events
--   after_position (JSONB): Optional position to start from
--   sort_dir (TEXT): Sort direction ('ASC' or 'DESC')
--   max_count (INT): Maximum number of events to return
--
-- Returns:
--   SETOF record: The matching events

CREATE OR REPLACE FUNCTION get_matching_events_v3(
    boundary_name TEXT,
    schema TEXT,
    criteria JSONB DEFAULT NULL,
    after_position JSONB DEFAULT NULL,
    sort_dir TEXT DEFAULT 'ASC',
    max_count INT DEFAULT 1000
)
    RETURNS TABLE
            (
                transaction_id BIGINT,
                global_id      BIGINT,
                event_id       UUID,
                event_type     TEXT,
                data           JSONB,
                metadata       JSONB,
                date_created   TIMESTAMPTZ
            )
    LANGUAGE plpgsql
    STABLE
AS
$$
DECLARE
    op                   TEXT  := CASE WHEN sort_dir = 'ASC' THEN '>' ELSE '<' END;
    qualified_table_name TEXT;
    criteria_array       JSONB := criteria -> 'criteria';
    tx_id                TEXT  := (after_position ->> 'transaction_id')::text;
    global_id            TEXT  := (after_position ->> 'global_id')::text;
BEGIN
    IF sort_dir NOT IN ('ASC', 'DESC') THEN
        RAISE EXCEPTION 'Invalid sort direction: "%"', sort_dir;
    END IF;

    -- Build qualified table name
    qualified_table_name := format('%I.%I_orisun_es_event', schema, boundary_name);

    -- Use dynamic SQL to query the prefixed table
    RETURN QUERY EXECUTE format(
            $q$
        SELECT * FROM %s
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
            qualified_table_name,
            criteria_array,
            after_position,
            op,
            tx_id,
            global_id,
            CASE
                WHEN after_position IS NOT NULL AND sort_dir != 'DESC' THEN
                    format(' AND %L::xid8 < (pg_snapshot_xmin(pg_current_snapshot()))', tx_id)
                ELSE
                    ''
                END,
            sort_dir,
            LEAST(GREATEST(max_count, 1), 10000)
                         );
END;
$$;