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

	"orisun/config"
	"orisun/eventstore"
	"orisun/logging"
	"orisun/postgres"
)

type BenchmarkSetup struct {
	postgresContainer testcontainers.Container
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
		"ORISUN_PG_SCHEMAS=benchmark_test:public,benchmark_admin:admin",
		// Increase connection pool limits for benchmark performance
		"ORISUN_PG_WRITE_MAX_OPEN_CONNS=100", // Increased from 20 to 100
		"ORISUN_PG_WRITE_MAX_IDLE_CONNS=20",  // Increased from 3 to 20
		"ORISUN_PG_READ_MAX_OPEN_CONNS=10",   // Increased from 10 to 50
		"ORISUN_PG_READ_MAX_IDLE_CONNS=5",    // Increased from 5 to 10
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

// BenchmarkSaveEvents_SameStreamWithSubsetQuery tests multiple parallel workers
// saving events to the same stream using SaveStreamQuery with SubsetQuery criteria
func BenchmarkSaveEvents_SameStreamWithSubsetQuery(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	// Test different worker counts
	workerCounts := []int{10000}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workers), func(b *testing.B) {
			boundary := "benchmark_test" // Use same boundary as other benchmarks
			streamName := "shared-subset-stream"

			b.ResetTimer()
			totalEvents := int64(0)

			b.RunParallel(func(pb *testing.PB) {
				// Each worker gets a unique user ID for subset filtering
				userID := uuid.New().String()
				localEvents := int64(0)

				for pb.Next() {
					event := &eventstore.EventToSave{
						EventId:   uuid.New().String(),
						EventType: "UserAction",
						Data:      fmt.Sprintf(`{"user_id": "%s", "action": "test_action", "timestamp": "%s"}`, userID, time.Now().Format(time.RFC3339)),
						Metadata:  fmt.Sprintf(`{"source": "benchmark", "user_id": "%s"}`, userID),
					}

					position := eventstore.NotExistsPosition()
					_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
						Boundary: boundary,
						Stream: &eventstore.SaveStreamQuery{
							Name:             streamName,
							ExpectedPosition: &position,
							// Remove ExpectedVersion entirely to allow concurrent appends
							SubsetQuery: &eventstore.Query{
								Criteria: []*eventstore.Criterion{
									{
										Tags: []*eventstore.Tag{
											{Key: "user_id", Value: userID},
											{Key: "eventType", Value: "UserAction"},
										},
									},
								},
							},
						},
						Events: []*eventstore.EventToSave{event},
					})
					if err != nil {
						b.Logf("Save error: %v", err)
					} else {
						localEvents++
					}
				}

				// Add local events to total atomically
				atomic.AddInt64(&totalEvents, localEvents)
			})

			// Report custom metric for events per second
			b.ReportMetric(float64(totalEvents)/b.Elapsed().Seconds(), "events/sec")
		})
	}
}

func BenchmarkSaveEvents_SameStreamDifferentCriteria(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "benchmark_test"
	streamName := "shared-criteria-stream"
	targetEvents := 10000
	criteriaCount := 5
	tagsPerCriteria := 3

	// Use a fixed number of workers for consistent results
	numWorkers := runtime.GOMAXPROCS(0)
	eventsPerWorker := targetEvents / numWorkers

	b.ResetTimer()
	totalEvents := int64(0)
	startTime := time.Now()

	// Create a channel to coordinate workers
	done := make(chan struct{})
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerID := uuid.New().String()
			localEvents := int64(0)

			// Create a WaitGroup for parallel save operations within this worker
			saveWg := sync.WaitGroup{}

			for j := 0; j < eventsPerWorker; j++ {
				select {
				case <-done:
					saveWg.Wait() // Wait for any pending saves
					return
				default:
					saveWg.Add(1)

					// FLOOD THE DATABASE - No semaphore, unlimited concurrent saves!
					go func(iteration int) {
						defer saveWg.Done()

						// Generate generic criteria based on worker ID and iteration
						criteriaIndex := iteration % criteriaCount
						criteria := generateGenericCriteria(workerID, criteriaIndex, tagsPerCriteria)

						// Generate generic event data
						event := &eventstore.EventToSave{
							EventId:   uuid.New().String(),
							EventType: fmt.Sprintf("Event_Type_%d", criteriaIndex),
							Data: fmt.Sprintf(`{"worker_id": "%s", "iteration": %d, "criteria_index": %d, "timestamp": "%s", "value": %d}`,
								workerID, iteration, criteriaIndex, time.Now().Format(time.RFC3339), iteration%1000),
							Metadata: fmt.Sprintf(`{"source": "benchmark", "worker_id": "%s", "criteria_index": %d}`, workerID, criteriaIndex),
						}

						position := eventstore.NotExistsPosition()
						_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
							Boundary: boundary,
							Stream: &eventstore.SaveStreamQuery{
								Name:             streamName,
								ExpectedPosition: &position, // Allow concurrent appends
								SubsetQuery: &eventstore.Query{
									Criteria: criteria,
								},
							},
							Events: []*eventstore.EventToSave{event},
						})
						if err != nil {
							b.Logf("Save error: %v", err)
						} else {
							atomic.AddInt64(&localEvents, 1)
						}
					}(j)
				}
			}

			// Wait for all saves in this worker to complete
			saveWg.Wait()
			atomic.AddInt64(&totalEvents, localEvents)
		}()
	}

	// Wait for all workers to complete
	wg.Wait()
	close(done)
	elapsed := time.Since(startTime)

	// Report metrics
	eventsPerSec := float64(totalEvents) / elapsed.Seconds()
	b.ReportMetric(eventsPerSec, "events/sec")
	b.ReportMetric(float64(totalEvents), "total_events")
	b.ReportMetric(elapsed.Seconds(), "duration_sec")

	// Log actual vs expected event count
	b.Logf("🔥 DATABASE FLOOD TEST: Processed %d events (target: %d) in %.2f seconds at %.1f events/sec",
		totalEvents, targetEvents, elapsed.Seconds(), eventsPerSec)
}

// BenchmarkSaveEvents_TrulySimultaneous10K launches all 10,000 save operations simultaneously
// for maximum contention testing, unlike the worker-based approach above.
func BenchmarkSaveEvents_TrulySimultaneous10K(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "benchmark_test"
	streamName := "high_contention_stream"
	targetEvents := 10000
	criteriaCount := 5
	tagsPerCriteria := 3

	b.ResetTimer()
	startTime := time.Now()

	// Create all goroutines first, then start them simultaneously
	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	totalEvents := int64(0)

	// Pre-create all 10,000 goroutines
	for i := 0; i < targetEvents; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()

			// Wait for start signal - this ensures true simultaneity
			<-startSignal

			criteriaIndex := iteration % criteriaCount
			criteria := generateGenericCriteria("simultaneous", criteriaIndex, tagsPerCriteria)

			event := &eventstore.EventToSave{
				EventId:   uuid.New().String(),
				EventType: fmt.Sprintf("Event_Type_%d", criteriaIndex),
				Data: fmt.Sprintf(`{"iteration": %d, "criteria_index": %d, "timestamp": "%s", "value": %d}`,
					iteration, criteriaIndex, time.Now().Format(time.RFC3339), iteration%1000),
				Metadata: fmt.Sprintf(`{"source": "benchmark", "criteria_index": %d}`, criteriaIndex),
			}

			position := eventstore.NotExistsPosition()
			_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
				Boundary: boundary,
				Stream: &eventstore.SaveStreamQuery{
					Name:             streamName,
					ExpectedPosition: &position, // Allow concurrent appends
					SubsetQuery: &eventstore.Query{
						Criteria: criteria,
					},
				},
				Events: []*eventstore.EventToSave{event},
			})
			if err != nil {
				b.Logf("Save error: %v", err)
			} else {
				atomic.AddInt64(&totalEvents, 1)
			}
		}(i)
	}

	// Small delay to ensure all goroutines are waiting
	time.Sleep(100 * time.Millisecond)

	// START ALL 10,000 GOROUTINES SIMULTANEOUSLY!
	close(startSignal)

	// Wait for all to complete
	wg.Wait()
	elapsed := time.Since(startTime)

	// Report metrics
	eventsPerSec := float64(totalEvents) / elapsed.Seconds()
	b.ReportMetric(eventsPerSec, "events/sec")
	b.ReportMetric(float64(totalEvents), "total_events")
	b.ReportMetric(elapsed.Seconds(), "duration_sec")

	b.Logf("🚀 TRUE SIMULTANEOUS TEST: %d events in %.2f seconds at %.1f events/sec",
		totalEvents, elapsed.Seconds(), eventsPerSec)
}

// generateGenericCriteria creates generic criteria based on worker ID and index
func generateGenericCriteria(workerID string, criteriaIndex, tagsCount int) []*eventstore.Criterion {
	tags := make([]*eventstore.Tag, tagsCount)

	// Always include worker_id as first tag
	tags[0] = &eventstore.Tag{Key: "worker_id", Value: workerID}

	// Generate additional tags based on criteria index
	for i := 1; i < tagsCount; i++ {
		key := fmt.Sprintf("tag_%d", i)
		value := fmt.Sprintf("value_%d_%d", criteriaIndex, i)
		tags[i] = &eventstore.Tag{Key: key, Value: value}
	}

	return []*eventstore.Criterion{
		{
			Tags: tags,
		},
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
		position := eventstore.NotExistsPosition()
		_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: "benchmark_test",
			Stream: &eventstore.SaveStreamQuery{
				Name:             streamIds[i],
				ExpectedPosition: &position,
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

	batchSizes := []int{10, 100, 1000, 5000} // Reduced max batch size to prevent memory issues

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			// Pre-generate a single batch template to avoid massive memory usage
			batchTemplate := make([]*eventstore.EventToSave, batchSize)
			for j := 0; j < batchSize; j++ {
				batchTemplate[j] = generateRandomEvent("BatchEvent")
			}

			b.ResetTimer()
			totalEvents := 0
			for i := 0; i < b.N; i++ {
				// Generate fresh stream ID and events for each iteration
				streamId := uuid.New().String()
				batch := make([]*eventstore.EventToSave, batchSize)
				for j := 0; j < batchSize; j++ {
					// Create new event with unique ID for each iteration
					batch[j] = &eventstore.EventToSave{
						EventId:   uuid.New().String(),
						EventType: batchTemplate[j].EventType,
						Data:      batchTemplate[j].Data,
						Metadata:  batchTemplate[j].Metadata,
					}
				}

				position := eventstore.NotExistsPosition()
				_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
					Boundary: "benchmark_test",
					Stream: &eventstore.SaveStreamQuery{
						Name:             streamId,
						ExpectedPosition: &position,
					},
					Events: batch,
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
			expectedPositions := make([]*eventstore.Position, b.N)

			for i := 0; i < b.N; i++ {
				batches[i] = make([]*eventstore.EventToSave, batchSize)
				for j := 0; j < batchSize; j++ {
					batches[i][j] = generateRandomEvent("SingleStreamEvent")
				}
				if i == 0 {
					p := eventstore.NotExistsPosition()
					expectedPositions[i] = &p
				} else {
					expectedPositions[i] = &eventstore.Position{
						CommitPosition:  expectedPositions[i-1].CommitPosition + int64(batchSize),
						PreparePosition: expectedPositions[i-1].PreparePosition + int64(batchSize),
					}
				}
			}

			b.ResetTimer()
			totalEvents := 0
			for i := 0; i < b.N; i++ {
				_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
					Boundary: "benchmark_test",
					Stream: &eventstore.SaveStreamQuery{
						Name:             streamId,
						ExpectedPosition: expectedPositions[i],
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
			b.ResetTimer()
			totalEvents := int64(0)

			b.RunParallel(func(pb *testing.PB) {
				// Each goroutine gets its own unique stream
				userStreamId := uuid.New().String()
				currentPosition := eventstore.NotExistsPosition()
				localEvents := 0

				for pb.Next() {
					event := generateRandomEvent("ConcurrentUserEvent")

					result, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
						Boundary: "benchmark_test",
						Stream: &eventstore.SaveStreamQuery{
							Name:             userStreamId,
							ExpectedPosition: &currentPosition,
						},
						Events: []*eventstore.EventToSave{event},
					})
					if err != nil {
						b.Errorf("Failed to save event to user stream: %v", err)
					}
					currentPosition = *result.LogPosition
					localEvents++
				}

				// Add local events to total atomically
				atomic.AddInt64(&totalEvents, int64(localEvents))
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
	p := eventstore.NotExistsPosition()
	_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
		Boundary: boundary,
		Stream: &eventstore.SaveStreamQuery{
			Name:             streamId,
			ExpectedPosition: &p,
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

	// Pre-populate with events using efficient batch approach
	streamName := "benchmark_stream"
	boundary := "stream_boundary"

	// Generate events in batches for efficiency
	batchSize := 100
	totalEvents := 1000
	currentPosition := eventstore.NotExistsPosition()

	for i := 0; i < totalEvents; i += batchSize {
		remainingEvents := totalEvents - i
		if remainingEvents > batchSize {
			remainingEvents = batchSize
		}

		// Generate batch of events
		events := make([]*eventstore.EventToSave, remainingEvents)
		for j := 0; j < remainingEvents; j++ {
			events[j] = generateRandomEvent("StreamTestEvent")
		}

		result, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name:             streamName,
				ExpectedPosition: &currentPosition,
			},
			Events: events,
		})
		if err != nil {
			b.Fatalf("Failed to pre-populate stream events: %v", err)
		}
		currentPosition = *result.LogPosition
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := setup.client.GetEvents(setup.authContext(), &eventstore.GetEventsRequest{
				Boundary: boundary,
				Stream: &eventstore.GetStreamQuery{
					Name:         streamName,
					FromPosition: &eventstore.Position{CommitPosition: 0, PreparePosition: 0},
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

	// Pre-populate with events to ensure subscription has data to read
	for i := 0; i < 100; i++ {
		event := generateRandomEvent("PrePopulateEvent")
		streamId := uuid.New().String()
		p := eventstore.NotExistsPosition()
		_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name:             streamId,
				ExpectedPosition: &p,
			},
			Events: []*eventstore.EventToSave{event},
		})
		if err != nil {
			b.Fatalf("Failed to pre-populate events: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := setup.client.CatchUpSubscribeToEvents(setup.authContext(), &eventstore.CatchUpSubscribeToEventStoreRequest{
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

// BenchmarkConcurrentOperations tests mixed concurrent operations
func BenchmarkConcurrentOperations(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	boundary := "concurrent_boundary"

	// Pre-populate with events outside of timing
	for i := 0; i < 100; i++ {
		event := generateRandomEvent("ConcurrentTestEvent")
		streamId := uuid.New().String()
		p := eventstore.NotExistsPosition()
		_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name:             streamId,
				ExpectedPosition: &p,
			},
			Events: []*eventstore.EventToSave{event},
		})
		if err != nil {
			b.Fatalf("Failed to pre-populate events: %v", err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Test concurrent operations without goroutines inside the measurement
			// This measures the time for a single mixed operation set

			// Save operation
			event := generateRandomEvent("ConcurrentSaveEvent")
			streamId := uuid.New().String()
			p := eventstore.NotExistsPosition()
			_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
				Boundary: boundary,
				Stream: &eventstore.SaveStreamQuery{
					Name:             streamId,
					ExpectedPosition: &p,
				},
				Events: []*eventstore.EventToSave{event},
			})
			if err != nil {
				b.Errorf("Failed to save event: %v", err)
			}

			// Get operation
			_, err = setup.client.GetEvents(setup.authContext(), &eventstore.GetEventsRequest{
				Boundary: boundary,
				Count:    10,
			})
			if err != nil {
				b.Errorf("Failed to get events: %v", err)
			}
		}
	})
}

// BenchmarkHighThroughput tests high-throughput scenarios
func BenchmarkHighThroughput(b *testing.B) {
	setup := setupBenchmark(b)
	defer setup.cleanup(b)

	// Test different concurrency levels
	concurrencyLevels := []int{10, 50, 100}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			boundary := fmt.Sprintf("throughput_boundary_%d", concurrency)

			// Set GOMAXPROCS to match concurrency for this test
			runtime.GOMAXPROCS(concurrency)

			b.ResetTimer()
			totalEvents := int64(0)

			b.RunParallel(func(pb *testing.PB) {
				localEvents := int64(0)
				localErrors := int64(0)
				for pb.Next() {
					event := generateRandomEvent("ThroughputEvent")
					streamId := uuid.New().String()
					p := eventstore.NotExistsPosition()
					_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
						Boundary: boundary,
						Stream: &eventstore.SaveStreamQuery{
							Name:             streamId,
							ExpectedPosition: &p,
						},
						Events: []*eventstore.EventToSave{event},
					})
					if err == nil {
						localEvents++
					} else {
						localErrors++
						// Don't use b.Errorf here as it would affect timing
						// Errors are tracked and reported separately
					}
				}
				// Add local events to total atomically
				atomic.AddInt64(&totalEvents, localEvents)
				// Track errors for reporting but don't fail the benchmark
				if localErrors > 0 {
					b.Logf("Worker had %d errors out of %d attempts", localErrors, localEvents+localErrors)
				}
			})

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
		p := eventstore.NotExistsPosition()
		_, err := setup.client.SaveEvents(setup.authContext(), &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name:             streamIds[i],
				ExpectedPosition: &p,
			},
			Events: []*eventstore.EventToSave{events[i]},
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
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(20)
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
	if err := postgres.RunDbScripts(db, "public", false, ctx); err != nil {
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
	const sameStreamName = "benchmark_concurrent_stream"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		startSignal := make(chan struct{})
		var successfulSaves int64
		var totalAttempts int64

		// Pre-create ALL goroutines first, but don't let them execute yet
		for eventIndex := 0; eventIndex < totalEvents; eventIndex++ {
			wg.Add(1)

			// Create a single event with different criteria for each goroutine
			event := eventstore.EventWithMapTags{
				EventId:   uuid.New().String(),
				EventType: "ConcurrentBenchmarkEvent",
				Data:      fmt.Sprintf(`{"event_id": %d, "criteria": "type_%d", "timestamp": "%s"}`, eventIndex, eventIndex%100, time.Now().Format(time.RFC3339)),
				Metadata:  fmt.Sprintf(`{"benchmark": "concurrent_same_stream", "event_index": %d}`, eventIndex),
			}

			// Create unique consistency condition for each event
			uniqueConsistencyCondition := &eventstore.Query{
				Criteria: []*eventstore.Criterion{
					{
						Tags: []*eventstore.Tag{
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

			go func(eventID int, event eventstore.EventWithMapTags, consistencyCondition *eventstore.Query) {
				defer wg.Done()

				// WAIT for the start signal - this ensures TRUE SIMULTANEITY!
				<-startSignal

				// ALL goroutines try to save to the SAME stream with expected version -1
				// This will test optimistic concurrency - only one should succeed, others should fail
				p := eventstore.NotExistsPosition()
				_, _, err := saveEvents.Save(
					ctx,
					[]eventstore.EventWithMapTags{event},
					"benchmark_test",
					sameStreamName,
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
	if err := postgres.RunDbScripts(db, "public", false, ctx); err != nil {
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
	streamName := "benchmark_batch_stream"
	events := make([]eventstore.EventWithMapTags, totalEvents)

	for i := 0; i < totalEvents; i++ {
		events[i] = eventstore.EventWithMapTags{
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
		p := eventstore.NotExistsPosition()
		_, _, err := saveEvents.Save(ctx, events, "benchmark_test", streamName, &p, nil)
		if err != nil {
			b.Fatalf("Failed to save batch events: %v", err)
		}

		duration := time.Since(start)
		eventsPerSecond := float64(totalEvents) / duration.Seconds()

		b.Logf("Batch save: %d events in %v (%.1f events/sec)",
			totalEvents, duration, eventsPerSecond)

		// Clean up for next iteration (if any)
		if i < b.N-1 {
			streamName = fmt.Sprintf("benchmark_batch_stream_%d", i+1) // Use new stream for next iteration
		}
	}
}

// Helper function to check if error is optimistic concurrency conflict
func isOptimisticConcurrencyError(err error) bool {
	return err != nil && (
	// Check for common optimistic concurrency error patterns
	fmt.Sprintf("%v", err) == "OptimisticConcurrencyException:StreamVersionConflict" ||
		// Add other patterns as needed
		false)
}
