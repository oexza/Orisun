package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	common "github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	benchNumStreams      = 500
	benchEventsPerStream = 20
)

// setupBenchmarkDB starts a Postgres container, opens a connection, runs migrations,
// and returns the database and a teardown function.
func setupBenchmarkDB(b *testing.B) (*sql.DB, func()) {
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "postgres:17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections"),
			wait.ForListeningPort("5432/tcp"),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(b, err, "failed to start container")

	// Give container a moment to fully initialize
	time.Sleep(2 * time.Second)

	host, err := container.Host(ctx)
	require.NoError(b, err, "failed to get container host")

	port, err := container.MappedPort(ctx, "5432")
	require.NoError(b, err, "failed to get container port")

	connStr := fmt.Sprintf(
		"host=%s port=%s user=test password=test dbname=testdb sslmode=disable",
		host,
		port.Port(),
	)

	var db *sql.DB
	for retries := 0; retries < 3; retries++ {
		db, err = sql.Open("postgres", connStr)
		require.NoError(b, err, "failed to open database")

		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(10)
		db.SetConnMaxLifetime(5 * time.Minute)

		pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = db.PingContext(pingCtx)
		cancel()

		if err == nil {
			break
		}
		db.Close()
		time.Sleep(time.Duration(retries+1) * time.Second)
	}
	require.NoError(b, err, "failed to ping database after retries")

	// Run migrations
	if err := RunDbScripts(db, "bench_boundary", "public", false, ctx); err != nil {
		db.Close()
		container.Terminate(ctx)
		require.NoError(b, err, "failed to run database migrations")
	}

	teardown := func() {
		db.Close()
		if err := container.Terminate(ctx); err != nil {
			b.Logf("Failed to terminate container: %v", err)
		}
	}

	return db, teardown
}

// prepopulateStreams creates and saves events for multiple streams.
// Returns the stream IDs and their last known positions (as pointers).
func prepopulateStreams(
	b *testing.B,
	ctx context.Context,
	saveEvents *PostgresSaveEvents,
	boundary string,
	numStreams, eventsPerStream int,
) (streamIds []string, lastPositions []*orisun.Position) {
	streamIds = make([]string, numStreams)
	lastPositions = make([]*orisun.Position, numStreams)

	// Generate stream IDs
	for i := 0; i < numStreams; i++ {
		streamId, err := uuid.NewV7()
		require.NoError(b, err)
		streamIds[i] = streamId.String()
	}

	// Pre-populate events for each stream
	for streamIdx := 0; streamIdx < numStreams; streamIdx++ {
		streamId := streamIds[streamIdx]
		pos := orisun.NotExistsPosition()

		for eventIdx := 0; eventIdx < eventsPerStream; eventIdx++ {
			eventId, err := uuid.NewV7()
			require.NoError(b, err)

			eventData := fmt.Sprintf(
				`{"stream_id": "%s", "eventType": "OrderPlaced", "sequence": %d}`,
				streamId,
				eventIdx,
			)

			metadata := fmt.Sprintf(`{"timestamp": "%s"}`, time.Now().Format(time.RFC3339))

			events := []orisun.EventWithMapTags{
				{
					EventId:   eventId.String(),
					EventType: "OrderPlaced",
					Data:      eventData,
					Metadata:  metadata,
				},
			}

			tranID, globalID, err := saveEvents.Save(ctx, events, boundary, &pos, nil)
			require.NoError(b, err, "failed to save pre-population event stream=%d event=%d", streamIdx, eventIdx)

			transactionIDInt, err := parseTransactionID(tranID)
			require.NoError(b, err)

			// Update position for next iteration
			pos = orisun.Position{
				PreparePosition: globalID,
				CommitPosition:  transactionIDInt,
			}
		}

		// Store final position for this stream
		lastPositions[streamIdx] = &pos
	}

	return streamIds, lastPositions
}

// parseTransactionID parses a transaction ID string to int64
func parseTransactionID(tranID string) (int64, error) {
	var tid int64
	_, err := fmt.Sscanf(tranID, "%d", &tid)
	return tid, err
}

// BenchmarkConsistencyCheck_NoIndex benchmarks the consistency check path
// WITHOUT a btree index - performs a sequential scan on the version check.
func BenchmarkConsistencyCheck_NoIndex(b *testing.B) {
	db, teardown := setupBenchmarkDB(b)
	defer teardown()

	ctx := context.Background()
	logger, err := logging.ZapLogger("warn") // Use warn to reduce noise in benchmarks
	require.NoError(b, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"bench_boundary": {
			Boundary: "bench_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(ctx, db, logger, mapping)

	// Pre-populate the database with events
	streamIds, lastPositions := prepopulateStreams(b, ctx, saveEvents, "bench_boundary", benchNumStreams, benchEventsPerStream)

	b.ResetTimer()

	// Benchmark loop: perform save operations with consistency checks
	for i := 0; i < b.N; i++ {
		// Pick a stream (round-robin)
		streamIdx := i % benchNumStreams
		streamId := streamIds[streamIdx]

		eventId, err := uuid.NewV7()
		require.NoError(b, err)

		eventData := fmt.Sprintf(
			`{"stream_id": "%s", "eventType": "OrderPlaced", "sequence": %d}`,
			streamId,
			benchEventsPerStream+i,
		)

		metadata := fmt.Sprintf(`{"timestamp": "%s"}`, time.Now().Format(time.RFC3339))

		events := []orisun.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "OrderPlaced",
				Data:      eventData,
				Metadata:  metadata,
			},
		}

		// Query with consistency condition - this is what triggers the version check
		query := &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "stream_id", Value: streamId},
					},
				},
			},
		}

		pos := lastPositions[streamIdx]
		tranID, globalID, err := saveEvents.Save(ctx, events, "bench_boundary", pos, query)
		require.NoError(b, err, "iteration %d failed", i)

		transactionIDInt, err := parseTransactionID(tranID)
		require.NoError(b, err)

		// Create new position and store it
		newPos := orisun.Position{
			PreparePosition: globalID,
			CommitPosition:  transactionIDInt,
		}
		lastPositions[streamIdx] = &newPos
	}

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "saves/sec")
}

// BenchmarkConsistencyCheck_WithIndex benchmarks the consistency check path
// WITH a btree index on stream_id - performs an indexed lookup on the version check.
func BenchmarkConsistencyCheck_WithIndex(b *testing.B) {
	db, teardown := setupBenchmarkDB(b)
	defer teardown()

	ctx := context.Background()
	logger, err := logging.ZapLogger("warn")
	require.NoError(b, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"bench_boundary": {
			Boundary: "bench_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(ctx, db, logger, mapping)
	adminDB := NewPostgresAdminDB(db, logger, "public", "bench_boundary", mapping)

	// Pre-populate the database with events
	streamIds, lastPositions := prepopulateStreams(b, ctx, saveEvents, "bench_boundary", benchNumStreams, benchEventsPerStream)

	// Create the index AFTER pre-population (as specified in plan)
	err = adminDB.CreateBoundaryIndex(ctx, "bench_boundary", "stream_id", []common.IndexField{
		{JsonKey: "stream_id", ValueType: "text"},
	}, nil, "")
	require.NoError(b, err, "failed to create index")

	b.ResetTimer()

	// Benchmark loop: perform save operations with consistency checks
	for i := 0; i < b.N; i++ {
		// Pick a stream (round-robin)
		streamIdx := i % benchNumStreams
		streamId := streamIds[streamIdx]

		eventId, err := uuid.NewV7()
		require.NoError(b, err)

		eventData := fmt.Sprintf(
			`{"stream_id": "%s", "eventType": "OrderPlaced", "sequence": %d}`,
			streamId,
			benchEventsPerStream+i,
		)

		metadata := fmt.Sprintf(`{"timestamp": "%s"}`, time.Now().Format(time.RFC3339))

		events := []orisun.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "OrderPlaced",
				Data:      eventData,
				Metadata:  metadata,
			},
		}

		// Query with consistency condition - this is what triggers the version check
		query := &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "stream_id", Value: streamId},
					},
				},
			},
		}

		pos := lastPositions[streamIdx]
		tranID, globalID, err := saveEvents.Save(ctx, events, "bench_boundary", pos, query)
		require.NoError(b, err, "iteration %d failed", i)

		transactionIDInt, err := parseTransactionID(tranID)
		require.NoError(b, err)

		// Create new position and store it
		newPos := orisun.Position{
			PreparePosition: globalID,
			CommitPosition:  transactionIDInt,
		}
		lastPositions[streamIdx] = &newPos
	}

	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "saves/sec")
}
