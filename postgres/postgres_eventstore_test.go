package postgres

import (
	"database/sql"
	"fmt"
	"testing"

	config "orisun/config"
	"orisun/eventstore"
	logging "orisun/logging"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	// Add PostgreSQL driver
	_ "github.com/lib/pq"
)

type PostgresContainer struct {
	container testcontainers.Container
	host      string
	port      string
}

func setupTestContainer(t *testing.T) (*PostgresContainer, error) {
	ctx := t.Context()
	req := testcontainers.ContainerRequest{
		Image:        "postgres:13",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %v", err)
	}

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

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	// Run database migrations using the common scripts
	if err := RunDbScripts(db, "public", false, t.Context()); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %v", err)
	}

	return db, nil
}

func TestSaveAndGetEvents(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer container.container.Terminate(t.Context())

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

	saveEvents := NewPostgresSaveEvents(db, &logger, mapping)
	getEvents := NewPostgresGetEvents(db, &logger, mapping)

	// Test saving events
	events := []*eventstore.EventWithMapTags{
		{
			EventId:   "test-event-1",
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
		nil,
		"test_boundary",
		"test-stream",
		0,
		nil,
	)

	assert.NoError(t, err)
	assert.NotEmpty(t, tranID)
	assert.Greater(t, globalID, uint64(0))

	// Get events
	resp, err := getEvents.Get(t.Context(), &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name: "test-stream",
		},
	})

	assert.NoError(t, err)
	assert.Len(t, resp.Events, 1)
	assert.Equal(t, "test-event-1", resp.Events[0].EventId)
	assert.Equal(t, "TestEvent", resp.Events[0].EventType)
	assert.Equal(t, "{\"key\": \"value\"}", resp.Events[0].Data)
	assert.Equal(t, "{\"meta\": \"data\"}", resp.Events[0].Metadata)

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

// Add more test cases
func TestOptimisticConcurrency(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer container.container.Terminate(t.Context())

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

	saveEvents := NewPostgresSaveEvents(db, &logger, mapping)

	// First save succeeds
	events := []*eventstore.EventWithMapTags{
		{
			EventId:   "test-event-1",
			EventType: "TestEvent",
			Data:      "{\"key\": \"value\"}",
			Tags:      map[string]interface{}{"tag1": "value1"},
		},
	}

	_, _, err = saveEvents.Save(
		t.Context(),
		events,
		nil,
		"test_boundary",
		"test-stream",
		0, // Expected version 0
		nil,
	)
	require.NoError(t, err)

	// Second save with wrong expected version should fail
	events2 := []*eventstore.EventWithMapTags{
		{
			EventId:   "test-event-2",
			EventType: "TestEvent",
			Data:      "{\"key\": \"value2\"}",
			Tags:      map[string]interface{}{"tag1": "value2"},
		},
	}

	_, _, err = saveEvents.Save(
		t.Context(),
		events2,
		nil,
		"test_boundary",
		"test-stream",
		0, // Expected version 0 again, but should be 1 now
		nil,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OptimisticConcurrencyException")
}

func TestGetEventsWithCriteria(t *testing.T) {
	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer container.container.Terminate(t.Context())

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

	saveEvents := NewPostgresSaveEvents(db, &logger, mapping)
	getEvents := NewPostgresGetEvents(db, &logger, mapping)

	// Save events with different tags
	events1 := []*eventstore.EventWithMapTags{
		{
			EventId:   "test-event-1",
			EventType: "TestEvent",
			Data:      "{\"key\": \"value1\"}",
			Tags:      map[string]interface{}{"category": "A", "priority": "high"},
		},
	}

	_, _, err = saveEvents.Save(t.Context(), events1, nil, "test_boundary", "test-stream", 0, nil)
	require.NoError(t, err)

	events2 := []*eventstore.EventWithMapTags{
		{
			EventId:   "test-event-2",
			EventType: "TestEvent",
			Data:      "{\"key\": \"value2\"}",
			Tags:      map[string]interface{}{"category": "B", "priority": "low"},
		},
	}

	_, _, err = saveEvents.Save(t.Context(), events2, nil, "test_boundary", "test-stream", 1, nil)
	require.NoError(t, err)

	// Test filtering by tag criteria
	resp, err := getEvents.Get(t.Context(), &eventstore.GetEventsRequest{
		Boundary:  "test_boundary",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Stream: &eventstore.GetStreamQuery{
			Name: "test-stream",
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
	})

	assert.NoError(t, err)
	assert.Len(t, resp.Events, 1)
	assert.Equal(t, "test-event-1", resp.Events[0].EventId)
}
