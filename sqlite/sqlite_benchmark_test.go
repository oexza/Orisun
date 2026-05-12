package sqlite

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	common "github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
)

const (
	benchBoundary        = "bench_boundary"
	benchNumStreams      = 500
	benchEventsPerStream = 20
)

// setupBenchmarkPools opens a single-boundary pool set against a temp directory.
// Returns the SqliteSaveEvents/SqliteAdminDB and a teardown closure.
func setupBenchmarkPools(b *testing.B) (*SqliteSaveEvents, *SqliteGetEvents, *SqliteAdminDB, func()) {
	b.Helper()
	dir := b.TempDir()
	logger, err := logging.ZapLogger("warn")
	require.NoError(b, err)

	bp, err := OpenBoundaryPools(context.Background(), dir, benchBoundary, benchBoundary)
	require.NoError(b, err, "open pools")
	pools := map[string]*BoundaryPools{benchBoundary: bp}

	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)
	admin := NewSqliteAdminDB(pools, benchBoundary, logger)

	return saver, getter, admin, func() { _ = bp.Close() }
}

func parseTxID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// prepopulateStreams creates events for `numStreams` streams with `eventsPerStream` each,
// returning the stream IDs and final positions per stream.
func prepopulateStreams(
	b *testing.B,
	ctx context.Context,
	saver *SqliteSaveEvents,
	numStreams, eventsPerStream int,
) ([]string, []*orisun.Position) {
	streamIds := make([]string, numStreams)
	positions := make([]*orisun.Position, numStreams)

	for i := 0; i < numStreams; i++ {
		id, err := uuid.NewV7()
		require.NoError(b, err)
		streamIds[i] = id.String()
	}

	for sIdx := 0; sIdx < numStreams; sIdx++ {
		streamID := streamIds[sIdx]
		pos := orisun.NotExistsPosition()

		for e := 0; e < eventsPerStream; e++ {
			eventID, err := uuid.NewV7()
			require.NoError(b, err)

			data := fmt.Sprintf(`{"stream_id":"%s","eventType":"OrderPlaced","sequence":%d}`, streamID, e)
			meta := fmt.Sprintf(`{"timestamp":"%s"}`, time.Now().Format(time.RFC3339))

			tranID, gid, err := saver.Save(ctx, []orisun.EventWithMapTags{{
				EventId:   eventID.String(),
				EventType: "OrderPlaced",
				Data:      data,
				Metadata:  meta,
			}}, benchBoundary, &pos, nil)
			require.NoError(b, err, "prepopulate stream=%d event=%d", sIdx, e)

			pos = orisun.Position{
				CommitPosition:  parseTxID(tranID),
				PreparePosition: gid,
			}
		}

		final := pos
		positions[sIdx] = &final
	}

	return streamIds, positions
}

// BenchmarkSqlite_ConsistencyCheck_NoIndex measures Save throughput with a CCC criterion
// against an unindexed table. Mirrors postgres/postgres_benchmark_test.go for direct comparison.
func BenchmarkSqlite_ConsistencyCheck_NoIndex(b *testing.B) {
	saver, _, _, teardown := setupBenchmarkPools(b)
	defer teardown()

	ctx := context.Background()
	streamIds, positions := prepopulateStreams(b, ctx, saver, benchNumStreams, benchEventsPerStream)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sIdx := i % benchNumStreams
		streamID := streamIds[sIdx]

		eventID, err := uuid.NewV7()
		require.NoError(b, err)

		data := fmt.Sprintf(`{"stream_id":"%s","eventType":"OrderPlaced","sequence":%d}`, streamID, benchEventsPerStream+i)
		meta := fmt.Sprintf(`{"timestamp":"%s"}`, time.Now().Format(time.RFC3339))

		query := &orisun.Query{
			Criteria: []*orisun.Criterion{{
				Tags: []*orisun.Tag{{Key: "stream_id", Value: streamID}},
			}},
		}

		pos := positions[sIdx]
		tranID, gid, err := saver.Save(ctx, []orisun.EventWithMapTags{{
			EventId:   eventID.String(),
			EventType: "OrderPlaced",
			Data:      data,
			Metadata:  meta,
		}}, benchBoundary, pos, query)
		require.NoError(b, err, "iteration %d", i)

		newPos := orisun.Position{CommitPosition: parseTxID(tranID), PreparePosition: gid}
		positions[sIdx] = &newPos
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "saves/sec")
}

// BenchmarkSqlite_ConsistencyCheck_WithIndex creates a JSON-expression partial index
// on stream_id before measuring, isolating the index-vs-scan delta on the CCC path.
func BenchmarkSqlite_ConsistencyCheck_WithIndex(b *testing.B) {
	saver, _, admin, teardown := setupBenchmarkPools(b)
	defer teardown()

	ctx := context.Background()
	streamIds, positions := prepopulateStreams(b, ctx, saver, benchNumStreams, benchEventsPerStream)

	require.NoError(b, admin.CreateBoundaryIndex(ctx, benchBoundary, "stream_id",
		[]common.IndexField{{JsonKey: "stream_id", ValueType: "text"}}, nil, ""))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sIdx := i % benchNumStreams
		streamID := streamIds[sIdx]

		eventID, err := uuid.NewV7()
		require.NoError(b, err)

		data := fmt.Sprintf(`{"stream_id":"%s","eventType":"OrderPlaced","sequence":%d}`, streamID, benchEventsPerStream+i)
		meta := fmt.Sprintf(`{"timestamp":"%s"}`, time.Now().Format(time.RFC3339))

		query := &orisun.Query{
			Criteria: []*orisun.Criterion{{
				Tags: []*orisun.Tag{{Key: "stream_id", Value: streamID}},
			}},
		}

		pos := positions[sIdx]
		tranID, gid, err := saver.Save(ctx, []orisun.EventWithMapTags{{
			EventId:   eventID.String(),
			EventType: "OrderPlaced",
			Data:      data,
			Metadata:  meta,
		}}, benchBoundary, pos, query)
		require.NoError(b, err, "iteration %d", i)

		positions[sIdx] = &orisun.Position{CommitPosition: parseTxID(tranID), PreparePosition: gid}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "saves/sec")
}

// BenchmarkSqlite_SerialSave_NoCriteria measures the bare insert path: no CCC,
// single-event batches, serial.
func BenchmarkSqlite_SerialSave_NoCriteria(b *testing.B) {
	saver, _, _, teardown := setupBenchmarkPools(b)
	defer teardown()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eventID, _ := uuid.NewV7()
		_, _, err := saver.Save(ctx, []orisun.EventWithMapTags{{
			EventId:   eventID.String(),
			EventType: "Bench",
			Data:      `{"k":"v"}`,
			Metadata:  `{}`,
		}}, benchBoundary, nil, nil)
		require.NoError(b, err)
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "saves/sec")
}

// BenchmarkSqlite_BatchSave varies events-per-call to show amortization across batch sizes.
func BenchmarkSqlite_BatchSave(b *testing.B) {
	for _, batchSize := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			saver, _, _, teardown := setupBenchmarkPools(b)
			defer teardown()

			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				events := make([]orisun.EventWithMapTags, batchSize)
				for j := 0; j < batchSize; j++ {
					id, _ := uuid.NewV7()
					events[j] = orisun.EventWithMapTags{
						EventId:   id.String(),
						EventType: "Bench",
						Data:      `{"k":"v"}`,
						Metadata:  `{}`,
					}
				}
				_, _, err := saver.Save(ctx, events, benchBoundary, nil, nil)
				require.NoError(b, err)
			}
			b.ReportMetric(float64(b.N*batchSize)/b.Elapsed().Seconds(), "events/sec")
		})
	}
}

// BenchmarkSqlite_ConcurrentSave saturates the write pool with N goroutines.
// Reports events/sec — the metric to watch for v2 writer-coalescing wins.
func BenchmarkSqlite_ConcurrentSave(b *testing.B) {
	for _, conc := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("workers=%d", conc), func(b *testing.B) {
			saver, _, _, teardown := setupBenchmarkPools(b)
			defer teardown()

			ctx := context.Background()
			b.ResetTimer()

			var done int64
			var wg sync.WaitGroup
			startCh := make(chan struct{})
			start := time.Now()

			for w := 0; w < conc; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-startCh
					for {
						i := atomic.AddInt64(&done, 1)
						if i > int64(b.N) {
							return
						}
						id, _ := uuid.NewV7()
						_, _, err := saver.Save(ctx, []orisun.EventWithMapTags{{
							EventId:   id.String(),
							EventType: "Bench",
							Data:      `{"k":"v"}`,
							Metadata:  `{}`,
						}}, benchBoundary, nil, nil)
						if err != nil {
							b.Errorf("save failed: %v", err)
							return
						}
					}
				}()
			}
			close(startCh)
			wg.Wait()
			elapsed := time.Since(start)

			b.ReportMetric(float64(b.N)/elapsed.Seconds(), "events/sec")
		})
	}
}

// BenchmarkSqlite_Burst5000 fires 5000 goroutines simultaneously, each issuing exactly
// one Save. Measures the queue-up + drain pattern at the boundary's write pool — the
// realistic worst-case shape for v1 (sequential through write pool size 1).
//
// Reports total wall time and events/sec across the burst. b.N controls how many bursts
// (`-benchtime=Nx` to run a fixed number).
func BenchmarkSqlite_Burst10000(b *testing.B) {
	const burst = 10000

	for i := 0; i < b.N; i++ {
		// Fresh DB per burst — avoids cross-iteration contamination on b.N>1.
		saver, _, _, teardown := setupBenchmarkPools(b)

		// Pre-build events so allocation isn't in the hot path.
		events := make([]orisun.EventWithMapTags, burst)
		for j := 0; j < burst; j++ {
			id, _ := uuid.NewV7()
			events[j] = orisun.EventWithMapTags{
				EventId:   id.String(),
				EventType: "BurstEvent",
				Data:      `{"k":"v"}`,
				Metadata:  `{}`,
			}
		}

		ctx := context.Background()
		var wg sync.WaitGroup
		var ok, fail int64
		startCh := make(chan struct{})
		wg.Add(burst)
		for j := 0; j < burst; j++ {
			ev := events[j]
			go func() {
				defer wg.Done()
				<-startCh
				if _, _, err := saver.Save(ctx, []orisun.EventWithMapTags{ev}, benchBoundary, nil, nil); err != nil {
					atomic.AddInt64(&fail, 1)
					return
				}
				atomic.AddInt64(&ok, 1)
			}()
		}

		b.ResetTimer()
		startTime := time.Now()
		close(startCh)
		wg.Wait()
		elapsed := time.Since(startTime)
		b.StopTimer()

		b.ReportMetric(float64(ok)/elapsed.Seconds(), "events/sec")
		b.ReportMetric(float64(elapsed.Milliseconds()), "ms/burst")
		if fail > 0 {
			b.Logf("burst had %d failures", fail)
		}

		teardown()
	}
}

// BenchmarkSqlite_GetEvents measures point-in-time read throughput with criteria filter.
func BenchmarkSqlite_GetEvents(b *testing.B) {
	saver, getter, _, teardown := setupBenchmarkPools(b)
	defer teardown()

	ctx := context.Background()
	streamIds, _ := prepopulateStreams(b, ctx, saver, 100, 20)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		streamID := streamIds[i%len(streamIds)]
		_, err := getter.Get(ctx, &orisun.GetEventsRequest{
			Boundary:  benchBoundary,
			Direction: orisun.Direction_ASC,
			Count:     50,
			Query: &orisun.Query{
				Criteria: []*orisun.Criterion{{
					Tags: []*orisun.Tag{{Key: "stream_id", Value: streamID}},
				}},
			},
		})
		require.NoError(b, err)
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "gets/sec")
}
