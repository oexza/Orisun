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

// BenchmarkQueuedEventStore_10K tests the queued event store with 10,000 concurrent requests
func BenchmarkQueuedEventStore_10K(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	pool := getBenchmarkDB(b)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())

	// Create queued event store with queue size of 10,000
	queueSize := 10000
	queuedStore := NewQueuedEventStore(saveEvents, queueSize, getTestLogger())
	queuedStore.Start()
	defer queuedStore.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	// Prepare test configuration
	const totalEvents = 10000
	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	var successfulSaves int64
	var totalAttempts int64

	// Pre-create ALL goroutines
	for eventIndex := range totalEvents {
		wg.Add(1)

		event := orisun.EventWithMapTags{
			EventId:   uuid.New().String(),
			EventType: "QueuedBenchmarkEvent",
			Data: map[string]interface{}{
				"event_id":  eventIndex,
				"timestamp": time.Now().UnixNano(),
			},
		}

		// Unique consistency condition for each event
		uniqueConsistencyCondition := &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{
							Key:   "event_index",
							Value: fmt.Sprintf("%d", eventIndex),
						},
					},
				},
			},
		}

		go func(eventID int, event orisun.EventWithMapTags, consistencyCondition *orisun.Query) {
			defer wg.Done()

			// Wait for start signal
			<-startSignal

			// Save via queued store
			_, _, err := queuedStore.Save(
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

	b.Logf("🚀 QUEUED EVENT STORE TEST: %d attempts (%d successful) in %.2f seconds at %.1f events/sec",
		finalTotalAttempts, finalSuccessfulSaves, duration.Seconds(), attemptsPerSecond)
}

// BenchmarkQueuedEventStore_Batched tests batching multiple requests together
func BenchmarkQueuedEventStore_Batched(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	pool := getBenchmarkDB(b)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())

	// Smaller queue size for batching
	queueSize := 100
	queuedStore := NewQueuedEventStore(saveEvents, queueSize, getTestLogger())
	queuedStore.Start()
	defer queuedStore.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	// Send 10,000 events in batches of 100
	const totalBatches = 100
	const batchSize = 100
	const totalEvents = totalBatches * batchSize

	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	var successfulSaves int64
	var totalAttempts int64

	for batchIdx := range totalBatches {
		wg.Add(1)

		go func(batchNum int) {
			defer wg.Done()
			<-startSignal

			// Create batch of events
			events := make([]orisun.EventWithMapTags, batchSize)
			for j := range batchSize {
				events[j] = orisun.EventWithMapTags{
					EventId:   uuid.New().String(),
					EventType: "BatchedQueuedEvent",
					Data: map[string]interface{}{
						"batch": batchNum,
						"index": j,
					},
				}
			}

			_, _, err := queuedStore.Save(
				ctx,
				events,
				"bench_boundary",
				nil,
				nil,
			)

			atomic.AddInt64(&totalAttempts, 1)
			if err == nil {
				atomic.AddInt64(&successfulSaves, int64(batchSize))
			} else {
				b.Logf("Batch %d failed: %v", batchNum, err)
			}
		}(batchIdx)
	}

	start := time.Now()
	close(startSignal)

	wg.Wait()
	duration := time.Since(start)

	finalSuccessfulSaves := atomic.LoadInt64(&successfulSaves)
	eventsPerSecond := float64(finalSuccessfulSaves) / duration.Seconds()

	b.ReportMetric(eventsPerSecond, "events/sec")

	b.Logf("🚀 QUEUED BATCHED TEST: %d events (%d successful) in %.2f seconds at %.1f events/sec",
		totalEvents, finalSuccessfulSaves, duration.Seconds(), eventsPerSecond)
}
