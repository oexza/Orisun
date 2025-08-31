-- Enhanced Query Function for Method 3 Support
-- This function extends get_matching_events to support complex multi-step queries

CREATE OR REPLACE FUNCTION get_matching_events_enhanced(
    schema TEXT,
    stream_name TEXT DEFAULT NULL,
    from_stream_version BIGINT DEFAULT NULL,
    enhanced_criteria JSONB DEFAULT NULL,
    after_position JSONB DEFAULT NULL,
    sort_dir TEXT DEFAULT 'ASC',
    max_count INT DEFAULT 1000
) RETURNS SETOF orisun_es_event
    LANGUAGE plpgsql
    STABLE AS
$$
DECLARE
    query_type TEXT;
    schema_prefix TEXT;
    op TEXT := CASE WHEN sort_dir = 'ASC' THEN '>' ELSE '<' END;
    tx_id TEXT := (after_position ->> 'transaction_id')::text;
    global_id TEXT := (after_position ->> 'global_id')::text;
    source_criterion JSONB;
    extract_field TEXT;
    target_field TEXT;
    event_types_array TEXT[];
BEGIN
    IF sort_dir NOT IN ('ASC', 'DESC') THEN
        RAISE EXCEPTION 'Invalid sort direction: "%"', sort_dir;
    END IF;
    
    schema_prefix := quote_ident(schema) || '.';
    
    -- Extract query type
    query_type := enhanced_criteria ->> 'type';
    
    IF query_type = 'RELATED_EVENTS' THEN
        -- Extract parameters for related events query
        source_criterion := enhanced_criteria -> 'related_query' -> 'source_criterion';
        extract_field := enhanced_criteria -> 'related_query' ->> 'extract_field';
        target_field := enhanced_criteria -> 'related_query' ->> 'target_field';
        
        -- Convert event_types JSONB array to PostgreSQL array
        IF enhanced_criteria -> 'related_query' -> 'event_types' IS NOT NULL THEN
            SELECT array_agg(value::text)
            INTO event_types_array
            FROM jsonb_array_elements_text(enhanced_criteria -> 'related_query' -> 'event_types');
        END IF;
        
        -- Execute Method 3 style query with CTE
        RETURN QUERY EXECUTE format(
            $q$
            WITH source_event AS (
                SELECT data ->> %2$L as extracted_value
                FROM %1$sorisun_es_event
                WHERE data @> %3$L
                ORDER BY transaction_id DESC, global_id DESC
                LIMIT 1
            )
            SELECT e.*
            FROM %1$sorisun_es_event e
            CROSS JOIN source_event s
            WHERE 
                e.data ->> %4$L = s.extracted_value
                %5$s
                %6$s
                %7$s
                %8$s
            ORDER BY e.transaction_id %9$s, e.global_id %9$s
            LIMIT %10$L
            $q$,
            schema_prefix,                                    -- %1$s
            extract_field,                                    -- %2$L
            source_criterion,                                 -- %3$L
            target_field,                                     -- %4$L
            CASE                                              -- %5$s - stream filter
                WHEN stream_name IS NOT NULL THEN
                    format('AND e.stream_name = %L', stream_name)
                ELSE ''
            END,
            CASE                                              -- %6$s - version filter
                WHEN from_stream_version IS NOT NULL THEN
                    format('AND e.stream_version %s= %L', op, from_stream_version)
                ELSE ''
            END,
            CASE                                              -- %7$s - event types filter
                WHEN event_types_array IS NOT NULL THEN
                    format('AND e.event_type = ANY(%L)', event_types_array)
                ELSE ''
            END,
            CASE                                              -- %8$s - position filter
                WHEN after_position IS NOT NULL THEN
                    format('AND (e.transaction_id, e.global_id) %s= (%L::BIGINT, %L::BIGINT)', 
                           op, tx_id, global_id)
                ELSE ''
            END,
            sort_dir,                                         -- %9$s
            LEAST(GREATEST(max_count, 1), 10000)            -- %10$L
        );
        
    ELSIF query_type = 'SIMPLE' OR enhanced_criteria IS NULL THEN
        -- Fall back to original simple query logic
        RETURN QUERY SELECT * FROM get_matching_events(
            schema, 
            stream_name, 
            from_stream_version, 
            enhanced_criteria -> 'simple_query', 
            after_position, 
            sort_dir, 
            max_count
        );
        
    ELSE
        RAISE EXCEPTION 'Unsupported query type: "%"', query_type;
    END IF;
END;
$$;

-- Example usage for Method 3:
-- Find UserCreated and UserDeleted events by username "nameit"
/*
SELECT * FROM get_matching_events_enhanced(
    'public',
    NULL,
    NULL,
    '{
        "type": "RELATED_EVENTS",
        "related_query": {
            "source_criterion": {
                "username": "nameit",
                "eventType": "UserCreated"
            },
            "extract_field": "userCreatedId",
            "target_field": "userCreatedId",
            "event_types": ["UserCreated", "UserDeleted"]
        }
    }',
    NULL,
    'ASC',
    1000
);
*/

-- Backward compatibility function
-- This allows existing code to continue working while providing enhanced capabilities
CREATE OR REPLACE FUNCTION get_matching_events_v2(
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
BEGIN
    -- Check if this is an enhanced query
    IF criteria ? 'type' THEN
        RETURN QUERY SELECT * FROM get_matching_events_enhanced(
            schema, stream_name, from_stream_version, criteria, 
            after_position, sort_dir, max_count
        );
    ELSE
        -- Use original function for backward compatibility
        RETURN QUERY SELECT * FROM get_matching_events(
            schema, stream_name, from_stream_version, criteria, 
            after_position, sort_dir, max_count
        );
    END IF;
END;
$$;