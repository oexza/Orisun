package orisun

import (
	"context"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/oexza/Orisun/logging"
)

// OrisunServer provides a high-level interface to interact with the Orisun event store
type OrisunServer struct {
	eventStore   *EventStore
	saveEvents   EventsSaver
	getEvents    EventsRetriever
	lockProvider LockProvider
	logger       logging.Logger
	js           jetstream.JetStream
}

// NewOrisunServer creates a new Orisun client with the provided configuration
func NewOrisunServer(
	ctx context.Context,
	saveEvents EventsSaver,
	getEvents EventsRetriever,
	lockProvider LockProvider,
	js jetstream.JetStream,
	boundaryNames []string,
	logger logging.Logger,
) (*OrisunServer, error) {

	if lockProvider == nil {
		var err error = nil
		lockProvider, err = NewJetStreamLockProvider(ctx, js, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create lock provider: %w", err)
		}
	}

	// Initialize event store
	eventStore := NewEventStoreServer(
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
func (c *OrisunServer) SaveEvents(ctx context.Context, events []EventWithMapTags, boundary string,
	expectedPosition *Position, streamSubSet *Query) (*Position, error) {

	// Save events
	transactionID, globalID, err := c.saveEvents.Save(
		ctx,
		events,
		boundary,
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

	return &Position{
		CommitPosition:  transID,
		PreparePosition: globalID,
	}, nil
}

// GetEvents retrieves events from the event store based on the request
func (c *OrisunServer) GetEvents(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error) {
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
	afterPosition *Position, query *Query,
	handler *MessageHandler[Event],
) error {

	// Subscribe to events
	return c.eventStore.SubscribeToAllEvents(
		ctx,
		boundary,
		subscriberName,
		afterPosition,
		query,
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
