package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	common "github.com/oexza/Orisun/admin/slices/common"
	config "github.com/oexza/Orisun/config"
	logging "github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	// PostgreSQL driver
	_ "github.com/lib/pq"
)

type PostgresContainer struct {
	container testcontainers.Container
	host      string
	port      string
}

func setupTestContainer(t *testing.T) (*PostgresContainer, error) {
	ctx := context.Background() // Use background context instead of t.Context()
	req := testcontainers.ContainerRequest{
		Image:        "postgres:13",
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
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %v", err)
	}

	// Add a small delay to ensure the container is fully ready
	time.Sleep(2 * time.Second)

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		return nil, fmt.Errorf("failed to get container port: %v", err)
	}

	return &PostgresContainer{
		container: container,
		host:      host,
		port:      port.Port(),
	}, nil
}

func setupTestDatabase(t *testing.T, container *PostgresContainer) (*sql.DB, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%s user=test password=test dbname=testdb sslmode=disable",
		container.host,
		container.port,
	)

	// Add connection timeout and retry logic
	var db *sql.DB
	var err error

	for retries := range 3 {
		db, err = sql.Open("postgres", connStr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %v", err)
		}

		// Set connection parameters
		db.SetMaxOpenConns(5)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(time.Minute * 5)

		// Try to ping with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = db.PingContext(ctx)
		cancel()

		if err == nil {
			break
		}

		db.Close()
		time.Sleep(time.Duration(retries+1) * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to ping database after retries: %v", err)
	}

	// Run database migrations using the common scripts
	ctx := context.Background()
	if err := RunDbScripts(db, "test_boundary", "public", false, ctx); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %v", err)
	}

	return db, nil
}

func TestSaveAndGetEvents(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)

	// Test saving events
	events := []orisun.EventWithMapTags{
		{
			EventId:   eventId.String(),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value\"}",
			Metadata:  "{\"meta\": \"data\", \"tags\": [{\"key\": \"key\", \"value\": \"value\"}]}",
		},
	}

	position := orisun.NotExistsPosition()
	// Save events
	tranID, globalID, err := saveEvents.Save(
		t.Context(),
		events,
		"test_boundary",
		&position,
		nil,
	)

	assert.NoError(t, err)
	assert.NotEmpty(t, tranID)
	assert.GreaterOrEqual(t, globalID, int64(0))

	// Get events
	resp, err := getEvents.Get(
		t.Context(),
		&orisun.GetEventsRequest{
			Boundary:  "test_boundary",
			Direction: orisun.Direction_ASC,
			Count:     10,
		},
	)

	assert.NoError(t, err)
	assert.Len(t, resp.Events, 1)
	assert.Equal(t, eventId.String(), resp.Events[0].EventId)
	assert.Equal(t, "TestEvent", resp.Events[0].EventType)

	// Check that the data contains the expected values
	assert.Contains(t, resp.Events[0].Data, "key")
	assert.Contains(t, resp.Events[0].Data, "value")

	// Check that the metadata contains the expected values
	assert.Contains(t, resp.Events[0].Metadata, "meta")
	assert.Contains(t, resp.Events[0].Metadata, "data")
	assert.Contains(t, resp.Events[0].Metadata, "tags")
}

func TestSave200EventsOneByOne(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)

	expectedPosition := orisun.NotExistsPosition()

	// Save 100 events one by one
	for i := range 200 {
		eventId, err := uuid.NewV7()
		require.NoError(t, err)

		events := []orisun.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "SequentialEvent",
				Data:      fmt.Sprintf("{\"sequence\": %d, \"message\": \"Event number %d\"}", i, i),
				Metadata:  fmt.Sprintf("{\"event_number\": %d, \"timestamp\": \"%s\"}", i, time.Now().Format(time.RFC3339)),
			},
		}

		// Save the event
		tranID, globalID, err := saveEvents.Save(
			t.Context(),
			events,
			"test_boundary",
			&expectedPosition,
			nil,
		)

		require.NoError(t, err, "Failed to save event %d", i)
		assert.NotEmpty(t, tranID, "Transaction ID should not be empty for event %d", i)
		assert.GreaterOrEqual(t, globalID, int64(0), "Global ID should be greater than or equal to 0 for event %d", i)

		transactionIDInt, err := strconv.ParseInt(tranID, 10, 64)
		require.NoError(t, err)
		expectedPosition = orisun.Position{
			PreparePosition: globalID,
			CommitPosition:  transactionIDInt,
		}
	}

	// Verify all 100 events were saved correctly
	resp, err := getEvents.Get(
		t.Context(),
		&orisun.GetEventsRequest{
			Boundary:  "test_boundary",
			Direction: orisun.Direction_ASC,
			Count:     100,
		},
	)

	require.NoError(t, err)
	assert.Len(t, resp.Events, 100, "Should have exactly 100 events")

	// // Verify the sequence and content of events
	// for i, event := range resp.Events {
	// 	assert.Equal(t, "SequentialEvent", event.EventType, "Event %d should have correct type", i)
	// 	assert.Contains(t, event.Data, fmt.Sprintf("\"sequence\": %d", i), "Event %d should have correct sequence in data", i)
	// 	assert.Contains(t, event.Data, fmt.Sprintf("\"message\": \"Event number %d\"", i), "Event %d should have correct message", i)
	// 	// assert.Equal(t, uint64(i), event.Version, "Event %d should have correct stream version", i)
	// }

	t.Logf("Successfully saved and verified 100 events")
}

func TestOptimisticConcurrency(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)

	// First save succeeds
	events := []orisun.EventWithMapTags{
		{
			EventId:   eventId.String(),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value\"}",
		},
	}

	position1 := orisun.NotExistsPosition()
	_, _, err = saveEvents.Save(
		t.Context(),
		events,
		"test_boundary",
		&position1,
		&orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "key", Value: "value"},
					},
				},
			},
		},
	)
	require.NoError(t, err)

	// Second save with wrong expected version should fail
	events2 := []orisun.EventWithMapTags{
		{
			EventId:   string(eventId.String()),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value2\"}",
			Metadata:  "{}",
		},
	}

	position2 := orisun.NotExistsPosition()
	_, _, err = saveEvents.Save(
		t.Context(),
		events2,
		"test_boundary",
		&position2,
		&orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "key", Value: "value"},
					},
				},
			},
		},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OptimisticConcurrencyException")
}

func TestConcurrentSaveEventsOptimisticConcurrency(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	// Create separate database connections for each operation to ensure true concurrency
	db2, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db2.Close()

	saveEvents1 := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	saveEvents2 := NewPostgresSaveEvents(t.Context(), db2, logger, mapping)

	// Create a shared stream consistency condition (Query) for both operations
	sharedStreamCondition := &orisun.Query{
		Criteria: []*orisun.Criterion{
			{
				Tags: []*orisun.Tag{
					{
						Key:   "test_type",
						Value: "concurrent_save",
					},
				},
			},
		},
	}

	// Pre-generate event IDs to avoid any timing issues
	eventId1, err := uuid.NewV7()
	require.NoError(t, err)
	eventId2, err := uuid.NewV7()
	require.NoError(t, err)

	events1 := []orisun.EventWithMapTags{
		{
			EventId:   eventId1.String(),
			EventType: "ConcurrentTestEvent1",
			Data:      "{\"operation\": \"first\", \"test_type\": \"concurrent_save\"}",
			Metadata:  "{\"test\": \"concurrent_save_1\"}",
		},
	}

	events2 := []orisun.EventWithMapTags{
		{
			EventId:   eventId2.String(),
			EventType: "ConcurrentTestEvent2",
			Data:      "{\"operation\": \"second\", \"test_type\": \"concurrent_save\"}",
			Metadata:  "{\"test\": \"concurrent_save_2\"}",
		},
	}

	// Use WaitGroup and a barrier to ensure both operations start simultaneously
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []error
	var concurrencyError error

	// Create a barrier to synchronize the start of both operations
	startBarrier := make(chan struct{})

	wg.Add(2)

	// Start first operation
	go func() {
		defer wg.Done()
		// Wait for the start signal
		<-startBarrier
		_, _, err := saveEvents1.Save(
			context.Background(),
			events1,
			"test_boundary",
			nil,
			sharedStreamCondition,
		)
		t.Logf("Operation 1 result: %v", err)
		mu.Lock()
		results = append(results, err)
		if err != nil && strings.Contains(err.Error(), "OptimisticConcurrencyException") {
			concurrencyError = err
		}
		mu.Unlock()
	}()

	// Start second operation
	go func() {
		defer wg.Done()
		// Wait for the start signal
		<-startBarrier
		_, _, err := saveEvents2.Save(
			context.Background(),
			events2,
			"test_boundary",
			nil,
			sharedStreamCondition,
		)
		t.Logf("Operation 2 result: %v", err)
		mu.Lock()
		results = append(results, err)
		if err != nil && strings.Contains(err.Error(), "OptimisticConcurrencyException") {
			concurrencyError = err
		}
		mu.Unlock()
	}()

	// Give both goroutines a moment to reach the barrier
	time.Sleep(10 * time.Millisecond)

	// Signal both operations to start simultaneously
	close(startBarrier)

	// Wait for both operations to complete
	wg.Wait()

	// Debug logging
	t.Logf("Operation 1 result: %v", results[0])
	t.Logf("Operation 2 result: %v", results[1])

	// One operation should succeed, one should fail with OptimisticConcurrencyException
	var successCount, failureCount int

	for i, err := range results {
		if err == nil {
			successCount++
			t.Logf("Operation %d: SUCCESS", i+1)
		} else {
			failureCount++
			t.Logf("Operation %d: FAILED with error: %v", i+1, err)
		}
	}

	t.Logf("Final counts - Success: %d, Failure: %d", successCount, failureCount)

	// Assertions
	assert.Equal(t, 1, successCount, "Exactly one save operation should succeed")
	assert.Equal(t, 1, failureCount, "Exactly one save operation should fail")
	assert.NotNil(t, concurrencyError, "There should be a concurrency error")
	if concurrencyError != nil {
		assert.Contains(t, concurrencyError.Error(), "OptimisticConcurrencyException", "The error should be an OptimisticConcurrencyException")
	}
}

func TestGetEventsWithCriteria(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)
	// Save events with different tags
	events1 := []orisun.EventWithMapTags{
		{
			EventId:   eventId.String(),
			EventType: "TestEvent",
			Data:      "{\"key\": \"key\", \"value\": \"value1\"}",
			Metadata:  "{\"tags\": [{\"key\": \"key\", \"value\": \"value1\"}]}",
		},
	}

	expectedPosition := orisun.NotExistsPosition()
	tranId, globalId, err := saveEvents.Save(
		t.Context(),
		events1,
		"test_boundary",
		&expectedPosition,
		nil,
	)
	require.NoError(t, err)

	events2 := []orisun.EventWithMapTags{
		{
			EventId:   eventId.String(),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value2\"}",
			Metadata:  "{}",
		},
	}

	tranIdConv, err := strconv.ParseInt(tranId, 10, 64)

	if err != nil {
		fmt.Printf("Error converting string to int64: %v\n", err)
		return
	}
	_, _, err = saveEvents.Save(
		t.Context(),
		events2,
		"test_boundary",
		&orisun.Position{
			PreparePosition: globalId,
			CommitPosition:  tranIdConv,
		},
		nil,
	)
	require.NoError(t, err)

	// Test filtering by tag criteria
	resp, err := getEvents.Get(
		t.Context(),
		&orisun.GetEventsRequest{
			Boundary:  "test_boundary",
			Direction: orisun.Direction_ASC,
			Count:     10,
			Query: &orisun.Query{
				Criteria: []*orisun.Criterion{
					{
						Tags: []*orisun.Tag{
							{Key: "key", Value: "value2"},
						},
					},
				},
			},
		},
	)

	assert.NoError(t, err)
	assert.Len(t, resp.Events, 1, "Expected to find one event with tag key=value2")
	if len(resp.Events) > 0 {
		assert.Equal(t, eventId.String(), resp.Events[0].EventId)
	}
}

func TestGetEventsByGlobalPosition(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save multiple events to get different global positions
	var globalPositions []int64
	var transactionIDs []string
	var lastPosition *orisun.Position
	for i := range 5 {
		eventId, err := uuid.NewV7()
		require.NoError(t, err)

		events := []orisun.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "TestEvent",
				Data:      fmt.Sprintf("{\"index\": %d}", i),
			},
		}

		transactionID, globalPos, err := saveEvents.Save(
			ctx,
			events,
			"test_boundary",
			lastPosition,
			nil,
		)
		require.NoError(t, err)
		globalPositions = append(globalPositions, globalPos)
		transactionIDs = append(transactionIDs, transactionID)
		transactionIDInt, err := strconv.ParseInt(transactionID, 10, 64)
		require.NoError(t, err)
		lastPosition = &orisun.Position{
			CommitPosition:  transactionIDInt,
			PreparePosition: globalPos,
		}
	}

	// Get events after the second event's global position
	// Position requires actual transaction_id and global_id from the saved events
	transactionIDInt, _ := strconv.ParseInt(transactionIDs[2], 10, 64)
	resp, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
		Count:     10,
		FromPosition: &orisun.Position{
			CommitPosition:  transactionIDInt,   // actual transaction_id
			PreparePosition: globalPositions[2], // actual global_id
		},
	})

	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Events), 3) // Should get at least events 2, 3, and 4

	for i, event := range resp.Events {
		expectedIndex := i + 2
		assert.Contains(t, event.Data, fmt.Sprintf("{\"index\": %d}", expectedIndex))
	}
}

func TestPagination(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save 10 events
	var lastPosition *orisun.Position
	for i := range 10 {
		eventId, err := uuid.NewV7()
		require.NoError(t, err)

		events := []orisun.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "TestEvent",
				Data:      fmt.Sprintf("{\"index\": %d}", i),
			},
		}

		transactionID, globalPos, err := saveEvents.Save(
			ctx,
			events,
			"test_boundary",
			lastPosition,
			nil,
		)
		require.NoError(t, err)
		transactionIDInt, err := strconv.ParseInt(transactionID, 10, 64)
		require.NoError(t, err)
		lastPosition = &orisun.Position{
			CommitPosition:  transactionIDInt,
			PreparePosition: globalPos,
		}
	}

	// Get first page (3 events)
	resp1, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
		Count:     3,
	})

	assert.NoError(t, err)
	assert.Len(t, resp1.Events, 3)

	// Get second page (3 events) using composite position of last from page 1
	lastPos := resp1.Events[len(resp1.Events)-1].Position
	resp2, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
		Count:     3,
		FromPosition: &orisun.Position{
			CommitPosition:  lastPos.CommitPosition,
			PreparePosition: lastPos.PreparePosition,
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp2.Events, 3)

	// Verify the events are different between pages
	assert.NotEqual(t, resp1.Events[0].EventId, resp2.Events[0].EventId)
	assert.NotEqual(t, resp1.Events[1].EventId, resp2.Events[1].EventId)
	assert.NotEqual(t, resp1.Events[2].EventId, resp2.Events[2].EventId)
}

func TestDirectionOrdering(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save 5 events
	var lastPosition *orisun.Position
	for i := range 5 {
		eventId, err := uuid.NewV7()
		require.NoError(t, err)

		events := []orisun.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "TestEvent",
				Data:      fmt.Sprintf("{\"index\": %d}", i),
			},
		}

		transactionID, globalPos, err := saveEvents.Save(
			ctx,
			events,
			"test_boundary",
			lastPosition,
			nil,
		)
		require.NoError(t, err)
		transactionIDInt, err := strconv.ParseInt(transactionID, 10, 64)
		require.NoError(t, err)
		lastPosition = &orisun.Position{
			CommitPosition:  transactionIDInt,
			PreparePosition: globalPos,
		}
	}

	// Get events in ascending order
	respAsc, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
		Count:     10,
	})

	assert.NoError(t, err)
	assert.Len(t, respAsc.Events, 5)

	// Get events in descending order
	respDesc, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_DESC,
		Count:     10,
	})

	assert.NoError(t, err)
	assert.Len(t, respDesc.Events, 5)

	// Verify the order is reversed
	for i := range 5 {
		assert.Equal(t, respAsc.Events[i].EventId, respDesc.Events[4-i].EventId)
	}
}

func TestCreateAndDropBoundaryIndex(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	adminDB := NewPostgresAdminDB(db, logger, "public", "test_boundary", mapping)
	ctx := t.Context()

	indexExists := func(indexName string) bool {
		var exists bool
		err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)",
			indexName,
		).Scan(&exists)
		require.NoError(t, err)
		return exists
	}

	t.Run("single field text index", func(t *testing.T) {
		err := adminDB.CreateBoundaryIndex(ctx, "test_boundary", "user_id", []common.IndexField{
			{JsonKey: "user_id", ValueType: "text"},
		}, nil, "")
		require.NoError(t, err)
		assert.True(t, indexExists("test_boundary_user_id_idx"), "index should exist after creation")

		err = adminDB.DropBoundaryIndex(ctx, "test_boundary", "user_id")
		require.NoError(t, err)
		assert.False(t, indexExists("test_boundary_user_id_idx"), "index should be gone after drop")
	})

	t.Run("composite index", func(t *testing.T) {
		err := adminDB.CreateBoundaryIndex(ctx, "test_boundary", "cat_prio", []common.IndexField{
			{JsonKey: "category", ValueType: "text"},
			{JsonKey: "priority", ValueType: "text"},
		}, nil, "")
		require.NoError(t, err)
		assert.True(t, indexExists("test_boundary_cat_prio_idx"))

		require.NoError(t, adminDB.DropBoundaryIndex(ctx, "test_boundary", "cat_prio"))
		assert.False(t, indexExists("test_boundary_cat_prio_idx"))
	})

	t.Run("partial index with condition", func(t *testing.T) {
		err := adminDB.CreateBoundaryIndex(ctx, "test_boundary", "placed_amount", []common.IndexField{
			{JsonKey: "amount", ValueType: "numeric"},
		}, []common.IndexCondition{
			{Key: "eventType", Operator: "=", Value: "OrderPlaced"},
		}, common.CombinatorAND)
		require.NoError(t, err)
		assert.True(t, indexExists("test_boundary_placed_amount_idx"))

		require.NoError(t, adminDB.DropBoundaryIndex(ctx, "test_boundary", "placed_amount"))
		assert.False(t, indexExists("test_boundary_placed_amount_idx"))
	})

	t.Run("unknown boundary returns error", func(t *testing.T) {
		err := adminDB.CreateBoundaryIndex(ctx, "nonexistent", "idx", []common.IndexField{
			{JsonKey: "id", ValueType: "text"},
		}, nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown boundary")
	})

	t.Run("no fields returns error", func(t *testing.T) {
		err := adminDB.CreateBoundaryIndex(ctx, "test_boundary", "empty", nil, nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one field")
	})

	t.Run("invalid operator returns error", func(t *testing.T) {
		err := adminDB.CreateBoundaryIndex(ctx, "test_boundary", "bad_op", []common.IndexField{
			{JsonKey: "id", ValueType: "text"},
		}, []common.IndexCondition{
			{Key: "eventType", Operator: "LIKE", Value: "Order%"},
		}, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid operator")
	})

	t.Run("invalid combinator returns error", func(t *testing.T) {
		err := adminDB.CreateBoundaryIndex(ctx, "test_boundary", "bad_comb", []common.IndexField{
			{JsonKey: "id", ValueType: "text"},
		}, []common.IndexCondition{
			{Key: "eventType", Operator: "=", Value: "Placed"},
		}, "XOR")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid combinator")
	})

	t.Run("drop unknown boundary returns error", func(t *testing.T) {
		err := adminDB.DropBoundaryIndex(ctx, "nonexistent", "idx")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown boundary")
	})
}

func TestComplexTagQueries(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	db, err := setupTestDatabase(t, container)
	require.NoError(t, err)
	defer db.Close()

	logger, err := logging.ZapLogger("debug")
	require.NoError(t, err)

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"test_boundary": {
			Boundary: "test_boundary",
			Schema:   "public",
		},
	}

	saveEvents := NewPostgresSaveEvents(t.Context(), db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save events with different tag combinations
	eventIds := make([]string, 4)

	var lastPosition *orisun.Position

	// Event 1: category=A, priority=high, region=east
	eventId1, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[0] = eventId1.String()
	events1 := []orisun.EventWithMapTags{
		{
			EventId:   eventId1.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event1\", \"category\": \"A\", \"priority\": \"high\", \"region\": \"east\"}",
		},
	}
	transactionID, globalPos, err := saveEvents.Save(
		ctx,
		events1,
		"test_boundary",
		lastPosition,
		nil,
	)
	require.NoError(t, err)
	transactionIDInt, err := strconv.ParseInt(transactionID, 10, 64)
	require.NoError(t, err)
	lastPosition = &orisun.Position{
		CommitPosition:  transactionIDInt,
		PreparePosition: globalPos,
	}

	// Event 2: category=A, priority=low, region=west
	eventId2, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[1] = eventId2.String()
	events2 := []orisun.EventWithMapTags{
		{
			EventId:   eventId2.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event2\", \"category\": \"A\", \"priority\": \"low\", \"region\": \"west\"}",
		},
	}
	transactionID, globalPos, err = saveEvents.Save(
		ctx,
		events2,
		"test_boundary",
		lastPosition,
		nil,
	)
	require.NoError(t, err)
	transactionIDInt, err = strconv.ParseInt(transactionID, 10, 64)
	require.NoError(t, err)
	lastPosition = &orisun.Position{
		CommitPosition:  transactionIDInt,
		PreparePosition: globalPos,
	}

	// Event 3: category=B, priority=high, region=east
	eventId3, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[2] = eventId3.String()
	events3 := []orisun.EventWithMapTags{
		{
			EventId:   eventId3.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event3\", \"category\": \"B\", \"priority\": \"high\", \"region\": \"east\"}",
		},
	}
	transactionID, globalPos, err = saveEvents.Save(
		ctx,
		events3,
		"test_boundary",
		lastPosition,
		nil,
	)
	require.NoError(t, err)
	transactionIDInt, err = strconv.ParseInt(transactionID, 10, 64)
	require.NoError(t, err)
	lastPosition = &orisun.Position{
		CommitPosition:  transactionIDInt,
		PreparePosition: globalPos,
	}

	// Event 4: category=B, priority=low, region=west
	eventId4, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[3] = eventId4.String()
	events4 := []orisun.EventWithMapTags{
		{
			EventId:   eventId4.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event4\", \"category\": \"B\", \"priority\": \"low\", \"region\": \"west\"}",
		},
	}
	_, _, err = saveEvents.Save(
		ctx,
		events4,
		"test_boundary",

		lastPosition,
		nil,
	)
	require.NoError(t, err)

	// Test 1: OR query - category A OR category B with priority high
	resp1, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
		Count:     10,
		Query: &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "category", Value: "A"},
						{Key: "priority", Value: "high"},
					},
				},
				{
					Tags: []*orisun.Tag{
						{Key: "category", Value: "B"},
						{Key: "priority", Value: "high"},
					},
				},
			},
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp1.Events, 2, "The query should return 0 events as the tags are in metadata, not in data")

	// Test 2: AND query - region east
	resp2, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
		Count:     10,
		Query: &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "region", Value: "east"},
					},
				},
			},
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp2.Events, 2, "The query should return 0 events as the tags are in metadata, not in data")

	// Test 3: Complex query - (category A AND region west) OR (category B AND priority high)
	resp3, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
		Count:     10,
		Query: &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "category", Value: "A"},
						{Key: "region", Value: "west"},
					},
				},
				{
					Tags: []*orisun.Tag{
						{Key: "category", Value: "B"},
						{Key: "priority", Value: "high"},
					},
				},
			},
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp3.Events, 2, "The query should return 0 events as the tags are in metadata, not in data")
}
