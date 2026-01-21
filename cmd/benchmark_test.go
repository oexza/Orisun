package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"github.com/oexza/Orisun/postgres"
)

type BenchmarkSetup struct {
	postgresContainer testcontainers.Container
	binaryPath        string
	binaryCmd         *exec.Cmd
	client            orisun.EventStoreClient
	conn              *grpc.ClientConn
	ctx               context.Context
	cancel            context.CancelFunc
	postgresHost      string
	postgresPort      string
	grpcPort          string
	adminPort         string
}

func setupBenchmark(b *testing.B) *BenchmarkSetup {
	b.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	setup := &BenchmarkSetup{
		ctx:       ctx,
		cancel:    cancel,
		grpcPort:  "15006", // Use different port for benchmarks
		adminPort: "18992",
	}

	// Start PostgreSQL container with optimized configuration
	req := testcontainers.ContainerRequest{
		Image:        "postgres:17.5-alpine3.22",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "orisun",
		},
		Cmd: []string{
			"postgres",
			// Memory settings
			//"-c", "shared_buffers=2GB",
			//"-c", "work_mem=128MB",
			//"-c", "maintenance_work_mem=512MB",
			//"-c", "effective_cache_size=6GB",
			//// WAL settings for write performance
			//"-c", "wal_buffers=128MB",
			//"-c", "min_wal_size=2GB",
			//"-c", "max_wal_size=8GB",
			//"-c", "checkpoint_completion_target=0.7",
			//"-c", "checkpoint_timeout=3min",
			//// Connection and concurrency
			"-c", "max_connections=300",
			//"-c", "max_worker_processes=20",
			//"-c", "max_parallel_workers=14",
			//"-c", "max_parallel_workers_per_gather=7",
			//"-c", "max_parallel_maintenance_workers=7",
			//// Performance tuning
			//"-c", "random_page_cost=1.1",
			//"-c", "seq_page_cost=1.0",
			//"-c", "cpu_tuple_cost=0.01",
			//"-c", "cpu_index_tuple_cost=0.005",
			//"-c", "cpu_operator_cost=0.0025",
			//// Logging for performance monitoring
			//"-c", "log_min_duration_statement=1000",
			//"-c", "log_checkpoints=on",
			//"-c", "log_connections=on",
			//"-c", "log_disconnections=on",
			//"-c", "log_lock_waits=on",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(b, err)

	// Store the container
	setup.postgresContainer = container

	// Get PostgreSQL connection details
	host, err := container.Host(ctx)
	require.NoError(b, err)
	setup.postgresHost = host

	port, err := container.MappedPort(ctx, "5432")
	require.NoError(b, err)
	setup.postgresPort = port.Port()

	// Build the binary
	setup.buildBinary(b)

	// Start the binary with proper environment variables
	setup.startBinary(b)

	// Wait for the gRPC server to be ready
	setup.waitForGRPCServer(b)

	// Create gRPC client
	setup.createGRPCClient(b)

	return setup
}

func (s *BenchmarkSetup) buildBinary(b *testing.B) {
	// Determine the target OS and architecture
	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH

	// Create build directory if it doesn't exist
	buildDir := "./build"
	err := os.MkdirAll(buildDir, 0755)
	require.NoError(b, err)

	// Set binary name
	binaryName := fmt.Sprintf("orisun-%s-%s", targetOS, targetArch)
	s.binaryPath = filepath.Join(buildDir, binaryName)

	// Build the binary using the same command as build.sh
	cmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", s.binaryPath, ".")
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	b.Logf("Building binary: %s", s.binaryPath)
	err = cmd.Run()
	require.NoError(b, err, "Failed to build binary")

	// Verify binary exists
	_, err = os.Stat(s.binaryPath)
	require.NoError(b, err, "Binary not found after build")
}

func (s *BenchmarkSetup) startBinary(b *testing.B) {
	// Set environment variables for the binary
	env := []string{
		fmt.Sprintf("ORISUN_PG_HOST=%s", s.postgresHost),
		fmt.Sprintf("ORISUN_PG_PORT=%s", s.postgresPort),
		"ORISUN_PG_USER=postgres",
		"ORISUN_PG_PASSWORD=postgres",
		"ORISUN_PG_NAME=orisun",
		"ORISUN_PG_SCHEMAS=benchmark_test:public,benchmark_admin:admin,subscribe_boundary:public",
		// Increase connection pool limits for benchmark performance
		"ORISUN_PG_WRITE_MAX_OPEN_CONNS=200", // Increased from 20 to 100
		"ORISUN_PG_WRITE_MAX_IDLE_CONNS=20",  // Increased from 3 to 20
		"ORISUN_PG_READ_MAX_OPEN_CONNS=10",   // Increased from 10 to 50
		"ORISUN_PG_READ_MAX_IDLE_CONNS=5",    // Increased from 5 to 10
		// Enable pprof for profiling during benchmarks
		"ORISUN_PPROF_ENABLED=true",
		"ORISUN_PPROF_PORT=6060",
		fmt.Sprintf("ORISUN_GRPC_PORT=%s", s.grpcPort),
		fmt.Sprintf("ORISUN_ADMIN_PORT=%s", s.adminPort),
		"ORISUN_GRPC_ENABLE_REFLECTION=true",
		"ORISUN_NATS_PORT=14224",
		"ORISUN_NATS_CLUSTER_PORT=16222",
		"ORISUN_NATS_CLUSTER_ENABLED=false",
		"ORISUN_LOGGING_LEVEL=INFO",
		"ORISUN_ADMIN_USERNAME=admin",
		"ORISUN_ADMIN_PASSWORD=changeit",
		"ORISUN_ADMIN_BOUNDARY=benchmark_admin",
		"ORISUN_BOUNDARIES=[{\"name\":\"benchmark_test\",\"description\":\"benchmark boundary\"},{\"name\":\"benchmark_admin\",\"description\":\"admin boundary\"},{\"name\":\"subscribe_boundary\",\"description\":\"subscription benchmark boundary\"}]",
	}

	// Add current environment variables
	env = append(env, os.Environ()...)

	// Start the binary
	s.binaryCmd = exec.Command(s.binaryPath)
	s.binaryCmd.Env = env
	// Capture output for debugging
	s.binaryCmd.Stdout = os.Stdout
	s.binaryCmd.Stderr = os.Stderr
	s.binaryCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	b.Logf("Starting binary: %s", s.binaryPath)
	err := s.binaryCmd.Start()
	require.NoError(b, err, "Failed to start binary")

	b.Logf("Binary started with PID: %d", s.binaryCmd.Process.Pid)

	// Give the binary some time to start
	time.Sleep(3 * time.Second)
}

func (s *BenchmarkSetup) waitForGRPCServer(b *testing.B) {
	// Wait for gRPC server to be ready
	for i := range 60 { // Increase timeout to 60 seconds
		conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%s", s.grpcPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn.Close()
			b.Logf("gRPC server is ready on port %s", s.grpcPort)
			// Add extra delay to ensure server is fully ready
			time.Sleep(2 * time.Second)
			return
		}
		b.Logf("Attempt %d: gRPC server not ready yet: %v", i+1, err)
		time.Sleep(1 * time.Second)
	}
	require.Fail(b, "gRPC server did not start within 60 seconds")
}

func (s *BenchmarkSetup) createGRPCClient(b *testing.B) {
	var token *string = nil

	// Create an interceptor to extract token from response metadata
	unaryInterceptor := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		//if token is not nil, add it to the outgoing context
		if token != nil {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-auth-token", *token)
		}

		// Create variables to capture response metadata
		var responseMD metadata.MD
		var responseTrailer metadata.MD

		// Add options to capture response headers and trailers
		opts = append(opts, grpc.Header(&responseMD), grpc.Trailer(&responseTrailer))

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err == nil {
			// Extract token from response headers if present
			if tokens := responseMD.Get("x-auth-token"); len(tokens) > 0 {
				// b.Logf("Extracted token from response headers: %s", tokens[0])
				token = &tokens[0]
			}
		}
		return err
	}

	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%s", s.grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(unaryInterceptor),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024*1024*100)), // 100MB max message size
	)
	require.NoError(b, err)
	s.conn = conn
	s.client = orisun.NewEventStoreClient(conn)

	// Wait for admin user to be created by the projector
	// Retry authentication until it succeeds
	var pingResp *orisun.PingResponse
	for i := 0; i < 30; i++ {
		pingResp, err = s.client.Ping(s.authContext(), &orisun.PingRequest{})
		if err == nil {
			b.Logf("Authentication successful on attempt %d", i+1)
			break
		}
		b.Logf("Authentication attempt %d failed: %v, retrying...", i+1, err)
		time.Sleep(time.Second)
	}
	if err != nil {
		b.Logf("Final ping response: %v, %v", pingResp, err)
	}
}

func (s *BenchmarkSetup) cleanup(b *testing.B) {
	b.Helper()

	// Close gRPC connection
	if s.conn != nil {
		s.conn.Close()
	}

	// Stop the binary
	if s.binaryCmd != nil && s.binaryCmd.Process != nil {
		b.Logf("Stopping binary with PID: %d", s.binaryCmd.Process.Pid)
		// Send SIGTERM to the process group
		err := syscall.Kill(-s.binaryCmd.Process.Pid, syscall.SIGTERM)
		if err != nil {
			b.Logf("Failed to send SIGTERM: %v", err)
		}

		// Wait for process to exit gracefully
		done := make(chan error, 1)
		go func() {
			done <- s.binaryCmd.Wait()
		}()

		select {
		case <-time.After(10 * time.Second):
			b.Logf("Binary did not exit gracefully, sending SIGKILL")
			syscall.Kill(-s.binaryCmd.Process.Pid, syscall.SIGKILL)
			s.binaryCmd.Wait()
		case <-done:
			b.Logf("Binary exited gracefully")
		}
	}

	// Stop PostgreSQL container
	if s.postgresContainer != nil {
		err := s.postgresContainer.Terminate(s.ctx)
		if err != nil {
			b.Logf("Failed to terminate postgres container: %v", err)
		}
	}

	s.cancel()
}

func (s *BenchmarkSetup) authContext() context.Context {
	creds := base64.StdEncoding.EncodeToString([]byte("admin:changeit"))
	return metadata.AppendToOutgoingContext(s.ctx, "authorization", "Basic "+creds)
}

func generateRandomEvent(eventType string) *orisun.EventToSave {
	timestamp := time.Now().Format(time.RFC3339)
	return &orisun.EventToSave{
		EventId:   uuid.New().String(),
		EventType: eventType,
		Data: fmt.Sprintf(`{
			"timestamp": "%s",
			"message": "random event",
			"user_id": "%s",
			"session_id": "%s",
			"event_id": "%s",
			"source_ip": "192.168.1.100",
			"user_agent": "Mozilla/5.0",
			"request_id": "%s",
			"correlation_id": "%s",
			"priority": "high",
			"status": "active",
			"region": "us-east-1",
			"environment": "production",
			"version": "1.0.0",
			"category": "user_action",
			"sub_category": "login",
			"retry_count": 3,
			"latency_ms": 45,
			"success": true,
			"tags": ["benchmark", "test", "performance"]
		}`, timestamp, uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()),
		Metadata: fmt.Sprintf(`{
			"source": "benchmark",
			"hostname": "test-server",
			"pid": 12345,
			"thread_id": "thread-1",
			"app_version": "2.1.0"
		}`),
	}
}

func generateEvents(count int, streamId string) []*orisun.EventToSave {
	timestamp := time.Now().Format(time.RFC3339)
	events := make([]*orisun.EventToSave, count)
	for i := 0; i < count; i++ {
		events[i] = &orisun.EventToSave{
			EventId:   uuid.New().String(),
			EventType: "TestEvent",
			Data: fmt.Sprintf(`{
				"timestamp": "%s",
				"message": "test event %d",
				"user_id": "%s",
				"session_id": "%s",
				"event_id": "%s",
				"source_ip": "192.168.1.100",
				"user_agent": "Mozilla/5.0",
				"request_id": "%s",
				"correlation_id": "%s",
				"priority": "high",
				"status": "active",
				"region": "us-east-1",
				"environment": "production",
				"version": "1.0.0",
				"category": "test",
				"sub_category": "benchmark",
				"retry_count": 0,
				"latency_ms": 25,
				"success": true,
				"tags": ["test", "benchmark", "performance"],
				"stream_id": "%s"
			}`, timestamp, i, uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(), streamId),
			Metadata: fmt.Sprintf(`{
				"source": "benchmark",
				"hostname": "test-server",
				"pid": 12345,
				"thread_id": "thread-1",
				"app_version": "2.1.0",
				"stream": "%s"
			}`, streamId),
		}
	}
	return events
}

// BenchmarkGetEvents tests event retrieval
func BenchmarkGetEvents(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	// Pre-populate with events
	boundary := "benchmark_test"
	streamId := uuid.New().String()
	events := generateEvents(1000, streamId)
	p := orisun.NotExistsPosition()
	_, err := setup.client.SaveEvents(setup.authContext(), &orisun.SaveEventsRequest{
		Boundary: boundary,
		Query: &orisun.SaveQuery{
			ExpectedPosition: &p,
		},
		Events: events,
	})
	if err != nil {
		b.Fatalf("Failed to pre-populate events: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := setup.client.GetEvents(setup.authContext(), &orisun.GetEventsRequest{
			Boundary: boundary,
			Count:    50,
		})
		if err != nil {
			b.Errorf("Failed to get events: %v", err)
		}
	}
}

// BenchmarkSubscribeToEvents tests event subscription performance
func BenchmarkSubscribeToEvents(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "subscribe_boundary"

	// Pre-populate with events to ensure subscription has data to read
	for i := 0; i < 100; i++ {
		event := generateRandomEvent("PrePopulateEvent")
		p := orisun.NotExistsPosition()
		_, err := setup.client.SaveEvents(setup.authContext(), &orisun.SaveEventsRequest{
			Boundary: boundary,
			Query: &orisun.SaveQuery{
				ExpectedPosition: &p,
			},
			Events: []*orisun.EventToSave{event},
		})
		if err != nil {
			b.Fatalf("Failed to pre-populate events: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := setup.client.CatchUpSubscribeToEvents(setup.authContext(), &orisun.CatchUpSubscribeToEventStoreRequest{
			Boundary:       boundary,
			SubscriberName: fmt.Sprintf("benchmark_subscriber_%d", i),
		})
		if err != nil {
			b.Errorf("Failed to subscribe to events: %v", err)
			continue
		}

		// Read events from the subscription (should have pre-populated data)
		eventsRead := 0
		for j := 0; j < 10; j++ {
			_, err := stream.Recv()
			if err != nil {
				break
			}
			eventsRead++
		}
		stream.CloseSend()

		// Only count successful subscriptions that read events
		if eventsRead == 0 {
			b.Errorf("Subscription %d read no events", i)
		}
	}
}

// BenchmarkSaveEvents_Burst10000 dispatches 10,000 SaveEvents concurrently at the same moment
func BenchmarkSaveEvents_Burst10000(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "benchmark_test"
	concurrency := 20000

	var totalEvents int64
	var totalErrors int64

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrency)

	// Pre-create all events to reduce allocation during timing
	events := make([]*orisun.EventToSave, concurrency)
	for i := 0; i < concurrency; i++ {
		events[i] = generateRandomEvent("BurstEvent")
	}

	var workerWg sync.WaitGroup

	// Start worker goroutines
	for w := 0; w < len(events); w++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			_, err := setup.client.SaveEvents(setup.authContext(), &orisun.SaveEventsRequest{
				Boundary: boundary,
				Events:   []*orisun.EventToSave{events[w]},
			})
			if err == nil {
				atomic.AddInt64(&totalEvents, 1)
			} else {
				atomic.AddInt64(&totalErrors, 1)
			}
		}()
	}

	// Measure the burst window only
	b.ResetTimer()
	startTime := time.Now()
	close(start) // Signal all workers to start

	workerWg.Wait()
	b.StopTimer()
	elapsed := time.Since(startTime)

	b.ReportMetric(float64(totalEvents)/elapsed.Seconds(), "events/sec")
	if totalErrors > 0 {
		b.Logf("Encountered %d errors during burst", totalErrors)
	}
}

// BenchmarkMemoryUsage tests memory efficiency
func BenchmarkMemoryUsage(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "benchmark_test"

	// Pre-generate all events and stream IDs before timing
	events := make([]*orisun.EventToSave, b.N)
	streamIds := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		// Create large events to test memory handling
		largeData := make([]byte, 1024) // 1KB per event
		rand.Read(largeData)

		events[i] = &orisun.EventToSave{
			EventId:   uuid.New().String(),
			EventType: "LargeEvent",
			Data:      fmt.Sprintf(`{"large_field": "%s", "size": %d, "iteration": %d}`, base64.StdEncoding.EncodeToString(largeData), len(largeData), i),
			Metadata:  `{"source": "benchmark", "test": "memory"}`,
		}
		streamIds[i] = uuid.New().String()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := orisun.NotExistsPosition()
		_, err := setup.client.SaveEvents(setup.authContext(), &orisun.SaveEventsRequest{
			Boundary: boundary,
			Query: &orisun.SaveQuery{
				ExpectedPosition: &p,
			},
			Events: []*orisun.EventToSave{events[i]},
		})
		if err != nil {
			b.Errorf("Failed to save large event: %v", err)
		}
	}
}

// BenchmarkSaveEvents_DirectDatabase tests the PostgreSQL eventstore Save method directly
// bypassing gRPC to isolate database performance from network/gRPC overhead
func BenchmarkSaveEvents_DirectDatabase10K(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	ctx := context.Background()

	// Create database connection
	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/orisun?sslmode=disable", setup.postgresHost, setup.postgresPort)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		b.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	// Configure connection pool for high performance
	db.SetMaxOpenConns(250)
	db.SetMaxIdleConns(100)
	db.SetConnMaxLifetime(time.Hour)

	// Wait for database to be ready
	for i := 0; i < 30; i++ {
		if err := db.Ping(); err == nil {
			break
		}
		time.Sleep(time.Second)
		if i == 29 {
			b.Fatalf("Database not ready after 30 seconds")
		}
	}

	// Run database migrations
	if err := postgres.RunDbScripts(db, "benchmark_test", "public", false, ctx); err != nil {
		b.Fatalf("Failed to migrate public schema: %v", err)
	}

	// Create PostgreSQL eventstore instance
	boundarySchemaMappings := map[string]config.BoundaryToPostgresSchemaMapping{
		"benchmark_test": {Schema: "public"},
	}

	logger, err := logging.ZapLogger("info")
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	saveEvents := postgres.NewPostgresSaveEvents(ctx, db, logger, boundarySchemaMappings)

	// Prepare test events - 10K simultaneous saves to the SAME stream
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
				Data:      fmt.Sprintf(`{"event_id": %d, "criteria": "type_%d", "timestamp": "%s"}`, eventIndex, eventIndex%100, time.Now().Format(time.RFC3339)),
				Metadata:  fmt.Sprintf(`{"benchmark": "concurrent_same_stream", "event_index": %d}`, eventIndex),
			}

			// Create unique consistency condition for each event
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

				// ALL goroutines try to save to the SAME stream with expected version -1
				// This will test optimistic concurrency - only one should succeed, others should fail
				p := orisun.NotExistsPosition()
				_, _, err := saveEvents.Save(
					ctx,
					[]orisun.EventWithMapTags{event},
					"benchmark_test",
					&p,                   // Expected position for a new stream.
					consistencyCondition, // Unique consistency condition for each event
				)

				// Use atomic operations to avoid mutex overhead
				atomic.AddInt64(&totalAttempts, 1)
				if err == nil {
					atomic.AddInt64(&successfulSaves, 1)
				}
				// Ignore concurrency errors - they're expected in this test
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

		b.Logf("🚀 TRULY SIMULTANEOUS TEST: %d attempts (%d successful) in %.2f seconds at %.1f events/sec",
			finalTotalAttempts, finalSuccessfulSaves, duration.Seconds(), attemptsPerSecond)
	}
}

// BenchmarkSaveEvents_DirectDatabase10KBatch - 10K events in a single batch operation
func BenchmarkSaveEvents_DirectDatabase10KBatch(b *testing.B) {
	const totalEvents = 10000

	// Setup PostgreSQL container with optimized configuration
	req := testcontainers.ContainerRequest{
		Image:        "postgres:17.5-alpine3.22",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "orisun",
		},
		Cmd: []string{
			"postgres",
			// Memory settings
			"-c", "shared_buffers=2GB",
			"-c", "work_mem=64MB",
			"-c", "maintenance_work_mem=512MB",
			"-c", "effective_cache_size=6GB",
			// WAL settings for write performance
			"-c", "wal_buffers=64MB",
			"-c", "min_wal_size=2GB",
			"-c", "max_wal_size=8GB",
			"-c", "checkpoint_completion_target=0.7",
			"-c", "checkpoint_timeout=3min",
			// Connection and concurrency
			"-c", "max_connections=200",
			"-c", "max_worker_processes=14",
			"-c", "max_parallel_workers=14",
			"-c", "max_parallel_workers_per_gather=7",
			"-c", "max_parallel_maintenance_workers=7",
			// Performance tuning
			"-c", "random_page_cost=1.1",
			"-c", "seq_page_cost=1.0",
			"-c", "cpu_tuple_cost=0.01",
			"-c", "cpu_index_tuple_cost=0.005",
			"-c", "cpu_operator_cost=0.0025",
			// Logging for performance monitoring
			"-c", "log_min_duration_statement=1000",
			"-c", "log_checkpoints=on",
			"-c", "log_connections=on",
			"-c", "log_disconnections=on",
			"-c", "log_lock_waits=on",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60 * time.Second),
	}

	ctx := context.Background()
	postgresContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		b.Fatalf("Failed to start PostgreSQL container: %v", err)
	}
	defer func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			b.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
	}()

	// Get PostgreSQL connection details
	host, err := postgresContainer.Host(ctx)
	if err != nil {
		b.Fatalf("Failed to get PostgreSQL host: %v", err)
	}

	port, err := postgresContainer.MappedPort(ctx, "5432")
	if err != nil {
		b.Fatalf("Failed to get PostgreSQL port: %v", err)
	}

	// Create database connection
	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/orisun?sslmode=disable", host, port.Port())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		b.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	// Configure connection pool for high performance
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(20)
	db.SetConnMaxLifetime(time.Hour)

	// Wait for database to be ready
	for i := range 30 {
		if err := db.Ping(); err == nil {
			break
		}
		time.Sleep(time.Second)
		if i == 29 {
			b.Fatalf("Database not ready after 30 seconds")
		}
	}

	// Run database migrations
	if err := postgres.RunDbScripts(db, "benchmark_test", "public", false, ctx); err != nil {
		b.Fatalf("Failed to migrate public schema: %v", err)
	}

	// Create PostgreSQL eventstore instance
	boundarySchemaMappings := map[string]config.BoundaryToPostgresSchemaMapping{
		"benchmark_test": {Schema: "public"},
	}

	logger, err := logging.ZapLogger("info")
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	saveEvents := postgres.NewPostgresSaveEvents(ctx, db, logger, boundarySchemaMappings)

	// Prepare test events - 10K events for batch save
	events := make([]orisun.EventWithMapTags, totalEvents)

	for i := range totalEvents {
		events[i] = orisun.EventWithMapTags{
			EventId:   uuid.New().String(),
			EventType: "BatchTestEvent",
			Data:      fmt.Sprintf(`{"message": "Batch event %d", "timestamp": "%s"}`, i, time.Now().Format(time.RFC3339)),
			Metadata:  fmt.Sprintf(`{"event_index": %d, "batch_test": true}`, i),
		}
	}

	// Reset timer to exclude setup time
	b.ResetTimer()
	b.ReportAllocs()

	// Run the benchmark
	for i := 0; i < b.N; i++ {
		start := time.Now()

		// Save all events in a single batch operation
		p := orisun.NotExistsPosition()
		_, _, err := saveEvents.Save(ctx, events, "benchmark_test", &p, nil)
		if err != nil {
			b.Fatalf("Failed to save batch events: %v", err)
		}

		duration := time.Since(start)
		eventsPerSecond := float64(totalEvents) / duration.Seconds()

		b.Logf("Batch save: %d events in %v (%.1f events/sec)",
			totalEvents, duration, eventsPerSecond)
	}
}

// Helper function to check if error is optimistic concurrency conflict
func isOptimisticConcurrencyError(err error) bool {
	return err != nil && (
	// Check for common optimistic concurrency error patterns
	fmt.Sprintf("%v", err) == "OptimisticConcurrencyException:StreamVersionConflict")
}
