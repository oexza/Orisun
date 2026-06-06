---
title: Go Embedding
description: Run Orisun directly inside a Go service.
---

Go services can embed Orisun directly instead of running the gRPC server as a separate process.

Backend-specific embedding packages keep deployments explicit:

- `embedded/postgres` imports the PostgreSQL backend.
- `embedded/sqlite` imports the SQLite backend.
- `embedded/foundationdb` imports the FoundationDB backend when built with `-tags foundationdb`.
- Neither package needs the unused backend.

Use embedding when Orisun should be part of your service process. Use the standalone server when you want a separate operational boundary and language-agnostic gRPC access.

## PostgreSQL Embedding

```go
import (
	"context"

	embeddedpg "github.com/oexza/Orisun/embedded/postgres"
	"github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
)

func start(ctx context.Context) (*embeddedpg.Store, error) {
	cfg := config.InitializeConfig()
	cfg.Backend.Type = "postgres"
	logger := logging.InitializeDefaultLogger(cfg.Logging)
	return embeddedpg.Start(ctx, cfg, logger)
}
```

By default, embedded stores start embedded NATS JetStream in the same process.
Use the store's NATS handles when your host process wants direct access without connecting to a NATS URL:

```go
nc := store.NATSConnection()
js := store.JetStream()
```

To use an existing JetStream-enabled NATS server instead:

```go
store, err := embeddedpg.Start(
	ctx,
	cfg,
	logger,
	embeddedpg.WithNATSURL("nats://localhost:4222"),
)
```

If your service already owns a NATS connection or JetStream handle, pass it directly:

```go
store, err := embeddedpg.Start(ctx, cfg, logger, embeddedpg.WithNATSConnection(conn))
store, err = embeddedpg.Start(ctx, cfg, logger, embeddedpg.WithJetStream(js))
```

## SQLite Embedding

```go
import (
	"context"

	embeddedsqlite "github.com/oexza/Orisun/embedded/sqlite"
	"github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
)

func start(ctx context.Context) (*embeddedsqlite.Store, error) {
	cfg := config.InitializeConfig()
	cfg.Backend.Type = "sqlite"
	cfg.Nats.Cluster.Enabled = false
	logger := logging.InitializeDefaultLogger(cfg.Logging)
	return embeddedsqlite.Start(ctx, cfg, logger)
}
```

SQLite embedding supports the same NATS options:

```go
store, err := embeddedsqlite.Start(
	ctx,
	cfg,
	logger,
	embeddedsqlite.WithNATSURL("nats://localhost:4222"),
)
```

SQLite remains single-node only. Keep `cfg.Nats.Cluster.Enabled = false`.

## FoundationDB Embedding

FoundationDB embedding requires the native FoundationDB client libraries and a build with the `foundationdb` tag.

```go
import (
	"context"

	embeddedfdb "github.com/oexza/Orisun/embedded/foundationdb"
	"github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
)

func start(ctx context.Context) (*embeddedfdb.Store, error) {
	cfg := config.InitializeConfig()
	cfg.Backend.Type = "foundationdb"
	cfg.FoundationDB.ClusterFile = "/etc/foundationdb/fdb.cluster"
	logger := logging.InitializeDefaultLogger(cfg.Logging)
	return embeddedfdb.Start(ctx, cfg, logger)
}
```

Use the same NATS options as the other embedded stores. Build the host service with:

```bash
go build -tags foundationdb ./...
```

## Embedded Index Management

Embedded stores expose boundary index management directly. Applications do not need to expose Admin to create JSON expression indexes.

```go
err := store.CreateBoundaryIndex(
	ctx,
	"orders",
	"customer_id",
	[]orisun.BoundaryIndexField{
		{JsonKey: "customer_id", ValueType: "text"},
	},
	nil,
	orisun.IndexCombinatorAND,
)
```

## Capabilities

Embedded stores expose the same high-level behavior as the server:

- save events
- query events
- subscribe to events
- create and drop boundary indexes
- preserve backend-specific publishing guarantees

## Shutdown

Call the store's close method during service shutdown so database pools, NATS resources, and background loops can stop cleanly. Orisun closes NATS resources it creates, but it does not close caller-owned connections or JetStream handles passed with `WithNATSConnection` or `WithJetStream`.
