-- Example: Query events by username when you only know the username
-- This demonstrates how to find both UserCreated and UserDeleted events for a specific user

-- Method 1: Using gRPC API with criteria (recommended)
-- Use this structure in your gRPC GetEvents request:
/*
{
  "boundary": "your_boundary_name",
  "query": {
    "criteria": [
      {
        "tags": [
          {"key": "username", "value": "nameit"}
        ]
      }
    ]
  },
  "direction": "ASC",
  "count": 1000
}
*/

-- Method 2: Direct PostgreSQL query using JSON containment
-- This queries the data field directly for the username
SELECT 
    event_id,
    event_type,
    data,
    metadata,
    stream_name,
    stream_version,
    date_created
FROM orisun_es_event 
WHERE 
    -- Query for events containing the username in the data field
    data @> '{"username": "nameit"}'
    -- Optionally filter by event types if you only want specific events
    AND event_type IN ('UserCreated', 'UserDeleted')
ORDER BY transaction_id ASC, global_id ASC;

-- Method 3: More complex query to find related events by userCreatedId
-- First find the UserCreated event to get the userCreatedId, then find all related events
WITH user_info AS (
    SELECT 
        data->>'userCreatedId' as user_id,
        data->>'username' as username
    FROM orisun_es_event 
    WHERE 
        event_type = 'UserCreated'
        AND data @> '{"username": "nameit"}'
    LIMIT 1
)
SELECT 
    e.event_id,
    e.event_type,
    e.data,
    e.metadata,
    e.stream_name,
    e.stream_version,
    e.date_created
FROM orisun_es_event e
CROSS JOIN user_info u
WHERE 
    -- Find all events for this user by userCreatedId
    e.data @> jsonb_build_object('userCreatedId', u.user_id)
    -- Or events that contain the username
    OR e.data @> jsonb_build_object('username', u.username)
ORDER BY e.transaction_id ASC, e.global_id ASC;

-- Method 4: Using the built-in get_matching_events function
-- This uses Orisun's optimized query function with criteria
SELECT * FROM get_matching_events(
    'public',  -- schema
    NULL,      -- stream_name (null to search all streams)
    NULL,      -- from_stream_version
    '{"criteria": [{"username": "nameit"}]}',  -- criteria as JSONB
    NULL,      -- after_position
    'ASC',     -- sort_dir
    1000       -- max_count
);