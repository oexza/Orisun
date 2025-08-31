# Method 3 Upgrade Guide: Enhanced Query Support for Orisun EventStore

This guide explains how to upgrade the `get_matching_events` function and query criteria parameter to support Method 3 queries (RELATED_EVENTS) as described in `experimental-queries.sql`.

## Overview

Method 3 allows you to query events by first finding a source event (e.g., UserCreated by username) and then retrieving all related events that share a common field (e.g., userCreatedId). This enables powerful relationship-based queries in your event store.

## What's Been Implemented

### 1. Enhanced Protobuf Definitions

New message types have been added to `eventstore/eventstore.proto`:

```protobuf
enum QueryType {
  SIMPLE = 0;        // Current behavior
  RELATED_EVENTS = 1; // New: find related events by extracted field
}

message RelatedEventsQuery {
  // Step 1: Find source event
  Criterion source_criterion = 1;
  string extract_field = 2;        // Field to extract from source event
  
  // Step 2: Find related events
  string target_field = 3;         // Field to match in target events
  repeated string event_types = 4; // Optional: filter by event types
}

message EnhancedQuery {
  QueryType type = 1;
  Query simple_query = 2;                    // For SIMPLE type
  RelatedEventsQuery related_query = 3;      // For RELATED_EVENTS type
}

message GetEventsRequestEnhanced {
  EnhancedQuery enhanced_query = 1;
  Position from_position = 2;
  uint32 count = 3;
  Direction direction = 4;
  string boundary = 5;
  GetStreamQuery stream = 6;
}
```

And a new gRPC service method:

```protobuf
service EventStore {
  // ... existing methods
  rpc GetEventsEnhanced(GetEventsRequestEnhanced) returns (GetEventsResponse) {}
}
```

### 2. Enhanced Database Function

A new PostgreSQL function `get_matching_events_enhanced` has been created in `postgres/scripts/common/enhanced_db_scripts.sql`:

```sql
CREATE OR REPLACE FUNCTION get_matching_events_enhanced(
    stream_name text,
    from_stream_version bigint,
    after_position bigint,
    enhanced_criteria jsonb,
    stream_criteria jsonb,
    direction text,
    max_count int
) RETURNS TABLE (
    -- same return structure as original function
) AS $$
BEGIN
    -- Handle RELATED_EVENTS queries with CTE
    IF enhanced_criteria->>'type' = 'RELATED_EVENTS' THEN
        RETURN QUERY
        WITH source_events AS (
            -- Find source event based on source_criterion
            SELECT data->>((enhanced_criteria->'related_query'->>'extract_field')) as extracted_value
            FROM events
            WHERE data @> (enhanced_criteria->'related_query'->'source_criterion'->>'tags')::jsonb
            LIMIT 1
        )
        SELECT e.event_id, e.event_type, e.data, e.metadata, e.position, e.date_created, e.stream_id, e.version
        FROM events e, source_events s
        WHERE e.data->>((enhanced_criteria->'related_query'->>'target_field')) = s.extracted_value
          AND (enhanced_criteria->'related_query'->'event_types' IS NULL 
               OR e.event_type = ANY(ARRAY(SELECT jsonb_array_elements_text(enhanced_criteria->'related_query'->'event_types'))))
        ORDER BY e.position ASC;
    ELSE
        -- Fallback to original function for SIMPLE queries
        RETURN QUERY SELECT * FROM get_matching_events(stream_name, from_stream_version, after_position, enhanced_criteria->'simple_query'->'criteria', stream_criteria, direction, max_count);
    END IF;
END;
$$ LANGUAGE plpgsql;
```

### 3. Enhanced Go Implementation

The `postgres/enhanced_postgres_eventstore.go` file provides:

#### Enhanced EventStore Struct
```go
type EnhancedPostgresGetEvents struct {
    *PostgresGetEvents
}

func NewEnhancedPostgresGetEvents(db *sql.DB, logger logging.Logger,
    boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *EnhancedPostgresGetEvents {
    return &EnhancedPostgresGetEvents{
        PostgresGetEvents: NewPostgresGetEvents(db, logger, boundarySchemaMappings),
    }
}
```

#### Enhanced Query Method
```go
func (s *EnhancedPostgresGetEvents) GetEnhanced(
    ctx context.Context,
    req *enhancedeventstore.GetEventsRequestEnhanced,
) (*eventstore.GetEventsResponse, error) {
    // Implementation that calls get_matching_events_enhanced
}
```

#### Convenience Functions
```go
// Create a RELATED_EVENTS query for finding user events by username
func CreateRelatedEventsQuery(
    username string,
    extractField string,
    targetField string,
    eventTypes []string,
) *enhancedeventstore.EnhancedQuery

// High-level convenience function
func (s *EnhancedPostgresGetEvents) GetUserEventsByUsername(
    ctx context.Context,
    boundary string,
    username string,
) (*eventstore.GetEventsResponse, error)
```

## How to Use Method 3 Queries

### Example 1: Find All Events for a User by Username

```go
// Create enhanced query
enhancedQuery := &enhancedeventstore.EnhancedQuery{
    Type: enhancedeventstore.QueryType_RELATED_EVENTS,
    RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
        // Step 1: Find UserCreated event by username
        SourceCriterion: &enhancedeventstore.Criterion{
            Tags: []*enhancedeventstore.Tag{
                {Key: "username", Value: "john_doe"},
                {Key: "eventType", Value: "UserCreated"},
            },
        },
        // Step 2: Extract userCreatedId from source event
        ExtractField: "userCreatedId",
        // Step 3: Find all events with matching userCreatedId
        TargetField: "userCreatedId",
        // Step 4: Filter by event types (optional)
        EventTypes: []string{"UserCreated", "UserDeleted", "UserUpdated"},
    },
}

// Create request
request := &enhancedeventstore.GetEventsRequestEnhanced{
    EnhancedQuery: enhancedQuery,
    Boundary:      "user_management",
    Count:         100,
    Direction:     enhancedeventstore.Direction_ASC,
}

// Execute query
enhancedStore := postgres.NewEnhancedPostgresGetEvents(db, logger, mappings)
response, err := enhancedStore.GetEnhanced(ctx, request)
if err != nil {
    log.Fatal(err)
}

// Process results
for _, event := range response.Events {
    fmt.Printf("Event: %s - %s\n", event.EventType, event.EventId)
}
```

### Example 2: Using the Convenience Function

```go
// Much simpler approach
enhancedStore := postgres.NewEnhancedPostgresGetEvents(db, logger, mappings)
response, err := enhancedStore.GetUserEventsByUsername(ctx, "user_management", "john_doe")
if err != nil {
    log.Fatal(err)
}

for _, event := range response.Events {
    fmt.Printf("Found user event: %s\n", event.EventType)
}
```

## Generated SQL Query

When you use a RELATED_EVENTS query, it generates SQL similar to this:

```sql
WITH source_events AS (
    SELECT data->>'userCreatedId' as extracted_value
    FROM events
    WHERE data @> '{"username": "john_doe", "eventType": "UserCreated"}'
    LIMIT 1
)
SELECT e.event_id, e.event_type, e.data, e.metadata, e.position, e.date_created, e.stream_id, e.version
FROM events e, source_events s
WHERE e.data->>'userCreatedId' = s.extracted_value
  AND e.event_type IN ('UserCreated', 'UserDeleted', 'UserUpdated')
ORDER BY e.position ASC;
```

## Migration Steps

### 1. Deploy Database Changes

```sql
-- Run the enhanced_db_scripts.sql
\i postgres/scripts/common/enhanced_db_scripts.sql
```

### 2. Update Your Application

```go
// Replace your existing eventstore usage
// OLD:
eventStore := postgres.NewPostgresGetEvents(db, logger, mappings)

// NEW:
enhancedEventStore := postgres.NewEnhancedPostgresGetEvents(db, logger, mappings)

// You can still use the old methods for backward compatibility
response, err := enhancedEventStore.Get(ctx, oldRequest)

// Or use the new enhanced methods
response, err := enhancedEventStore.GetEnhanced(ctx, enhancedRequest)
```

### 3. Update gRPC Service (if using gRPC directly)

```go
// Add the new method to your gRPC service implementation
func (s *EventStoreService) GetEventsEnhanced(
    ctx context.Context,
    req *enhancedeventstore.GetEventsRequestEnhanced,
) (*eventstore.GetEventsResponse, error) {
    return s.enhancedEventStore.GetEnhanced(ctx, req)
}
```

## Benefits of Method 3

1. **Powerful Relationships**: Query events based on relationships between different event types
2. **Performance**: Uses optimized CTE queries with proper indexing
3. **Flexibility**: Can extract any field from source events to find related events
4. **Backward Compatibility**: Existing queries continue to work unchanged
5. **Type Safety**: Full protobuf type safety for all query parameters

## Query Variations

You can create various types of related event queries:

```go
// Find events by email
findByEmail := &enhancedeventstore.EnhancedQuery{
    Type: enhancedeventstore.QueryType_RELATED_EVENTS,
    RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
        SourceCriterion: &enhancedeventstore.Criterion{
            Tags: []*enhancedeventstore.Tag{
                {Key: "email", Value: "john@example.com"},
                {Key: "eventType", Value: "UserCreated"},
            },
        },
        ExtractField: "userCreatedId",
        TargetField:  "userCreatedId",
        EventTypes:   []string{"UserCreated", "UserDeleted"},
    },
}

// Find events by organization
findByOrg := &enhancedeventstore.EnhancedQuery{
    Type: enhancedeventstore.QueryType_RELATED_EVENTS,
    RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
        SourceCriterion: &enhancedeventstore.Criterion{
            Tags: []*enhancedeventstore.Tag{
                {Key: "organizationId", Value: "org-123"},
            },
        },
        ExtractField: "organizationId",
        TargetField:  "organizationId",
        EventTypes:   []string{"UserCreated", "UserDeleted", "OrgUpdated"},
    },
}
```

## Performance Considerations

1. **Indexing**: Ensure you have proper indexes on JSON fields used in queries:
   ```sql
   CREATE INDEX idx_events_username ON events USING GIN ((data->>'username'));
   CREATE INDEX idx_events_user_created_id ON events USING GIN ((data->>'userCreatedId'));
   ```

2. **Query Optimization**: The CTE approach is optimized for PostgreSQL's query planner

3. **Limit Results**: Always use appropriate `Count` limits to avoid large result sets

This upgrade transforms Orisun into a powerful event relationship query engine while maintaining full backward compatibility with existing code.