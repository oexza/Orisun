package main

import (
	"context"
	"fmt"
	logger "log"
	"runtime"
	"time"

	"github.com/common-nighthawk/go-figure"
	"github.com/nats-io/nats.go/jetstream"
	c "github.com/OrisunLabs/Orisun/config"
	fdbbackend "github.com/OrisunLabs/Orisun/foundationdb"
	l "github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	pg "github.com/OrisunLabs/Orisun/postgres"
	"github.com/OrisunLabs/Orisun/server"
	sqlitebackend "github.com/OrisunLabs/Orisun/sqlite"
	"google.golang.org/grpc/credentials"
)

func main() {
	defer logger.Println("Server shutting down")
	fmt.Printf("Starting Orisun %s (Go %s)\n", orisun.GetVersion(), runtime.Version())

	config := c.InitializeConfig()
	appLogger := l.InitializeDefaultLogger(config.Logging)
	myFigure := figure.NewColorFigure("Orisun", "isometric1", "cyan", true)
	appLogger.Infof("\n%s", myFigure.String())

	server.Run(context.Background(), config, appLogger, initializeBackend)
}

func initializeBackend(ctx context.Context, config c.AppConfig, js jetstream.JetStream, logger l.Logger) (server.Backend, error) {
	switch config.BackendType() {
	case "postgres":
		saveEvents, getEvents, lockProvider, adminDB, eventPublishing, pgListener := pg.InitializePostgresDatabase(ctx, config.Postgres, config.Admin, js, logger)
		backend := server.Backend{
			SaveEvents:      saveEvents,
			GetEvents:       getEvents,
			LockProvider:    lockProvider,
			AdminDB:         adminDB,
			EventPublishing: eventPublishing,
		}
		if pgListener != nil {
			var stopListener context.CancelFunc
			backend.Start = func(parent context.Context) {
				listenerCtx, cancel := context.WithCancel(parent)
				stopListener = cancel
				go pgListener.Start(listenerCtx)
			}
			backend.Close = func(ctx context.Context) {
				if stopListener != nil {
					stopListener()
				}
				waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
				defer waitCancel()
				pgListener.Close(waitCtx)
			}
			backend.SignalProvider = func(boundary string) orisun.EventSignal {
				return pgListener.Signal(boundary, 30*time.Second)
			}
		}
		return backend, nil
	case "sqlite":
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
	case "foundationdb":
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
	default:
		return server.Backend{}, fmt.Errorf("unsupported backend: %s", config.BackendType())
	}
}

func loadTLSCredentials(config c.AppConfig, logger l.Logger) (credentials.TransportCredentials, error) {
	return server.LoadTLSCredentials(config, logger)
}
