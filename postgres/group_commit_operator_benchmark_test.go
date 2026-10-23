package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Competing database sessions target the same boundary, so preparation can
// overlap but state observations and writes must serialize under its lock.
func BenchmarkPostgres_GroupCommitContendedPreparation(b *testing.B) {
	db, teardown := setupBenchmarkDB(b)
	defer teardown()
	logger, err := logging.ZapLogger("error")
	require.NoError(b, err)
	saver := NewPostgresSaveEvents(context.Background(), db, logger, map[string]config.BoundaryToPostgresSchemaMapping{
		"bench_boundary": {Boundary: "bench_boundary", Schema: "public"},
	})
	defer saver.close()
	const batchSize = 64
	for _, writers := range []int{1, 4} {
		b.Run(fmt.Sprintf("writers_%d", writers), func(b *testing.B) {
			batches := make([][]*postgresTestRequest, writers)
			for writer := range batches {
				for i := range batchSize {
					scope := fmt.Sprintf("%d:%d", writer, i)
					events, err := orisun.PrepareEventsForSave([]orisun.EventWithMapTags{{EventId: uuid.NewString(), EventType: "ContentionBench", Data: map[string]any{"scope": scope, "amount": 15}}})
					require.NoError(b, err)
					batches[writer] = append(batches[writer], &postgresTestRequest{ctx: context.Background(), events: events, consistency: []orisun.ConsistencyCheck{{Criteria: []orisun.ReadCriterion{{Tags: []orisun.ReadTag{{Key: "scope", Value: scope}, {Key: "amount", Operator: "gt", Value: "10"}}}}, Position: orisun.NotExistsPosition()}}})
				}
			}
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				_, err := db.Exec("TRUNCATE public.bench_boundary_orisun_es_event, public.bench_boundary_orisun_es_write")
				require.NoError(b, err)
				start := make(chan struct{})
				outcomes := make([][]postgresTestOutcome, writers)
				errors := make([]error, writers)
				var workers sync.WaitGroup
				for writer := range writers {
					workers.Add(1)
					go func() {
						defer workers.Done()
						<-start
						outcomes[writer], errors[writer] = saver.executeTestBatch(context.Background(), "bench_boundary", batches[writer])
					}()
				}
				b.StartTimer()
				close(start)
				workers.Wait()
				b.StopTimer()
				for writer := range writers {
					require.NoError(b, errors[writer])
					require.Len(b, outcomes[writer], batchSize)
					for _, outcome := range outcomes[writer] {
						require.NoError(b, outcome.err)
					}
				}
				b.StartTimer()
			}
			b.StopTimer()
			b.ReportMetric(float64(writers*batchSize*b.N)/b.Elapsed().Seconds(), "accepted/sec")
		})
	}
}

// This measures complete flushes (serialization, SQL, and durable commit), not
// queueing latency. Each iteration starts with an empty table so history growth
// cannot disguise the cost of matching earlier accepted writes in the batch.
func BenchmarkPostgres_GroupCommitOperators(b *testing.B) {
	db, teardown := setupBenchmarkDB(b)
	defer teardown()
	logger, err := logging.ZapLogger("error")
	require.NoError(b, err)
	saver := NewPostgresSaveEvents(context.Background(), db, logger, map[string]config.BoundaryToPostgresSchemaMapping{
		"bench_boundary": {Boundary: "bench_boundary", Schema: "public"},
	})
	defer saver.close()
	for _, operator := range []string{"eq", "ne", "gt", "gte", "lt", "lte"} {
		for _, size := range []int{16, 64} {
			for _, events := range []int{1, 8} {
				for _, bytes := range []int{128, 4096} {
					for _, conflicts := range []int{0, 50} {
						name := fmt.Sprintf("%s/requests_%d/events_%d/bytes_%d/conflicts_%d", operator, size, events, bytes, conflicts)
						b.Run(name, func(b *testing.B) {
							value := map[string]int{"eq": 10, "ne": 11, "gt": 11, "gte": 10, "lt": 9, "lte": 10}[operator]
							requests := make([]*postgresTestRequest, size)
							for i := range requests {
								scope := fmt.Sprint(i)
								if conflicts == 50 {
									scope = fmt.Sprint(i / 2)
								}
								input := make([]orisun.EventWithMapTags, events)
								for j := range input {
									input[j] = orisun.EventWithMapTags{EventId: uuid.NewString(), EventType: "OperatorBench", Data: map[string]any{"scope": scope, "amount": value, "payload": strings.Repeat("x", bytes)}}
								}
								prepared, err := orisun.PrepareEventsForSave(input)
								require.NoError(b, err)
								requests[i] = &postgresTestRequest{ctx: context.Background(), events: prepared, consistency: []orisun.ConsistencyCheck{{Criteria: []orisun.ReadCriterion{{Tags: []orisun.ReadTag{{Key: "scope", Value: scope}, {Key: "amount", Value: "10", Operator: operator}}}}, Position: orisun.NotExistsPosition()}}}
							}
							latencies := make([]time.Duration, 0, b.N)
							b.ResetTimer()
							for range b.N {
								b.StopTimer()
								_, err := db.Exec("TRUNCATE public.bench_boundary_orisun_es_event, public.bench_boundary_orisun_es_write")
								require.NoError(b, err)
								b.StartTimer()
								start := time.Now()
								outcomes, err := saver.executeTestBatch(context.Background(), "bench_boundary", requests)
								latencies = append(latencies, time.Since(start))
								b.StopTimer()
								require.NoError(b, err)
								require.Len(b, outcomes, size)
								for i, outcome := range outcomes {
									if conflicts == 50 && i%2 == 1 {
										require.ErrorContains(b, outcome.err, "OptimisticConcurrencyException")
									} else {
										require.NoError(b, outcome.err)
									}
								}
								b.StartTimer()
							}
							b.StopTimer()
							sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
							b.ReportMetric(float64(latencies[(len(latencies)*95+99)/100-1].Nanoseconds())/1e6, "flush-p95-ms")
							b.ReportMetric(float64(latencies[(len(latencies)*99+99)/100-1].Nanoseconds())/1e6, "flush-p99-ms")
							b.ReportMetric(float64(size*b.N)*float64(100-conflicts)/100/b.Elapsed().Seconds(), "accepted/sec")
						})
					}
				}
			}
		}
	}
}
