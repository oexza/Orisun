package main

import (
	"context"
	"fmt"
	logger "log"
	"runtime"

	"github.com/common-nighthawk/go-figure"
	"github.com/nats-io/nats.go/jetstream"
	c "github.com/OrisunLabs/Orisun/config"
	fdbbackend "github.com/OrisunLabs/Orisun/foundationdb"
	l "github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/OrisunLabs/Orisun/server"
)

func main() {
	defer logger.Println("FoundationDB server shutting down")
	fmt.Printf("Starting Orisun FoundationDB %s (Go %s)\n", orisun.GetVersion(), runtime.Version())

	config := c.InitializeConfig()
	if config.Backend.Type != "" && config.Backend.Type != "foundationdb" {
		logger.Fatalf("orisun-fdb only supports ORISUN_BACKEND=foundationdb, got %q", config.Backend.Type)
	}
	config.Backend.Type = "foundationdb"

	appLogger := l.InitializeDefaultLogger(config.Logging)
	myFigure := figure.NewColorFigure("Orisun FDB", "isometric1", "cyan", true)
	appLogger.Infof("\n%s", myFigure.String())

	server.Run(context.Background(), config, appLogger, initializeBackend)
}

func initializeBackend(ctx context.Context, config c.AppConfig, js jetstream.JetStream, logger l.Logger) (server.Backend, error) {
	saveEvents, getEvents, lockProvider, adminDB, eventPublishing, signalProvider, closeFn, err := fdbbackend.InitializeFoundationDB(
		ctx,
		config.FoundationDB,
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
		Close:           closeFn,
	}, nil
}
