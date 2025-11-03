package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "orisun/eventstore"
)

type E2ETestSuite struct {
	ctx              context.Context
	postgresContainer *postgres.PostgresContainer
	binaryPath       string
	binaryCmd        *exec.Cmd
	grpcConn         *grpc.ClientConn
	eventStoreClient pb.EventStoreClient
	postgresHost     string
	postgresPort     string
	grpcPort         string
	adminPort        string
}

func setupE2ETest(t *testing.T) *E2ETestSuite {
	ctx := context.Background()
	suite := &E2ETestSuite{
		ctx:      ctx,
		grpcPort: "15005", // Use different port to avoid conflicts
		adminPort: "18991",
	}

	// Start PostgreSQL container
	postgresContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("orisun"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	suite.postgresContainer = postgresContainer

	// Get PostgreSQL connection details
	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)
	suite.postgresHost = host

	port, err := postgresContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)
	suite.postgresPort = port.Port()

	// Build the binary
	suite.buildBinary(t)

	// Start the binary with proper environment variables
	suite.startBinary(t)

	// Wait for the gRPC server to be ready
	suite.waitForGRPCServer(t)

	// Create gRPC client
	suite.createGRPCClient(t)

	return suite
}

func (s *E2ETestSuite) buildBinary(t *testing.T) {
	// Determine the target OS and architecture
	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH

	// Create build directory if it doesn't exist
	buildDir := "./build"
	err := os.MkdirAll(buildDir, 0755)
	require.NoError(t, err)

	// Set binary name
	binaryName := fmt.Sprintf("orisun-%s-%s", targetOS, targetArch)
	s.binaryPath = filepath.Join(buildDir, binaryName)

	// Build the binary using the same command as build.sh
	cmd := exec.Command("go", "build",
		"-tags", "development=false",
		"-a",
		"-installsuffix", "cgo",
		"-ldflags=-w -s",
		"-gcflags=-m",
		"-o", s.binaryPath,
		"./main.go")

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GOOS=%s", targetOS),
		fmt.Sprintf("GOARCH=%s", targetArch),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, string(output))
	}

	t.Logf("Binary built successfully: %s", s.binaryPath)
}

func (s *E2ETestSuite) startBinary(t *testing.T) {
	// Set environment variables for the binary
	env := []string{
		fmt.Sprintf("ORISUN_PG_HOST=%s", s.postgresHost),
		fmt.Sprintf("ORISUN_PG_PORT=%s", s.postgresPort),
		"ORISUN_PG_USER=postgres",
		"ORISUN_PG_PASSWORD=postgres",
		"ORISUN_PG_NAME=orisun",
		"ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_admin:admin",
		fmt.Sprintf("ORISUN_GRPC_PORT=%s", s.grpcPort),
		fmt.Sprintf("ORISUN_ADMIN_PORT=%s", s.adminPort),
		"ORISUN_GRPC_ENABLE_REFLECTION=true",
		"ORISUN_NATS_PORT=14224", // Use different NATS port
		"ORISUN_NATS_CLUSTER_PORT=16222",
		"ORISUN_NATS_CLUSTER_ENABLED=false",
		"ORISUN_LOGGING_LEVEL=INFO",
		"ORISUN_ADMIN_USERNAME=admin",
		"ORISUN_ADMIN_PASSWORD=changeit",
		"ORISUN_ADMIN_BOUNDARY=orisun_admin",
		"ORISUN_BOUNDARIES=[{\"name\":\"orisun_test_1\",\"description\":\"boundary1\"},{\"name\":\"orisun_test_2\",\"description\":\"boundary2\"},{\"name\":\"orisun_admin\",\"description\":\"boundary3\"}]",
	}

	// Add current environment variables
	env = append(env, os.Environ()...)

	// Start the binary
	s.binaryCmd = exec.Command(s.binaryPath)
	s.binaryCmd.Env = env
	s.binaryCmd.Stdout = os.Stdout
	s.binaryCmd.Stderr = os.Stderr

	err := s.binaryCmd.Start()
	require.NoError(t, err)

	t.Logf("Binary started with PID: %d", s.binaryCmd.Process.Pid)
}

func (s *E2ETestSuite) waitForGRPCServer(t *testing.T) {
	// Wait for the gRPC server to be ready
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		conn, err := grpc.Dial(fmt.Sprintf("localhost:%s", s.grpcPort),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
			grpc.WithTimeout(1*time.Second),
		)
		if err == nil {
			conn.Close()
			t.Logf("gRPC server is ready after %d attempts", i+1)
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("gRPC server did not start within expected time")
}

func (s *E2ETestSuite) createGRPCClient(t *testing.T) {
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%s", s.grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	s.grpcConn = conn
	s.eventStoreClient = pb.NewEventStoreClient(conn)
}

// createAuthenticatedContext creates a context with Basic Auth headers
func createAuthenticatedContext(username, password string) context.Context {
	// Create Basic Auth header
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))

	// Create metadata with the Authorization header
	md := metadata.New(map[string]string{
		"authorization": authHeader,
	})

	// Attach metadata to the context
	return metadata.NewOutgoingContext(context.Background(), md)
}

func (s *E2ETestSuite) teardown(t *testing.T) {
	// Close gRPC connection
	if s.grpcConn != nil {
		s.grpcConn.Close()
	}

	// Stop the binary
	if s.binaryCmd != nil && s.binaryCmd.Process != nil {
		err := s.binaryCmd.Process.Signal(syscall.SIGTERM)
		if err != nil {
			t.Logf("Failed to send SIGTERM to binary: %v", err)
			// Force kill if SIGTERM fails
			s.binaryCmd.Process.Kill()
		}
		// Wait for process to exit
		s.binaryCmd.Wait()
		t.Logf("Binary process stopped")
	}

	// Stop PostgreSQL container
	if s.postgresContainer != nil {
		err := s.postgresContainer.Terminate(s.ctx)
		if err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}

	// Clean up binary
	if s.binaryPath != "" {
		os.Remove(s.binaryPath)
	}
}



func TestE2E_SaveAndGetEvents(t *testing.T) {
	suite := setupE2ETest(t)
	defer suite.teardown(t)

	// Create authenticated context with admin credentials
	ctx := createAuthenticatedContext("admin", "changeit")
	streamName := "test-stream-" + uuid.New().String()

	// Test SaveEvents
	saveReq := &pb.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Stream: &pb.SaveStreamQuery{
			Name:            streamName,
			ExpectedVersion: -1,
		},
		Events: []*pb.EventToSave{
			{
				EventId:   uuid.New().String(),
				EventType: "TestEvent",
				Data:      `{"message": "Hello World"}`,
				Metadata:  `{"source": "e2e-test"}`,
			},
			{
				EventId:   uuid.New().String(),
				EventType: "TestEvent2",
				Data:      `{"message": "Hello World 2"}`,
				Metadata:  `{"source": "e2e-test"}`,
			},
		},
	}

	saveResp, err := suite.eventStoreClient.SaveEvents(ctx, saveReq)
	require.NoError(t, err)
	require.NotNil(t, saveResp)
	assert.GreaterOrEqual(t, saveResp.LogPosition.PreparePosition, int64(0))
	assert.GreaterOrEqual(t, saveResp.LogPosition.CommitPosition, int64(0))

	// Test GetEvents
	getReq := &pb.GetEventsRequest{
		Boundary: "orisun_test_1",
		Stream: &pb.GetStreamQuery{
			Name: streamName,
		},
		Count:     10,
		Direction: pb.Direction_ASC,
	}

	getResp, err := suite.eventStoreClient.GetEvents(ctx, getReq)
	require.NoError(t, err)
	require.NotNil(t, getResp)
	assert.Len(t, getResp.Events, 2)

	// Verify first event
	firstEvent := getResp.Events[0]
	assert.Equal(t, "TestEvent", firstEvent.EventType)
	assert.Equal(t, streamName, firstEvent.StreamId)
	assert.Equal(t, uint64(0), firstEvent.Version)
	assert.Contains(t, string(firstEvent.Data), "Hello World")

	// Verify second event
	secondEvent := getResp.Events[1]
	assert.Equal(t, "TestEvent2", secondEvent.EventType)
	assert.Equal(t, streamName, secondEvent.StreamId)
	assert.Equal(t, uint64(1), secondEvent.Version)
	assert.Contains(t, string(secondEvent.Data), "Hello World 2")
}

func TestE2E_Save200EventsOneByOne(t *testing.T) {
	suite := setupE2ETest(t)
	defer suite.teardown(t)

	// Create authenticated context with admin credentials
	ctx := createAuthenticatedContext("admin", "changeit")
	streamName := "test-stream-200-events-" + uuid.New().String()

	expectedVersion := int64(-1) // Start with empty stream

	// Save 200 events one by one with enhanced error handling
	for i := range 200 {
		eventId := uuid.New().String()
		
		// Add timeout context for each operation
		saveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		
		saveReq := &pb.SaveEventsRequest{
			Boundary: "orisun_test_1",
			Stream: &pb.SaveStreamQuery{
				Name:            streamName,
				ExpectedVersion: expectedVersion,
			},
			Events: []*pb.EventToSave{
				{
					EventId:   eventId,
					EventType: "SequentialEvent",
					Data:      fmt.Sprintf(`{"sequence": %d, "message": "Event number %d", "timestamp": "%s"}`, i, i, time.Now().Format(time.RFC3339)),
					Metadata:  fmt.Sprintf(`{"event_number": %d, "source": "e2e-test-sequential"}`, i),
				},
			},
		}

		t.Logf("Saving event %d/%d", i+1, 200)
		saveResp, err := suite.eventStoreClient.SaveEvents(saveCtx, saveReq)
		cancel() // Always cancel the context
		
		if err != nil {
			t.Fatalf("Failed to save event %d: %v", i, err)
		}
		require.NotNil(t, saveResp, "Save response should not be nil for event %d", i)
		
		// Verify response
		assert.GreaterOrEqual(t, saveResp.LogPosition.PreparePosition, int64(0), "Prepare position should be greater than or equal to 0 for event %d", i)
		assert.GreaterOrEqual(t, saveResp.LogPosition.CommitPosition, int64(0), "Commit position should be greater than or equal to 0 for event %d", i)
		assert.Equal(t, int64(i), saveResp.NewStreamVersion, "Stream version should match sequence for event %d", i)

		// Update expected version for next event
		expectedVersion = saveResp.NewStreamVersion
		
		// Add a small delay every 50 events to allow connection pool to recover
		if (i+1)%50 == 0 {
			t.Logf("Completed %d events, pausing briefly...", i+1)
			time.Sleep(100 * time.Millisecond)
		}
	}

	t.Logf("All 200 events saved successfully, now verifying...")

	// Verify all 200 events were saved correctly by retrieving them in batches
	totalVerified := 0
	batchSize := 500
	for offset := 0; offset < 200; offset += batchSize {
		count := batchSize
		if offset + batchSize > 200 {
			count = 200 - offset
		}
		
		getReq := &pb.GetEventsRequest{
			Boundary: "orisun_test_1",
			Stream: &pb.GetStreamQuery{
				Name:        streamName,
				FromVersion: int64(offset),
			},
			Count:     uint32(count),
			Direction: pb.Direction_ASC,
		}

		getResp, err := suite.eventStoreClient.GetEvents(ctx, getReq)
		require.NoError(t, err)
		require.NotNil(t, getResp)
		assert.Len(t, getResp.Events, count, "Should have exactly %d events in batch", count)

		// Verify the sequence and content of events in this batch
		for i, event := range getResp.Events {
			expectedSequence := offset + i
			assert.Equal(t, "SequentialEvent", event.EventType, "Event %d should have correct type", expectedSequence)
			assert.Equal(t, streamName, event.StreamId, "Event %d should belong to correct stream", expectedSequence)
			assert.Equal(t, uint64(expectedSequence), event.Version, "Event %d should have correct stream version", expectedSequence)
			assert.Contains(t, string(event.Data), fmt.Sprintf(`"sequence": %d`, expectedSequence), "Event %d should have correct sequence in data", expectedSequence)
			assert.Contains(t, string(event.Data), fmt.Sprintf(`"message": "Event number %d"`, expectedSequence), "Event %d should have correct message", expectedSequence)
			assert.Contains(t, string(event.Metadata), fmt.Sprintf(`"event_number": %d`, expectedSequence), "Event %d should have correct event number in metadata", expectedSequence)
			assert.Contains(t, string(event.Metadata), `"source": "e2e-test-sequential"`, "Event %d should have correct source in metadata", expectedSequence)
		}
		
		totalVerified += len(getResp.Events)
		t.Logf("Verified batch %d-%d (%d events), total verified: %d/200", offset, offset+count-1, count, totalVerified)
	}

	assert.Equal(t, 200, totalVerified, "Should have verified exactly 200 events")
	t.Logf("Successfully saved and verified 200 events in stream '%s'", streamName)
}

func TestE2E_OptimisticConcurrency(t *testing.T) {
	suite := setupE2ETest(t)
	defer suite.teardown(t)

	// Create authenticated context with admin credentials
	ctx := createAuthenticatedContext("admin", "changeit")
	streamName := "concurrency-test-" + uuid.New().String()

	// Save first event
	firstSaveReq := &pb.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Stream: &pb.SaveStreamQuery{
			Name:            streamName,
			ExpectedVersion: -1,
		},
		Events: []*pb.EventToSave{
			{
				EventId:   uuid.New().String(),
				EventType: "FirstEvent",
				Data:      `{"message": "First"}`,
				Metadata:  `{"source": "e2e-test"}`,
			},
		},
	}

	firstSaveResp, err := suite.eventStoreClient.SaveEvents(ctx, firstSaveReq)
	require.NoError(t, err)
	require.NotNil(t, firstSaveResp.LogPosition)

	// Try to save with wrong expected version (should fail)
	wrongVersionReq := &pb.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Stream: &pb.SaveStreamQuery{
			Name:            streamName,
			ExpectedVersion: -1, // Wrong version, should be 0
		},
		Events: []*pb.EventToSave{
			{
				EventId:   uuid.New().String(),
				EventType: "SecondEvent",
				Data:      `{"message": "Second"}`,
				Metadata:  `{"source": "e2e-test"}`,
			},
		},
	}

	_, err = suite.eventStoreClient.SaveEvents(ctx, wrongVersionReq)
	assert.Error(t, err, "Expected optimistic concurrency error")

	// Save with correct expected version (should succeed)
	correctVersionReq := &pb.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Stream: &pb.SaveStreamQuery{
			Name:            streamName,
			ExpectedVersion: 0, // Correct version
		},
		Events: []*pb.EventToSave{
			{
				EventId:   uuid.New().String(),
				EventType: "SecondEvent",
				Data:      `{"message": "Second"}`,
				Metadata:  `{"source": "e2e-test"}`,
			},
		},
	}

	correctSaveResp, err := suite.eventStoreClient.SaveEvents(ctx, correctVersionReq)
	require.NoError(t, err)
	require.NotNil(t, correctSaveResp.LogPosition)
}

func TestE2E_MultipleBoundaries(t *testing.T) {
	suite := setupE2ETest(t)
	defer suite.teardown(t)

	// Create authenticated context with admin credentials
	ctx := createAuthenticatedContext("admin", "changeit")
	streamName := "multi-boundary-test-" + uuid.New().String()

	// Save events in different boundaries
	boundaries := []string{"orisun_test_1", "orisun_test_2"}

	for i, boundary := range boundaries {
		saveReq := &pb.SaveEventsRequest{
			Boundary: boundary,
			Stream: &pb.SaveStreamQuery{
				Name:            streamName,
				ExpectedVersion: -1,
			},
			Events: []*pb.EventToSave{
				{
					EventId:   uuid.New().String(),
					EventType: "BoundaryEvent",
					Data:      fmt.Sprintf(`{"boundary": "%s", "index": %d}`, boundary, i),
					Metadata:  `{"source": "e2e-test"}`,
				},
			},
		}

		saveResp, err := suite.eventStoreClient.SaveEvents(ctx, saveReq)
		require.NoError(t, err)
		require.NotNil(t, saveResp.LogPosition)
	}

	// Get events from each boundary
	for _, boundary := range boundaries {
		getReq := &pb.GetEventsRequest{
			Boundary: boundary,
			Stream: &pb.GetStreamQuery{
				Name: streamName,
			},
			Count:     10,
			Direction: pb.Direction_ASC,
		}

		getResp, err := suite.eventStoreClient.GetEvents(ctx, getReq)
		require.NoError(t, err)
		assert.Len(t, getResp.Events, 1)
		assert.Equal(t, "BoundaryEvent", getResp.Events[0].EventType)
		assert.Contains(t, string(getResp.Events[0].Data), boundary)
	}
}

func TestE2E_CatchUpSubscribeToEvents(t *testing.T) {
	suite := setupE2ETest(t)
	defer suite.teardown(t)

	// Create authenticated context with admin credentials
	ctx, cancel := context.WithTimeout(createAuthenticatedContext("admin", "changeit"), 30*time.Second)
	defer cancel()
	
	streamName := "subscription-test-" + uuid.New().String()

	// Save events first
	saveReq := &pb.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Stream: &pb.SaveStreamQuery{
			Name:            streamName,
			ExpectedVersion: -1,
		},
		Events: []*pb.EventToSave{
			{
				EventId:   uuid.New().String(),
				EventType: "SubscriptionTest",
				Data:      `{"message": "First subscription event"}`,
				Metadata:  `{"source": "e2e-subscription-test"}`,
			},
		},
	}

	_, err := suite.eventStoreClient.SaveEvents(ctx, saveReq)
	require.NoError(t, err)

	// Create subscription request to catch up from beginning
	subscribeReq := &pb.CatchUpSubscribeToEventStoreRequest{
		Boundary:       "orisun_test_1",
		SubscriberName: "test-subscriber",
		AfterPosition:  &pb.Position{CommitPosition: 0, PreparePosition: 0}, // Start from beginning
		Query: &pb.Query{
			Criteria: []*pb.Criterion{
				{
					Tags: []*pb.Tag{
						{Key: "eventType", Value: "SubscriptionTest"}, // Note: eventType, not event_type
					},
				},
			},
		},
	}

	// Start subscription
	stream, err := suite.eventStoreClient.CatchUpSubscribeToEvents(ctx, subscribeReq)
	require.NoError(t, err)

	// Try to receive at least one event
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive event from subscription: %v", err)
	}

	// Verify the received event
	assert.Equal(t, "SubscriptionTest", event.EventType)
	assert.Contains(t, event.Data, "First subscription event")
	t.Logf("Successfully received event: %s", event.EventId)
}

func TestE2E_CatchUpSubscribeToStream(t *testing.T) {
	suite := setupE2ETest(t)
	defer suite.teardown(t)

	// Create authenticated context with admin credentials
	ctx, cancel := context.WithTimeout(createAuthenticatedContext("admin", "changeit"), 30*time.Second)
	defer cancel()
	
	streamName := "stream-subscription-test--" + uuid.New().String()

	// Save some initial events to a specific stream
	saveReq := &pb.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Stream: &pb.SaveStreamQuery{
			Name:            streamName,
			ExpectedVersion: -1,
		},
		Events: []*pb.EventToSave{
			{
				EventId:   uuid.New().String(),
				EventType: "StreamSubscriptionTest",
				Data:      `{"message": "First stream event"}`,
				Metadata:  `{"source": "e2e-stream-subscription-test"}`,
			},
			{
				EventId:   uuid.New().String(),
				EventType: "StreamSubscriptionTest",
				Data:      `{"message": "Second stream event"}`,
				Metadata:  `{"source": "e2e-stream-subscription-test"}`,
			},
			{
				EventId:   uuid.New().String(),
				EventType: "StreamSubscriptionTest",
				Data:      `{"message": "Third stream event"}`,
				Metadata:  `{"source": "e2e-stream-subscription-test"}`,
			},
		},
	}

	_, err := suite.eventStoreClient.SaveEvents(ctx, saveReq)
	require.NoError(t, err)

	// Create stream subscription request (subscribe from version 1 onwards)
	streamSubscribeReq := &pb.CatchUpSubscribeToStreamRequest{
		Boundary:       "orisun_test_1",
		SubscriberName: "test-stream-subscriber",
		Stream:         streamName,
		AfterVersion:   -1, // Start from beginning
		Query:          nil, // Remove query to get all events from the stream
	}

	// Start stream subscription
	streamSub, err := suite.eventStoreClient.CatchUpSubscribeToStream(ctx, streamSubscribeReq)
	require.NoError(t, err)

	// Try to receive at least one event
	event, err := streamSub.Recv()
	if err != nil {
		t.Fatalf("Failed to receive event from stream subscription: %v", err)
	}

	// Verify the received event
	assert.Equal(t, "StreamSubscriptionTest", event.EventType)
	assert.Equal(t, streamName, event.StreamId)
	assert.Contains(t, event.Data, "stream event")
	t.Logf("Successfully received stream event: %s from stream: %s (version: %d)", event.EventId, event.StreamId, event.Version)

	// Try to receive the second event
	event2, err := streamSub.Recv()
	if err != nil {
		t.Fatalf("Failed to receive second event from stream subscription: %v", err)
	}

	// Verify the second event
	assert.Equal(t, "StreamSubscriptionTest", event2.EventType)
	assert.Equal(t, streamName, event2.StreamId)
	assert.Contains(t, event2.Data, "stream event")
	t.Logf("Successfully received second stream event: %s from stream: %s (version: %d)", event2.EventId, event2.StreamId, event2.Version)
}