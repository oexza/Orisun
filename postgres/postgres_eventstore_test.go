package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	config "orisun/config"
	"orisun/eventstore"
	logging "orisun/logging"

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

	for retries := 0; retries < 3; retries++ {
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
	if err := RunDbScripts(db, "public", false, ctx); err != nil {
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)

	// Test saving events
	events := []eventstore.EventWithMapTags{
		{
			EventId:   eventId.String(),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value\"}",
			Metadata:  "{\"meta\": \"data\"}",
			Tags: map[string]interface{}{
				"tag1": "value1",
				"tag2": "value2",
			},
		},
	}

	// Save events
	tranID, globalID, err := saveEvents.Save(
		t.Context(),
		events,
		// nil,
		"test_boundary",
		"test-stream",
		-1,
		nil,
	)

	assert.NoError(t, err)
	assert.NotEmpty(t, tranID)
	assert.Greater(t, globalID, uint64(0))

	// Get events
	resp, err := getEvents.Get(
		t.Context(),
		&eventstore.GetEventsRequest{
			Boundary:  "test_boundary",
			Direction: eventstore.Direction_ASC,
			Count:     10,
			Stream: &eventstore.GetStreamQuery{
				Name:        "test-stream",
				FromVersion: -1,
			},
		},
	)

	assert.NoError(t, err)
	assert.Len(t, resp.Events, 1)
	assert.Equal(t, eventId.String(), resp.Events[0].EventId)
	assert.Equal(t, "TestEvent", resp.Events[0].EventType)
	assert.Equal(t, "\"{\\\"key\\\": \\\"value\\\"}\"", resp.Events[0].Data)
	assert.Equal(t, "\"{\\\"meta\\\": \\\"data\\\"}\"", resp.Events[0].Metadata)

	// Verify tags
	assert.Len(t, resp.Events[0].Tags, 2)
	for _, tag := range resp.Events[0].Tags {
		switch tag.Key {
		case "tag1":
			assert.Equal(t, "value1", tag.Value)
		case "tag2":
			assert.Equal(t, "value2", tag.Value)
		default:
			t.Errorf("unexpected tag key: %s", tag.Key)
		}
	}
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)

	// First save succeeds
	events := []eventstore.EventWithMapTags{
		{
			EventId:   eventId.String(),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value\"}",
			Tags:      map[string]interface{}{"tag1": "value1"},
		},
	}

	_, _, err = saveEvents.Save(
		t.Context(),
		events,
		// nil,
		"test_boundary",
		"test-stream",
		-1, // Expected version 0
		nil,
	)
	require.NoError(t, err)

	// Second save with wrong expected version should fail
	events2 := []eventstore.EventWithMapTags{
		{
			EventId:   string(eventId.String()),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value2\"}",
			Tags:      map[string]interface{}{"tag1": "value2"},
		},
	}

	_, _, err = saveEvents.Save(
		t.Context(),
		events2,
		// nil,
		"test_boundary",
		"test-stream",
		-1, // Expected version -1 again, but should be 0 now
		nil,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OptimisticConcurrencyException")
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)
	// Save events with different tags
	events1 := []eventstore.EventWithMapTags{
		{
			EventId:   string(eventId.String()),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value1\"}",
			Tags:      map[string]interface{}{"category": "A", "priority": "high"},
		},
	}

	_, _, err = saveEvents.Save(
		t.Context(),
		events1,
		// nil,
		"test_boundary",
		"test-stream",
		-1,
		nil,
	)
	require.NoError(t, err)

	events2 := []eventstore.EventWithMapTags{
		{
			EventId:   string(eventId.String()),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value2\"}",
			Tags:      map[string]interface{}{"category": "B", "priority": "low"},
		},
	}

	_, _, err = saveEvents.Save(
		t.Context(),
		events2,
		// nil,
		"test_boundary",
		"test-stream",
		0,
		nil,
	)
	require.NoError(t, err)

	// Test filtering by tag criteria
	resp, err := getEvents.Get(
		t.Context(),
		&eventstore.GetEventsRequest{
			Boundary:  "test_boundary",
			Direction: eventstore.Direction_ASC,
			Count:     10,
			Stream: &eventstore.GetStreamQuery{
				Name:        "test-stream",
				FromVersion: -1,
			},
			Query: &eventstore.Query{
				Criteria: []*eventstore.Criterion{
					&eventstore.Criterion{
						Tags: []*eventstore.Tag{
							{Key: "category", Value: "A"},
						},
					},
				},
			},
		},
	)

	assert.NoError(t, err)
	assert.Len(t, resp.Events, 1)
	assert.Equal(t, eventId.String(), resp.Events[0].EventId)
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save multiple events to get different global positions
	var globalPositions []uint64
	for i := 0; i < 5; i++ {
		eventId, err := uuid.NewV7()
		require.NoError(t, err)

		events := []eventstore.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "TestEvent",
				Data:      fmt.Sprintf("{\"index\": %d}", i),
				Tags:      map[string]interface{}{"index": fmt.Sprintf("%d", i)},
			},
		}

		_, globalPos, err := saveEvents.Save(
			ctx,
			events,
			// nil,
			"test_boundary",
			"global-pos-stream",
			int32(i-1),
			nil,
		)
		require.NoError(t, err)
		globalPositions = append(globalPositions, globalPos)
	}

	// Get events after the second event's global position
	resp, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		FromPosition: &eventstore.Position{
			CommitPosition: globalPositions[1],
		},
	})

	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Events), 3) // Should get at least events 2, 3, and 4

	// Verify the events are in the correct order
	for i, event := range resp.Events {
		assert.Contains(t, event.Data, fmt.Sprintf("index\\\": %d", i+1))
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save 10 events
	for i := 0; i < 10; i++ {
		eventId, err := uuid.NewV7()
		require.NoError(t, err)

		events := []eventstore.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "TestEvent",
				Data:      fmt.Sprintf("{\"index\": %d}", i),
				Tags:      map[string]interface{}{"index": fmt.Sprintf("%d", i)},
			},
		}

		_, _, err = saveEvents.Save(
			ctx,
			events,
			// nil,
			"test_boundary",
			"pagination-stream",
			int32(i-1),
			nil,
		)
		require.NoError(t, err)
	}

	// Get first page (3 events)
	resp1, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     3,
		Stream: &eventstore.GetStreamQuery{
			Name:        "pagination-stream",
			FromVersion: -1,
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp1.Events, 3)

	// Get second page (3 events)
	resp2, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     3,
		Stream: &eventstore.GetStreamQuery{
			Name:        "pagination-stream",
			FromVersion: 2, // Start from version 2 (third event)
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save 5 events
	for i := 0; i < 5; i++ {
		eventId, err := uuid.NewV7()
		require.NoError(t, err)

		events := []eventstore.EventWithMapTags{
			{
				EventId:   eventId.String(),
				EventType: "TestEvent",
				Data:      fmt.Sprintf("{\"index\": %d}", i),
				Tags:      map[string]interface{}{"index": fmt.Sprintf("%d", i)},
			},
		}

		_, _, err = saveEvents.Save(
			ctx,
			events,
			// nil,
			"test_boundary",
			"direction-stream",
			int32(i-1),
			nil,
		)
		require.NoError(t, err)
	}

	// Get events in ascending order
	respAsc, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name:        "direction-stream",
			FromVersion: -1,
		},
	})

	assert.NoError(t, err)
	assert.Len(t, respAsc.Events, 5)

	// Get events in descending order
	respDesc, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_DESC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name:        "direction-stream",
			FromVersion: 999999999,
		},
	})

	assert.NoError(t, err)
	assert.Len(t, respDesc.Events, 5)

	// Verify the order is reversed
	for i := 0; i < 5; i++ {
		assert.Equal(t, respAsc.Events[i].EventId, respDesc.Events[4-i].EventId)
	}
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Save events with different tag combinations
	eventIds := make([]string, 4)

	// Event 1: category=A, priority=high, region=east
	eventId1, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[0] = eventId1.String()
	events1 := []eventstore.EventWithMapTags{
		{
			EventId:   eventId1.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event1\"}",
			Tags:      map[string]interface{}{"category": "A", "priority": "high", "region": "east"},
		},
	}
	_, _, err = saveEvents.Save(
		ctx,
		events1,
		// nil,
		"test_boundary",
		"complex-query-stream",
		-1,
		nil,
	)
	require.NoError(t, err)

	// Event 2: category=A, priority=low, region=west
	eventId2, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[1] = eventId2.String()
	events2 := []eventstore.EventWithMapTags{
		{
			EventId:   eventId2.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event2\"}",
			Tags:      map[string]interface{}{"category": "A", "priority": "low", "region": "west"},
		},
	}
	_, _, err = saveEvents.Save(
		ctx,
		events2,
		// nil,
		"test_boundary",
		"complex-query-stream",
		0,
		nil,
	)
	require.NoError(t, err)

	// Event 3: category=B, priority=high, region=east
	eventId3, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[2] = eventId3.String()
	events3 := []eventstore.EventWithMapTags{
		{
			EventId:   eventId3.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event3\"}",
			Tags:      map[string]interface{}{"category": "B", "priority": "high", "region": "east"},
		},
	}
	_, _, err = saveEvents.Save(
		ctx, 
		events3, 
		// nil, 
		"test_boundary", 
		"complex-query-stream", 
		1, nil,
	)
	require.NoError(t, err)

	// Event 4: category=B, priority=low, region=west
	eventId4, err := uuid.NewV7()
	require.NoError(t, err)
	eventIds[3] = eventId4.String()
	events4 := []eventstore.EventWithMapTags{
		{
			EventId:   eventId4.String(),
			EventType: "TestEvent",
			Data:      "{\"data\": \"event4\"}",
			Tags:      map[string]interface{}{"category": "B", "priority": "low", "region": "west"},
		},
	}
	_, _, err = saveEvents.Save(
		ctx,
		events4,
		// nil,
		"test_boundary",
		"complex-query-stream",
		2,
		nil,
	)
	require.NoError(t, err)

	// Test 1: OR query - category A OR category B with priority high
	resp1, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name:        "complex-query-stream",
			FromVersion: -1,
		},
		Query: &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "category", Value: "A"},
						{Key: "priority", Value: "high"},
					},
				},
				{
					Tags: []*eventstore.Tag{
						{Key: "category", Value: "B"},
						{Key: "priority", Value: "high"},
					},
				},
			},
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp1.Events, 2)

	// Test 2: AND query - region east
	resp2, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name:        "complex-query-stream",
			FromVersion: -1,
		},
		Query: &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "region", Value: "east"},
					},
				},
			},
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp2.Events, 2)

	// Test 3: Complex query - (category A AND region west) OR (category B AND priority high)
	resp3, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name:        "complex-query-stream",
			FromVersion: -1,
		},
		Query: &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "category", Value: "A"},
						{Key: "region", Value: "west"},
					},
				},
				{
					Tags: []*eventstore.Tag{
						{Key: "category", Value: "B"},
						{Key: "priority", Value: "high"},
					},
				},
			},
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp3.Events, 2)
}

func TestErrorConditions(t *testing.T) {
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

	saveEvents := NewPostgresSaveEvents(db, logger, mapping)
	getEvents := NewPostgresGetEvents(db, logger, mapping)
	ctx := t.Context()

	// Test 1: Non-existent boundary
	eventId, err := uuid.NewV7()
	require.NoError(t, err)

	events := []eventstore.EventWithMapTags{
		{
			EventId:   eventId.String(),
			EventType: "TestEvent",
			Data:      "{\"key\": \"value\"}",
			Tags:      map[string]interface{}{"tag1": "value1"},
		},
	}

	_, _, err = saveEvents.Save(
		ctx,
		events,
		// nil,
		"non_existent_boundary",
		"test-stream", -1,
		nil,
	)
	assert.Error(t, err)

	// Test 2: Invalid expected version (too high)
	_, _, err = saveEvents.Save(
		ctx, events,
		// nil,
		"test_boundary",
		"version-test-stream",
		100,
		nil,
	)
	assert.Error(t, err)

	// Test 3: Empty event list
	_, _, err = saveEvents.Save(
		ctx, []eventstore.EventWithMapTags{},
		// nil,
		"test_boundary",
		"empty-stream",
		-1,
		nil,
	)
	assert.Error(t, err)

	// Test 4: Get from non-existent stream
	resp, err := getEvents.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name: "non-existent-stream",
		},
	})

	assert.NoError(t, err) // Should not error, just return empty results
	assert.Len(t, resp.Events, 0)
}
