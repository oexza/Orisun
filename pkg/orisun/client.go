package orisun

import (
	"context"
	"fmt"
	"orisun/common"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go/jetstream"

	"orisun/eventstore"
	"orisun/logging"
)

// OrisunServer provides a high-level interface to interact with the Orisun event store
type OrisunServer struct {
	eventStore   *eventstore.EventStore
	saveEvents   eventstore.EventstoreSaveEvents
	getEvents    eventstore.EventstoreGetEvents
	lockProvider eventstore.LockProvider
	logger       logging.Logger
	js           jetstream.JetStream
}

// NewOrisunServer creates a new Orisun client with the provided configuration
func NewOrisunServer(
	ctx context.Context,
	saveEvents eventstore.EventstoreSaveEvents,
	getEvents eventstore.EventstoreGetEvents,
	lockProvider eventstore.LockProvider,
	js jetstream.JetStream,
	boundaryNames []string,
	logger logging.Logger,
) (*OrisunServer, error) {

	if lockProvider == nil {
		var err error = nil
		lockProvider, err = eventstore.NewJetStreamLockProvider(ctx, js, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create lock provider: %w", err)
		}
	}

	// Initialize event store
	eventStore := eventstore.NewEventStoreServer(
		ctx,
		js,
		saveEvents,
		getEvents,
		lockProvider,
		&boundaryNames,
		logger,
	)

	return &OrisunServer{
		eventStore:   eventStore,
		saveEvents:   saveEvents,
		getEvents:    getEvents,
		lockProvider: lockProvider,
		logger:       logger,
		js:           js,
	}, nil
}

// SaveEvents saves a batch of events to the event store
func (c *OrisunServer) SaveEvents(ctx context.Context, events []eventstore.EventWithMapTags, boundary string,
	streamName string, expectedPosition *eventstore.Position, streamSubSet *eventstore.Query) (*eventstore.Position, error) {

	// Save events
	transactionID, globalID, err := c.saveEvents.Save(
		ctx,
		events,
		boundary,
		streamName,
		expectedPosition,
		streamSubSet,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to save events: %w", err)
	}

	transID, err := parseInt64(transactionID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction id: %w", err)
	}

	return &eventstore.Position{
		CommitPosition:  transID,
		PreparePosition: globalID,
	}, nil
}

// GetEvents retrieves events from the event store based on the request
func (c *OrisunServer) GetEvents(ctx context.Context, req *eventstore.GetEventsRequest) (*eventstore.GetEventsResponse, error) {
	// Get events
	internalResp, err := c.getEvents.Get(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	return internalResp, nil
}

// SubscribeToEvents subscribes to events from a boundary with the given handler
func (c *OrisunServer) SubscribeToEvents(
	ctx context.Context,
	boundary string,
	subscriberName string,
	afterPosition *eventstore.Position, query *eventstore.Query,
	streamName *string,
	handler *common.MessageHandler[eventstore.Event],
) error {

	// Subscribe to events
	if streamName == nil {
		return c.eventStore.SubscribeToAllEvents(
			ctx,
			boundary,
			subscriberName,
			afterPosition,
			query,
			handler,
		)
	}
	return c.eventStore.SubscribeToStream(
		ctx,
		boundary,
		subscriberName,
		query,
		*streamName,
		afterPosition,
		handler,
	)
}

// Helper function to parse int64 from string
func parseInt64(s string) (int64, error) {
	var i int64
	_, err := fmt.Sscanf(s, "%d", &i)
	if err != nil {
		return 0, fmt.Errorf("failed to parse int64: %w", err)
	}
	return i, nil
}
