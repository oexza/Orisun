//go:build foundationdb

package foundationdb

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// fdbImage matches defaultAPIVersion (730 -> 7.3.x). Keep the two in lock-step:
// the client API version must be <= the server version.
const fdbImage = "foundationdb/foundationdb:7.3.27"

// startFDBCluster boots a single-node FoundationDB via testcontainers and returns
// the path to a cluster file the client can open.
//
// If ORISUN_FDB_TEST_CLUSTER_FILE is set, that existing cluster is used verbatim
// and no container is started — useful for pointing tests at a real cluster.
//
// Networking caveat: FoundationDB embeds the coordinator's host:port inside the
// cluster file, and the client must reach that exact address. The only portable
// way to make the in-container address reachable from the host is Docker host
// networking, which this helper uses (FDB_NETWORKING_MODE=host + NetworkMode
// "host"). Host networking works on Linux CI. On macOS/Windows Docker Desktop the
// container runs inside a VM and host networking does not bridge to the host, so
// these tests are skipped there — set ORISUN_FDB_TEST_CLUSTER_FILE to run against
// an externally provisioned cluster instead.
func startFDBCluster(tb testing.TB) string {
	tb.Helper()

	if existing := os.Getenv("ORISUN_FDB_TEST_CLUSTER_FILE"); existing != "" {
		return existing
	}

	if hostNetworkingUnsupported() {
		tb.Skip("FoundationDB testcontainer requires Docker host networking (Linux). " +
			"Set ORISUN_FDB_TEST_CLUSTER_FILE to run against an external cluster.")
	}

	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image: fdbImage,
		Env: map[string]string{
			"FDB_NETWORKING_MODE": "host",
		},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.NetworkMode = "host"
		},
		WaitingFor: wait.ForExec([]string{"pgrep", "-x", "fdbserver"}).
			WithExitCode(0).
			WithStartupTimeout(60 * time.Second),
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		tb.Fatalf("start FoundationDB container: %v", err)
	}
	tb.Cleanup(func() {
		if err := ctr.Terminate(context.Background()); err != nil {
			tb.Logf("terminate FoundationDB container: %v", err)
		}
	})

	configureFDB(tb, ctr)

	clusterFile := filepath.Join(tb.TempDir(), "fdb.cluster")
	copyClusterFile(tb, ctr, "/var/fdb/fdb.cluster", clusterFile)
	return clusterFile
}

// configureFDB initialises the database (a fresh fdbserver has no database until
// "configure new" runs) and waits until it reports healthy.
func configureFDB(tb testing.TB, ctr testcontainers.Container) {
	tb.Helper()

	deadline := time.Now().Add(60 * time.Second)
	for {
		code, out := execFDBCLI(tb, ctr, "configure new single memory")
		text := readAll(out)
		if code == 0 || strings.Contains(text, "Database already exists") {
			break
		}
		if time.Now().After(deadline) {
			tb.Fatalf("configure FoundationDB timed out (exit %d): %s", code, text)
		}
		time.Sleep(time.Second)
	}

	deadline = time.Now().Add(60 * time.Second)
	for {
		code, out := execFDBCLI(tb, ctr, "status minimal")
		text := readAll(out)
		if code == 0 && strings.Contains(text, "available") {
			return
		}
		if time.Now().After(deadline) {
			tb.Fatalf("FoundationDB did not become available: %s", text)
		}
		time.Sleep(time.Second)
	}
}

func execFDBCLI(tb testing.TB, ctr testcontainers.Container, command string) (int, io.Reader) {
	tb.Helper()
	code, reader, err := ctr.Exec(context.Background(),
		[]string{"fdbcli", "--exec", command, "--timeout", "10"})
	if err != nil {
		tb.Fatalf("exec fdbcli %q: %v", command, err)
	}
	return code, reader
}

func copyClusterFile(tb testing.TB, ctr testcontainers.Container, containerPath, destPath string) {
	tb.Helper()
	rc, err := ctr.CopyFileFromContainer(context.Background(), containerPath)
	if err != nil {
		tb.Fatalf("copy %s from container: %v", containerPath, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		tb.Fatalf("read cluster file: %v", err)
	}
	if err := os.WriteFile(destPath, data, 0o600); err != nil {
		tb.Fatalf("write cluster file: %v", err)
	}
}

func readAll(r io.Reader) string {
	if r == nil {
		return ""
	}
	data, _ := io.ReadAll(r)
	return string(data)
}

// hostNetworkingUnsupported reports whether Docker host networking is unavailable
// for reaching containers from the host (Docker Desktop on macOS/Windows).
func hostNetworkingUnsupported() bool {
	if os.Getenv("ORISUN_FDB_TEST_FORCE_HOST_NET") == "1" {
		return false
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return false
	}
}
