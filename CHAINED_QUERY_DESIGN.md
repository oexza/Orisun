# Chained Query System Design

## Overview

The chained query system extends the current enhanced query functionality to support multi-hop event traversal. Instead of a single source → extract → target relationship, queries can now chain multiple steps together, allowing complex event relationship navigation.

## Architecture

### Query Chain Structure

```
Step 1: Find source event by username
  ↓ Extract: userCreatedId
Step 2: Find UserUpdated event by userCreatedId
  ↓ Extract: userUpdatedId  
Step 3: Find ProfileChanged event by userUpdatedId
  ↓ Extract: profileId
Step 4: Find final events by profileId
```

### Core Concepts

1. **Query Chain**: A sequence of linked query steps
2. **Query Step**: Individual hop in the chain (source criterion → extract field → target field)
3. **Chain Result**: Accumulated results from all steps in the chain
4. **Field Mapping**: How extracted values flow between steps

## Data Structures

### 1. ChainedQuery

```protobuf
message ChainedQuery {
  repeated QueryStep steps = 1;
  ChainExecutionMode execution_mode = 2;
  int32 max_results_per_step = 3;
  bool return_intermediate_results = 4;
}

enum ChainExecutionMode {
  SEQUENTIAL = 0;     // Execute steps one by one
  PARALLEL = 1;       // Execute independent steps in parallel
  BATCH_OPTIMIZED = 2; // Optimize for batch processing
}
```

### 2. QueryStep

```protobuf
message QueryStep {
  string step_id = 1;
  Criterion source_criterion = 2;
  string extract_field = 3;
  string target_field = 4;
  repeated string event_types = 5;
  StepCondition condition = 6;
  repeated string depends_on_steps = 7; // For parallel execution
}

message StepCondition {
  ConditionType type = 1;
  string field_name = 2;
  repeated string allowed_values = 3;
  string regex_pattern = 4;
}

enum ConditionType {
  ALWAYS = 0;
  FIELD_EXISTS = 1;
  FIELD_EQUALS = 2;
  FIELD_MATCHES_REGEX = 3;
  FIELD_IN_LIST = 4;
}
```

### 3. ChainedQueryResult

```protobuf
message ChainedQueryResult {
  repeated StepResult step_results = 1;
  repeated Event final_events = 2;
  ChainExecutionStats stats = 3;
}

message StepResult {
  string step_id = 1;
  repeated Event events = 2;
  map<string, string> extracted_values = 3;
  int32 events_found = 4;
  int64 execution_time_ms = 5;
}

message ChainExecutionStats {
  int32 total_steps_executed = 1;
  int32 total_events_processed = 2;
  int64 total_execution_time_ms = 3;
  repeated string failed_steps = 4;
}
```

## Query Examples

### Example 1: User Lifecycle Tracking

```yaml
Query: "Find all events related to a user's complete lifecycle"
Steps:
  1. Find UserCreated by username → extract userCreatedId
  2. Find UserUpdated by userCreatedId → extract userUpdatedId
  3. Find ProfileChanged by userUpdatedId → extract profileId
  4. Find ActivityLogged by profileId → return all events
```

### Example 2: Organization Hierarchy Traversal

```yaml
Query: "Find all events in an organization hierarchy"
Steps:
  1. Find Organization by name → extract orgId
  2. Find Department by orgId → extract deptId
  3. Find Team by deptId → extract teamId
  4. Find UserAssigned by teamId → return all events
```

### Example 3: Transaction Flow Tracking

```yaml
Query: "Track a transaction through multiple systems"
Steps:
  1. Find PaymentInitiated by transactionId → extract paymentId
  2. Find PaymentProcessed by paymentId → extract processingId
  3. Find PaymentCompleted by processingId → extract completionId
  4. Find NotificationSent by completionId → return all events
```

## SQL Implementation Strategy

### Recursive CTE Approach

```sql
CREATE OR REPLACE FUNCTION get_chained_events(
    chained_criteria JSONB,
    boundary_name TEXT DEFAULT NULL,
    stream_name TEXT DEFAULT NULL,
    position_start BIGINT DEFAULT NULL,
    position_end BIGINT DEFAULT NULL,
    direction TEXT DEFAULT 'ASC',
    max_count INTEGER DEFAULT 1000
) RETURNS TABLE(
    event_id UUID,
    event_type TEXT,
    event_data JSONB,
    event_metadata JSONB,
    stream_name TEXT,
    stream_position BIGINT,
    global_position BIGINT,
    created_at TIMESTAMP WITH TIME ZONE,
    step_id TEXT,
    step_order INTEGER
) AS $$
DECLARE
    step_config JSONB;
    current_step JSONB;
    step_index INTEGER := 0;
    extracted_values JSONB := '{}'::JSONB;
BEGIN
    -- Process each step in the chain
    FOR step_config IN SELECT jsonb_array_elements(chained_criteria->'steps')
    LOOP
        step_index := step_index + 1;
        
        -- Execute current step with accumulated extracted values
        RETURN QUERY
        WITH step_events AS (
            SELECT e.*
            FROM events e
            WHERE apply_step_criteria(e, step_config, extracted_values)
              AND (boundary_name IS NULL OR e.boundary = boundary_name)
              AND (stream_name IS NULL OR e.stream_name = stream_name)
              AND apply_position_filters(e, position_start, position_end)
        ),
        step_extractions AS (
            SELECT 
                se.*,
                extract_field_value(se.event_data, step_config->>'extract_field') as extracted_value
            FROM step_events se
        )
        SELECT 
            se.event_id,
            se.event_type,
            se.event_data,
            se.event_metadata,
            se.stream_name,
            se.stream_position,
            se.global_position,
            se.created_at,
            step_config->>'step_id' as step_id,
            step_index as step_order
        FROM step_extractions se;
        
        -- Update extracted values for next step
        extracted_values := update_extracted_values(extracted_values, step_config, step_index);
    END LOOP;
END;
$$ LANGUAGE plpgsql;
```

### Helper Functions

```sql
-- Apply step criteria with dynamic field mapping
CREATE OR REPLACE FUNCTION apply_step_criteria(
    event_row events,
    step_config JSONB,
    extracted_values JSONB
) RETURNS BOOLEAN AS $$
DECLARE
    criterion JSONB;
    tag JSONB;
    tag_value TEXT;
BEGIN
    -- Process source criterion with dynamic value substitution
    FOR criterion IN SELECT jsonb_array_elements(step_config->'source_criterion'->'criteria')
    LOOP
        FOR tag IN SELECT jsonb_array_elements(criterion->'tags')
        LOOP
            tag_value := tag->>'value';
            
            -- Replace with extracted value if it's a reference
            IF tag_value LIKE '${%}' THEN
                tag_value := extracted_values->>substring(tag_value from 3 for length(tag_value)-3);
            END IF;
            
            -- Apply tag matching logic
            IF NOT event_matches_tag(event_row, tag->>'key', tag_value) THEN
                RETURN FALSE;
            END IF;
        END LOOP;
    END LOOP;
    
    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;

-- Extract field value from event data
CREATE OR REPLACE FUNCTION extract_field_value(
    event_data JSONB,
    field_path TEXT
) RETURNS TEXT AS $$
BEGIN
    -- Support nested field extraction using JSON path
    RETURN event_data #>> string_to_array(field_path, '.');
END;
$$ LANGUAGE plpgsql;

-- Update extracted values for next step
CREATE OR REPLACE FUNCTION update_extracted_values(
    current_values JSONB,
    step_config JSONB,
    step_index INTEGER
) RETURNS JSONB AS $$
DECLARE
    extract_field TEXT;
    step_id TEXT;
BEGIN
    extract_field := step_config->>'extract_field';
    step_id := step_config->>'step_id';
    
    -- This would be populated from the actual step execution results
    -- For now, return the current values (implementation detail)
    RETURN current_values;
END;
$$ LANGUAGE plpgsql;
```

## Performance Considerations

### 1. Indexing Strategy

```sql
-- Composite indexes for common field combinations
CREATE INDEX idx_events_composite_fields ON events 
USING GIN ((event_data || event_metadata));

-- Specific indexes for common extraction fields
CREATE INDEX idx_events_user_created_id ON events 
((event_data->>'userCreatedId')) 
WHERE event_data ? 'userCreatedId';

CREATE INDEX idx_events_user_updated_id ON events 
((event_data->>'userUpdatedId')) 
WHERE event_data ? 'userUpdatedId';
```

### 2. Query Optimization

- **Step Batching**: Group similar steps for batch execution
- **Early Termination**: Stop chain execution if no results found
- **Result Caching**: Cache intermediate results for repeated patterns
- **Parallel Execution**: Execute independent steps concurrently

### 3. Memory Management

- **Streaming Results**: Process large result sets in chunks
- **Result Limits**: Enforce limits per step and overall
- **Garbage Collection**: Clean up intermediate results

## Error Handling

### Chain Execution Errors

1. **Step Failure**: Continue with remaining steps or abort entire chain
2. **Field Extraction Failure**: Use default values or skip step
3. **Circular References**: Detect and prevent infinite loops
4. **Resource Limits**: Handle memory and time constraints

### Recovery Strategies

```protobuf
message ChainErrorHandling {
  ErrorStrategy on_step_failure = 1;
  ErrorStrategy on_extraction_failure = 2;
  int32 max_retries = 3;
  int64 timeout_ms = 4;
}

enum ErrorStrategy {
  ABORT_CHAIN = 0;
  CONTINUE_CHAIN = 1;
  RETRY_STEP = 2;
  USE_DEFAULT_VALUE = 3;
}
```

## Integration Points

### 1. Existing Enhanced Query System

- Extend current `EnhancedQuery` to include `ChainedQuery`
- Maintain backward compatibility with single-step queries
- Reuse existing field extraction and criterion matching logic

### 2. gRPC API Extension

```protobuf
service EventStore {
  // Existing methods...
  
  rpc GetEventsChained(GetEventsChainedRequest) returns (GetEventsChainedResponse);
  rpc GetEventsChainedStream(GetEventsChainedRequest) returns (stream GetEventsChainedResponse);
}

message GetEventsChainedRequest {
  ChainedQuery chained_query = 1;
  string boundary = 2;
  string stream = 3;
  int64 position_start = 4;
  int64 position_end = 5;
  Direction direction = 6;
  int32 count = 7;
}

message GetEventsChainedResponse {
  ChainedQueryResult result = 1;
  bool has_more = 2;
  string continuation_token = 3;
}
```

## Implementation Phases

### Phase 1: Core Infrastructure
- [ ] Extend protobuf definitions
- [ ] Implement basic chained query parsing
- [ ] Create SQL function framework

### Phase 2: Query Execution
- [ ] Implement sequential chain execution
- [ ] Add field extraction and mapping
- [ ] Create result aggregation logic

### Phase 3: Optimization
- [ ] Add parallel execution support
- [ ] Implement result caching
- [ ] Add performance monitoring

### Phase 4: Advanced Features
- [ ] Conditional step execution
- [ ] Dynamic query modification
- [ ] Chain composition and reuse

## Testing Strategy

### Unit Tests
- Individual step execution
- Field extraction accuracy
- Error handling scenarios

### Integration Tests
- Multi-step chain execution
- Performance benchmarks
- Memory usage validation

### End-to-End Tests
- Real-world query scenarios
- Large dataset processing
- Concurrent chain execution

This design provides a robust foundation for implementing complex, multi-hop event traversal queries while maintaining performance and reliability.