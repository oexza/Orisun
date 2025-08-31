package main

import (
	"context"
	"fmt"

	enhancedeventstore "orisun/eventstore"
)

// Example demonstrating how to use the enhanced query functionality for Method 3
// This shows how to query events by username using the RELATED_EVENTS query type

func demonstrateEnhancedQueries() {
	// This is a demonstration of how to use the enhanced query functionality
	// In a real application, you would have a database connection and proper setup

	fmt.Println("Enhanced Query Example for Method 3 - Query Events by Username")
	fmt.Println("============================================================")

	// Example 1: Create a RELATED_EVENTS query to find all events for a user
	enhancedQuery := createUserEventsQuery("john_doe")
	fmt.Printf("\n1. Enhanced Query Structure:\n%+v\n", enhancedQuery)

	// Example 2: Create a GetEventsRequestEnhanced
	request := &enhancedeventstore.GetEventsRequestEnhanced{
		EnhancedQuery: enhancedQuery,
		Boundary:      "user_management",
		Count:         100,
		Direction:     enhancedeventstore.Direction_ASC,
	}
	fmt.Printf("\n2. Enhanced Request Structure:\n%+v\n", request)

	// Example 3: Show how this would be used with the enhanced eventstore
	fmt.Println("\n3. Usage with Enhanced EventStore:")
	fmt.Println("```go")
	fmt.Println("// Initialize enhanced eventstore")
	fmt.Println("enhancedStore := postgres.NewEnhancedPostgresGetEvents(db, logger, mappings)")
	fmt.Println("")
	fmt.Println("// Execute enhanced query")
	fmt.Println("response, err := enhancedStore.GetEnhanced(ctx, request)")
	fmt.Println("if err != nil {")
	fmt.Println("    log.Fatal(err)")
	fmt.Println("}")
	fmt.Println("")
	fmt.Println("// Process results")
	fmt.Println("for _, event := range response.Events {")
	fmt.Println("    fmt.Printf(\"Event: %s - %s\\n\", event.EventType, event.EventId)")
	fmt.Println("}")
	fmt.Println("```")

	// Example 4: Show the SQL that would be generated
	fmt.Println("\n4. Generated SQL Query (conceptual):")
	fmt.Println("```sql")
	fmt.Println("WITH source_events AS (")
	fmt.Println("    SELECT data->>'userCreatedId' as user_id")
	fmt.Println("    FROM events")
	fmt.Println("    WHERE data @> '{\"username\": \"john_doe\", \"eventType\": \"UserCreated\"}'")
	fmt.Println("    LIMIT 1")
	fmt.Println(")")
	fmt.Println("SELECT e.*")
	fmt.Println("FROM events e, source_events s")
	fmt.Println("WHERE e.data->>'userCreatedId' = s.user_id")
	fmt.Println("  AND e.event_type IN ('UserCreated', 'UserDeleted')")
	fmt.Println("ORDER BY e.position ASC;")
	fmt.Println("```")

	// Example 5: Show different query variations
	fmt.Println("\n5. Query Variations:")
	
	// Find user events by email
	emailQuery := createUserEventsByEmail("john@example.com")
	fmt.Printf("\nFind by email: %+v\n", emailQuery.RelatedQuery.SourceCriterion.Tags)

	// Find all events for a specific user ID
	userIdQuery := createEventsByUserId("user-123")
	fmt.Printf("Find by user ID: %+v\n", userIdQuery.RelatedQuery.SourceCriterion.Tags)
}

// createUserEventsQuery creates an enhanced query to find all events for a user by username
func createUserEventsQuery(username string) *enhancedeventstore.EnhancedQuery {
	return &enhancedeventstore.EnhancedQuery{
		Type: enhancedeventstore.QueryType_RELATED_EVENTS,
		RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
			// Step 1: Find the UserCreated event by username
			SourceCriterion: &enhancedeventstore.Criterion{
				Tags: []*enhancedeventstore.Tag{
					{Key: "username", Value: username},
					{Key: "eventType", Value: "UserCreated"},
				},
			},
			// Step 2: Extract userCreatedId from the source event
			ExtractField: "userCreatedId",
			// Step 3: Find all events with matching userCreatedId
			TargetField: "userCreatedId",
			// Step 4: Filter by event types (optional)
			EventTypes: []string{"UserCreated", "UserDeleted", "UserUpdated"},
		},
	}
}

// createUserEventsByEmail creates a query to find user events by email
func createUserEventsByEmail(email string) *enhancedeventstore.EnhancedQuery {
	return &enhancedeventstore.EnhancedQuery{
		Type: enhancedeventstore.QueryType_RELATED_EVENTS,
		RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
			SourceCriterion: &enhancedeventstore.Criterion{
				Tags: []*enhancedeventstore.Tag{
					{Key: "email", Value: email},
					{Key: "eventType", Value: "UserCreated"},
				},
			},
			ExtractField: "userCreatedId",
			TargetField:  "userCreatedId",
			EventTypes:   []string{"UserCreated", "UserDeleted", "UserUpdated"},
		},
	}
}

// createEventsByUserId creates a query to find events directly by user ID
func createEventsByUserId(userId string) *enhancedeventstore.EnhancedQuery {
	return &enhancedeventstore.EnhancedQuery{
		Type: enhancedeventstore.QueryType_RELATED_EVENTS,
		RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
			SourceCriterion: &enhancedeventstore.Criterion{
				Tags: []*enhancedeventstore.Tag{
					{Key: "userCreatedId", Value: userId},
				},
			},
			ExtractField: "userCreatedId",
			TargetField:  "userCreatedId",
			EventTypes:   []string{"UserCreated", "UserDeleted", "UserUpdated"},
		},
	}
}

// demonstrateConvenienceFunction shows how to use the convenience function
func demonstrateConvenienceFunction() {
	_ = context.Background()
	
	// This would be used in a real application with proper database setup
	fmt.Println("\n6. Convenience Function Usage:")
	fmt.Println("```go")
	fmt.Println("// Using the convenience function")
	fmt.Println("enhancedStore := postgres.NewEnhancedPostgresGetEvents(db, logger, mappings)")
	fmt.Println("response, err := enhancedStore.GetUserEventsByUsername(ctx, \"user_management\", \"john_doe\")")
	fmt.Println("```")
	
	// Show what the convenience function does internally
	fmt.Println("\nThis convenience function internally creates the enhanced query and executes it.")
}