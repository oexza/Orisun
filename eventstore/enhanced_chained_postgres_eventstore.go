package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ChainedEventStorePostgres extends the basic eventstore with chained query capabilities
type ChainedEventStorePostgres struct {
	db *sql.DB
}

// NewChainedEventStorePostgres creates a new chained eventstore instance
func NewChainedEventStorePostgres(db *sql.DB) *ChainedEventStorePostgres {
	return &ChainedEventStorePostgres{
		db: db,
	}
}

// GetEventsChained processes chained queries for multi-hop event traversal
func (es *ChainedEventStorePostgres) GetEventsChained(
	ctx context.Context,
	req *GetEventsChainedRequest,
) (*GetEventsChainedResponse, error) {
	if req.ChainedQuery == nil {
		return nil, fmt.Errorf("chained query is required")
	}

	// Convert protobuf steps to JSON for PostgreSQL function
	stepsJSON, err := es.convertStepsToJSON(req.ChainedQuery.Steps)
	if err != nil {
		return nil, fmt.Errorf("failed to convert steps to JSON: %w", err)
	}

	// Execute the chained query
	results, err := es.executeChainedQuery(ctx, req.Boundary, stepsJSON, req.ChainedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute chained query: %w", err)
	}

	// Build response
	response := &GetEventsChainedResponse{
		Result: &ChainedQueryResult{
			StepResults: results.StepResults,
			FinalEvents: results.FinalEvents,
			Stats:       results.Stats,
		},
	}

	return response, nil
}

// convertStepsToJSON converts protobuf QueryStep array to JSON for PostgreSQL
func (es *ChainedEventStorePostgres) convertStepsToJSON(steps []*QueryStep) (string, error) {
	type jsonStep struct {
		StepID           string            `json:"step_id"`
		SourceCriterion  map[string]interface{} `json:"source_criterion,omitempty"`
		ExtractField     string            `json:"extract_field"`
		TargetField      string            `json:"target_field"`
		EventTypes       []string          `json:"event_types,omitempty"`
		Condition        map[string]interface{} `json:"condition,omitempty"`
		DependsOnSteps   []string          `json:"depends_on_steps,omitempty"`
	}

	jsonSteps := make([]jsonStep, len(steps))
	for i, step := range steps {
		jsonSteps[i] = jsonStep{
			StepID:         step.StepId,
			ExtractField:   step.ExtractField,
			TargetField:    step.TargetField,
			EventTypes:     step.EventTypes,
			DependsOnSteps: step.DependsOnSteps,
		}

		// Convert source criterion
		if step.SourceCriterion != nil {
			jsonSteps[i].SourceCriterion = es.convertCriterionToMap(step.SourceCriterion)
		}

		// Convert step condition
		if step.Condition != nil {
			jsonSteps[i].Condition = map[string]interface{}{
				"type":           int(step.Condition.Type),
				"field_name":     step.Condition.FieldName,
				"allowed_values": step.Condition.AllowedValues,
				"regex_pattern":  step.Condition.RegexPattern,
			}
		}
	}

	jsonBytes, err := json.Marshal(jsonSteps)
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}

// convertCriterionToMap converts protobuf Criterion to map for JSON serialization
func (es *ChainedEventStorePostgres) convertCriterionToMap(criterion *Criterion) map[string]interface{} {
	tags := make([]map[string]string, len(criterion.Tags))
	for i, tag := range criterion.Tags {
		tags[i] = map[string]string{
			"key":   tag.Key,
			"value": tag.Value,
		}
	}
	return map[string]interface{}{
		"tags": tags,
	}
}

// executeChainedQuery executes the chained query using PostgreSQL function
func (es *ChainedEventStorePostgres) executeChainedQuery(
	ctx context.Context,
	boundary string,
	stepsJSON string,
	chainedQuery *ChainedQuery,
) (*ChainedQueryResult, error) {
	query := `
		SELECT 
			step_id,
			event_id,
			event_type,
			data,
			metadata,
			stream_id,
			version,
			date_created,
			commit_position,
			prepare_position,
			extracted_value,
			step_order
		FROM get_events_chained($1, $2, $3, $4, $5)
	`

	// Execute PostgreSQL function
	rows, err := es.db.QueryContext(ctx, query, boundary, stepsJSON,
		int(chainedQuery.ExecutionMode),
		chainedQuery.MaxResultsPerStep,
		chainedQuery.ReturnIntermediateResults,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute chained query: %w", err)
	}
	defer rows.Close()

	// Group results by step
	stepResults := make(map[string]*StepResult)
	finalEvents := make([]*Event, 0)
	totalEvents := 0
	startTime := time.Now()

	for rows.Next() {
		var (
			stepID          string
			eventID         string
			eventType       string
			data            string
			metadata        string
			streamID        string
			version         int64
			dateCreated     time.Time
			commitPosition  int64
			preparePosition int64
			extractedValue  sql.NullString
			stepOrder       int32
		)

		err := rows.Scan(
			&stepID,
			&eventID,
			&eventType,
			&data,
			&metadata,
			&streamID,
			&version,
			&dateCreated,
			&commitPosition,
			&preparePosition,
			&extractedValue,
			&stepOrder,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Create event
		event := &Event{
			EventId:   eventID,
			EventType: eventType,
			Data:      data,
			Metadata:  metadata,
			StreamId:  streamID,
			Version:   uint64(version),
			Position: &Position{
				CommitPosition:  commitPosition,
				PreparePosition: preparePosition,
			},
			DateCreated: timestamppb.New(dateCreated),
		}

		// Group by step
		if _, exists := stepResults[stepID]; !exists {
			stepResults[stepID] = &StepResult{
				StepId:          fmt.Sprintf("%d", stepID),
				Events:          make([]*Event, 0),
				ExtractedValues: make(map[string]string),
				EventsFound:     0,
				ExecutionTimeMs: 0,
			}
		}

		stepResults[stepID].Events = append(stepResults[stepID].Events, event)
		if extractedValue.Valid {
			if stepResults[stepID].ExtractedValues == nil {
				stepResults[stepID].ExtractedValues = make(map[string]string)
			}
			stepResults[stepID].ExtractedValues[fmt.Sprintf("value_%d", len(stepResults[stepID].ExtractedValues))] = extractedValue.String
		}

		// Add to final events if it's the last step or intermediate results are requested
		if chainedQuery.ReturnIntermediateResults || es.isLastStep(stepOrder, len(chainedQuery.Steps)) {
			finalEvents = append(finalEvents, event)
		}

		totalEvents++
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Convert map to slice
	stepResultsSlice := make([]*StepResult, 0, len(stepResults))
	for _, result := range stepResults {
		stepResultsSlice = append(stepResultsSlice, result)
	}

	// Create execution stats
	stats := &ChainExecutionStats{
			TotalStepsExecuted:   int32(len(stepResults)),
			TotalEventsProcessed: int32(totalEvents),
			TotalExecutionTimeMs: int64(time.Since(startTime).Milliseconds()),
			FailedSteps:          make([]string, 0),
		}

	return &ChainedQueryResult{
		StepResults: stepResultsSlice,
		FinalEvents: finalEvents,
		Stats:       stats,
	}, nil
}

// isLastStep checks if the given step order is the last step
func (es *ChainedEventStorePostgres) isLastStep(stepOrder int32, totalSteps int) bool {
	return int(stepOrder) == totalSteps
}

// GetEventsChainedSimple provides a simplified interface for two-step chained queries
func (es *ChainedEventStorePostgres) GetEventsChainedSimple(
	ctx context.Context,
	boundary string,
	sourceTags map[string]string,
	extractField string,
	targetField string,
	targetEventTypes []string,
	maxResults int32,
) ([]*ChainedEventPair, error) {
	// Convert source tags to JSON
	sourceTagsJSON, err := json.Marshal(sourceTags)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal source tags: %w", err)
	}

	query := `
		SELECT 
			source_event_id,
			source_event_type,
			source_data,
			target_event_id,
			target_event_type,
			target_data,
			extracted_value,
			target_date_created
		FROM get_events_chained_simple($1, $2, $3, $4, $5, $6)
	`

	// Execute PostgreSQL function
	rows, err := es.db.QueryContext(ctx, query, boundary, string(sourceTagsJSON),
		extractField,
		targetField,
		pq.Array(targetEventTypes),
		maxResults,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute simple chained query: %w", err)
	}
	defer rows.Close()

	results := make([]*ChainedEventPair, 0)
	for rows.Next() {
		var (
			sourceEventID   string
			sourceEventType string
			sourceData      string
			targetEventID   string
			targetEventType string
			targetData      string
			extractedValue  string
			targetDateCreated time.Time
		)

		err := rows.Scan(
			&sourceEventID,
			&sourceEventType,
			&sourceData,
			&targetEventID,
			&targetEventType,
			&targetData,
			&extractedValue,
			&targetDateCreated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		pair := &ChainedEventPair{
			SourceEvent: &Event{
				EventId:   sourceEventID,
				EventType: sourceEventType,
				Data:      sourceData,
			},
			TargetEvent: &Event{
				EventId:     targetEventID,
				EventType:   targetEventType,
				Data:        targetData,
				DateCreated: timestamppb.New(targetDateCreated),
			},
			ExtractedValue: extractedValue,
		}

		results = append(results, pair)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// ChainedEventPair represents a pair of related events in a simple chain
type ChainedEventPair struct {
	SourceEvent    *Event
	TargetEvent    *Event
	ExtractedValue string
}

// ValidateChainedQuery validates the structure and dependencies of a chained query
func (es *ChainedEventStorePostgres) ValidateChainedQuery(query *ChainedQuery) error {
	if len(query.Steps) == 0 {
		return fmt.Errorf("chained query must have at least one step")
	}

	// Validate step dependencies
	stepIDs := make(map[string]bool)
	for _, step := range query.Steps {
		if step.StepId == "" {
			return fmt.Errorf("step ID cannot be empty")
		}
		if stepIDs[step.StepId] {
			return fmt.Errorf("duplicate step ID: %s", step.StepId)
		}
		stepIDs[step.StepId] = true
	}

	// Validate dependencies exist
	for _, step := range query.Steps {
		for _, depID := range step.DependsOnSteps {
			if !stepIDs[depID] {
				return fmt.Errorf("step %s depends on non-existent step %s", step.StepId, depID)
			}
		}
	}

	// Validate extract and target fields are specified for chaining
	for i, step := range query.Steps {
		if i < len(query.Steps)-1 { // Not the last step
			if step.ExtractField == "" {
				return fmt.Errorf("step %s must specify extract_field for chaining", step.StepId)
			}
			if step.TargetField == "" {
				return fmt.Errorf("step %s must specify target_field for chaining", step.StepId)
			}
		}
	}

	return nil
}

// BuildUserLifecycleQuery creates a predefined query for user lifecycle tracking
func BuildUserLifecycleQuery(username string, maxResults int32) *ChainedQuery {
	return &ChainedQuery{
		Steps: []*QueryStep{
			{
				StepId: "find_user_creation",
				SourceCriterion: &Criterion{
					Tags: []*Tag{
						{Key: "username", Value: username},
						{Key: "event_type", Value: "UserCreated"},
					},
				},
				ExtractField: "userCreatedId",
				TargetField:  "userId",
				EventTypes:   []string{"UserUpdated", "UserActivated"},
			},
			{
				StepId:       "find_user_updates",
				ExtractField: "organizationId",
				TargetField:  "orgId",
				EventTypes:   []string{"OrganizationEvent"},
			},
			{
				StepId:      "find_org_events",
				EventTypes:  []string{"OrganizationUpdated", "OrganizationDeleted"},
			},
		},
		ExecutionMode:              ChainExecutionMode_SEQUENTIAL,
		MaxResultsPerStep:          maxResults,
		ReturnIntermediateResults:  true,
	}
}