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

	saveEvents := NewPostgresSaveEvents(db, &logger, mapping)
	getEvents := NewPostgresGetEvents(db, &logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)

	// Test saving events
	events := []*eventstore.EventWithMapTags{
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
		nil,
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

	saveEvents := NewPostgresSaveEvents(db, &logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)

	// First save succeeds
	events := []*eventstore.EventWithMapTags{
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
		nil,
		"test_boundary",
		"test-stream",
		-1, // Expected version 0
		nil,
	)
	require.NoError(t, err)

	// Second save with wrong expected version should fail
	events2 := []*eventstore.EventWithMapTags{
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
		nil,
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

	saveEvents := NewPostgresSaveEvents(db, &logger, mapping)
	getEvents := NewPostgresGetEvents(db, &logger, mapping)

	eventId, err := uuid.NewV7()
	require.NoError(t, err)
	// Save events with different tags
	events1 := []*eventstore.EventWithMapTags{
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
		nil,
		"test_boundary",
		"test-stream",
		-1,
		nil,
	)
	require.NoError(t, err)

	events2 := []*eventstore.EventWithMapTags{
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
		nil,
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
				Name: "test-stream",
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
