package main

import (
	"context"
	"github.com/google/uuid"

	"time"

	c "github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/nats"
	"github.com/oexza/Orisun/orisun"
	pg "github.com/oexza/Orisun/postgres"
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
	_ = orisun.InitializeEventStore(
		ctx,
		config,
		saveEvents,
		getEvents,
		lockProvider,
		jetStream,
		logger,
	)

	// Start polling events from the event store and publish them to NATS jetstream
	orisun.StartEventPolling(ctx, config, lockProvider, getEvents, jetStream, eventPublishing, logger)

	// Create Orisun server instance for our examples
	orisunServer, err := orisun.NewOrisunServer(
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

	// Example 1: Save events to the event store
	logger.Info("=== Saving Events Example ===")

	// Create sample events
	events := []orisun.EventWithMapTags{
		{
			EventId:   generateUUID(),
			EventType: "UserCreated",
			Data: map[string]any{
				"userId":   "user-123",
				"username": "john_doe",
				"email":    "john@example.com",
			},
			Metadata: map[string]any{
				"source":  "user-service",
				"version": "1.0",
			},
		},
		{
			EventId:   generateUUID(),
			EventType: "UserUpdated",
			Data: map[string]interface{}{
				"userId":   "user-123",
				"username": "john_doe_updated",
				"email":    "john_updated@example.com",
			},
			Metadata: map[string]interface{}{
				"source":  "user-service",
				"version": "1.0",
			},
		},
	}

	// Save events to the event store
	boundary := config.GetBoundaryNames()[0]
	position := orisun.NotExistsPosition()
	// Save events
	newPosition, err := orisunServer.SaveEvents(
		ctx,
		events,
		boundary,
		&position, // No expected position for new stream
		nil,       // No subset query
	)
	if err != nil {
		logger.Fatalf("Failed to save events: %v", err)
	}

	logger.Infof("Successfully saved events at position: Commit=%d, Prepare=%d",
		newPosition.CommitPosition, newPosition.PreparePosition)

	// Example 2: Get events from the event store
	logger.Info("=== Getting Events Example ===")

	// Get events from the stream
	getEventsReq := &orisun.GetEventsRequest{
		Count:     10,
		Direction: orisun.Direction_ASC,
		Boundary:  boundary,
	}

	eventsResponse, err := orisunServer.GetEvents(ctx, getEventsReq)
	if err != nil {
		logger.Fatalf("Failed to get events: %v", err)
	}

	logger.Infof("Retrieved %d events:", len(eventsResponse.Events))
	for i, event := range eventsResponse.Events {
		logger.Infof("Event %d: ID=%s, Type=%s, Position=Commit=%d,Prepare=%d",
			i+1, event.EventId, event.EventType,
			event.Position.CommitPosition, event.Position.PreparePosition)
	}

	// Example 3: Subscribe to events
	logger.Info("=== Subscribing to Events Example ===")

	// Create a message handler to process received events
	messageHandler := orisun.NewMessageHandler[orisun.Event](ctx)

	// Start a goroutine to process received events
	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("Stopping event processor")
				return
			default:
				event, err := messageHandler.Recv()

				if err != nil {
					if ctx.Err() != nil {
						// Context was cancelled
						return
					}
					logger.Errorf("Error receiving event: %v", err)
					continue
				}

				logger.Infof("Received event: ID=%s, Type=%s, Data=%s",
					event.EventId, event.EventType, event.Data)
			}
		}
	}()

	// Subscribe to all events in the boundary
	subscriberName := "example-subscriber"

	// Start subscription in a separate goroutine to not block the main function
	go func() {
		logger.Info("Starting subscription to all events...")
		err := orisunServer.SubscribeToEvents(
			ctx,
			boundary,
			subscriberName,
			nil, // Start from the beginning
			nil, // No query filter
			messageHandler,
		)
		if err != nil {
			logger.Errorf("Subscription failed: %v", err)
		}
	}()

	// Wait a bit to receive some events
	logger.Info("Waiting to receive events...")
	select {
	case <-time.After(5 * time.Second):
		logger.Info("Example completed after 5 seconds")
	case <-ctx.Done():
		logger.Info("Example cancelled")
	}
}

func generateUUID() string {
	id, _ := uuid.NewRandom()

	return id.String()
}
