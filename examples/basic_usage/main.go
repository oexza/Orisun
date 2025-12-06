package main

import (
	"context"
	"github.com/google/uuid"
	"time"

	globalCommon "github.com/oexza/Orisun/common"
	c "github.com/oexza/Orisun/config"
	evStore "github.com/oexza/Orisun/eventstore"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/nats"
	"github.com/oexza/Orisun/pkg/orisun"
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

	// generate example for getting, saving and subscribing to eventstore

	// Example 1: Save events to the event store
	logger.Info("=== Saving Events Example ===")

	// Create sample events
	events := []evStore.EventWithMapTags{
		{
			EventId:   generateUUID(),
			EventType: "UserCreated",
			Data: map[string]interface{}{
				"userId":   "user-123",
				"username": "john_doe",
				"email":    "john@example.com",
			},
			Metadata: map[string]interface{}{
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
	streamName := "user_" + generateUUID()
	position := evStore.NotExistsPosition()
	// Save events
	newPosition, err := orisunServer.SaveEvents(
		ctx,
		events,
		boundary,
		streamName,
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
	getEventsReq := &evStore.GetEventsRequest{
		Count:     10,
		Direction: evStore.Direction_ASC,
		Boundary:  boundary,
		Stream: &evStore.GetStreamQuery{
			Name: streamName,
		},
	}

	eventsResponse, err := orisunServer.GetEvents(ctx, getEventsReq)
	if err != nil {
		logger.Fatalf("Failed to get events: %v", err)
	}

	logger.Infof("Retrieved %d events from stream %s:", len(eventsResponse.Events), streamName)
	for i, event := range eventsResponse.Events {
		logger.Infof("Event %d: ID=%s, Type=%s, Stream=%s, Position=Commit=%d,Prepare=%d",
			i+1, event.EventId, event.EventType, event.StreamId,
			event.Position.CommitPosition, event.Position.PreparePosition)
	}

	// Example 3: Subscribe to events
	logger.Info("=== Subscribing to Events Example ===")

	// Create a message handler to process received events
	messageHandler := globalCommon.NewMessageHandler[evStore.Event](ctx)

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

				logger.Infof("Received event: ID=%s, Type=%s, Stream=%s, Data=%s",
					event.EventId, event.EventType, event.StreamId, event.Data)
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
			nil, // Subscribe to all streams, not a specific one
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

	// Example 4: Subscribe to a specific stream
	logger.Info("=== Subscribing to Specific Stream Example ===")

	// Create a new message handler for the stream subscription
	streamMessageHandler := globalCommon.NewMessageHandler[evStore.Event](ctx)

	// Start a goroutine to process received events from the specific stream
	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("Stopping stream event processor")
				return
			default:
				event, err := streamMessageHandler.Recv()
				if err != nil {
					if ctx.Err() != nil {
						// Context was cancelled
						return
					}
					logger.Errorf("Error receiving stream event: %v", err)
					continue
				}

				logger.Infof("Received stream event: ID=%s, Type=%s, Stream=%s, Data=%s",
					event.EventId, event.EventType, event.StreamId, event.Data)
			}
		}
	}()

	// Subscribe to events from a specific stream
	streamSubscriberName := "example_stream_subscriber"
	targetStream := streamName

	fromPosition := evStore.FirstPosition()
	// Start stream subscription in a separate goroutine
	go func() {
		logger.Infof("Starting subscription to stream: %s...", targetStream)
		err := orisunServer.SubscribeToEvents(
			ctx,
			boundary,
			streamSubscriberName,
			&fromPosition, // Start from the beginning
			nil,           // No query filter
			&targetStream, // Subscribe to a specific stream
			streamMessageHandler,
		)
		if err != nil {
			logger.Errorf("Stream subscription failed: %v", err)
		}
	}()

	// Save more events to trigger the subscriptions
	logger.Info("Saving more events to trigger subscriptions...")

	moreEvents := []evStore.EventWithMapTags{
		{
			EventId:   generateUUID(),
			EventType: "UserPasswordChanged",
			Data: map[string]interface{}{
				"userId":    "user-123",
				"timestamp": time.Now().Unix(),
			},
			Metadata: map[string]interface{}{
				"source":  "auth-service",
				"version": "1.0",
			},
		},
		{
			EventId:   generateUUID(),
			EventType: "UserDeleted",
			Data: map[string]interface{}{
				"userId": "user-123",
				"reason": "account closure",
			},
			Metadata: map[string]interface{}{
				"source":  "user-service",
				"version": "1.0",
			},
		},
	}

	// Save the additional events
	_, err = orisunServer.SaveEvents(
		ctx,
		moreEvents,
		boundary,
		streamName,
		newPosition, // Use the previous position as expected position
		nil,         // No subset query
	)
	if err != nil {
		logger.Fatalf("Failed to save additional events: %v", err)
	}

	logger.Info("Additional events saved successfully")

	// Wait a bit more to receive the new events through subscriptions
	logger.Info("Waiting to receive new events through subscriptions...")
	select {
	case <-time.After(5 * time.Second):
		logger.Info("Example completed after additional 5 seconds")
	case <-ctx.Done():
		logger.Info("Example cancelled")
	}
}

func generateUUID() string {
	id, _ := uuid.NewRandom()

	return id.String()
}
