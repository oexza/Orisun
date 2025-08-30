package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"orisun/eventstore"
)

type BenchmarkSetup struct {
	postgresContainer *postgres.PostgresContainer
	binaryPath        string
	binaryCmd         *exec.Cmd
	client            eventstore.EventStoreClient
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
	require.NoError(b, err)
	setup.postgresContainer = postgresContainer

	// Get PostgreSQL connection details
	host, err := postgresContainer.Host(ctx)
	require.NoError(b, err)
	setup.postgresHost = host

	port, err := postgresContainer.MappedPort(ctx, "5432")
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
		"ORISUN_PG_SCHEMAS=benchmark_test:public,benchmark_admin:admin",
		fmt.Sprintf("ORISUN_GRPC_PORT=%s", s.grpcPort),
		fmt.Sprintf("ORISUN_ADMIN_PORT=%s", s.adminPort),
		"ORISUN_GRPC_ENABLE_REFLECTION=true",
		"ORISUN_NATS_PORT=14224",
		"ORISUN_NATS_CLUSTER_PORT=16222",
		"ORISUN_NATS_CLUSTER_ENABLED=false",
		"ORISUN_LOGGING_LEVEL=ERROR",
		"ORISUN_ADMIN_USERNAME=admin",
		"ORISUN_ADMIN_PASSWORD=changeit",
		"ORISUN_ADMIN_BOUNDARY=benchmark_admin",
		"ORISUN_BOUNDARIES=[{\"name\":\"benchmark_test\",\"description\":\"benchmark boundary\"},{\"name\":\"benchmark_admin\",\"description\":\"admin boundary\"}]",
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
	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%s", s.grpcPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(b, err)
	s.conn = conn
	s.client = eventstore.NewEventStoreClient(conn)
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

func generateRandomEvent(eventType string) *eventstore.EventToSave {
	return &eventstore.EventToSave{
		EventId:   uuid.New().String(),
		EventType: eventType,
		Data:      fmt.Sprintf(`{"timestamp": "%s", "message": "random event"}`, time.Now().Format(time.RFC3339)),
		Metadata:  `{"source": "benchmark"}`,
	}
}

func generateEvents(count int, streamId string) []*eventstore.EventToSave {
	events := make([]*eventstore.EventToSave, count)
	for i := 0; i < count; i++ {
		events[i] = &eventstore.EventToSave{
			EventId:   uuid.New().String(),
			EventType: "TestEvent",
			Data:      fmt.Sprintf(`{"message": "test event %d", "timestamp": "%s"}`, i, time.Now().Format(time.RFC3339)),
			Metadata:  fmt.Sprintf(`{"source": "benchmark", "stream": "%s"}`, streamId),
		}
	}
	return events
}

// BenchmarkSaveEvents_Single tests saving single events
func BenchmarkSaveEvents_Single(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	// Pre-generate all events and stream IDs before timing
	events := make([]*eventstore.EventToSave, b.N)
	streamIds := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		events[i] = generateRandomEvent("SingleEvent")
		streamIds[i] = uuid.New().String()
	}

	b.ResetTimer()
	totalEvents := 0
	for i := 0; i < b.N; i++ {
		_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: "benchmark_test",
			Stream: &eventstore.SaveStreamQuery{
				Name:            streamIds[i],
				ExpectedVersion: eventstore.StreamDoesNotExist,
			},
			Events: []*eventstore.EventToSave{events[i]},
		})
		if err != nil {
			b.Errorf("Failed to save single event: %v", err)
		}
		totalEvents++
	}
	// Report custom metric for events per second
	b.ReportMetric(float64(totalEvents)/b.Elapsed().Seconds(), "events/sec")
}

// BenchmarkSaveEvents_Batch tests saving events in batches
func BenchmarkSaveEvents_Batch(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	batchSizes := []int{10, 100, 1000, 10000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			// Pre-generate all events and stream IDs before timing
			batches := make([][]*eventstore.EventToSave, b.N)
			streamIds := make([]string, b.N)
			for i := 0; i < b.N; i++ {
				batches[i] = make([]*eventstore.EventToSave, batchSize)
				streamIds[i] = uuid.New().String()
				for j := 0; j < batchSize; j++ {
					batches[i][j] = generateRandomEvent("BatchEvent")
				}
			}

			b.ResetTimer()
			totalEvents := 0
			for i := 0; i < b.N; i++ {
				_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
					Boundary: "benchmark_test",
					Stream: &eventstore.SaveStreamQuery{
						Name:            streamIds[i],
						ExpectedVersion: -1,
					},
					Events: batches[i],
				})
				if err != nil {
					b.Errorf("Failed to save batch: %v", err)
				}
				totalEvents += batchSize
			}
			// Report custom metric for events per second
			b.ReportMetric(float64(totalEvents)/b.Elapsed().Seconds(), "events/sec")
		})
	}
}

// BenchmarkSaveEvents_SingleStream tests saving large batches to the same stream
func BenchmarkSaveEvents_SingleStream(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	batchSizes := []int{100, 1000, 5000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			// Create unique stream ID for this sub-benchmark to avoid conflicts
			streamId := uuid.New().String()
			
			// Pre-generate all events and expected versions before timing
			batches := make([][]*eventstore.EventToSave, b.N)
			expectedVersions := make([]int64, b.N)
			
			for i := 0; i < b.N; i++ {
				batches[i] = make([]*eventstore.EventToSave, batchSize)
				for j := 0; j < batchSize; j++ {
					batches[i][j] = generateRandomEvent("SingleStreamEvent")
				}
				if i == 0 {
					expectedVersions[i] = int64(eventstore.StreamDoesNotExist)
				} else {
					expectedVersions[i] = expectedVersions[i-1] + int64(batchSize)
				}
			}
			
			b.ResetTimer()
			totalEvents := 0
			for i := 0; i < b.N; i++ {
				_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
					Boundary: "benchmark_test",
					Stream: &eventstore.SaveStreamQuery{
						Name:            streamId,
						ExpectedVersion: expectedVersions[i],
					},
					Events: batches[i],
				})
				if err != nil {
					b.Errorf("Failed to save batch to single stream: %v", err)
				}
				totalEvents += batchSize
			}
			// Report custom metric for events per second
			b.ReportMetric(float64(totalEvents)/b.Elapsed().Seconds(), "events/sec")
		})
	}
}

// BenchmarkSaveEvents_ConcurrentStreams tests concurrent access to multiple streams
// This simulates real-world scenarios where multiple users save events to their own streams concurrently
func BenchmarkSaveEvents_ConcurrentStreams(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	workerCounts := []int{10, 50, 100}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workers), func(b *testing.B) {
			// Pre-generate events for each worker before timing
			workerEvents := make(map[int][]*eventstore.EventToSave)
			workerStreamIds := make(map[int]string)
			
			// Estimate events per worker (b.N gets distributed across workers)
			eventsPerWorker := (b.N + workers - 1) / workers
			for w := 0; w < workers; w++ {
				workerEvents[w] = make([]*eventstore.EventToSave, eventsPerWorker)
				workerStreamIds[w] = uuid.New().String()
				for i := 0; i < eventsPerWorker; i++ {
					workerEvents[w][i] = generateRandomEvent("ConcurrentUserEvent")
				}
			}
			
			b.ResetTimer()
			totalEvents := 0
			eventsMutex := sync.Mutex{}
			
			b.RunParallel(func(pb *testing.PB) {
				// Each goroutine gets its own stream and pre-generated events
				workerID := 0 // This will be different for each goroutine
				eventsMutex.Lock()
				workerID = len(workerStreamIds) % workers
				userStreamId := workerStreamIds[workerID]
				userEvents := workerEvents[workerID]
				eventsMutex.Unlock()
				
				currentVersion := int64(eventstore.StreamDoesNotExist)
				eventIndex := 0
				
				for pb.Next() {
					if eventIndex >= len(userEvents) {
						break // No more pre-generated events
					}
					
					_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
						Boundary: "benchmark_test",
						Stream: &eventstore.SaveStreamQuery{
							Name:            userStreamId,
							ExpectedVersion: currentVersion,
						},
						Events: []*eventstore.EventToSave{userEvents[eventIndex]},
					})
					if err != nil {
						b.Errorf("Failed to save event to user stream: %v", err)
					}
					currentVersion++
					eventIndex++
					
					eventsMutex.Lock()
					totalEvents++
					eventsMutex.Unlock()
				}
			})
			
			// Report custom metric for events per second
			b.ReportMetric(float64(totalEvents)/b.Elapsed().Seconds(), "events/sec")
		})
	}
}

// BenchmarkGetEvents tests event retrieval
func BenchmarkGetEvents(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	// Pre-populate with events
	boundary := "benchmark_test"
	streamId := uuid.New().String()
	events := generateEvents(1000, streamId)
	_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
		Boundary: boundary,
		Stream: &eventstore.SaveStreamQuery{
			Name:            streamId,
			ExpectedVersion: -1,
		},
		Events: events,
	})
	if err != nil {
		b.Fatalf("Failed to pre-populate events: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := setup.client.GetEvents(setup.authContext(), &eventstore.GetEventsRequest{
			Boundary: boundary,
			Count:    50,
		})
		if err != nil {
			b.Errorf("Failed to get events: %v", err)
		}
	}
}

// BenchmarkGetStream tests stream retrieval using stream query
func BenchmarkGetStream(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	// Pre-populate with events using stream
	streamName := "benchmark_stream"
	boundary := "stream_boundary"
	for i := 0; i < 1000; i++ {
		event := generateRandomEvent("StreamTestEvent")
		_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name: streamName,
				ExpectedVersion: -1, // Any version
			},
			Events: []*eventstore.EventToSave{event},
		})
		if err != nil {
			b.Fatalf("Failed to pre-populate stream events: %v", err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := setup.client.GetEvents(setup.authContext(), &eventstore.GetEventsRequest{
				Boundary: boundary,
				Stream: &eventstore.GetStreamQuery{
					Name: streamName,
					FromVersion: 0,
				},
				Count: 100,
			})
			if err != nil {
				b.Errorf("Failed to get stream: %v", err)
			}
		}
	})
}

// BenchmarkSubscribeToEvents tests event subscription performance
func BenchmarkSubscribeToEvents(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "subscribe_boundary"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := setup.client.CatchUpSubscribeToEvents(setup.authContext(), &eventstore.CatchUpSubscribeToEventStoreRequest{
			Boundary: boundary,
			SubscriberName: fmt.Sprintf("benchmark_subscriber_%d", i),
		})
		if err != nil {
			b.Errorf("Failed to subscribe to events: %v", err)
			continue
		}

		// Read a few events to test streaming performance
		for j := 0; j < 10; j++ {
			// Save an event to trigger the subscription
			go func() {
				event := generateRandomEvent("SubscribeTestEvent")
				streamId := uuid.New().String()
				setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
					Boundary: boundary,
					Stream: &eventstore.SaveStreamQuery{
						Name:            streamId,
						ExpectedVersion: -1,
					},
					Events: []*eventstore.EventToSave{event},
				})
			}()

			_, err := stream.Recv()
			if err != nil {
				break
			}
		}
		stream.CloseSend()
	}
}

// BenchmarkConcurrentOperations tests mixed concurrent operations
func BenchmarkConcurrentOperations(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "concurrent_boundary"

	// Pre-populate with some events
	for i := 0; i < 100; i++ {
		event := generateRandomEvent("ConcurrentTestEvent")
		streamId := uuid.New().String()
		setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name:            streamId,
				ExpectedVersion: -1,
			},
			Events: []*eventstore.EventToSave{event},
		})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var wg sync.WaitGroup
			wg.Add(3)

			// Concurrent save
			go func() {
				defer wg.Done()
				event := generateRandomEvent("ConcurrentSaveEvent")
				streamId := uuid.New().String()
				setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
					Boundary: boundary,
					Stream: &eventstore.SaveStreamQuery{
						Name:            streamId,
						ExpectedVersion: -1,
					},
					Events: []*eventstore.EventToSave{event},
				})
			}()

			// Concurrent get
			go func() {
				defer wg.Done()
				setup.client.GetEvents(setup.authContext(), &eventstore.GetEventsRequest{
					Boundary: boundary,
					Count:    10,
				})
			}()

			// Concurrent get (another operation)
			go func() {
				defer wg.Done()
				setup.client.GetEvents(setup.authContext(), &eventstore.GetEventsRequest{
					Boundary: boundary,
					Count:    5,
				})
			}()

			wg.Wait()
		}
	})
}

// BenchmarkHighThroughput tests high-throughput scenarios
func BenchmarkHighThroughput(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	workerCounts := []int{10, 50, 100}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workers), func(b *testing.B) {
			boundary := fmt.Sprintf("throughput_boundary_%d", workers)
			b.ResetTimer()
			totalEvents := 0
			var eventsMutex sync.Mutex

			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					workerEvents := 0
					for i := 0; i < b.N/workers; i++ {
						event := generateRandomEvent(fmt.Sprintf("ThroughputEvent_Worker_%d", workerID))
						streamId := uuid.New().String()
						_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
							Boundary: boundary,
							Stream: &eventstore.SaveStreamQuery{
								Name:            streamId,
								ExpectedVersion: -1,
							},
							Events: []*eventstore.EventToSave{event},
						})
						if err == nil {
							workerEvents++
						}
					}
					eventsMutex.Lock()
					totalEvents += workerEvents
					eventsMutex.Unlock()
				}(w)
			}
			wg.Wait()
			// Report custom metric for events per second
			b.ReportMetric(float64(totalEvents)/b.Elapsed().Seconds(), "events/sec")
		})
	}
}

// BenchmarkMemoryUsage tests memory efficiency
func BenchmarkMemoryUsage(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "benchmark_test"

	// Pre-generate all events and stream IDs before timing
	events := make([]*eventstore.EventToSave, b.N)
	streamIds := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		// Create large events to test memory handling
		largeData := make([]byte, 1024) // 1KB per event
		rand.Read(largeData)

		events[i] = &eventstore.EventToSave{
			EventId:   uuid.New().String(),
			EventType: "LargeEvent",
			Data:      fmt.Sprintf(`{"large_field": "%s", "size": %d, "iteration": %d}`, base64.StdEncoding.EncodeToString(largeData), len(largeData), i),
			Metadata:  `{"source": "benchmark", "test": "memory"}`,
		}
		streamIds[i] = uuid.New().String()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name:            streamIds[i],
				ExpectedVersion: -1,
			},
			Events: []*eventstore.EventToSave{events[i]},
		})
		if err != nil {
			b.Errorf("Failed to save large event: %v", err)
		}
	}
}