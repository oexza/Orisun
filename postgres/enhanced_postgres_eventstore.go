package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"orisun/config"
	"orisun/eventstore"
	"orisun/logging"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnhancedPostgresGetEvents extends PostgresGetEvents with enhanced query capabilities
type EnhancedPostgresGetEvents struct {
	*PostgresGetEvents
}

// NewEnhancedPostgresGetEvents creates a new enhanced event store with Method 3 support
func NewEnhancedPostgresGetEvents(db *sql.DB, logger logging.Logger,
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *EnhancedPostgresGetEvents {
	return &EnhancedPostgresGetEvents{
		PostgresGetEvents: NewPostgresGetEvents(db, logger, boundarySchemaMappings),
	}
}

// GetEnhanced implements the enhanced query functionality for Method 3 support
func (s *EnhancedPostgresGetEvents) GetEnhanced(
	ctx context.Context,
	req *eventstore.GetEventsRequestEnhanced,
) (*eventstore.GetEventsResponse, error) {
	schema, err := s.Schema(req.Boundary)
	if err != nil {
		return nil, err
	}

	// Convert enhanced query to JSON
	var criteriaJSON []byte
	if req.EnhancedQuery != nil {
		criteriaJSON, err = convertEnhancedQueryToJSON(req.EnhancedQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to convert enhanced query: %w", err)
		}
	}

	// Handle stream parameters
	var streamName *string
	var fromVersion *int64
	if req.Stream != nil {
		streamName = &req.Stream.Name
		if req.Stream.FromVersion != 0 {
			fromVersion = &req.Stream.FromVersion
		}
	}

	// Handle position parameters
	var afterPosJSON []byte
	if req.FromPosition != nil {
		afterPosJSON, err = json.Marshal(map[string]interface{}{
			"transaction_id": req.FromPosition.CommitPosition,
			"global_id":      req.FromPosition.PreparePosition,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal position: %w", err)
		}
	}

	// Convert direction
	direction := "ASC"
	if req.Direction == eventstore.Direction_DESC {
		direction = "DESC"
	}

	// Execute enhanced query
	rows, err := s.db.QueryContext(ctx,
		"SELECT event_id, event_type, data, metadata, stream_name, stream_version, transaction_id, global_id, date_created FROM get_matching_events_enhanced($1, $2, $3, $4, $5, $6, $7)",
		schema, streamName, fromVersion, criteriaJSON, afterPosJSON, direction, req.Count,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute enhanced query: %w", err)
	}
	defer rows.Close()

	// Parse results
	events := make([]*eventstore.Event, 0)
	for rows.Next() {
		var event eventstore.Event
		var dateCreated time.Time
		var transactionId, globalId int64

		err := rows.Scan(
			&event.EventId,
			&event.EventType,
			&event.Data,
			&event.Metadata,
			&event.StreamId,
			&event.Version,
			&transactionId,
			&globalId,
			&dateCreated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Set position and timestamp
		event.Position = &eventstore.Position{
			CommitPosition:  transactionId,
			PreparePosition: globalId,
		}
		event.DateCreated = timestamppb.New(dateCreated)

		events = append(events, &event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return &eventstore.GetEventsResponse{
		Events: events,
	}, nil
}

// convertEnhancedQueryToJSON converts protobuf EnhancedQuery to JSON for SQL function
func convertEnhancedQueryToJSON(query *eventstore.EnhancedQuery) ([]byte, error) {
	switch query.Type {
	case eventstore.QueryType_SIMPLE:
		if query.SimpleQuery == nil {
			return json.Marshal(map[string]interface{}{
				"type": "SIMPLE",
				"simple_query": map[string]interface{}{"criteria": []interface{}{}},
			})
		}
		return json.Marshal(map[string]interface{}{
			"type": "SIMPLE",
			"simple_query": convertQueryToMap(query.SimpleQuery),
		})

	case eventstore.QueryType_RELATED_EVENTS:
		if query.RelatedQuery == nil {
			return nil, fmt.Errorf("related_query is required for RELATED_EVENTS type")
		}
		return json.Marshal(map[string]interface{}{
			"type": "RELATED_EVENTS",
			"related_query": map[string]interface{}{
				"source_criterion": convertCriterionToMap(query.RelatedQuery.SourceCriterion),
				"extract_field":    query.RelatedQuery.ExtractField,
				"target_field":     query.RelatedQuery.TargetField,
				"event_types":      query.RelatedQuery.EventTypes,
			},
		})

	default:
		return nil, fmt.Errorf("unsupported query type: %v", query.Type)
	}
}

// convertQueryToMap converts protobuf Query to map for JSON serialization
func convertQueryToMap(query *eventstore.Query) map[string]interface{} {
	if query == nil {
		return map[string]interface{}{"criteria": []interface{}{}}
	}

	criteria := make([]map[string]interface{}, len(query.Criteria))
	for i, criterion := range query.Criteria {
		criteria[i] = convertCriterionToMap(criterion)
	}

	return map[string]interface{}{
		"criteria": criteria,
	}
}

// convertCriterionToMap converts protobuf Criterion to map for JSON serialization
func convertCriterionToMap(criterion *eventstore.Criterion) map[string]interface{} {
	if criterion == nil {
		return map[string]interface{}{}
	}

	tags := make(map[string]interface{})
	for _, tag := range criterion.Tags {
		tags[tag.Key] = tag.Value
	}

	return tags
}

// Helper function to create Method 3 queries easily
func CreateRelatedEventsQuery(
	username string,
	extractField string,
	targetField string,
	eventTypes []string,
) *eventstore.EnhancedQuery {
	return &eventstore.EnhancedQuery{
		Type: eventstore.QueryType_RELATED_EVENTS,
		RelatedQuery: &eventstore.RelatedEventsQuery{
			SourceCriterion: &eventstore.Criterion{
				Tags: []*eventstore.Tag{
					{Key: "username", Value: username},
					{Key: "eventType", Value: "UserCreated"},
				},
			},
			ExtractField: extractField,
			TargetField:  targetField,
			EventTypes:   eventTypes,
		},
	}
}

// Example usage function for Method 3
func (s *EnhancedPostgresGetEvents) GetUserEventsByUsername(
	ctx context.Context,
	boundary string,
	username string,
) (*eventstore.GetEventsResponse, error) {
	return s.GetEnhanced(ctx, &eventstore.GetEventsRequestEnhanced{
		Boundary: boundary,
		EnhancedQuery: CreateRelatedEventsQuery(
			username,
			"userCreatedId",
			"userCreatedId",
			[]string{"UserCreated", "UserDeleted"},
		),
		Direction:     eventstore.Direction_ASC,
		Count:     1000,
	})
}