package sqlite

import (
	"fmt"
	"testing"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/stretchr/testify/require"
)

// Each flush has 100 requests. Requests sharing a context all expect absence;
// only the first may succeed, including when its write is still uncommitted.
func BenchmarkSqlite_GroupCommitConflicts(b *testing.B) {
	for _, copies := range []int{1, 2, 10} {
		b.Run(fmt.Sprintf("rejected=%d", 100-100/copies), func(b *testing.B) {
			saver, _, _, cleanup := setupBenchmarkPools(b)
			defer cleanup()
			pool, _ := saver.registry.eventPool(benchBoundary)
			conn, err := pool.Write.Take(b.Context())
			require.NoError(b, err)
			require.NoError(b, sqlitex.Execute(conn, "CREATE INDEX benchmark_scope ON orisun_es_event("+sqliteJSONScalarTextExpr("scope")+", transaction_id DESC, global_id DESC)", nil))
			pool.Write.Put(conn)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				b.StopTimer()
				requests := make([]*sqliteSaveRequest, 100)
				for i := range requests {
					scope := fmt.Sprintf("%d-%d", n, i/copies)
					checks := []orisun.ConsistencyCheck{{Position: orisun.NotExistsPosition(), Criteria: []orisun.ReadCriterion{{Tags: []orisun.ReadTag{{Key: "scope", Value: scope}}}}}}
					encoded, err := orisun.MarshalConsistency(checks)
					require.NoError(b, err)
					requests[i] = &sqliteSaveRequest{ctx: b.Context(), inserts: orisun.PreparedEventBatch{{EventId: fmt.Sprintf("%d-%d", n, i), EventType: "Fact", DataJSON: fmt.Sprintf(`{"scope":%q}`, scope), MetadataJSON: "{}"}}, consistency: checks, preparedChecks: prepareConsistencyChecks(checks), consistencyJSON: string(encoded), result: make(chan sqliteSaveResult, 1)}
				}
				b.StartTimer()
				saver.runFlush(benchBoundary, pool, requests)
				b.StopTimer()
				for i, req := range requests {
					result := <-req.result
					if i%copies == 0 {
						require.NoError(b, result.err)
					} else {
						require.Equal(b, statuscode.AlreadyExists, statuscode.CodeOf(result.err))
					}
				}
				b.StartTimer()
			}
			b.StopTimer()
		})
	}
}
