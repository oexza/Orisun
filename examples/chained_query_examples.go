package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"orisun/eventstore"
	_ "github.com/lib/pq"
)

func main() {
	// Initialize database connection
	db, err := sql.Open("postgres", "postgres://user:password@localhost/eventstore?sslmode=disable")
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Create chained eventstore
	chainedStore := eventstore.NewChainedEventStorePostgres(db)

	// Example 1: User Lifecycle Tracking
	fmt.Println("=== Example 1: User Lifecycle Tracking ===")
	userLifecycleExample(chainedStore)

	// Example 2: Transaction Flow Analysis
	fmt.Println("\n=== Example 2: Transaction Flow Analysis ===")
	transactionFlowExample(chainedStore)

	// Example 3: Organization Hierarchy Traversal
	fmt.Println("\n=== Example 3: Organization Hierarchy Traversal ===")
	organizationHierarchyExample(chainedStore)

	// Example 4: Complex Multi-Hop Query
	fmt.Println("\n=== Example 4: Complex Multi-Hop Query ===")
	complexMultiHopExample(chainedStore)

	// Example 5: Simple Two-Step Query
	fmt.Println("\n=== Example 5: Simple Two-Step Query ===")
	simpleTwoStepExample(chainedStore)
}

// userLifecycleExample demonstrates tracking a user's complete lifecycle
func userLifecycleExample(store *eventstore.ChainedEventStorePostgres) {
	ctx := context.Background()

	// Build user lifecycle query
	query := eventstore.BuildUserLifecycleQuery("john.doe@example.com", 100)

	// Execute the chained query
	req := &eventstore.GetEventsChainedRequest{
		Boundary:     "stream-boundary",
		ChainedQuery: query,
	}

	resp, err := store.GetEventsChained(ctx, req)
	if err != nil {
		log.Printf("Error executing user lifecycle query: %v", err)
		return
	}

	// Display results
	fmt.Printf("User Lifecycle Query Results:\n")
	fmt.Printf("Total Steps Executed: %d\n", resp.Result.Stats.TotalStepsExecuted)
	fmt.Printf("Total Events Processed: %d\n", resp.Result.Stats.TotalEventsProcessed)
	fmt.Printf("Execution Time: %dms\n", resp.Result.Stats.TotalExecutionTimeMs)

	for _, stepResult := range resp.Result.StepResults {
		fmt.Printf("\nStep: %s\n", stepResult.StepId)
		fmt.Printf("Events Found: %d\n", len(stepResult.Events))
		fmt.Printf("Extracted Values: %v\n", stepResult.ExtractedValues)
	}
}

// transactionFlowExample demonstrates tracking transaction flows
func transactionFlowExample(store *eventstore.ChainedEventStorePostgres) {
	ctx := context.Background()

	// Create a complex transaction flow query
	query := &eventstore.ChainedQuery{
		Steps: []*eventstore.QueryStep{
			{
				StepId: "find_transaction_initiated",
				SourceCriterion: &eventstore.Criterion{
					Tags: []*eventstore.Tag{
						{Key: "transaction_id", Value: "txn-12345"},
						{Key: "event_type", Value: "TransactionInitiated"},
					},
				},
				ExtractField: "paymentMethodId",
				TargetField:  "payment_method_id",
				EventTypes:   []string{"PaymentProcessed", "PaymentFailed"},
			},
			{
				StepId:       "find_payment_events",
				ExtractField: "merchantId",
				TargetField:  "merchant_id",
				EventTypes:   []string{"MerchantSettlement", "MerchantFeeCalculated"},
				Condition: &eventstore.StepCondition{
					Type:      eventstore.ConditionType_FIELD_EXISTS,
					FieldName: "amount",
				},
			},
			{
				StepId:      "find_merchant_events",
				EventTypes:  []string{"SettlementCompleted", "FeeDeducted"},
			},
		},
		ExecutionMode:             eventstore.ChainExecutionMode_SEQUENTIAL,
		MaxResultsPerStep:         50,
		ReturnIntermediateResults: true,
	}

	req := &eventstore.GetEventsChainedRequest{
		Boundary:     "transaction-boundary",
		ChainedQuery: query,
	}

	resp, err := store.GetEventsChained(ctx, req)
	if err != nil {
		log.Printf("Error executing transaction flow query: %v", err)
		return
	}

	fmt.Printf("Transaction Flow Analysis:\n")
	fmt.Printf("Steps Executed: %d\n", resp.Result.Stats.TotalStepsExecuted)
	fmt.Printf("Total Events in Flow: %d\n", resp.Result.Stats.TotalEventsProcessed)

	for _, stepResult := range resp.Result.StepResults {
		fmt.Printf("\n%s: %d events\n", stepResult.StepId, len(stepResult.Events))
		for _, event := range stepResult.Events {
			fmt.Printf("  - %s: %s\n", event.EventType, event.EventId)
		}
	}
}

// organizationHierarchyExample demonstrates traversing organization hierarchies
func organizationHierarchyExample(store *eventstore.ChainedEventStorePostgres) {
	ctx := context.Background()

	query := &eventstore.ChainedQuery{
		Steps: []*eventstore.QueryStep{
			{
				StepId: "find_user_org",
				SourceCriterion: &eventstore.Criterion{
					Tags: []*eventstore.Tag{
						{Key: "user_id", Value: "user-789"},
						{Key: "event_type", Value: "UserAssignedToOrg"},
					},
				},
				ExtractField: "organizationId",
				TargetField:  "org_id",
				EventTypes:   []string{"OrganizationCreated", "OrganizationUpdated"},
			},
			{
				StepId:       "find_parent_org",
				ExtractField: "parentOrganizationId",
				TargetField:  "parent_org_id",
				EventTypes:   []string{"OrganizationHierarchyChanged"},
				Condition: &eventstore.StepCondition{
					Type:      eventstore.ConditionType_FIELD_EXISTS,
					FieldName: "parentOrganizationId",
				},
			},
			{
				StepId:      "find_hierarchy_events",
				EventTypes:  []string{"OrgStructureUpdated", "OrgMerged", "OrgSplit"},
			},
		},
		ExecutionMode:             eventstore.ChainExecutionMode_PARALLEL,
		MaxResultsPerStep:         25,
		ReturnIntermediateResults: false,
	}

	req := &eventstore.GetEventsChainedRequest{
		Boundary:     "org-boundary",
		ChainedQuery: query,
	}

	resp, err := store.GetEventsChained(ctx, req)
	if err != nil {
		log.Printf("Error executing organization hierarchy query: %v", err)
		return
	}

	fmt.Printf("Organization Hierarchy Traversal:\n")
	fmt.Printf("Execution Mode: Parallel\n")
	fmt.Printf("Final Events: %d\n", len(resp.Result.FinalEvents))

	for _, event := range resp.Result.FinalEvents {
		fmt.Printf("  - %s (%s): %s\n", event.EventType, event.StreamId, event.EventId)
	}
}

// complexMultiHopExample demonstrates a complex 5-step query
func complexMultiHopExample(store *eventstore.ChainedEventStorePostgres) {
	ctx := context.Background()

	query := &eventstore.ChainedQuery{
		Steps: []*eventstore.QueryStep{
			{
				StepId: "step1_find_user",
				SourceCriterion: &eventstore.Criterion{
					Tags: []*eventstore.Tag{
						{Key: "username", Value: "admin@company.com"},
					},
				},
				ExtractField: "userId",
				TargetField:  "user_id",
				EventTypes:   []string{"UserCreated", "UserUpdated"},
			},
			{
				StepId:         "step2_find_sessions",
				ExtractField:   "sessionId",
				TargetField:    "session_id",
				EventTypes:     []string{"SessionStarted"},
				DependsOnSteps: []string{"step1_find_user"},
			},
			{
				StepId:         "step3_find_actions",
				ExtractField:   "actionId",
				TargetField:    "action_id",
				EventTypes:     []string{"ActionPerformed", "ActionLogged"},
				DependsOnSteps: []string{"step2_find_sessions"},
				Condition: &eventstore.StepCondition{
					Type:           eventstore.ConditionType_FIELD_IN_LIST,
					FieldName:      "actionType",
					AllowedValues:  []string{"CREATE", "UPDATE", "DELETE"},
				},
			},
			{
				StepId:         "step4_find_resources",
				ExtractField:   "resourceId",
				TargetField:    "resource_id",
				EventTypes:     []string{"ResourceModified", "ResourceAccessed"},
				DependsOnSteps: []string{"step3_find_actions"},
			},
			{
				StepId:         "step5_find_audit_logs",
				EventTypes:     []string{"AuditLogCreated", "ComplianceCheck"},
				DependsOnSteps: []string{"step4_find_resources"},
			},
		},
		ExecutionMode:             eventstore.ChainExecutionMode_SEQUENTIAL,
		MaxResultsPerStep:         10,
		ReturnIntermediateResults: true,
	}

	// Validate the query before execution
	if err := store.ValidateChainedQuery(query); err != nil {
		log.Printf("Query validation failed: %v", err)
		return
	}

	req := &eventstore.GetEventsChainedRequest{
		Boundary:     "audit-boundary",
		ChainedQuery: query,
	}

	start := time.Now()
	resp, err := store.GetEventsChained(ctx, req)
	if err != nil {
		log.Printf("Error executing complex multi-hop query: %v", err)
		return
	}
	executionTime := time.Since(start)

	fmt.Printf("Complex Multi-Hop Query (5 steps):\n")
	fmt.Printf("Total Execution Time: %v\n", executionTime)
	fmt.Printf("Database Execution Time: %dms\n", resp.Result.Stats.TotalExecutionTimeMs)
	fmt.Printf("Steps Executed: %d\n", resp.Result.Stats.TotalStepsExecuted)
	fmt.Printf("Total Events Found: %d\n", resp.Result.Stats.TotalEventsProcessed)

	// Show step-by-step results
	for _, stepResult := range resp.Result.StepResults {
		fmt.Printf("\n%s:\n", stepResult.StepId)
		fmt.Printf("  Events: %d\n", len(stepResult.Events))
		fmt.Printf("  Extracted Values: %d\n", len(stepResult.ExtractedValues))
		if len(stepResult.ExtractedValues) > 0 {
			// Show first few extracted values
			count := 0
			fmt.Printf("  Sample Values: ")
			for key, value := range stepResult.ExtractedValues {
				if count >= 3 {
					break
				}
				fmt.Printf("%s=%s ", key, value)
				count++
			}
			fmt.Println()
		}
	}
}

// simpleTwoStepExample demonstrates the simplified API for two-step queries
func simpleTwoStepExample(store *eventstore.ChainedEventStorePostgres) {
	ctx := context.Background()

	// Use the simplified API for a common two-step pattern
	sourceTags := map[string]string{
		"user_email": "jane.smith@example.com",
		"event_type": "UserRegistered",
	}

	results, err := store.GetEventsChainedSimple(
		ctx,
		"user-boundary",
		sourceTags,
		"userId",           // Extract this field from source events
		"user_id",          // Match against this field in target events
		[]string{"UserActivated", "UserProfileUpdated"}, // Target event types
		20, // Max results
	)

	if err != nil {
		log.Printf("Error executing simple two-step query: %v", err)
		return
	}

	fmt.Printf("Simple Two-Step Query Results:\n")
	fmt.Printf("Event Pairs Found: %d\n", len(results))

	for i, pair := range results {
		fmt.Printf("\nPair %d:\n", i+1)
		fmt.Printf("  Source: %s (%s)\n", pair.SourceEvent.EventType, pair.SourceEvent.EventId)
		fmt.Printf("  Target: %s (%s)\n", pair.TargetEvent.EventType, pair.TargetEvent.EventId)
		fmt.Printf("  Extracted Value: %s\n", pair.ExtractedValue)
		fmt.Printf("  Target Created: %v\n", pair.TargetEvent.DateCreated.AsTime())
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Additional utility functions for building common query patterns

// BuildTransactionTrackingQuery creates a query to track transaction lifecycle
func BuildTransactionTrackingQuery(transactionID string) *eventstore.ChainedQuery {
	return &eventstore.ChainedQuery{
		Steps: []*eventstore.QueryStep{
			{
				StepId: "transaction_start",
				SourceCriterion: &eventstore.Criterion{
					Tags: []*eventstore.Tag{
						{Key: "transaction_id", Value: transactionID},
						{Key: "event_type", Value: "TransactionStarted"},
					},
				},
				ExtractField: "paymentId",
				TargetField:  "payment_id",
				EventTypes:   []string{"PaymentProcessed", "PaymentFailed"},
			},
			{
				StepId:      "payment_completion",
				EventTypes:  []string{"TransactionCompleted", "TransactionFailed"},
			},
		},
		ExecutionMode:             eventstore.ChainExecutionMode_SEQUENTIAL,
		MaxResultsPerStep:         100,
		ReturnIntermediateResults: true,
	}
}

// BuildAuditTrailQuery creates a query to follow audit trails
func BuildAuditTrailQuery(entityID, entityType string) *eventstore.ChainedQuery {
	return &eventstore.ChainedQuery{
		Steps: []*eventstore.QueryStep{
			{
				StepId: "entity_changes",
				SourceCriterion: &eventstore.Criterion{
					Tags: []*eventstore.Tag{
						{Key: "entity_id", Value: entityID},
						{Key: "entity_type", Value: entityType},
					},
				},
				ExtractField: "userId",
				TargetField:  "user_id",
				EventTypes:   []string{"UserAction", "SystemAction"},
			},
			{
				StepId:       "user_context",
				ExtractField: "sessionId",
				TargetField:  "session_id",
				EventTypes:   []string{"SessionActivity"},
			},
			{
				StepId:      "session_details",
				EventTypes:  []string{"LoginEvent", "LogoutEvent", "SessionExpired"},
			},
		},
		ExecutionMode:             eventstore.ChainExecutionMode_SEQUENTIAL,
		MaxResultsPerStep:         50,
		ReturnIntermediateResults: false,
	}
}