package orisun

import (
	"context"
	"fmt"
	c "github.com/oexza/Orisun/config"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
	"github.com/google/uuid"

	"runtime/debug"
	"time"

	"github.com/oexza/Orisun/logging"

	"github.com/nats-io/nats.go/jetstream"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// EventsSaver accepts only canonical batches prepared at an API boundary.
// Storage backends must not decode the original flexible input representation.
type EventsSaver interface {
	SavePrepared(ctx context.Context,
		events PreparedEventBatch,
		boundary string,
		expectedPosition *Position,
		subSet *Query,
	) (transactionID string, globalID int64, err error)
}

type EventsRetriever interface {
	Get(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error)
	// GetLatestByCriteria returns the latest event per criterion from ONE
	// backend read snapshot, plus the max observed position as the
	// optimistic-lock token for the combined context.
	GetLatestByCriteria(ctx context.Context, req *GetLatestByCriteriaRequest) (*GetLatestByCriteriaResponse, error)
}

type LockProvider interface {
	Lock(ctx context.Context, lockName string) error
}

// LockLease is an optional stronger lock contract for providers that can prove
// ongoing ownership. The polling publisher uses it to stop before publish or
// checkpoint work if a lease has expired or been taken by another process.
type LockLease interface {
	Context() context.Context
	Check(ctx context.Context) error
	Release()
}

type LockLeaseProvider interface {
	AcquireLock(ctx context.Context, lockName string) (LockLease, error)
}

type contextLockLease struct {
	ctx context.Context
}

func (l contextLockLease) Context() context.Context { return l.ctx }
func (l contextLockLease) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.ctx.Err()
}
func (l contextLockLease) Release() {}

func acquireLockLease(ctx context.Context, provider LockProvider, lockName string) (LockLease, error) {
	if leaseProvider, ok := provider.(LockLeaseProvider); ok {
		return leaseProvider.AcquireLock(ctx, lockName)
	}
	if err := provider.Lock(ctx, lockName); err != nil {
		return nil, err
	}
	return contextLockLease{ctx: ctx}, nil
}

//type EventstoreDependencies interface {
//	Save(ctx context.Context,
//		events []EventWithMapTags,
//		boundary string,
//		streamName string,
//		expectedPosition *Position,
//		streamSubSet *Query,
//	) (transactionID string, globalID int64, err error)
//
//	Get(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error)
//
//	Lock(ctx context.Context, lockName string) error
//}

type EventStore struct {
	UnimplementedEventStoreServer
	js           jetstream.JetStream
	saveEventsFn EventsSaver
	getEventsFn  EventsRetriever
	lockProvider LockProvider
	indexManager BoundaryIndexManager
	logger       logging.Logger
}

func NotExistsPosition() Position {
	return Position{
		CommitPosition:  -1,
		PreparePosition: -1,
	}
}

func FirstPosition() Position {
	return Position{
		CommitPosition:  0,
		PreparePosition: 0,
	}
}

const (
	eventsStreamPrefix = "ORISUN_EVENTS"
	EventsSubjectName  = "events"
)

func GetEventsNatsJetstreamStreamStreamName(boundary string) string {
	return eventsStreamPrefix + "___" + boundary
}

func GetEventsSubjectName(boundary string) string {
	return GetEventsNatsJetstreamStreamStreamName(boundary) + "." + EventsSubjectName + ".>"
}

func GetEventsStreamSubjectFilterForSubscription(boundary string, stream *string) string {
	subject := GetEventsNatsJetstreamStreamStreamName(boundary) + "." + EventsSubjectName + "."
	if stream != nil {
		return subject + *stream + ".>"
	}

	return subject + ">"
}

func GetEventJetstreamSubjectName(boundary string, position *Position) string {
	return GetEventsNatsJetstreamStreamStreamName(boundary) + "." + EventsSubjectName + "." + GetEventNatsMessageId(int64(position.PreparePosition), int64(position.CommitPosition))
}

type EventStreamConfig struct {
	MaxBytes int64
	MaxMsgs  int64
	MaxAge   time.Duration
}

func NewEventStoreServer(
	ctx context.Context,
	js jetstream.JetStream,
	saveEventsFn EventsSaver,
	getEventsFn EventsRetriever,
	lockProvider LockProvider,
	indexManager BoundaryIndexManager,
	boundaries *[]string,
	streamCfg EventStreamConfig,
	logger logging.Logger,
) *EventStore {
	if streamCfg.MaxAge <= 0 {
		streamCfg.MaxAge = 5 * time.Minute
	}
	if streamCfg.MaxMsgs == 0 {
		streamCfg.MaxMsgs = -1
	}
	if streamCfg.MaxBytes == 0 {
		streamCfg.MaxBytes = -1
	}

	for _, boundary := range *boundaries {
		streamName := GetEventsNatsJetstreamStreamStreamName(boundary)
		info, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name: streamName,
			Subjects: []string{
				GetEventsSubjectName(boundary),
			},
			MaxMsgs:  streamCfg.MaxMsgs,
			MaxBytes: streamCfg.MaxBytes,
			Storage:  jetstream.MemoryStorage,
			MaxAge:   streamCfg.MaxAge,
		})

		if err != nil {
			logger.Fatalf("failed to add stream: %v %v", streamName, err)
		}
		logger.Infof("stream info: %v", info)
	}

	return &EventStore{
		js:           js,
		saveEventsFn: saveEventsFn,
		getEventsFn:  getEventsFn,
		lockProvider: lockProvider,
		indexManager: indexManager,
		logger:       logger,
	}
}

type EventWithMapTags struct {
	EventId   string `json:"event_id"`
	EventType string `json:"event_type"`
	Data      any    `json:"data"`
	Metadata  any    `json:"metadata"`
}

// PreparedEvent is the canonical backend-facing event representation. JSON is
// held as immutable text so SQLite and FoundationDB can store it directly and
// PostgreSQL can embed it as raw JSON without another decode. Values are always
// valid JSON; DataJSON is always an object and contains the authoritative
// eventType field.
type PreparedEvent struct {
	EventId      string
	EventType    string
	DataJSON     string
	MetadataJSON string
}

// MarshalJSON preserves the canonical data and metadata as JSON values rather
// than quoting them as strings. PostgreSQL uses this to send the prepared batch
// directly to its insert function without constructing another event slice.
func (e PreparedEvent) MarshalJSON() ([]byte, error) {
	type wireEvent struct {
		EventId   string          `json:"event_id"`
		EventType string          `json:"event_type"`
		Data      json.RawMessage `json:"data"`
		Metadata  json.RawMessage `json:"metadata"`
	}
	return json.Marshal(wireEvent{
		EventId: e.EventId, EventType: e.EventType,
		Data: json.RawMessage(e.DataJSON), Metadata: json.RawMessage(e.MetadataJSON),
	})
}

// PreparedEventBatch keeps event descriptors contiguous and gives ownership of
// the canonical JSON strings to the save operation.
type PreparedEventBatch []PreparedEvent

// PrepareEventsForSave converts the embedding API's flexible input shape into
// the canonical backend representation. Each JSON value is normalized once.
func PrepareEventsForSave(events []EventWithMapTags) (PreparedEventBatch, error) {
	prepared := make(PreparedEventBatch, len(events))
	for i, event := range events {
		dataJSON, err := prepareEventDataJSON(event.Data, event.EventType)
		if err != nil {
			return nil, fmt.Errorf("event %d data: %w", i, err)
		}
		metadataJSON, err := prepareJSONValue(event.Metadata)
		if err != nil {
			return nil, fmt.Errorf("event %d metadata: %w", i, err)
		}
		prepared[i] = PreparedEvent{
			EventId:      event.EventId,
			EventType:    event.EventType,
			DataJSON:     dataJSON,
			MetadataJSON: metadataJSON,
		}
	}
	return prepared, nil
}

func prepareProtoEventsForSave(events []*EventToSave) (PreparedEventBatch, error) {
	prepared := make(PreparedEventBatch, len(events))
	for i, event := range events {
		if event == nil {
			return nil, fmt.Errorf("event %d is nil", i)
		}
		dataJSON, err := prepareEventDataJSON(event.Data, event.EventType)
		if err != nil {
			return nil, fmt.Errorf("event %d data: %w", i, err)
		}
		// The gRPC contract historically accepts metadata objects (or null), not
		// arbitrary JSON scalars. Keep that validation at the transport edge.
		metadataJSON, err := prepareJSONObjectJSON(event.Metadata, "", false)
		if err != nil {
			return nil, fmt.Errorf("event %d metadata: %w", i, err)
		}
		prepared[i] = PreparedEvent{
			EventId:      event.EventId,
			EventType:    event.EventType,
			DataJSON:     dataJSON,
			MetadataJSON: metadataJSON,
		}
	}
	return prepared, nil
}

func prepareEventDataJSON(data any, eventType string) (string, error) {
	return prepareJSONObjectJSON(data, eventType, true)
}

// prepareJSONObjectJSON normalizes an object and optionally sets eventType to
// the supplied value. The generic object exists only at the API boundary and
// is discarded after producing the immutable backend representation.
func prepareJSONObjectJSON(value any, eventType string, setEventType bool) (string, error) {
	var object map[string]any
	switch value := value.(type) {
	case nil:
		object = make(map[string]any, 1)
	case string:
		if value == "" {
			object = make(map[string]any, 1)
		} else if err := json.Unmarshal([]byte(value), &object); err != nil {
			return "", err
		}
	case []byte:
		if len(value) == 0 {
			object = make(map[string]any, 1)
		} else if err := json.Unmarshal(value, &object); err != nil {
			return "", err
		}
	case map[string]any:
		object = make(map[string]any, len(value)+1)
		for key, item := range value {
			object[key] = item
		}
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(encoded, &object); err != nil {
			return "", err
		}
	}
	if object == nil {
		object = make(map[string]any, 1)
	}
	if setEventType {
		object["eventType"] = eventType
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func prepareJSONValue(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	switch value := value.(type) {
	case string:
		if value == "" {
			return "{}", nil
		}
		if !json.Valid([]byte(value)) {
			return "", fmt.Errorf("invalid JSON")
		}
		return value, nil
	case []byte:
		if len(value) == 0 {
			return "{}", nil
		}
		if !json.Valid(value) {
			return "", fmt.Errorf("invalid JSON")
		}
		return string(value), nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
}

func authorizeRequest(ctx context.Context, roles []Role) error {
	// Check if the user has the necessary permissions to perform the query
	user := ctx.Value(UserContextKey)
	if user == nil {
		return nil
	}

	// Check if the user has any of the necessary permissions to perform the query
	userObj := user.(User)

	// If no roles are specified, allow access
	if len(roles) == 0 {
		return nil
	}

	// Check if the user has any of the required roles
	for _, requiredRole := range roles {
		if slices.Contains(userObj.Roles, requiredRole) {
			// User has at least one of the required roles
			return nil
		}
	}

	// User doesn't have any of the required roles
	return status.Errorf(codes.PermissionDenied, "user does not have any of the required roles")
}

func (s *EventStore) Ping(ctx context.Context, req *PingRequest) (resp *PingResponse, err error) {
	if s.logger.IsDebugEnabled() {
		s.logger.Debugf("Ping called")
	}
	return &PingResponse{}, nil
}

func (s *EventStore) CreateIndex(ctx context.Context, req *CreateIndexRequest) (*CreateIndexResponse, error) {
	if err := authorizeRequest(ctx, []Role{RoleAdmin}); err != nil {
		return nil, err
	}
	if s.indexManager == nil {
		return nil, status.Errorf(codes.Unimplemented, "index management is not configured")
	}
	if req.Boundary == "" || req.Name == "" || len(req.Fields) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "boundary, name, and at least one field are required")
	}

	fields := make([]BoundaryIndexField, len(req.Fields))
	for i, f := range req.Fields {
		if f.JsonKey == "" {
			return nil, status.Errorf(codes.InvalidArgument, "each field must have a json_key")
		}
		fields[i] = BoundaryIndexField{
			JsonKey:   f.JsonKey,
			ValueType: strings.ToLower(f.ValueType.String()),
		}
	}

	conditions := make([]BoundaryIndexCondition, len(req.Conditions))
	for i, c := range req.Conditions {
		if c.Key == "" {
			return nil, status.Errorf(codes.InvalidArgument, "each condition must have a key")
		}
		conditions[i] = BoundaryIndexCondition{
			Key:      c.Key,
			Operator: c.Operator,
			Value:    c.Value,
		}
	}

	combinator := IndexCombinatorAND
	if req.ConditionCombinator == ConditionCombinator_OR {
		combinator = IndexCombinatorOR
	}

	if err := s.indexManager.CreateBoundaryIndex(ctx, req.Boundary, req.Name, fields, conditions, combinator); err != nil {
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return nil, err
		}
		return nil, status.Errorf(codes.Internal, "failed to create index: %v", err)
	}
	return &CreateIndexResponse{}, nil
}

func (s *EventStore) DropIndex(ctx context.Context, req *DropIndexRequest) (*DropIndexResponse, error) {
	if err := authorizeRequest(ctx, []Role{RoleAdmin}); err != nil {
		return nil, err
	}
	if s.indexManager == nil {
		return nil, status.Errorf(codes.Unimplemented, "index management is not configured")
	}
	if req.Boundary == "" || req.Name == "" {
		return nil, status.Errorf(codes.InvalidArgument, "boundary and name are required")
	}
	if err := s.indexManager.DropBoundaryIndex(ctx, req.Boundary, req.Name); err != nil {
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return nil, err
		}
		return nil, status.Errorf(codes.Internal, "failed to drop index: %v", err)
	}
	return &DropIndexResponse{}, nil
}

func (s *EventStore) SaveEvents(ctx context.Context, req *SaveEventsRequest) (resp *WriteResult, err error) {
	if s.logger.IsDebugEnabled() {
		s.logger.Debugf("SaveEvents called with req: %v", req)
	}

	err = authorizeRequest(ctx, []Role{RoleAdmin, RoleOperations})
	if err != nil {
		return nil, err
	}
	// Defer a recovery function to catch any panics
	defer func() {
		if r := recover(); r != nil {
			s.logger.Errorf("Panic in SaveEvents: %v\nStack Trace:\n%s", r, debug.Stack())
			err = status.Errorf(codes.Internal, "Internal server error")
		}
	}()

	if err = validateSaveEventsRequest(req); err != nil {
		return nil, err
	}

	var transactionID string
	var globalID int64

	// Extract ExpectedPosition and SubsetQuery from Query if present
	var expectedPosition *Position
	var subsetQuery *Query
	if req.Query != nil {
		expectedPosition = req.Query.ExpectedPosition
		subsetQuery = req.Query.SubsetQuery
	}

	prepared, prepareErr := prepareProtoEventsForSave(req.Events)
	if prepareErr != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid event JSON: %v", prepareErr)
	}
	transactionID, globalID, err = s.saveEventsFn.SavePrepared(
		ctx, prepared, req.Boundary, expectedPosition, subsetQuery,
	)

	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return nil, err
		}
		if strings.Contains(err.Error(), "OptimisticConcurrencyException") {
			return nil, status.Errorf(codes.AlreadyExists, "failed to save events: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to save events: %v", err)
	}

	tranId, err := parseInt64(transactionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save events: %v", err)
	}

	return &WriteResult{
		LogPosition: &Position{
			CommitPosition:  tranId,
			PreparePosition: globalID,
		},
	}, nil
}

func (s *EventStore) GetEvents(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error) {
	if s.logger.IsDebugEnabled() {
		s.logger.Debugf("GetEvents called with req: %v", req)
	}
	if req.Count == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Count cannot be 0")
	}
	// if req.FromPosition != nil && req.Stream != nil {
	// 	return nil, status.Error(codes.InvalidArgument, "fromPosition and stream cannot be set together, you can only set one of both")
	// }
	return s.getEventsFn.Get(ctx, req)
}

func (s *EventStore) GetLatestByCriteria(ctx context.Context, req *GetLatestByCriteriaRequest) (*GetLatestByCriteriaResponse, error) {
	if s.logger.IsDebugEnabled() {
		s.logger.Debugf("GetLatestByCriteria called with req: %v", req)
	}
	if req.Boundary == "" {
		return nil, status.Errorf(codes.InvalidArgument, "boundary is required")
	}
	if len(req.Criteria) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "at least one criterion is required")
	}
	for i, criterion := range req.Criteria {
		if criterion == nil || len(criterion.Tags) == 0 {
			return nil, status.Errorf(codes.InvalidArgument, "criterion %d has no tags", i)
		}
	}
	return s.getEventsFn.GetLatestByCriteria(ctx, req)
}

func (s *EventStore) SubscribeToAllEvents(
	ctx context.Context,
	boundary string,
	subscriberName string,
	afterPosition *Position,
	query *Query,
	handler *MessageHandler[Event],
) error {
	subscriptionName := boundary + "__" + subscriberName

	// Use errgroup for coordinated error handling and cancellation
	g, gCtx := errgroup.WithContext(ctx)
	lease, err := acquireLockLease(gCtx, s.lockProvider, subscriptionName)

	if err != nil {
		return status.Errorf(codes.AlreadyExists, "failed to acquire lock: %v", err)
	}
	defer lease.Release()
	gCtx = lease.Context()

	// Initialize position tracking
	lastProcessedPosition := afterPosition

	// Phase 1: Catch-up by polling the event store until we're up to date
	s.logger.Info("Starting catch-up phase: polling event store")

	const batchSize = 100
	for {
		if err := lease.Check(gCtx); err != nil {
			return err
		}

		// Get events from the event store starting from our last processed position
		var getEventsReq *GetEventsRequest
		if lastProcessedPosition == nil {
			// Start from the end if no position specified
			getEventsReq = &GetEventsRequest{
				Count:     batchSize,
				Direction: Direction_DESC,
				Boundary:  boundary,
				Query:     query,
			}
		} else {
			// Continue from the last processed position
			getEventsReq = &GetEventsRequest{
				Count:     batchSize,
				Direction: Direction_ASC,
				Boundary:  boundary,
				Query:     query,
				FromPosition: &Position{
					PreparePosition: lastProcessedPosition.PreparePosition,
					CommitPosition:  lastProcessedPosition.CommitPosition,
				},
			}
		}

		resp, err := s.getEventsFn.Get(gCtx, getEventsReq)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to get events during catch-up: %v", err)
		}

		// If no more events, we're caught up
		if len(resp.Events) == 0 {
			s.logger.Info("Catch-up phase completed: no more events in event store")
			break
		}

		// Send all events in this batch
		for _, event := range resp.Events {
			if err := lease.Check(gCtx); err != nil {
				return err
			}

			if lastProcessedPosition == nil || isEventPositionNewerThanPosition(event.Position, lastProcessedPosition) {
				if err := handler.Send(event); err != nil {
					return status.Errorf(codes.Internal, "failed to send event during catch-up: %v", err)
				}
			}

			lastProcessedPosition = event.Position
		}

		// If we got fewer events than requested, we're caught up
		if len(resp.Events) < batchSize {
			s.logger.Info("Catch-up phase completed: reached end of event store")
			break
		}
	}

	/***
	Capture the time immediately after polling is completed, and set to 10 seconds before
	to make sure no event is ever missed.
	***/
	pollingCompletedTime := time.Now().Add(-10 * time.Second)

	// Phase 2: Subscribe to NATS for live updates
	s.logger.Info("Starting live phase: subscribing to NATS for real-time updates")

	// Determine the time to start NATS subscription from
	var timeToSubscribeFromJetstream time.Time
	if lastProcessedPosition != nil {
		// Start NATS subscription from the time of the last processed event
		// We need to get the event to find its timestamp
		lastEventReq := &GetEventsRequest{
			Count:     1,
			Direction: Direction_DESC,
			Boundary:  boundary,
			FromPosition: &Position{
				PreparePosition: lastProcessedPosition.PreparePosition,
				CommitPosition:  lastProcessedPosition.CommitPosition,
			},
		}

		lastEventResp, err := s.getEventsFn.Get(gCtx, lastEventReq)
		if err != nil {
			s.logger.Errorf("Failed to get last processed event for timestamp: %v", err)
			return fmt.Errorf("cannot determine NATS subscription start time: failed to retrieve last processed event: %w", err)
		} else if len(lastEventResp.Events) == 0 {
			s.logger.Warn("No events found after last processed position, using polling completion time for NATS subscription")
			timeToSubscribeFromJetstream = pollingCompletedTime
		} else {
			timeToSubscribeFromJetstream = lastEventResp.Events[0].DateCreated.AsTime()
			if s.logger.IsDebugEnabled() {
				s.logger.Debugf("Starting NATS subscription from last processed event time: %v", timeToSubscribeFromJetstream)
			}
		}
	} else {
		// No events processed, start from polling completion time
		timeToSubscribeFromJetstream = pollingCompletedTime
	}

	// Set up NATS subscription for live events
	subs, err := s.js.Stream(gCtx, GetEventsNatsJetstreamStreamStreamName(boundary))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get stream: %v", err)
	}

	natsSubscriptionName := subscriptionName + uuid.New().String()

	consumer, err := subs.CreateOrUpdateConsumer(gCtx, jetstream.ConsumerConfig{
		Name:          natsSubscriptionName,
		DeliverPolicy: jetstream.DeliverByStartTimePolicy,
		AckPolicy:     jetstream.AckNonePolicy,
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
		OptStartTime:  &timeToSubscribeFromJetstream,
	})

	if err != nil {
		return status.Errorf(codes.Internal, "failed to create consumer: %v", err)
	}
	// Ensure the consumer is cleaned up using the same name
	defer subs.DeleteConsumer(context.Background(), natsSubscriptionName)

	// Start consuming messages
	msgs, err := consumer.Messages(jetstream.PullMaxMessages(200))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get message iterator: %v", err)
	}

	// Use errgroup to manage the message processing goroutine
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Errorf("Message processing goroutine panicked: %v\nStack: %s", r, debug.Stack())
			}
		}()

		for {
			if err := lease.Check(gCtx); err != nil {
				s.logger.Info("Message processing stopped due to context cancellation")
				return err
			}
			msg, err := msgs.Next()
			if err != nil {
				if gCtx.Err() != nil {
					s.logger.Info("Context cancelled, stopping message processing")
					return gCtx.Err()
				}
				s.logger.Errorf("Error getting next message: %v", err)
				// Small backoff to avoid tight loop on repeated errors
				return fmt.Errorf("failed to get next message: %w", err)
			}

			var event Event
			if err := json.Unmarshal(msg.Data(), &event); err != nil {
				s.logger.Errorf("Failed to unmarshal event: %v", err)
				return fmt.Errorf("failed to unmarshal event: %w", err)
			}

			// Only process events newer than our last processed position
			isNewer := false
			if lastProcessedPosition != nil {
				isNewer = isEventPositionNewerThanPosition(event.Position, lastProcessedPosition)
			} else {
				isNewer = true // If no position, all events are considered new
			}

			if isNewer && s.eventMatchesQueryCriteria(&event, query) {
				if err := lease.Check(gCtx); err != nil {
					s.logger.Info("Context cancelled, not sending event")
					return err
				}
				if err := handler.Send(&event); err != nil {
					s.logger.Errorf("Failed to send event: %v", err)
					return fmt.Errorf("failed to send event: %w", err)
				}

				lastProcessedPosition = event.Position

				if err := msg.Ack(); err != nil {
					s.logger.Errorf("Failed to acknowledge message: %v", err)
				}
			} else {
				msg.Ack() // Acknowledge messages that don't match criteria or are duplicates
			}
		}
	})

	// Wait for all goroutines to complete or for an error to occur
	return g.Wait()
}

func (s *EventStore) CatchUpSubscribeToEvents(req *CatchUpSubscribeToEventStoreRequest, stream EventStore_CatchUpSubscribeToEventsServer) error {
	s.logger.Infof("CatchUpSubscribeToEvents: %v", req)

	g, gctx := errgroup.WithContext(stream.Context())

	messageHandler := NewMessageHandler[Event](gctx)

	// Forwarder worker: reads from handler and writes to gRPC stream
	g.Go(func() error {
		for {
			select {
			case <-gctx.Done():
				s.logger.Info("Message processing stopped")
				return gctx.Err()
			default:
				event, err := messageHandler.Recv()
				if err != nil {
					if gctx.Err() != nil {
						s.logger.Info("Context cancelled, stopping event processing")
						return gctx.Err()
					}
					s.logger.Errorf("Failed to receive event: %v", err)
					// small backoff to avoid tight error loop
					time.Sleep(100 * time.Millisecond)
					continue
				}

				// Check context before sending to stream
				select {
				case <-gctx.Done():
					s.logger.Info("Context cancelled, not sending event to stream")
					return gctx.Err()
				default:
					if err := stream.Send(event); err != nil {
						s.logger.Errorf("Failed to send event: %v", err)
						// backoff to avoid tight loop on send errors
						time.Sleep(100 * time.Millisecond)
						continue
					}
				}
			}
		}
	})

	// Subscription worker: implements catch-up phase followed by live subscription
	g.Go(func() error {
		return s.SubscribeToAllEvents(
			gctx,
			req.Boundary,
			req.SubscriberName,
			req.AfterPosition,
			req.Query,
			messageHandler,
		)
	})

	return g.Wait()
}

type ComparationResult int

const IsLessThan ComparationResult = -1
const IsEqual ComparationResult = 0
const IsGreaterThan ComparationResult = 1

func ComparePositions(p1, p2 *Position) ComparationResult {
	if p1.CommitPosition == p2.CommitPosition && p1.PreparePosition == p2.PreparePosition {
		return 0
	}

	if (p1.CommitPosition < p2.CommitPosition) ||
		(p1.CommitPosition == p2.CommitPosition && p1.PreparePosition < p2.PreparePosition) {
		return -1
	}

	return 1
}

// isEventPositionNewerThanPosition checks if the new event position is greater than the last processed position
func isEventPositionNewerThanPosition(newPosition, lastPosition *Position) bool {
	compResult := ComparePositions(newPosition, lastPosition)

	return compResult == IsGreaterThan
}

// Add the validation function
func validateSaveEventsRequest(req *SaveEventsRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "Invalid request: missing request body")
	}

	if len(req.Events) == 0 {
		return status.Error(codes.InvalidArgument, "Invalid request: no events provided")
	}

	return nil
}

func (s *EventStore) eventMatchesQueryCriteria(event *Event, criteria *Query) bool {
	if criteria == nil || len(criteria.Criteria) == 0 {
		return true
	}

	unmarshaledData := map[string]any{}
	if err := json.Unmarshal([]byte(event.Data), &unmarshaledData); err != nil {
		return false
	}

	// OR across criteria groups; AND within a group
	for _, criteriaGroup := range criteria.Criteria {
		allTagsMatch := true
		for _, criteriaTag := range criteriaGroup.Tags {
			eventTag, ok := unmarshaledData[criteriaTag.Key]
			if !ok || !eventTagEquals(eventTag, criteriaTag.Value) {
				allTagsMatch = false
				break
			}
		}
		if allTagsMatch {
			return true
		}
	}
	return false
}

// eventTagEquals compares a JSON-decoded value against the criteria's string form.
// Avoids fmt.Sprintf reflection on the hot path for the common scalar cases.
func eventTagEquals(v any, target string) bool {
	switch x := v.(type) {
	case string:
		return x == target
	case bool:
		if x {
			return target == "true"
		}
		return target == "false"
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64) == target
	case json.Number:
		return string(x) == target
	case nil:
		return target == "" || target == "null"
	default:
		return fmt.Sprintf("%v", v) == target
	}
}

func GetEventNatsMessageId(preparePosition int64, commitPosition int64) string {
	return fmt.Sprintf("%d%d", preparePosition, commitPosition)
}

func InitializeEventStore(
	ctx context.Context,
	config c.AppConfig,
	saveEvents EventsSaver,
	getEvents EventsRetriever,
	lockProvider LockProvider,
	indexManager BoundaryIndexManager,
	js jetstream.JetStream,
	logger logging.Logger) *EventStore {

	logger.Info("Initializing EventStore")
	eventStore := NewEventStoreServer(
		ctx,
		js,
		saveEvents,
		getEvents,
		lockProvider,
		indexManager,
		getBoundaryNames(config.GetBoundaries()),
		EventStreamConfig{
			MaxBytes: config.Nats.EventStreamMaxBytes,
			MaxMsgs:  config.Nats.EventStreamMaxMsgs,
			MaxAge:   config.Nats.EventStreamMaxAge,
		},
		logger,
	)
	logger.Info("EventStore initialized")

	return eventStore
}

func getBoundaryNames(boundary *[]c.Boundary) *[]string {
	var names []string
	for _, boundary := range *boundary {
		names = append(names, boundary.Name)
	}
	return &names
}

type EventPublishingTracker interface {
	GetLastPublishedEventPosition(ctx context.Context, boundary string) (Position, error)
	InsertLastPublishedEvent(ctx context.Context, boundaryOfInterest string, transactionId int64, globalId int64) error
}

// EventSignal provides notification that new events may be available for publishing.
type EventSignal interface {
	Wait(ctx context.Context) error
	Stop()
}

type PollingSignal struct {
	ticker *time.Ticker
}

func NewPollingSignal(interval time.Duration) *PollingSignal {
	return &PollingSignal{ticker: time.NewTicker(interval)}
}

func (s *PollingSignal) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ticker.C:
		return nil
	}
}

func (s *PollingSignal) Stop() {
	s.ticker.Stop()
}

// Backoff implements capped exponential backoff with jitter.
// Use for retry loops to avoid thundering-herd on shared resources (PG advisory locks, etc).
type Backoff struct {
	Base, Max time.Duration
	cur       time.Duration
}

// Wait sleeps for the current interval (with up to 50% jitter), doubles it for next time
// (capped at Max), and returns ctx.Err if cancelled.
func (b *Backoff) Wait(ctx context.Context) error {
	if b.cur <= 0 {
		b.cur = b.Base
	}
	jitter := time.Duration(rand.Int64N(int64(b.cur)/2 + 1))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(b.cur + jitter):
	}
	b.cur *= 2
	if b.cur > b.Max {
		b.cur = b.Max
	}
	return nil
}

func (b *Backoff) Reset() { b.cur = 0 }

func StartEventPolling(
	ctx context.Context,
	config c.AppConfig,
	lockProvider LockProvider,
	getEvents EventsRetriever,
	js jetstream.JetStream,
	eventPublishingTracker EventPublishingTracker,
	signalProvider func(string) EventSignal,
	logger logging.Logger) {
	g, gctx := errgroup.WithContext(ctx)
	for _, name := range config.GetBoundaryNames() {
		boundary := name
		g.Go(func() error {
			backoff := Backoff{Base: 100 * time.Millisecond, Max: 5 * time.Second}
			for {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
					lease, err := acquireLockLease(gctx, lockProvider, boundary)
					if err != nil {
						logger.Warnf("Failed to acquire lock for boundary %s: %v - will retry", boundary, err)
						if waitErr := backoff.Wait(gctx); waitErr != nil {
							return waitErr
						}
						continue
					}
					backoff.Reset()
					logger.Infof("Successfully acquired polling lock for boundary %v", boundary)

					lockCtx := lease.Context()
					lastPosition, err := eventPublishingTracker.GetLastPublishedEventPosition(lockCtx, boundary)
					if err != nil {
						lease.Release()
						logger.Errorf("Failed to get last published position for boundary %s: %v", boundary, err)
						if waitErr := backoff.Wait(gctx); waitErr != nil {
							return waitErr
						}
						continue
					}
					logger.Infof("Last published position for boundary %v: %v", boundary, &lastPosition)

					err = publishEventsLoopWithLease(
						lockCtx,
						js,
						getEvents,
						config.PollingPublisher.BatchSize,
						&lastPosition,
						boundary,
						eventPublishingTracker,
						signalProvider(boundary),
						lease,
						logger,
					)
					lease.Release()

					if err != nil {
						logger.Errorf("Polling stopped for boundary %s: %v - will retry", boundary, err)
						if waitErr := backoff.Wait(gctx); waitErr != nil {
							return waitErr
						}
					}
				}
			}
		})
	}
	// Not waiting here; goroutines will be governed by ctx lifecycle.
}

func publishEventsLoop(
	ctx context.Context,
	js jetstream.JetStream,
	eventStore EventsRetriever,
	batchSize uint32,
	lastPosition *Position,
	boundary string,
	db EventPublishingTracker,
	signal EventSignal,
	logger logging.Logger,
) error {
	return publishEventsLoopWithLease(ctx, js, eventStore, batchSize, lastPosition, boundary, db, signal, contextLockLease{ctx: ctx}, logger)
}

func publishEventsLoopWithLease(
	ctx context.Context,
	js jetstream.JetStream,
	eventStore EventsRetriever,
	batchSize uint32,
	lastPosition *Position,
	boundary string,
	db EventPublishingTracker,
	signal EventSignal,
	lease LockLease,
	logger logging.Logger,
) error {
	const readRetryBase = 100 * time.Millisecond
	const readRetryMax = 2 * time.Second
	const publishRetryBase = 100 * time.Millisecond
	const publishRetryMax = 2 * time.Second

	defer signal.Stop()
	readBackoff := readRetryBase

	for {
		if err := lease.Check(ctx); err != nil {
			return err
		}

		// Drain all pending events from DB in order. We drain BEFORE waiting on
		// the signal so events already persisted at startup (or committed while
		// the publisher was down) are published immediately rather than waiting
		// for the next NOTIFY / catch-up tick.
		for {
			if err := lease.Check(ctx); err != nil {
				return err
			}

			req := &GetEventsRequest{
				FromPosition: &Position{
					CommitPosition:  lastPosition.CommitPosition,
					PreparePosition: lastPosition.PreparePosition + 1,
				},
				Count:     batchSize,
				Direction: Direction_ASC,
				Boundary:  boundary,
			}
			resp, err := eventStore.Get(ctx, req)
			if err != nil {
				logger.Errorf("Failed to get events for boundary %s: %v", boundary, err)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(readBackoff):
				}
				readBackoff *= 2
				if readBackoff > readRetryMax {
					readBackoff = readRetryMax
				}
				continue
			}
			readBackoff = readRetryBase

			if len(resp.Events) == 0 {
				break
			}

			if err := validatePublishBatch(resp.Events, lastPosition); err != nil {
				return err
			}

			for _, event := range resp.Events {
				if err := lease.Check(ctx); err != nil {
					return err
				}
				if event.Position == nil {
					return fmt.Errorf("event %s has nil position", event.EventId)
				}
				if !positionAfter(event.Position, lastPosition) {
					return fmt.Errorf(
						"event %s position (%d, %d) is not after cursor (%d, %d)",
						event.EventId,
						event.Position.CommitPosition,
						event.Position.PreparePosition,
						lastPosition.CommitPosition,
						lastPosition.PreparePosition,
					)
				}

				subjectName := GetEventJetstreamSubjectName(
					boundary,
					&Position{
						CommitPosition:  event.Position.CommitPosition,
						PreparePosition: event.Position.PreparePosition,
					},
				)
				eventData, err := json.Marshal(event)
				if err != nil {
					return fmt.Errorf("marshal event: %w", err)
				}

				publishBackoff := publishRetryBase
				for {
					if err := lease.Check(ctx); err != nil {
						return err
					}
					_, err = js.Publish(
						ctx,
						subjectName,
						eventData,
						jetstream.WithMsgID(GetEventNatsMessageId(event.Position.PreparePosition, event.Position.CommitPosition)),
						jetstream.WithRetryAttempts(5),
					)
					if err == nil {
						break
					}
					logger.Errorf("Failed to publish event to NATS for boundary %s: %v (retrying)", boundary, err)
					select {
					case <-ctx.Done():
						return fmt.Errorf("publish event: context cancelled: %w", ctx.Err())
					case <-time.After(publishBackoff):
					}
					publishBackoff *= 2
					if publishBackoff > publishRetryMax {
						publishBackoff = publishRetryMax
					}
				}

				if err := lease.Check(ctx); err != nil {
					return err
				}
				if err := db.InsertLastPublishedEvent(
					ctx,
					boundary,
					event.Position.CommitPosition,
					event.Position.PreparePosition,
				); err != nil {
					return fmt.Errorf("insert last published: %w", err)
				}
				lastPosition = event.Position
			}
		}

		// Wait for the next signal (NOTIFY, catch-up tick, or poll) before
		// draining again.
		if err := signal.Wait(ctx); err != nil {
			return err
		}
	}
}

func positionAfter(pos, cursor *Position) bool {
	if pos == nil || cursor == nil {
		return false
	}
	if pos.CommitPosition != cursor.CommitPosition {
		return pos.CommitPosition > cursor.CommitPosition
	}
	return pos.PreparePosition > cursor.PreparePosition
}

func validatePublishBatch(events []*Event, cursor *Position) error {
	validationCursor := cursor
	for _, event := range events {
		if event.Position == nil {
			return fmt.Errorf("event %s has nil position", event.EventId)
		}
		if !positionAfter(event.Position, validationCursor) {
			return fmt.Errorf(
				"event %s position (%d, %d) is not after cursor (%d, %d)",
				event.EventId,
				event.Position.CommitPosition,
				event.Position.PreparePosition,
				validationCursor.CommitPosition,
				validationCursor.PreparePosition,
			)
		}
		validationCursor = event.Position
	}
	return nil
}
