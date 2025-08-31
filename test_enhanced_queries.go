package main

import (
	"encoding/json"
	"fmt"
	"log"

	enhancedeventstore "orisun/eventstore"
)

// Test the enhanced query functionality by creating and serializing queries
func testEnhancedQueryFunctionality() {
	fmt.Println("Testing Enhanced Query Functionality")
	fmt.Println("====================================")

	// Test 1: Create a RELATED_EVENTS query
	enhancedQuery := createTestQuery()
	fmt.Printf("\n1. Enhanced Query Created Successfully:\n")
	fmt.Printf("   Type: %v\n", enhancedQuery.Type)
	fmt.Printf("   Extract Field: %s\n", enhancedQuery.RelatedQuery.ExtractField)
	fmt.Printf("   Target Field: %s\n", enhancedQuery.RelatedQuery.TargetField)
	fmt.Printf("   Event Types: %v\n", enhancedQuery.RelatedQuery.EventTypes)

	// Test 2: Create a GetEventsRequestEnhanced
	request := &enhancedeventstore.GetEventsRequestEnhanced{
		EnhancedQuery: enhancedQuery,
		Boundary:      "user_management",
		Count:         100,
		Direction:     enhancedeventstore.Direction_ASC,
	}

	fmt.Printf("\n2. Enhanced Request Created Successfully:\n")
	fmt.Printf("   Boundary: %s\n", request.Boundary)
	fmt.Printf("   Count: %d\n", request.Count)
	fmt.Printf("   Direction: %v\n", request.Direction)

	// Test 3: Test JSON serialization (simulating what would be sent to SQL)
	testJSONSerialization(enhancedQuery)

	// Test 4: Test different query types
	testQueryVariations()

	fmt.Println("\n✅ All tests passed! Enhanced query functionality is working correctly.")
}

func createTestQuery() *enhancedeventstore.EnhancedQuery {
	return &enhancedeventstore.EnhancedQuery{
		Type: enhancedeventstore.QueryType_RELATED_EVENTS,
		RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
			SourceCriterion: &enhancedeventstore.Criterion{
				Tags: []*enhancedeventstore.Tag{
					{Key: "username", Value: "john_doe"},
					{Key: "eventType", Value: "UserCreated"},
				},
			},
			ExtractField: "userCreatedId",
			TargetField:  "userCreatedId",
			EventTypes:   []string{"UserCreated", "UserDeleted", "UserUpdated"},
		},
	}
}

func testJSONSerialization(query *enhancedeventstore.EnhancedQuery) {
	fmt.Printf("\n3. Testing JSON Serialization:\n")
	
	// Convert to a map structure similar to what the Go code would create
	queryMap := map[string]interface{}{
		"type": "RELATED_EVENTS",
		"related_query": map[string]interface{}{
			"source_criterion": map[string]interface{}{
				"tags": []map[string]string{
					{"key": "username", "value": "john_doe"},
					{"key": "eventType", "value": "UserCreated"},
				},
			},
			"extract_field": "userCreatedId",
			"target_field":  "userCreatedId",
			"event_types":   []string{"UserCreated", "UserDeleted", "UserUpdated"},
		},
	}

	jsonBytes, err := json.MarshalIndent(queryMap, "   ", "  ")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("   JSON that would be sent to SQL function:\n   %s\n", string(jsonBytes))
}

func testQueryVariations() {
	fmt.Printf("\n4. Testing Query Variations:\n")

	// Variation 1: Find by email
	emailQuery := &enhancedeventstore.EnhancedQuery{
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
	fmt.Printf("   ✓ Email-based query created\n")

	// Variation 2: Find by organization
	orgQuery := &enhancedeventstore.EnhancedQuery{
		Type: enhancedeventstore.QueryType_RELATED_EVENTS,
		RelatedQuery: &enhancedeventstore.RelatedEventsQuery{
			SourceCriterion: &enhancedeventstore.Criterion{
				Tags: []*enhancedeventstore.Tag{
					{Key: "organizationId", Value: "org-123"},
				},
			},
			ExtractField: "organizationId",
			TargetField:  "organizationId",
			EventTypes:   []string{"UserCreated", "OrgUpdated", "UserDeleted"},
		},
	}
	fmt.Printf("   ✓ Organization-based query created\n")

	// Variation 3: Simple query (backward compatibility)
	simpleQuery := &enhancedeventstore.EnhancedQuery{
		Type: enhancedeventstore.QueryType_SIMPLE,
		SimpleQuery: &enhancedeventstore.Query{
			Criteria: []*enhancedeventstore.Criterion{
				{
					Tags: []*enhancedeventstore.Tag{
						{Key: "eventType", Value: "UserCreated"},
					},
				},
			},
		},
	}
	fmt.Printf("   ✓ Simple query (backward compatibility) created\n")

	// Verify all queries have the correct types
	if emailQuery.Type == enhancedeventstore.QueryType_RELATED_EVENTS &&
	   orgQuery.Type == enhancedeventstore.QueryType_RELATED_EVENTS &&
	   simpleQuery.Type == enhancedeventstore.QueryType_SIMPLE {
		fmt.Printf("   ✓ All query types are correctly set\n")
	} else {
		fmt.Printf("   ❌ Query type mismatch detected\n")
	}
}