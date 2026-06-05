package sqlite

import (
	"context"
	"testing"
	"time"

	c "github.com/oexza/Orisun/config"
	natsruntime "github.com/oexza/Orisun/nats"
)

type testLogger struct{}

func (testLogger) Debug(...any)          {}
func (testLogger) Debugf(string, ...any) {}
func (testLogger) Info(...any)           {}
func (testLogger) Infof(string, ...any)  {}
func (testLogger) Warn(...any)           {}
func (testLogger) Warnf(string, ...any)  {}
func (testLogger) Error(...any)          {}
func (testLogger) Errorf(string, ...any) {}
func (testLogger) Fatal(...any)          {}
func (testLogger) Fatalf(string, ...any) {}

func TestStartWithNATSConnectionDoesNotCloseCallerConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := c.InitializeConfig()
	cfg.Backend.Type = "sqlite"
	cfg.Sqlite.Dir = t.TempDir()
	cfg.Nats.Port = -1
	cfg.Nats.StoreDir = t.TempDir()
	cfg.Nats.Cluster.Enabled = false
	cfg.Nats.Cluster.Timeout = 5 * time.Second

	natsRuntime, err := natsruntime.Start(ctx, cfg.Nats, testLogger{})
	if err != nil {
		t.Fatalf("failed to start NATS runtime: %v", err)
	}
	defer natsRuntime.Close()

	store, err := Start(ctx, cfg, testLogger{}, WithNATSConnection(natsRuntime.Conn))
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	store.Close()

	if natsRuntime.Conn.IsClosed() {
		t.Fatal("caller-owned NATS connection was closed by embedded store")
	}
}

func TestStartExposesEmbeddedNATSHandles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := c.InitializeConfig()
	cfg.Backend.Type = "sqlite"
	cfg.Sqlite.Dir = t.TempDir()
	cfg.Nats.Port = -1
	cfg.Nats.StoreDir = t.TempDir()
	cfg.Nats.Cluster.Enabled = false
	cfg.Nats.Cluster.Timeout = 5 * time.Second

	store, err := Start(ctx, cfg, testLogger{})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	defer store.Close()

	if store.NATSConnection() == nil {
		t.Fatal("expected embedded NATS connection")
	}
	if store.NATSConnection().IsClosed() {
		t.Fatal("embedded NATS connection is closed")
	}
	if store.JetStream() == nil {
		t.Fatal("expected embedded JetStream context")
	}
	if _, err := store.JetStream().Publish(ctx, "test_jetstream.test", []byte("direct")); err != nil {
		t.Fatalf("failed to publish through embedded JetStream handle: %v", err)
	}
}
