package main

import (
	"context"
	c "orisun/config"
	evStore "orisun/eventstore"
	"orisun/logging"
	"orisun/nats"
	"orisun/pkg/orisun"
	pg "orisun/postgres"
)

func main() {
	// Create a context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load default configuration
	config := c.InitializeConfig()

	logger := logging.InitializeDefaultLogger(config.Logging)

	// you can bring your own nats
	jetStream, conn, server := nats.InitializeNATS(ctx, config.Nats, logger)
	defer conn.Close()
	defer server.Shutdown()

	// Initialize database connection
	config.Postgres.User = "postgres"
	config.Postgres.Password = "password@1"
	saveEvents, getEvents, lockProvider, _, eventPublishing := pg.InitializePostgresDatabase(ctx, config.Postgres, config.Admin, jetStream, logger)

	// Initialize EventStore
	_ = evStore.InitializeEventStore(
		ctx,
		config,
		saveEvents,
		getEvents,
		lockProvider,
		jetStream,
		logger,
	)

	// Start polling events from the event store and publish them to NATS jetstream
	evStore.StartEventPolling(ctx, config, lockProvider, getEvents, jetStream, eventPublishing, logger)

	_, err := orisun.NewClient(
		ctx,
		saveEvents,
		getEvents,
		lockProvider, jetStream,
		config.GetBoundaryNames(),
		logger,
	)
	if err != nil {
		return
	}
}
