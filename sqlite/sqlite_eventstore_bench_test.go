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

// BenchmarkWriteThroughput tests write performance with sequential inserts
func BenchmarkWriteThroughput(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		events := []orisun.EventWithMapTags{
			{
				EventId:   uuid.New().String(),
				EventType: "BenchmarkEvent",
				Data:      map[string]interface{}{"index": i, "timestamp": time.Now().UnixNano()},
			},
		}

		_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
		if err != nil {
			b.Fatalf("Failed to save event: %v", err)
		}
	}
}

// BenchmarkBatchWrite tests batch insert performance
func BenchmarkBatchWrite(b *testing.B) {
	db := getBenchmarkDB(&testing.B{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	ctx := context.Background()

	batchSizes := []int{10000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				events := make([]orisun.EventWithMapTags, batchSize)
				for j := 0; j < batchSize; j++ {
					events[j] = orisun.EventWithMapTags{
						EventId:   uuid.New().String(),
						EventType: "BatchEvent",
						Data:      map[string]interface{}{"batch": i, "index": j, "data": "some test data with more content"},
					}
				}

				_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
				if err != nil {
					b.Fatalf("Failed to save batch: %v", err)
				}
			}
		})
	}
}

// BenchmarkReadThroughput tests read performance
func BenchmarkReadThroughput(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	getEvents := NewSQLiteGetEvents(db, getTestLogger())
	ctx := context.Background()

	// Pre-populate with 10,000 events
	const totalEvents = 10000
	b.Log("Pre-populating database with", totalEvents, "events...")
	preStart := time.Now()

	for i := 0; i < totalEvents/100; i++ {
		events := make([]orisun.EventWithMapTags, 100)
		for j := 0; j < 100; j++ {
			events[j] = orisun.EventWithMapTags{
				EventId:   uuid.New().String(),
				EventType: "SetupEvent",
				Data:      map[string]interface{}{"setup": i*100 + j},
			}
		}
		_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
		if err != nil {
			b.Fatalf("Failed to populate: %v", err)
		}
	}

	b.Log("Pre-population completed in", time.Since(preStart))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
			Count:    100,
			Boundary: "bench_boundary",
		})
		if err != nil {
			b.Fatalf("Failed to get events: %v", err)
		}
	}
}

// BenchmarkReadWithCriteria tests FTS5 search performance
func BenchmarkReadWithCriteria(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	getEvents := NewSQLiteGetEvents(db, getTestLogger())
	ctx := context.Background()

	// Pre-populate with 10,000 events with varying data
	b.Log("Pre-populating database with criteria data...")
	preStart := time.Now()

	userTypes := []string{"user1", "user2", "user3", "user4", "user5"}
	statuses := []string{"active", "inactive", "pending", "suspended"}

	for i := 0; i < 10000/100; i++ {
		events := make([]orisun.EventWithMapTags, 100)
		for j := 0; j < 100; j++ {
			idx := i*100 + j
			events[j] = orisun.EventWithMapTags{
				EventId:   uuid.New().String(),
				EventType: "UserEvent",
				Data: map[string]interface{}{
					"user_id": userTypes[idx%5],
					"status":  statuses[idx%4],
					"index":   idx,
				},
			}
		}
		_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
		if err != nil {
			b.Fatalf("Failed to populate: %v", err)
		}
	}

	b.Log("Pre-population completed in", time.Since(preStart))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
			Count:    100,
			Boundary: "bench_boundary",
			Query: &orisun.Query{
				Criteria: []*orisun.Criterion{
					{
						Tags: []*orisun.Tag{
							{Key: "user_id", Value: "user1"},
							{Key: "status", Value: "active"},
						},
					},
				},
			},
		})
		if err != nil {
			b.Fatalf("Failed to get events with criteria: %v", err)
		}
	}
}

// BenchmarkMixedWorkload simulates realistic read/write mix
func BenchmarkMixedWorkload(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	getEvents := NewSQLiteGetEvents(db, getTestLogger())
	ctx := context.Background()

	// Pre-populate
	b.Log("Pre-populating database...")
	for i := 0; i < 1000; i++ {
		events := []orisun.EventWithMapTags{
			{
				EventId:   uuid.New().String(),
				EventType: "InitialEvent",
				Data:      map[string]interface{}{"index": i},
			},
		}
		saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
	}

	workloads := []struct {
		name          string
		writePercent  int
		readsPerWrite int
		withCriteria  bool
	}{
		{"Write_Only", 100, 0, false},
		{"Read_Only", 0, 1, false},
		{"Write_90_Read_10", 90, 1, false},
		{"Write_50_Read_50", 50, 1, false},
		{"Write_10_Read_90", 10, 9, false},
		{"Read_With_Criteria", 0, 1, true},
	}

	for _, workload := range workloads {
		b.Run(workload.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Writes
				if workload.writePercent > 0 && (i%100 < workload.writePercent) {
					events := []orisun.EventWithMapTags{
						{
							EventId:   uuid.New().String(),
							EventType: "MixedEvent",
							Data:      map[string]interface{}{"iteration": i, "type": "write"},
						},
					}
					_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
					if err != nil {
						b.Fatalf("Write failed: %v", err)
					}
				}

				// Reads
				if workload.writePercent < 100 {
					req := &orisun.GetEventsRequest{
						Count:    10,
						Boundary: "bench_boundary",
					}

					if workload.withCriteria {
						req.Query = &orisun.Query{
							Criteria: []*orisun.Criterion{
								{
									Tags: []*orisun.Tag{
										{Key: "type", Value: "write"},
									},
								},
							},
						}
					}

					_, err := getEvents.Get(ctx, req)
					if err != nil {
						b.Fatalf("Read failed: %v", err)
					}
				}
			}
		})
	}
}

// BenchmarkThroughput measures total throughput with 10k operations
func BenchmarkThroughput(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	getEvents := NewSQLiteGetEvents(db, getTestLogger())
	ctx := context.Background()

	operations := []struct {
		name    string
		opCount int
		op      func() error
	}{
		{
			name:    "Write_10000",
			opCount: 10000,
			op: func() error {
				events := []orisun.EventWithMapTags{
					{
						EventId:   uuid.New().String(),
						EventType: "ThroughputEvent",
						Data:      map[string]interface{}{"data": "throughput test data"},
					},
				}
				_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
				return err
			},
		},
		{
			name:    "Read_10000",
			opCount: 10000,
			op: func() error {
				_, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
					Count:    10,
					Boundary: "bench_boundary",
				})
				return err
			},
		},
		{
			name:    "Write_10000_Batch_100",
			opCount: 10000,
			op: func() error {
				events := make([]orisun.EventWithMapTags, 100)
				for i := 0; i < 100; i++ {
					events[i] = orisun.EventWithMapTags{
						EventId:   uuid.New().String(),
						EventType: "BatchThroughputEvent",
						Data:      map[string]interface{}{"index": i},
					}
				}
				_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
				return err
			},
		},
	}

	for _, op := range operations {
		b.Run(op.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				start := time.Now()
				opsCompleted := 0

				for opsCompleted < op.opCount {
					if err := op.op(); err != nil {
						b.Fatalf("Operation failed: %v", err)
					}
					opsCompleted++
				}

				elapsed := time.Since(start)
				opsPerSec := float64(op.opCount) / elapsed.Seconds()
				b.ReportMetric(opsPerSec, "ops/sec")
			}
		})
	}
}

// BenchmarkConcurrentAccess tests concurrent read/write performance
func BenchmarkConcurrentAccess(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	getEvents := NewSQLiteGetEvents(db, getTestLogger())
	ctx := context.Background()

	concurrencyLevels := []int{1, 2, 4, 8, 16}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var err error
					if (int(time.Now().UnixNano()) % 2) == 0 {
						// Write
						events := []orisun.EventWithMapTags{
							{
								EventId:   uuid.New().String(),
								EventType: "ConcurrentEvent",
								Data:      map[string]interface{}{"goroutine": concurrency},
							},
						}
						_, _, err = saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
					} else {
						// Read
						_, err = getEvents.Get(ctx, &orisun.GetEventsRequest{
							Count:    10,
							Boundary: "bench_boundary",
						})
					}
					if err != nil {
						b.Fatalf("Concurrent operation failed: %v", err)
					}
				}
			})
		})
	}
}

// BenchmarkOptimisticConcurrency tests the overhead of optimistic concurrency checks
func BenchmarkOptimisticConcurrency(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		events := []orisun.EventWithMapTags{
			{
				EventId:   uuid.New().String(),
				EventType: "OptimisticEvent",
				Data:      map[string]interface{}{"user_id": "user1", "index": i},
			},
		}
		saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
	}

	b.Run("With_Concurrency_Check", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			criteria := &orisun.Query{
				Criteria: []*orisun.Criterion{
					{
						Tags: []*orisun.Tag{
							{Key: "user_id", Value: "user1"},
						},
					},
				},
			}

			events := []orisun.EventWithMapTags{
				{
					EventId:   uuid.New().String(),
					EventType: "OptimisticEvent",
					Data:      map[string]interface{}{"user_id": "user1", "index": 1000 + i},
				},
			}

			_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, criteria)
			if err != nil {
				b.Fatalf("Save with concurrency check failed: %v", err)
			}
		}
	})

	b.Run("Without_Concurrency_Check", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			events := []orisun.EventWithMapTags{
				{
					EventId:   uuid.New().String(),
					EventType: "OptimisticEvent",
					Data:      map[string]interface{}{"user_id": "user1", "index": 10000 + i},
				},
			}

			_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
			if err != nil {
				b.Fatalf("Save without concurrency check failed: %v", err)
			}
		}
	})
}

// BenchmarkPagination tests pagination performance
func BenchmarkPagination(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	getEvents := NewSQLiteGetEvents(db, getTestLogger())
	ctx := context.Background()

	// Pre-populate
	b.Log("Pre-populating with 50,000 events...")
	const totalEvents = 50000
	for i := 0; i < totalEvents/100; i++ {
		events := make([]orisun.EventWithMapTags, 100)
		for j := 0; j < 100; j++ {
			events[j] = orisun.EventWithMapTags{
				EventId:   uuid.New().String(),
				EventType: "PaginationEvent",
				Data:      map[string]interface{}{"index": i*100 + j},
			}
		}
		saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
	}

	b.Log("Pre-population complete")

	pageSizes := []int{10, 50, 100, 500, 1000}

	for _, pageSize := range pageSizes {
		b.Run(fmt.Sprintf("PageSize_%d", pageSize), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Read in pages
				fromPosition := &orisun.Position{CommitPosition: 0, PreparePosition: 0}

				for page := 0; page < 10; page++ {
					resp, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
						Count:        uint32(pageSize),
						Boundary:     "bench_boundary",
						Direction:    orisun.Direction_ASC,
						FromPosition: fromPosition,
					})
					if err != nil {
						b.Fatalf("Failed to get page: %v", err)
					}

					if len(resp.Events) > 0 {
						fromPosition = resp.Events[len(resp.Events)-1].Position
					} else {
						break
					}
				}
			}
		})
	}
}

// Custom benchmark for 10k requests
func Benchmark10kRequests(b *testing.B) {
	db := getTestDB(&testing.T{})
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	getEvents := NewSQLiteGetEvents(db, getTestLogger())
	ctx := context.Background()

	b.Log("Starting 10,000 request benchmark...")

	scenarios := []struct {
		name          string
		writePercent  int
		batchSize     int
		useCriteria   bool
		requestsTotal int
	}{
		{"100pct_Writes_Single", 100, 1, false, 10000},
		{"100pct_Writes_Batch_10", 100, 10, false, 10000},
		{"100pct_Writes_Batch_100", 100, 100, false, 10000},
		{"90pct_Writes_10pct_Reads", 90, 1, false, 10000},
		{"50pct_Writes_50pct_Reads", 50, 1, false, 10000},
		{"10pct_Writes_90pct_Reads", 10, 1, false, 10000},
		{"100pct_Reads_Simple", 0, 1, false, 10000},
		{"100pct_Reads_With_Criteria", 0, 1, true, 10000},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			var writes, reads int
			startTime := time.Now()

			for i := 0; i < scenario.requestsTotal; i++ {
				isWrite := (i % 100) < scenario.writePercent

				if isWrite {
					// Perform write
					events := make([]orisun.EventWithMapTags, scenario.batchSize)
					for j := 0; j < scenario.batchSize; j++ {
						events[j] = orisun.EventWithMapTags{
							EventId:   uuid.New().String(),
							EventType: "BenchEvent",
							Data:      map[string]interface{}{"iteration": i, "batch_index": j},
						}
					}

					_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
					if err != nil {
						b.Fatalf("Write failed at iteration %d: %v", i, err)
					}
					writes += len(events)
				} else {
					// Perform read
					req := &orisun.GetEventsRequest{
						Count:    10,
						Boundary: "bench_boundary",
					}

					if scenario.useCriteria {
						req.Query = &orisun.Query{
							Criteria: []*orisun.Criterion{
								{
									Tags: []*orisun.Tag{
										{Key: "iteration", Value: fmt.Sprintf("%d", i)},
									},
								},
							},
						}
					}

					_, err := getEvents.Get(ctx, req)
					if err != nil {
						b.Fatalf("Read failed at iteration %d: %v", i, err)
					}
					reads++
				}

				// Progress reporting every 1000 operations
				if (i+1)%1000 == 0 {
					elapsed := time.Since(startTime)
					rate := float64(i+1) / elapsed.Seconds()
					b.Logf("Progress: %d/%d (%.1f%%) - %.2f req/sec", i+1, scenario.requestsTotal,
						float64(i+1)*100/float64(scenario.requestsTotal), rate)
				}
			}

			totalTime := time.Since(startTime)
			totalOps := writes + reads
			opsPerSec := float64(totalOps) / totalTime.Seconds()
			eventsPerSec := float64(writes) / totalTime.Seconds()

			b.ReportMetric(opsPerSec, "total_ops/sec")
			b.ReportMetric(eventsPerSec, "events/sec")

			b.Logf("\n=== Results for %s ===", scenario.name)
			b.Logf("Total Requests: %d", totalOps)
			b.Logf("  Writes: %d events in %d batches (batch size: %d)", writes, writes/scenario.batchSize, scenario.batchSize)
			b.Logf("  Reads: %d", reads)
			b.Logf("Total Time: %v", totalTime)
			b.Logf("Throughput: %.2f ops/sec (%.2f events/sec)", opsPerSec, eventsPerSec)
			b.Logf("Avg Latency: %.2f ms", float64(totalTime.Milliseconds())/float64(totalOps))
		})
	}
}

// BenchmarkSaveEvents_DirectDatabase10K tests the SQLite eventstore Save method directly
// with 10,000 concurrent goroutines all saving simultaneously to test optimistic concurrency
// Similar to the PostgreSQL version in cmd/benchmark_test.go
func BenchmarkSaveEvents_DirectDatabase10K(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	db := getBenchmarkDB(b)
	defer db.Close()

	saveEvents := NewSQLiteSaveEvents(db, getTestLogger())
	ctx := context.Background()

	// Prepare test events - 10K simultaneous saves to the SAME boundary
	const totalEvents = 10000

	b.ReportAllocs()

	for b.Loop() {
		var wg sync.WaitGroup
		startSignal := make(chan struct{})
		var successfulSaves int64
		var totalAttempts int64

		// Pre-create ALL goroutines first, but don't let them execute yet
		for eventIndex := range totalEvents {
			wg.Add(1)

			// Create a single event with different criteria for each goroutine
			event := orisun.EventWithMapTags{
				EventId:   uuid.New().String(),
				EventType: "ConcurrentBenchmarkEvent",
				Data: map[string]interface{}{
					"event_id":  eventIndex,
					"criteria":  fmt.Sprintf("type_%d", eventIndex%100),
					"timestamp": time.Now().Format(time.RFC3339),
					"benchmark": "sqlite_concurrent",
				},
				Metadata: map[string]interface{}{
					"benchmark":   "concurrent_same_boundary",
					"event_index": eventIndex,
				},
			}

			// Create unique consistency condition for each event
			// This prevents optimistic concurrency conflicts between different events
			uniqueConsistencyCondition := &orisun.Query{
				Criteria: []*orisun.Criterion{
					{
						Tags: []*orisun.Tag{
							{
								Key:   "event_index",
								Value: fmt.Sprintf("%d", eventIndex),
							},
							{
								Key:   "benchmark_run",
								Value: fmt.Sprintf("%d", time.Now().UnixNano()),
							},
						},
					},
				},
			}

			go func(eventID int, event orisun.EventWithMapTags, consistencyCondition *orisun.Query) {
				defer wg.Done()

				// WAIT for the start signal - this ensures TRUE SIMULTANEITY!
				<-startSignal

				// ALL goroutines try to save to the SAME boundary with expected position -1
				// Each has unique consistency condition, so all should succeed
				p := orisun.NotExistsPosition()
				_, _, err := saveEvents.Save(
					ctx,
					[]orisun.EventWithMapTags{event},
					"bench_boundary",
					&p,                   // Expected position for a new stream
					consistencyCondition, // Unique consistency condition for each event
				)

				// Use atomic operations to avoid mutex overhead
				atomic.AddInt64(&totalAttempts, 1)
				if err == nil {
					atomic.AddInt64(&successfulSaves, 1)
				} else {
					// Log unexpected errors (optimistic concurrency is expected with same criteria)
					b.Logf("Event %d failed: %v", eventID, err)
				}
			}(eventIndex, event, uniqueConsistencyCondition)
		}

		// NOW start the timer and release all goroutines simultaneously
		start := time.Now()
		close(startSignal) // This releases ALL 10,000 goroutines at the EXACT same time!

		wg.Wait()
		duration := time.Since(start)

		finalTotalAttempts := atomic.LoadInt64(&totalAttempts)
		finalSuccessfulSaves := atomic.LoadInt64(&successfulSaves)
		attemptsPerSecond := float64(finalTotalAttempts) / duration.Seconds()

		// Focus on events per second metric
		b.ReportMetric(attemptsPerSecond, "events/sec")

		b.Logf("🚀 TRULY SIMULTANEOUS TEST (SQLite): %d attempts (%d successful) in %.2f seconds at %.1f events/sec",
			finalTotalAttempts, finalSuccessfulSaves, duration.Seconds(), attemptsPerSecond)
	}
}
