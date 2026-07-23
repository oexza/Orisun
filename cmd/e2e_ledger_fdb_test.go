//go:build foundationdb

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	pb "github.com/OrisunLabs/Orisun/orisun/grpcapi"
)

// TestE2E_LedgerWorkload_FoundationDB runs the ledger workload through gRPC
// against a server built with the foundationdb tag. It needs an existing
// cluster reachable via ORISUN_FDB_TEST_CLUSTER_FILE (and the native client
// libraries to build the binary) — scripts/fdb_test_container.sh provides
// both:
//
//	TEST_PKGS=./cmd/ scripts/fdb_test_container.sh -run TestE2E_LedgerWorkload_FoundationDB -v
func TestE2E_LedgerWorkload_FoundationDB(t *testing.T) {
	suite := setupFoundationDBE2E(t)
	defer suite.teardown(t)

	runLedgerWorkload(t, suite)
}

func TestE2E_LedgerWorkload_FoundationDBSoak(t *testing.T) {
	if os.Getenv("ORISUN_FDB_SOAK") != "1" {
		t.Skip("set ORISUN_FDB_SOAK=1 to run the extended FoundationDB ledger soak")
	}

	suite := setupFoundationDBE2E(t)
	defer suite.teardown(t)

	runLedgerWorkloadWithConfig(t, suite, ledgerWorkloadConfig{
		accounts:           16,
		initialBalance:     5000,
		workers:            12,
		transfersPerWorker: 60,
		maxAttempts:        1000,
	})
}

func setupFoundationDBE2E(t *testing.T) *E2ETestSuite {
	t.Helper()
	clusterFile := os.Getenv("ORISUN_FDB_TEST_CLUSTER_FILE")
	if clusterFile == "" {
		t.Skip("set ORISUN_FDB_TEST_CLUSTER_FILE to run the FoundationDB ledger e2e test")
	}

	tempDir := t.TempDir()
	suite := &E2ETestSuite{
		ctx:            context.Background(),
		grpcPort:       "15009",
		adminPort:      "18995",
		natsPort:       "14228",
		natsStoreDir:   filepath.Join(tempDir, "nats"),
		backend:        "foundationdb",
		buildTags:      "foundationdb",
		fdbClusterFile: clusterFile,
		// Unique root per run keeps repeated test runs isolated in one cluster.
		fdbRoot: "orisun_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
	}
	suite.buildBinary(t)
	suite.startBinary(t)
	suite.waitForGRPCServer(t)
	suite.createGRPCClient(t)

	// FoundationDB no longer receives a static startup boundary list. Exercise
	// the supported control-plane path and wait for durable activation before
	// the shared ledger workload creates its index.
	ctx := createAuthenticatedContext("admin", "changeit")
	adminClient := pb.NewAdminClient(suite.grpcConn)
	created, err := adminClient.CreateBoundary(ctx, &pb.CreateBoundaryRequest{
		Name:        "orisun_test_1",
		Description: "FoundationDB ledger E2E boundary",
		Placement: &pb.BoundaryPlacementInput{
			Backend:   "foundationdb",
			Namespace: suite.fdbRoot,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, created.Boundary)
	require.Equal(
		t,
		pb.BoundaryLifecycleStatus_BOUNDARY_LIFECYCLE_STATUS_PROVISIONING,
		created.Boundary.Status,
	)
	require.Eventually(t, func() bool {
		response, getErr := adminClient.GetBoundary(ctx, &pb.GetBoundaryRequest{Name: "orisun_test_1"})
		return getErr == nil &&
			response != nil &&
			response.Boundary != nil &&
			response.Boundary.Status == pb.BoundaryLifecycleStatus_BOUNDARY_LIFECYCLE_STATUS_ACTIVE
	}, 10*time.Second, 25*time.Millisecond)
	return suite
}
