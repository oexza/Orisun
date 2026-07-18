package main

import (
	"context"
	"fmt"
	logger "log"
	"runtime"

	"github.com/common-nighthawk/go-figure"
	"github.com/nats-io/nats.go/jetstream"
	c "github.com/OrisunLabs/Orisun/config"
	l "github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/OrisunLabs/Orisun/server"
	sqlitebackend "github.com/OrisunLabs/Orisun/sqlite"
)

func main() {
	defer logger.Println("SQLite server shutting down")
	fmt.Printf("Starting Orisun SQLite %s (Go %s)\n", orisun.GetVersion(), runtime.Version())

	config := c.InitializeConfig()
	if config.Backend.Type != "" && config.Backend.Type != "sqlite" {
		logger.Fatalf("orisun-sqlite only supports ORISUN_BACKEND=sqlite, got %q", config.Backend.Type)
	}
	config.Backend.Type = "sqlite"
	if config.Nats.Cluster.Enabled {
		logger.Fatal("orisun-sqlite does not support ORISUN_NATS_CLUSTER_ENABLED=true")
	}
	if config.Sqlite.Dir == "" {
		logger.Fatal("orisun-sqlite requires ORISUN_SQLITE_DIR")
	}

	appLogger := l.InitializeDefaultLogger(config.Logging)
	myFigure := figure.NewColorFigure("Orisun SQLite", "isometric1", "cyan", true)
	appLogger.Infof("\n%s", myFigure.String())

	server.Run(context.Background(), config, appLogger, initializeBackend)
}

func initializeBackend(ctx context.Context, config c.AppConfig, js jetstream.JetStream, logger l.Logger) (server.Backend, error) {
	saveEvents, getEvents, lockProvider, adminDB, eventPublishing, signalProvider, err := sqlitebackend.InitializeSqliteDatabase(
		ctx,
		config.Sqlite,
		config.Admin,
		config.GetBoundaryNames(),
		js,
		logger,
	)
	if err != nil {
		return server.Backend{}, err
	}
	return server.Backend{
		SaveEvents:      saveEvents,
		GetEvents:       getEvents,
		LockProvider:    lockProvider,
		AdminDB:         adminDB,
		EventPublishing: eventPublishing,
		SignalProvider:  signalProvider,
	}, nil
}
