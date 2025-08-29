package main

import (
	"context"
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
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "orisun/eventstore"
)

type ClusterTestSuite struct {
	ctx               context.Context
	postgresContainer *postgres.PostgresContainer
	nodes             []*ClusterNode
	postgresHost      string
	postgresPort      string
}

type ClusterNode struct {
	binaryPath  string
	binaryCmd   *exec.Cmd
	grpcPort    string
	natsPort    string
	clusterPort string
	adminPort   string
	client      pb.EventStoreClient
	conn        *grpc.ClientConn
}

func setupClusterTest(t *testing.T) *ClusterTestSuite {
	ctx := context.Background()
	suite := &ClusterTestSuite{
		ctx: ctx,
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

	// Create 3 cluster nodes (minimum for JetStream quorum)
	suite.nodes = make([]*ClusterNode, 3)
	for i := 0; i < 3; i++ {
		suite.nodes[i] = &ClusterNode{
			grpcPort:    fmt.Sprintf("%d", 15010+i),
			natsPort:    fmt.Sprintf("%d", 14222+i),
			clusterPort: fmt.Sprintf("%d", 16222+i),
			adminPort:   fmt.Sprintf("%d", 18991+i),
		}
		suite.buildBinary(t, i)
	}

	// Start nodes sequentially to allow proper cluster formation
	// RAFT consensus works better when nodes join one at a time
	for i := 0; i < 3; i++ {
		suite.startBinary(t, i)
		// Allow time for each node to join the cluster before starting the next
		if i < 2 {
			time.Sleep(3 * time.Second)
		}
	}

	// Allow extra time for JetStream cluster formation and meta leader election
	// JetStream requires RAFT consensus which can take time to establish
	t.Logf("Waiting for JetStream cluster formation and meta leader election...")
	time.Sleep(20 * time.Second)

	// Wait for all gRPC servers to be ready
	for i := 0; i < 3; i++ {
		suite.waitForGRPCServer(t, i)
		suite.createGRPCClient(t, i)
	}

	return suite
}

func (s *ClusterTestSuite) buildBinary(t *testing.T, nodeIndex int) {
	// Determine the target OS and architecture
	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH

	// Create build directory if it doesn't exist
	buildDir := "./build"
	err := os.MkdirAll(buildDir, 0755)
	require.NoError(t, err)

	// Set binary name for each node
	binaryName := fmt.Sprintf("orisun-cluster-node-%d-%s-%s", nodeIndex, targetOS, targetArch)
	s.nodes[nodeIndex].binaryPath = filepath.Join(buildDir, binaryName)

	// Build the binary
	cmd := exec.Command("go", "build", "-o", s.nodes[nodeIndex].binaryPath, ".")
	cmd.Env = os.Environ()
	err = cmd.Run()
	require.NoError(t, err)
}

func (s *ClusterTestSuite) startBinary(t *testing.T, nodeIndex int) {
	node := s.nodes[nodeIndex]
	
	// Create NATS cluster routes for this node
	// Include all cluster ports (NATS will handle connections as nodes become available)
	routes := ""
	for i := 0; i < 3; i++ {
		if i != nodeIndex {
			if routes != "" {
				routes += ","
			}
			// Use the actual cluster port from the node configuration
			otherClusterPort := s.nodes[i].clusterPort
			routes += fmt.Sprintf("nats://localhost:%s", otherClusterPort)
		}
	}

	// Set environment variables
	env := []string{
		fmt.Sprintf("ORISUN_PG_HOST=%s", s.postgresHost),
		fmt.Sprintf("ORISUN_PG_PORT=%s", s.postgresPort),
		"ORISUN_PG_USER=postgres",
		"ORISUN_PG_PASSWORD=postgres",
		"ORISUN_PG_NAME=orisun",
		"ORISUN_PG_SCHEMAS=orisun_test_1:public,orisun_test_2:test2,orisun_test_3:test3,orisun_admin:admin",
		fmt.Sprintf("ORISUN_GRPC_PORT=%s", node.grpcPort),
		fmt.Sprintf("ORISUN_ADMIN_PORT=%s", node.adminPort),
		"ORISUN_GRPC_ENABLE_REFLECTION=true",
		fmt.Sprintf("ORISUN_NATS_PORT=%s", node.natsPort),
		fmt.Sprintf("ORISUN_NATS_CLUSTER_PORT=%s", node.clusterPort),
		"ORISUN_NATS_CLUSTER_ENABLED=true",
		"ORISUN_NATS_CLUSTER_NAME=orisun-nats-cluster", // All nodes must use same cluster name
		fmt.Sprintf("ORISUN_NATS_CLUSTER_ROUTES=%s", routes),
		fmt.Sprintf("ORISUN_NATS_SERVER_NAME=orisun-nats-%d", nodeIndex),
		fmt.Sprintf("ORISUN_NATS_STORE_DIR=./data/orisun/nats/node-%d", nodeIndex), // Each node needs unique store directory
		"ORISUN_LOGGING_LEVEL=DEBUG",
		"ORISUN_ADMIN_USERNAME=admin",
		"ORISUN_ADMIN_PASSWORD=changeit",
		"ORISUN_ADMIN_BOUNDARY=orisun_admin",
		"ORISUN_BOUNDARIES=[{\"name\":\"orisun_test_1\",\"description\":\"boundary1\"},{\"name\":\"orisun_test_2\",\"description\":\"boundary2\"},{\"name\":\"orisun_test_3\",\"description\":\"boundary3\"},{\"name\":\"orisun_admin\",\"description\":\"admin\"}]",
	}

	// Start the binary with environment variables
	node.binaryCmd = exec.Command(node.binaryPath)
	node.binaryCmd.Env = append(os.Environ(), env...)

	// Capture output for debugging
	// Create log files for each node to separate their output
	stdoutFile, err := os.Create(fmt.Sprintf("node-%d-stdout.log", nodeIndex))
	require.NoError(t, err)
	stderrFile, err2 := os.Create(fmt.Sprintf("node-%d-stderr.log", nodeIndex))
	require.NoError(t, err2)
	node.binaryCmd.Stdout = stdoutFile
	node.binaryCmd.Stderr = stderrFile

	err = node.binaryCmd.Start()
	require.NoError(t, err)
	t.Logf("Started cluster node %d on gRPC port %s, NATS port %s, cluster port %s", nodeIndex, node.grpcPort, node.natsPort, node.clusterPort)

	// Give the binary more time to start up and establish cluster connections
	// JetStream clustering requires time for RAFT consensus
	time.Sleep(5 * time.Second)
}

func (s *ClusterTestSuite) waitForGRPCServer(t *testing.T, nodeIndex int) {
	node := s.nodes[nodeIndex]
	address := fmt.Sprintf("127.0.0.1:%s", node.grpcPort)
	
	// Wait for the gRPC server to be ready
	for i := 0; i < 30; i++ {
		conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn.Close()
			t.Logf("Node %d gRPC server is ready on %s", nodeIndex, address)
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("Node %d gRPC server failed to start on %s", nodeIndex, address)
}

func (s *ClusterTestSuite) createGRPCClient(t *testing.T, nodeIndex int) {
	node := s.nodes[nodeIndex]
	address := fmt.Sprintf("127.0.0.1:%s", node.grpcPort)
	
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	node.conn = conn
	node.client = pb.NewEventStoreClient(conn)
}



func (s *ClusterTestSuite) teardown(t *testing.T) {
	// Stop all nodes
	for i, node := range s.nodes {
		if node.conn != nil {
			node.conn.Close()
		}
		if node.binaryCmd != nil && node.binaryCmd.Process != nil {
			if err := node.binaryCmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Logf("Failed to send SIGTERM to node %d: %v", i, err)
				node.binaryCmd.Process.Kill()
			}
			node.binaryCmd.Wait()
		}
		if node.binaryPath != "" {
			os.Remove(node.binaryPath)
		}
	}

	// Stop PostgreSQL container
	if s.postgresContainer != nil {
		if err := s.postgresContainer.Terminate(s.ctx); err != nil {
			t.Logf("Failed to terminate postgres container: %v", err)
		}
	}
}

func TestE2E_ClusteredDeployment(t *testing.T) {
	suite := setupClusterTest(t)
	defer suite.teardown(t)

	t.Run("BasicClusterFunctionality", func(t *testing.T) {
		testBasicClusterFunctionality(t, suite)
	})

	t.Run("EventConsistencyAcrossNodes", func(t *testing.T) {
		testEventConsistencyAcrossNodes(t, suite)
	})
}

func testBasicClusterFunctionality(t *testing.T, suite *ClusterTestSuite) {
	// Create authenticated context with admin credentials
	ctx := createAuthenticatedContext("admin", "changeit")

	// Check if server processes are still running
	for i, node := range suite.nodes {
		if node.binaryCmd.Process != nil {
			if node.binaryCmd.ProcessState != nil && node.binaryCmd.ProcessState.Exited() {
				t.Fatalf("Node %d process has exited: %v", i, node.binaryCmd.ProcessState)
			}
			t.Logf("Node %d process is running with PID %d", i, node.binaryCmd.Process.Pid)
		} else {
			t.Fatalf("Node %d process is nil", i)
		}
	}

	// Test that both nodes are operational
	for i, node := range suite.nodes {
		// Save an event to each node
		event := &pb.EventToSave{
			EventId:   uuid.New().String(),
			EventType: "ClusterTest",
			Data:      fmt.Sprintf(`{"message": "event from node %d", "nodeId": %d}`, i, i),
			Metadata:  `{"source": "cluster_test"}`,
		}

		req := &pb.SaveEventsRequest{
			Boundary: fmt.Sprintf("orisun_test_%d", i+1),
			Stream:   &pb.SaveStreamQuery{Name: fmt.Sprintf("cluster-test-stream-%d", i), ExpectedVersion: -1},
			Events:   []*pb.EventToSave{event},
		}

		resp, err := node.client.SaveEvents(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		t.Logf("Node %d successfully saved event: %v", i, resp)
	}

	// Verify each node can read its own events
	for i, node := range suite.nodes {
		getReq := &pb.GetEventsRequest{
			Boundary: fmt.Sprintf("orisun_test_%d", i+1),
			Stream:   &pb.GetStreamQuery{Name: fmt.Sprintf("cluster-test-stream-%d", i)},
			Count:    10,
		}

		getResp, err := node.client.GetEvents(ctx, getReq)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(getResp.Events), 1, "Node %d should see at least 1 event", i)
		t.Logf("Node %d retrieved %d events from its own stream", i, len(getResp.Events))
	}

	t.Logf("Basic cluster functionality test completed successfully")
}

func testEventConsistencyAcrossNodes(t *testing.T, suite *ClusterTestSuite) {
	// Create authenticated context with admin credentials
	ctx := createAuthenticatedContext("admin", "changeit")

	// Save events to the first node
	sharedStreamName := "shared-cluster-stream"
	eventsToSave := []*pb.EventToSave{
		{
			EventId:   uuid.New().String(),
			EventType: "SharedEvent",
			Data:      `{"message": "shared event 1", "timestamp": "2024-01-01T00:00:00Z"}`,
			Metadata:  `{"source": "consistency_test"}`,
		},
		{
			EventId:   uuid.New().String(),
			EventType: "SharedEvent",
			Data:      `{"message": "shared event 2", "timestamp": "2024-01-01T00:01:00Z"}`,
			Metadata:  `{"source": "consistency_test"}`,
		},
	}

	// Save all events in a single request to avoid version conflicts
	// Use node 0's boundary since only node 0 can poll orisun_test_1
	req := &pb.SaveEventsRequest{
		Boundary: "orisun_test_1",
		Stream:   &pb.SaveStreamQuery{Name: sharedStreamName, ExpectedVersion: -1}, // Any version
		Events:   eventsToSave,
	}

	resp, err := suite.nodes[0].client.SaveEvents(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	t.Logf("Saved %d shared events to node 0: %v", len(eventsToSave), resp)

	// Wait for potential replication/consistency
	time.Sleep(2 * time.Second)

	// Verify all nodes can read the shared events from node 0's boundary
	// Since each node only polls its own boundary, we test that all nodes can still
	// read from any boundary via gRPC API calls
	for nodeIndex, node := range suite.nodes {
		getReq := &pb.GetEventsRequest{
			Boundary: "orisun_test_1", // All nodes read from node 0's boundary
			Stream:   &pb.GetStreamQuery{Name: sharedStreamName},
			Count:    10,
		}

		getResp, err := node.client.GetEvents(ctx, getReq)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(getResp.Events), 2, "Node %d should see at least 2 shared events", nodeIndex)
		t.Logf("Node %d retrieved %d shared events", nodeIndex, len(getResp.Events))
	}

	t.Logf("Event consistency test completed successfully")
}