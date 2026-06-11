//go:build foundationdb

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestE2E_LedgerWorkload_FoundationDB runs the ledger workload through gRPC
// against a server built with the foundationdb tag. It needs an existing
// cluster reachable via ORISUN_FDB_TEST_CLUSTER_FILE (and the native client
// libraries to build the binary) — scripts/fdb_test_container.sh provides
// both:
//
//	TEST_PKGS=./cmd/ scripts/fdb_test_container.sh -run TestE2E_LedgerWorkload_FoundationDB -v
func TestE2E_LedgerWorkload_FoundationDB(t *testing.T) {
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
	defer suite.teardown(t)

	runLedgerWorkload(t, suite)
}
