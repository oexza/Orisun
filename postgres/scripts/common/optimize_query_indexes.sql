-- -- Indexes to optimize the query at lines 125-131 in db_scripts.sql
-- -- Query:
-- -- SELECT oe.transaction_id, oe.global_id
-- --     INTO latest_tx_id, latest_gid
-- --     FROM orisun_es_event oe
-- --     WHERE oe.stream_name = stream
-- --       AND (stream_criteria IS NULL OR data @> ANY (SELECT jsonb_array_elements(stream_criteria)))
-- --     ORDER BY oe.transaction_id DESC, oe.global_id DESC
-- --     LIMIT 1;

-- -- 1. COMPOSITE INDEX for the primary query path
-- -- This index covers the WHERE clause and ORDER BY in the most efficient way
-- CREATE INDEX IF NOT EXISTS idx_stream_data_tx_gid_desc 
-- ON orisun_es_event (stream_name, transaction_id DESC, global_id DESC);

-- -- 2. GIN INDEX for the JSONB containment check
-- -- This optimizes the "data @> ANY (SELECT jsonb_array_elements(stream_criteria))" condition
-- -- The existing idx_stream_tags is good, but we can create a more focused one
-- CREATE INDEX IF NOT EXISTS idx_data_gin 
-- ON orisun_es_event USING GIN (data);

-- -- 3. COMPOSITE INDEX with JSONB for combined filtering
-- -- This index can handle both the stream_name filter and the JSONB containment check
-- CREATE INDEX IF NOT EXISTS idx_stream_data_gin 
-- ON orisun_es_event USING GIN (stream_name, data);

-- -- 4. PARTIAL INDEX for streams with criteria (optional optimization)
-- -- If most queries use stream_criteria, this can be more efficient
-- CREATE INDEX IF NOT EXISTS idx_stream_with_criteria 
-- ON orisun_es_event (stream_name, transaction_id DESC, global_id DESC)
-- WHERE (data IS NOT NULL);

-- -- 5. ALTERNATIVE: Multi-column GIN index (PostgreSQL 9.5+)
-- -- This combines the stream_name and data in a single GIN index
-- CREATE INDEX IF NOT EXISTS idx_stream_data_multi_gin 
-- ON orisun_es_event USING GIN (stream_name gin__btree_ops, data);

-- -- ANALYZE TABLE to update statistics
-- ANALYZE orisun_es_event;

-- -- Query to verify index usage
-- EXPLAIN (ANALYZE, BUFFERS) 
-- SELECT oe.transaction_id, oe.global_id
-- FROM orisun_es_event oe
-- WHERE oe.stream_name = 'your_stream_name_here'
--   AND (NULL IS NULL OR data @> ANY (SELECT jsonb_array_elements('[]'::jsonb)))
-- ORDER BY oe.transaction_id DESC, oe.global_id DESC
-- LIMIT 1;