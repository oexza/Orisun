# Delayed Lock CTE Implementation Guide

## Quick Start

### 1. Deploy the Function
```sql
-- Execute the delayed lock CTE script
\i db_scripts_delayed_lock_cte.sql

-- Verify deployment
SELECT proname, pronargs 
FROM pg_proc 
WHERE proname LIKE '%delayed_lock%';
```

### 2. Test Basic Functionality
```sql
-- Test 1: Simple insertion
SELECT insert_events_with_consistency_delayed_lock(
    'test-stream-1',
    ARRAY[
        ROW('TestEvent', '{"data": "test1"}', '{"meta": "test"}')::orisun_es_event_data
    ],
    -1,  -- expected version
    NULL -- no criteria
);

-- Test 2: Version conflict (should use advisory lock fallback)
BEGIN;
    -- This should succeed
    SELECT insert_events_with_consistency_delayed_lock(
        'test-stream-2', 
        ARRAY[ROW('Event1', '{}', '{}')::orisun_es_event_data], 
        -1, NULL
    );
    
    -- This should trigger delayed lock (version conflict)
    SELECT insert_events_with_consistency_delayed_lock(
        'test-stream-2', 
        ARRAY[ROW('Event2', '{}', '{}')::orisun_es_event_data], 
        -1, NULL  -- Wrong expected version
    );
COMMIT;
```

## Performance Testing

### Benchmark Script
```sql
-- Create benchmark function
CREATE OR REPLACE FUNCTION benchmark_delayed_lock(
    stream_count INTEGER DEFAULT 100,
    events_per_stream INTEGER DEFAULT 10,
    concurrent_sessions INTEGER DEFAULT 5
) RETURNS TABLE(
    approach TEXT,
    total_time_ms NUMERIC,
    avg_time_per_operation_ms NUMERIC,
    operations_per_second NUMERIC,
    conflict_rate NUMERIC
) AS $$
DECLARE
    start_time TIMESTAMP;
    end_time TIMESTAMP;
    total_operations INTEGER;
BEGIN
    total_operations := stream_count * events_per_stream;
    
    -- Test delayed lock approach
    start_time := clock_timestamp();
    
    FOR i IN 1..stream_count LOOP
        PERFORM insert_events_with_consistency_delayed_lock(
            'benchmark-stream-' || i,
            ARRAY[
                ROW('BenchmarkEvent', 
                    '{"iteration": ' || j || '}', 
                    '{"timestamp": "' || clock_timestamp() || '"}'
                )::orisun_es_event_data
            ],
            j - 1,  -- Expected version
            NULL
        ) FROM generate_series(1, events_per_stream) j;
    END LOOP;
    
    end_time := clock_timestamp();
    
    RETURN QUERY SELECT 
        'delayed_lock_cte'::TEXT,
        EXTRACT(EPOCH FROM (end_time - start_time)) * 1000,
        (EXTRACT(EPOCH FROM (end_time - start_time)) * 1000) / total_operations,
        total_operations / EXTRACT(EPOCH FROM (end_time - start_time)),
        0.0;  -- Calculate actual conflict rate separately
END;
$$ LANGUAGE plpgsql;

-- Run benchmark
SELECT * FROM benchmark_delayed_lock(50, 5, 3);
```

### Concurrent Load Test
```sql
-- Create concurrent test script (run in multiple sessions)
CREATE OR REPLACE FUNCTION concurrent_load_test(
    session_id INTEGER,
    operations INTEGER DEFAULT 100
) RETURNS TABLE(
    session INTEGER,
    successful_operations INTEGER,
    failed_operations INTEGER,
    avg_response_time_ms NUMERIC
) AS $$
DECLARE
    success_count INTEGER := 0;
    failure_count INTEGER := 0;
    start_time TIMESTAMP;
    operation_start TIMESTAMP;
    total_time NUMERIC := 0;
BEGIN
    FOR i IN 1..operations LOOP
        BEGIN
            operation_start := clock_timestamp();
            
            PERFORM insert_events_with_consistency_delayed_lock(
                'concurrent-stream-' || (session_id * 1000 + i),
                ARRAY[
                    ROW('ConcurrentEvent', 
                        '{"session": ' || session_id || ', "op": ' || i || '}', 
                        '{}'
                    )::orisun_es_event_data
                ],
                -1,
                NULL
            );
            
            success_count := success_count + 1;
            total_time := total_time + EXTRACT(EPOCH FROM (clock_timestamp() - operation_start)) * 1000;
            
        EXCEPTION WHEN OTHERS THEN
            failure_count := failure_count + 1;
        END;
    END LOOP;
    
    RETURN QUERY SELECT 
        session_id,
        success_count,
        failure_count,
        CASE WHEN success_count > 0 THEN total_time / success_count ELSE 0 END;
END;
$$ LANGUAGE plpgsql;

-- Run in multiple sessions simultaneously:
-- Session 1: SELECT * FROM concurrent_load_test(1, 200);
-- Session 2: SELECT * FROM concurrent_load_test(2, 200);
-- Session 3: SELECT * FROM concurrent_load_test(3, 200);
```

## Monitoring and Observability

### Performance Monitoring Function
```sql
CREATE OR REPLACE FUNCTION get_delayed_lock_performance_stats()
RETURNS TABLE(
    metric_name TEXT,
    metric_value NUMERIC,
    description TEXT
) AS $$
BEGIN
    RETURN QUERY
    WITH stats AS (
        SELECT 
            COUNT(*) as total_streams,
            SUM(CASE WHEN stream_version > 0 THEN stream_version + 1 ELSE 1 END) as total_events,
            AVG(CASE WHEN stream_version > 0 THEN stream_version + 1 ELSE 1 END) as avg_events_per_stream,
            COUNT(CASE WHEN criteria IS NOT NULL THEN 1 END) as streams_with_criteria
        FROM (
            SELECT 
                stream_name,
                MAX(stream_version) as stream_version,
                MAX(criteria) as criteria
            FROM orisun_es_event 
            GROUP BY stream_name
        ) stream_stats
    )
    SELECT 'total_streams'::TEXT, total_streams::NUMERIC, 'Total number of event streams'::TEXT FROM stats
    UNION ALL
    SELECT 'total_events'::TEXT, total_events::NUMERIC, 'Total number of events across all streams'::TEXT FROM stats
    UNION ALL
    SELECT 'avg_events_per_stream'::TEXT, avg_events_per_stream::NUMERIC, 'Average events per stream'::TEXT FROM stats
    UNION ALL
    SELECT 'streams_with_criteria'::TEXT, streams_with_criteria::NUMERIC, 'Streams using criteria-based filtering'::TEXT FROM stats;
END;
$$ LANGUAGE plpgsql;

-- Usage
SELECT * FROM get_delayed_lock_performance_stats();
```

### Lock Contention Monitoring
```sql
-- Monitor advisory lock usage
CREATE OR REPLACE VIEW delayed_lock_contention AS
SELECT 
    locktype,
    mode,
    granted,
    COUNT(*) as lock_count,
    AVG(EXTRACT(EPOCH FROM (clock_timestamp() - query_start))) as avg_wait_time_seconds
FROM pg_locks l
JOIN pg_stat_activity a ON l.pid = a.pid
WHERE locktype = 'advisory'
GROUP BY locktype, mode, granted;

-- Check current lock contention
SELECT * FROM delayed_lock_contention;
```

## Migration from Advisory Lock Approach

### Step 1: Side-by-Side Comparison
```sql
-- Create comparison test
CREATE OR REPLACE FUNCTION compare_approaches(
    test_streams INTEGER DEFAULT 10,
    events_per_stream INTEGER DEFAULT 5
) RETURNS TABLE(
    approach TEXT,
    execution_time_ms NUMERIC,
    events_inserted INTEGER
) AS $$
DECLARE
    start_time TIMESTAMP;
    end_time TIMESTAMP;
    inserted_count INTEGER;
BEGIN
    -- Test original advisory lock approach
    start_time := clock_timestamp();
    inserted_count := 0;
    
    FOR i IN 1..test_streams LOOP
        FOR j IN 1..events_per_stream LOOP
            PERFORM insert_events_with_consistency(
                'comparison-original-' || i,
                ARRAY[ROW('TestEvent', '{}', '{}')::orisun_es_event_data],
                j - 1,
                NULL
            );
            inserted_count := inserted_count + 1;
        END LOOP;
    END LOOP;
    
    end_time := clock_timestamp();
    
    RETURN QUERY SELECT 
        'original_advisory_lock'::TEXT,
        EXTRACT(EPOCH FROM (end_time - start_time)) * 1000,
        inserted_count;
    
    -- Test delayed lock CTE approach
    start_time := clock_timestamp();
    inserted_count := 0;
    
    FOR i IN 1..test_streams LOOP
        FOR j IN 1..events_per_stream LOOP
            PERFORM insert_events_with_consistency_delayed_lock(
                'comparison-delayed-' || i,
                ARRAY[ROW('TestEvent', '{}', '{}')::orisun_es_event_data],
                j - 1,
                NULL
            );
            inserted_count := inserted_count + 1;
        END LOOP;
    END LOOP;
    
    end_time := clock_timestamp();
    
    RETURN QUERY SELECT 
        'delayed_lock_cte'::TEXT,
        EXTRACT(EPOCH FROM (end_time - start_time)) * 1000,
        inserted_count;
END;
$$ LANGUAGE plpgsql;

-- Run comparison
SELECT 
    approach,
    execution_time_ms,
    events_inserted,
    ROUND((execution_time_ms / events_inserted), 2) as ms_per_event
FROM compare_approaches(20, 10)
ORDER BY execution_time_ms;
```

### Step 2: Gradual Migration
```sql
-- Create feature flag function
CREATE OR REPLACE FUNCTION should_use_delayed_lock(
    stream_name TEXT
) RETURNS BOOLEAN AS $$
BEGIN
    -- Start with test streams
    IF stream_name LIKE 'test-%' THEN
        RETURN TRUE;
    END IF;
    
    -- Gradually enable for other streams
    -- Example: enable for 10% of streams based on hash
    IF (hashtext(stream_name) % 10) = 0 THEN
        RETURN TRUE;
    END IF;
    
    RETURN FALSE;
END;
$$ LANGUAGE plpgsql;

-- Wrapper function for gradual migration
CREATE OR REPLACE FUNCTION insert_events_with_consistency_adaptive(
    stream TEXT,
    events orisun_es_event_data[],
    expected_stream_version BIGINT,
    criteria JSONB DEFAULT NULL
) RETURNS BIGINT AS $$
BEGIN
    IF should_use_delayed_lock(stream) THEN
        RETURN insert_events_with_consistency_delayed_lock(
            stream, events, expected_stream_version, criteria
        );
    ELSE
        RETURN insert_events_with_consistency(
            stream, events, expected_stream_version, criteria
        );
    END IF;
END;
$$ LANGUAGE plpgsql;
```

## Troubleshooting

### Common Issues and Solutions

#### 1. High Conflict Rate
```sql
-- Check conflict patterns
SELECT 
    stream_name,
    COUNT(*) as event_count,
    MAX(stream_version) as max_version,
    COUNT(DISTINCT DATE_TRUNC('minute', created_at)) as active_minutes
FROM orisun_es_event 
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY stream_name
HAVING COUNT(*) > 100
ORDER BY event_count DESC;

-- Solution: Consider stream partitioning for high-volume streams
```

#### 2. Lock Timeout Issues
```sql
-- Check for long-running transactions
SELECT 
    pid,
    query_start,
    state,
    query
FROM pg_stat_activity 
WHERE state = 'active' 
  AND query_start < NOW() - INTERVAL '30 seconds'
  AND query LIKE '%insert_events_with_consistency%';

-- Solution: Implement query timeout
SET statement_timeout = '30s';
```

#### 3. Memory Usage
```sql
-- Monitor memory usage for large event batches
SELECT 
    schemaname,
    tablename,
    n_tup_ins as inserts_today,
    n_tup_upd as updates_today,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_stat_user_tables 
WHERE tablename = 'orisun_es_event';

-- Solution: Limit batch sizes
-- Recommended: max 1000 events per batch
```

## Production Deployment Checklist

- [ ] **Database Backup**: Create backup before deployment
- [ ] **Function Deployment**: Deploy `insert_events_with_consistency_delayed_lock`
- [ ] **Index Verification**: Ensure all recommended indexes exist
- [ ] **Permission Setup**: Grant execute permissions to application users
- [ ] **Monitoring Setup**: Deploy performance monitoring functions
- [ ] **Load Testing**: Run concurrent load tests in staging
- [ ] **Gradual Rollout**: Start with test streams, gradually expand
- [ ] **Performance Baseline**: Establish baseline metrics
- [ ] **Rollback Plan**: Prepare rollback to original function if needed
- [ ] **Documentation**: Update application documentation

## Next Steps

1. **Deploy in staging environment**
2. **Run performance benchmarks**
3. **Test with production-like load**
4. **Monitor for 24-48 hours**
5. **Begin gradual production rollout**
6. **Collect performance metrics**
7. **Full migration after validation**

The delayed lock CTE approach provides the optimal balance of performance and consistency for most event store workloads. Start with the testing scripts above to validate the benefits in your specific environment.