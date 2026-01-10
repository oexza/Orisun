package sqlite

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oexza/Orisun/orisun"
)

// BenchmarkBatchedQueuedEventStore_10K tests the batched queued event store with 10,000 concurrent requests
func BenchmarkBatchedQueuedEventStore_10K(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	db := getBenchmarkDB(b)
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())

	// Create batched queued event store with queue size of 10,000 and max batch size of 10,000
	queueSize := 15000
	maxBatchSize := 20000
	batchedStore := NewBatchedQueuedEventStore(saveEvents, queueSize, maxBatchSize, getTestLogger())
	batchedStore.Start()
	defer batchedStore.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	// Prepare test configuration
	const totalEvents = 15000
	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	var successfulSaves int64
	var totalAttempts int64

	// Pre-create ALL goroutines
	for eventIndex := range totalEvents {
		wg.Add(1)

		event := orisun.EventWithMapTags{
			EventId:   uuid.New().String(),
			EventType: "BatchedQueuedBenchmarkEvent",
			Data: map[string]interface{}{
				"event_id":       eventIndex,
				"timestamp":      time.Now().UnixNano(),
				"user_id":        fmt.Sprintf("user_%d", eventIndex%1000),
				"session_id":     uuid.New().String(),
				"action":         "create",
				"resource_type":  "document",
				"resource_id":    fmt.Sprintf("resource_%d", eventIndex),
				"ip_address":     "192.168.1.1",
				"user_agent":     "Mozilla/5.0",
				"request_id":     uuid.New().String(),
				"status":         "pending",
				"priority":       eventIndex % 5,
				"tags":           []string{"tag1", "tag2", "tag3"},
				"source":         "api",
				"version":        "1.0",
				"env":            "production",
				"client_version": "2.1.0",
				"device_type":    "mobile",
				"location":       "US",
				"retry_count":    0,
				"timeout":        30,
			},
		}

		// Unique consistency condition for each event
		// Multiple criteria with multiple tags to test real-world scenarios
		uniqueConsistencyCondition := &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "user_id", Value: fmt.Sprintf("user_%d", eventIndex)},
						{Key: "action", Value: fmt.Sprintf("action_%d", eventIndex)},
					},
				},
				{
					Tags: []*orisun.Tag{
						{Key: "resource_type", Value: fmt.Sprintf("type_%d", eventIndex)},
						{Key: "status", Value: fmt.Sprintf("status_%d", eventIndex)},
					},
				},
				{
					Tags: []*orisun.Tag{
						{Key: "resource_typee", Value: fmt.Sprintf("typee_%d", eventIndex)},
						{Key: "statuss", Value: fmt.Sprintf("statuss_%d", eventIndex)},
					},
				},
			},
		}

		go func(eventID int, event orisun.EventWithMapTags, consistencyCondition *orisun.Query) {
			defer wg.Done()

			// Wait for start signal
			<-startSignal

			// Save via batched queued store
			_, _, err := batchedStore.Save(
				ctx,
				[]orisun.EventWithMapTags{event},
				"bench_boundary",
				nil,
				consistencyCondition,
			)

			atomic.AddInt64(&totalAttempts, 1)
			if err == nil {
				atomic.AddInt64(&successfulSaves, 1)
			} else {
				b.Logf("Event %d failed: %v", eventID, err)
			}
		}(eventIndex, event, uniqueConsistencyCondition)
	}

	// Start timer and release all goroutines
	start := time.Now()
	close(startSignal)

	wg.Wait()
	duration := time.Since(start)

	finalTotalAttempts := atomic.LoadInt64(&totalAttempts)
	finalSuccessfulSaves := atomic.LoadInt64(&successfulSaves)
	attemptsPerSecond := float64(finalTotalAttempts) / duration.Seconds()

	b.ReportMetric(attemptsPerSecond, "events/sec")

	b.Logf("🚀 BATCHED QUEUED EVENT STORE TEST: %d attempts (%d successful) in %.2f seconds at %.1f events/sec",
		finalTotalAttempts, finalSuccessfulSaves, duration.Seconds(), attemptsPerSecond)
}

// BenchmarkBatchedQueuedEventStore_Concurrent tests cross-batch concurrency validation
// This test verifies that multiple requests for the same criteria in a batch are properly validated
func BenchmarkBatchedQueuedEventStore_Concurrent(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	db := getBenchmarkDB(b)
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())

	// Create batched queued event store with batch size >= total requests
	// This ensures all requests are processed in a single batch
	queueSize := 100
	maxBatchSize := 100
	batchedStore := NewBatchedQueuedEventStore(saveEvents, queueSize, maxBatchSize, getTestLogger())
	batchedStore.Start()
	defer batchedStore.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	// Test scenario: 100 concurrent requests for the SAME criteria
	// Only the first should succeed, the rest should fail with optimistic concurrency error
	const totalRequests = 100
	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	var successfulSaves int64
	var failedSaves int64

	// All requests target the same criteria with multiple tags
	// This tests OR logic between criteria and AND logic within criteria
	sharedConsistencyCondition := &orisun.Query{
		Criteria: []*orisun.Criterion{
			{
				Tags: []*orisun.Tag{
					{Key: "device_type", Value: "mobile"},
					{Key: "location", Value: "US"},
				},
			},
			{
				Tags: []*orisun.Tag{
					{Key: "device_type", Value: "desktop"},
					{Key: "location", Value: "EU"},
				},
			},
		},
	}

	// All requests expect position -1, -1 (new stream)
	// Only the first request should succeed; the rest should fail with optimistic concurrency error
	expectedPosition := &orisun.Position{
		CommitPosition:  -1,
		PreparePosition: -1,
	}

	for requestIndex := range totalRequests {
		wg.Add(1)

		event := orisun.EventWithMapTags{
			EventId:   uuid.New().String(),
			EventType: "ConcurrentTestEvent",
			Data: map[string]interface{}{
				"request_id":     requestIndex,
				"timestamp":      time.Now().UnixNano(),
				"user_id":        fmt.Sprintf("user_%d", requestIndex%100),
				"session_id":     uuid.New().String(),
				"action":         "update",
				"resource_type":  "profile",
				"resource_id":    fmt.Sprintf("resource_%d", requestIndex),
				"ip_address":     "10.0.0.1",
				"user_agent":     "Chrome/120.0",
				"status":         "active",
				"priority":       requestIndex % 3,
				"tags":           []string{"concurrent", "test"},
				"source":         "test",
				"version":        "2.0",
				"env":            "staging",
				"client_version": "3.0.1",
				"device_type":    "desktop",
				"location":       "EU",
				"retry_count":    1,
				"timeout":        15,
			},
		}

		go func(reqID int, event orisun.EventWithMapTags) {
			defer wg.Done()

			// Wait for start signal
			<-startSignal

			// Save via batched queued store
			_, _, err := batchedStore.Save(
				ctx,
				[]orisun.EventWithMapTags{event},
				"bench_boundary",
				expectedPosition,
				sharedConsistencyCondition,
			)

			if err == nil {
				atomic.AddInt64(&successfulSaves, 1)
			} else {
				atomic.AddInt64(&failedSaves, 1)
			}
		}(requestIndex, event)
	}

	// Start timer and release all goroutines
	start := time.Now()
	close(startSignal)

	wg.Wait()
	duration := time.Since(start)

	finalSuccessfulSaves := atomic.LoadInt64(&successfulSaves)
	finalFailedSaves := atomic.LoadInt64(&failedSaves)

	b.ReportMetric(float64(totalRequests)/duration.Seconds(), "requests/sec")

	b.Logf("🎯 CROSS-BATCH CONCURRENCY TEST: %d successful, %d failed out of %d total requests in %.2f seconds",
		finalSuccessfulSaves, finalFailedSaves, totalRequests, duration.Seconds())

	// We expect only 1 success (the first request) and 99 failures
	if finalSuccessfulSaves != 1 {
		b.Errorf("Expected 1 successful save, got %d", finalSuccessfulSaves)
	}
}
