# Enhanced Query System Design for Method 3 Support

## Current System Analysis

The current Orisun query system uses:
- **Protobuf**: `Query` contains `Criterion[]`, each `Criterion` contains `Tag[]` (key-value pairs)
- **SQL**: `get_matching_events` function uses `data @> ANY (SELECT jsonb_array_elements(criteria_array))`
- **Logic**: OR between criteria groups, AND within each criterion's tags

## Problem with Method 3

Method 3 requires a two-step query:
1. Find UserCreated event by username to get userCreatedId
2. Find all events (UserCreated + UserDeleted) by userCreatedId

This cannot be expressed with current simple tag matching.

## Proposed Enhancement

### 1. Extended Protobuf Schema

```protobuf
// Add to eventstore.proto
enum QueryType {
  SIMPLE = 0;        // Current behavior
  RELATED_EVENTS = 1; // New: find related events by extracted field
}

message RelatedEventsQuery {
  // Step 1: Find source event
  Criterion source_criterion = 1;
  string extract_field = 2;        // Field to extract from source event (e.g., "userCreatedId")
  
  // Step 2: Find related events
  string target_field = 3;         // Field to match in target events (e.g., "userCreatedId")
  repeated string event_types = 4; // Optional: filter by event types
}

message EnhancedQuery {
  QueryType type = 1;
  
  // Use one of these based on type
  Query simple_query = 2;                    // For SIMPLE type
  RelatedEventsQuery related_query = 3;      // For RELATED_EVENTS type
}
```

### 2. Enhanced SQL Function

```sql
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
BEGIN
    schema_prefix := quote_ident(schema) || '.';
    query_type := enhanced_criteria ->> 'type';
    
    IF query_type = 'RELATED_EVENTS' THEN
        -- Handle Method 3 style queries
        RETURN QUERY EXECUTE format(
            $q$
            WITH source_event AS (
                SELECT data ->> %2$L as extracted_value
                FROM %1$sorisun_es_event
                WHERE data @> %3$L
                LIMIT 1
            )
            SELECT e.*
            FROM %1$sorisun_es_event e
            CROSS JOIN source_event s
            WHERE e.data ->> %4$L = s.extracted_value
            %5$s
            ORDER BY e.transaction_id %6$s, e.global_id %6$s
            LIMIT %7$L
            $q$,
            schema_prefix,
            enhanced_criteria -> 'related_query' ->> 'extract_field',
            enhanced_criteria -> 'related_query' -> 'source_criterion',
            enhanced_criteria -> 'related_query' ->> 'target_field',
            CASE 
                WHEN enhanced_criteria -> 'related_query' -> 'event_types' IS NOT NULL THEN
                    format('AND e.event_type = ANY(%L)', 
                           array(SELECT jsonb_array_elements_text(enhanced_criteria -> 'related_query' -> 'event_types'))
                    )
                ELSE ''
            END,
            sort_dir,
            LEAST(GREATEST(max_count, 1), 10000)
        );
    ELSE
        -- Fall back to original simple query logic
        RETURN QUERY SELECT * FROM get_matching_events(
            schema, stream_name, from_stream_version, 
            enhanced_criteria -> 'simple_query', 
            after_position, sort_dir, max_count
        );
    END IF;
END;
$$;
```

### 3. Go Code Changes

#### Update postgres_eventstore.go

```go
// Add new function to handle enhanced queries
func (s *PostgresGetEvents) GetEnhanced(
    ctx context.Context,
    req *eventstore.GetEventsRequestEnhanced,
) (*eventstore.GetEventsResponse, error) {
    schema, err := s.Schema(req.Boundary)
    if err != nil {
        return nil, err
    }

    var criteriaJSON []byte
    if req.EnhancedQuery != nil {
        criteriaJSON, err = convertEnhancedQueryToJSON(req.EnhancedQuery)
        if err != nil {
            return nil, err
        }
    }

    // Use enhanced SQL function
    rows, err := s.db.QueryContext(ctx, 
        "SELECT * FROM get_matching_events_enhanced($1, $2, $3, $4, $5, $6, $7)",
        schema, streamName, fromVersion, criteriaJSON, afterPosJSON, direction, req.Count,
    )
    // ... rest of implementation
}

func convertEnhancedQueryToJSON(query *eventstore.EnhancedQuery) ([]byte, error) {
    switch query.Type {
    case eventstore.QueryType_SIMPLE:
        return json.Marshal(map[string]interface{}{
            "type": "SIMPLE",
            "simple_query": convertQueryToMap(query.SimpleQuery),
        })
    case eventstore.QueryType_RELATED_EVENTS:
        return json.Marshal(map[string]interface{}{
            "type": "RELATED_EVENTS",
            "related_query": map[string]interface{}{
                "source_criterion": convertCriterionToMap(query.RelatedQuery.SourceCriterion),
                "extract_field": query.RelatedQuery.ExtractField,
                "target_field": query.RelatedQuery.TargetField,
                "event_types": query.RelatedQuery.EventTypes,
            },
        })
    }
    return nil, fmt.Errorf("unsupported query type")
}
```

### 4. Usage Example

```go
// Method 3 query: Find UserCreated and UserDeleted events by username
resp, err := getEvents.GetEnhanced(ctx, &eventstore.GetEventsRequestEnhanced{
    Boundary: "user_boundary",
    EnhancedQuery: &eventstore.EnhancedQuery{
        Type: eventstore.QueryType_RELATED_EVENTS,
        RelatedQuery: &eventstore.RelatedEventsQuery{
            SourceCriterion: &eventstore.Criterion{
                Tags: []*eventstore.Tag{
                    {Key: "username", Value: "nameit"},
                    {Key: "eventType", Value: "UserCreated"},
                },
            },
            ExtractField: "userCreatedId",
            TargetField: "userCreatedId",
            EventTypes: []string{"UserCreated", "UserDeleted"},
        },
    },
    Direction: eventstore.Direction_ASC,
    Count: 1000,
})
```

### 5. Migration Strategy

1. **Backward Compatibility**: Keep existing `get_matching_events` function
2. **Gradual Adoption**: Add new `GetEnhanced` method alongside existing `Get`
3. **Testing**: Comprehensive tests for both simple and complex queries
4. **Documentation**: Update API docs with new query capabilities

### 6. Benefits

- **Solves Method 3**: Enables complex multi-step queries
- **Performance**: Single SQL query instead of multiple round trips
- **Extensible**: Framework for future complex query types
- **Backward Compatible**: Existing code continues to work
- **Type Safe**: Protobuf ensures proper query structure

This enhancement transforms Orisun from a simple tag-based query system into a powerful event relationship query engine while maintaining full backward compatibility.