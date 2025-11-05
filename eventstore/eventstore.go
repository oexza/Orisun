package eventstore

import (
	"context"
	"fmt"
	reflect "reflect"
	"slices"
	"strings"

	"github.com/goccy/go-json"
	"github.com/google/uuid"

	"runtime/debug"
	"time"

	globalCommon "orisun/common"
	logging "orisun/logging"

	"github.com/nats-io/nats.go/jetstream"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type EventstoreSaveEvents interface {
	Save(ctx context.Context,
		events []EventWithMapTags,
		boundary string,
		streamName string,
		expectedPosition *Position,
		streamSubSet *Query,
	) (transactionID string, globalID int64, err error)
}

type EventstoreGetEvents interface {
	Get(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error)
}

type LockProvider interface {
	Lock(ctx context.Context, lockName string) error
}

type EventStore struct {
	UnimplementedEventStoreServer
	js           jetstream.JetStream
	saveEventsFn EventstoreSaveEvents
	getEventsFn  EventstoreGetEvents
	lockProvider LockProvider
	logger       logging.Logger
}

func NotExistsPosition() Position {
	return Position{
		CommitPosition:  -1,
		PreparePosition: -1,
	}
}

var FirstPosition = Position{
	CommitPosition:  0,
	PreparePosition: 0,
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

func GetEventJetstreamSubjectName(boundary string, stream string, position *Position) string {
	return GetEventsNatsJetstreamStreamStreamName(boundary) + "." + EventsSubjectName + "." + stream + "." + GetEventNatsMessageId(int64(position.PreparePosition), int64(position.CommitPosition))
}

func NewEventStoreServer(
	ctx context.Context,
	js jetstream.JetStream,
	saveEventsFn EventstoreSaveEvents,
	getEventsFn EventstoreGetEvents,
	lockProvider LockProvider,
	boundaries *[]string,
	logger logging.Logger,
) *EventStore {
	for _, boundary := range *boundaries {
		streamName := GetEventsNatsJetstreamStreamStreamName(boundary)
		info, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name: streamName,
			Subjects: []string{
				GetEventsSubjectName(boundary),
			},
			MaxMsgs: -1,
			Storage: jetstream.MemoryStorage,
			MaxAge:  time.Minute * 5,
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
		logger:       logger,
	}
}

type EventWithMapTags struct {
	EventId   string `json:"event_id"`
	EventType string `json:"event_type"`
	Data      any    `json:"data"`
	Metadata  any    `json:"metadata"`
}

func authorizeRequest(ctx context.Context, roles []globalCommon.Role) error {
	// Check if the user has the necessary permissions to perform the query
	user := ctx.Value(globalCommon.UserContextKey)
	if user == nil {
		return nil
	}

	// Check if the user has any of the necessary permissions to perform the query
	userObj := user.(globalCommon.User)

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

func (s *EventStore) SaveEvents(ctx context.Context, req *SaveEventsRequest) (resp *WriteResult, err error) {
	s.logger.Debugf("SaveEvents called with req: %v", req)

	err = authorizeRequest(ctx, []globalCommon.Role{globalCommon.RoleAdmin, globalCommon.RoleOperations})
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

	eventsForMarshaling := make([]EventWithMapTags, len(req.Events))
	for i, event := range req.Events {
		var dataMap, metadataMap map[string]any

		if err = json.Unmarshal([]byte(event.Data), &dataMap); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Invalid JSON in data field: %v", err)
		}
		dataMap["eventType"] = event.EventType

		if err = json.Unmarshal([]byte(event.Metadata), &metadataMap); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Invalid JSON in metadata field: %v", err)
		}

		eventsForMarshaling[i] = EventWithMapTags{
			EventId:   event.EventId,
			EventType: event.EventType,
			Data:      dataMap,
			Metadata:  metadataMap,
		}
	}

	var transactionID string
	var globalID int64

	// Execute the query
	transactionID, globalID, err = s.saveEventsFn.Save(
		ctx,
		eventsForMarshaling,
		req.Boundary,
		req.Stream.Name,
		req.Stream.ExpectedPosition,
		req.Stream.SubsetQuery,
	)

	if err != nil {
		if strings.Contains(err.Error(), "OptimisticConcurrencyException") {
			return nil, status.Errorf(codes.AlreadyExists, "failed to save events: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to save events: %v", err)
	}

	return &WriteResult{
		LogPosition: &Position{
			CommitPosition:  parseInt64(transactionID),
			PreparePosition: globalID,
		},
	}, nil
}

func (s *EventStore) GetEvents(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error) {
	if req.Count == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Count cannot be 0")
	}
	// if req.FromPosition != nil && req.Stream != nil {
	// 	return nil, status.Error(codes.InvalidArgument, "fromPosition and stream cannot be set together, you can only set one of both")
	// }
	return s.getEventsFn.Get(ctx, req)
}

func (s *EventStore) SubscribeToAllEvents(
	ctx context.Context,
	boundary string,
	subscriberName string,
	afterPosition *Position,
	query *Query,
	handler *globalCommon.MessageHandler[Event],
) error {
	subscriptionName := boundary + "__" + subscriberName
	// Use errgroup for coordinated error handling and cancellation
	g, gCtx := errgroup.WithContext(ctx)
	err := s.lockProvider.Lock(gCtx, subscriptionName)

	if err != nil {
		return status.Errorf(codes.AlreadyExists, "failed to acquire lock: %v", err)
	}

	// Initialize position tracking
	lastProcessedPosition := afterPosition

	// Phase 1: Catch-up by polling the event store until we're up to date
	s.logger.Info("Starting catch-up phase: polling event store")

	const batchSize = 100
	for {
		select {
		case <-gCtx.Done():
			return gCtx.Err()
		default:
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
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			default:
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
		lastEventResp, err := s.getEventsFn.Get(gCtx, &GetEventsRequest{
			Count:     1,
			Direction: Direction_DESC,
			Boundary:  boundary,
			FromPosition: &Position{
				PreparePosition: lastProcessedPosition.PreparePosition,
				CommitPosition:  lastProcessedPosition.CommitPosition,
			},
		})
		if err != nil {
			s.logger.Errorf("Failed to get last processed event for timestamp: %v", err)
			return fmt.Errorf("cannot determine NATS subscription start time: failed to retrieve last processed event: %w", err)
		} else if len(lastEventResp.Events) == 0 {
			s.logger.Warn("No events found at last processed position, using polling completion time for NATS subscription")
			timeToSubscribeFromJetstream = pollingCompletedTime
		} else {
			timeToSubscribeFromJetstream = lastEventResp.Events[0].DateCreated.AsTime()
			s.logger.Debugf("Starting NATS subscription from last processed event time: %v", timeToSubscribeFromJetstream)
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
			select {
			case <-gCtx.Done():
				s.logger.Info("Message processing stopped due to context cancellation")
				return gCtx.Err()
			default:
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

				if isNewer && s.eventMatchesQueryCriteria(&event, query, nil) {
					// Check context before attempting to send
					select {
					case <-gCtx.Done():
						// Context is cancelled, don't try to send
						s.logger.Info("Context cancelled, not sending event")
						return gCtx.Err()
					default:
						// Context is still active, proceed with send
						if err := handler.Send(&event); err != nil {
							s.logger.Errorf("Failed to send event: %v", err)
							// Continue processing other events instead of returning error
							continue
						}
					}

					lastProcessedPosition = event.Position

					if err := msg.Ack(); err != nil {
						s.logger.Errorf("Failed to acknowledge message: %v", err)
					}
				} else {
					msg.Ack() // Acknowledge messages that don't match criteria or are duplicates
				}
			}
		}
	})

	// Wait for all goroutines to complete or for an error to occur
	return g.Wait()
}

func (s *EventStore) SubscribeToStream(
	ctx context.Context,
	boundary string,
	subscriberName string,
	query *Query,
	stream string,
	afterPosition *Position,
	handler *globalCommon.MessageHandler[Event],
) error {
	subscriptionName := boundary + "__" + subscriberName + "__" + stream
	// Use errgroup for coordinated goroutine management
	g, gCtx := errgroup.WithContext(ctx)
	err := s.lockProvider.Lock(gCtx, subscriptionName)

	if err != nil {
		return status.Errorf(codes.AlreadyExists, "failed to acquire lock: %v", err)
	}

	// Initialize position tracking
	lastProcessedPosition := afterPosition

	// Phase 1: Catch-up by polling the event store until we're up to date
	s.logger.Info("Starting catch-up phase: polling event store for stream")

	const batchSize = 100
	for {
		select {
		case <-gCtx.Done():
			return gCtx.Err()
		default:
		}

		// Get events from the stream starting from our last processed position
		var getEventsReq *GetEventsRequest
		if lastProcessedPosition == nil {
			// Start from the latest version if no version specified
			getEventsReq = &GetEventsRequest{
				Count:     batchSize,
				Direction: Direction_DESC,
				Boundary:  boundary,
				Query:     query,
				Stream: &GetStreamQuery{
					Name: stream,
				},
			}
		} else {
			// Continue from the last processed stream version
			getEventsReq = &GetEventsRequest{
				Count:        batchSize,
				Direction:    Direction_ASC,
				Boundary:     boundary,
				Query:        query,
				FromPosition: lastProcessedPosition,
				Stream: &GetStreamQuery{
					Name: stream,
				},
			}
		}

		resp, err := s.getEventsFn.Get(gCtx, getEventsReq)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to get events during catch-up: %v", err)
		}

		// If no more events, we're caught up
		if len(resp.Events) == 0 {
			s.logger.Info("Catch-up phase completed: no more events in stream")
			break
		}

		// Send all events in this batch
		for _, event := range resp.Events {
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			default:
			}

			if err := handler.Send(event); err != nil {
				return status.Errorf(codes.Internal, "failed to send event during catch-up: %v", err)
			}
			lastProcessedPosition = event.Position
		}

		// If we got fewer events than requested, we're caught up
		if len(resp.Events) < batchSize {
			s.logger.Info("Catch-up phase completed: reached end of stream")
			break
		}
	}

	// Capture the time immediately after polling is completed
	pollingCompletedTime := time.Now()

	// Phase 2: Subscribe to NATS for live updates
	s.logger.Info("Starting live phase: subscribing to NATS for real-time updates")

	// Determine the time to start NATS subscription from
	var timeToSubscribeFromJetstream time.Time
	if lastProcessedPosition != nil {
		// Start NATS subscription from the time of the last processed event
		// We need to get the event to find its timestamp
		lastEventResp, err := s.getEventsFn.Get(gCtx, &GetEventsRequest{
			Count:        1,
			Direction:    Direction_DESC,
			Boundary:     boundary,
			FromPosition: lastProcessedPosition,
			Stream: &GetStreamQuery{
				Name: stream,
			},
		})
		if err != nil {
			s.logger.Errorf("Failed to get last processed event for stream %s timestamp: %v", stream, err)
			return fmt.Errorf("cannot determine NATS subscription start time for stream %s: failed to retrieve last processed event: %w", stream, err)
		} else if len(lastEventResp.Events) == 0 {
			s.logger.Warnf("No events found at last processed position for stream %s, using polling completion time for NATS subscription", stream)
			timeToSubscribeFromJetstream = pollingCompletedTime
		} else {
			timeToSubscribeFromJetstream = lastEventResp.Events[0].DateCreated.AsTime()
			s.logger.Debugf("Starting NATS subscription for stream %s from last processed event time: %v", stream, timeToSubscribeFromJetstream)
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

	natsSubscriptionName := subscriberName + uuid.New().String()

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
	defer subs.DeleteConsumer(gCtx, natsSubscriptionName)

	// Start consuming messages
	msgs, err := consumer.Messages(jetstream.PullMaxMessages(200))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get message iterator: %v", err)
	}

	// NATS Consumer Worker: processes messages from NATS stream
	g.Go(func() error {
		for {
			select {
			case <-gCtx.Done():
				s.logger.Info("Message processing stopped")
				return gCtx.Err()
			default:
				msg, err := msgs.Next()
				if err != nil {
					if gCtx.Err() != nil {
						s.logger.Info("Context cancelled, stopping message processing")
						return gCtx.Err()
					}
					s.logger.Errorf("Error getting next message: %v", err)
					// Small backoff to avoid tight loop on repeated errors
					time.Sleep(100 * time.Millisecond)
					continue
				}

				var event Event
				if err := json.Unmarshal(msg.Data(), &event); err != nil {
					s.logger.Errorf("Failed to unmarshal event: %v", err)
					msg.Ack()
					continue
				}

				// Only process events from the target stream and newer than our last processed position
				isTargetStream := event.StreamId == stream
				isNewer := false
				if lastProcessedPosition != nil {
					isNewer = isEventPositionNewerThanPosition(event.Position, lastProcessedPosition)
				} else {
					isNewer = true // If no position, all events are considered new
				}

				if isTargetStream && isNewer && s.eventMatchesQueryCriteria(&event, query, &stream) {
					// Check context before attempting to send
					select {
					case <-gCtx.Done():
						// Context is cancelled, don't try to send
						s.logger.Info("Context cancelled, not sending event")
						msg.Ack() // Acknowledge the message since we're shutting down
						return gCtx.Err()
					default:
						// Context is still active, proceed with send
						if err := handler.Send(&event); err != nil {
							s.logger.Errorf("Failed to send event: %v", err)
							msg.Nak() // Negative acknowledgment to retry later
							continue
						}
					}

					lastProcessedPosition = event.Position

					if err := msg.Ack(); err != nil {
						s.logger.Errorf("Failed to acknowledge message: %v", err)
					}
				} else {
					msg.Ack() // Acknowledge messages that don't match criteria or are duplicates
				}
			}
		}
	})

	return g.Wait()
}

func (s *EventStore) CatchUpSubscribeToEvents(req *CatchUpSubscribeToEventStoreRequest, stream EventStore_CatchUpSubscribeToEventsServer) error {
	s.logger.Infof("CatchUpSubscribeToEvents: %v", req)

	g, gctx := errgroup.WithContext(stream.Context())

	messageHandler := globalCommon.NewMessageHandler[Event](gctx)

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

func (s *EventStore) CatchUpSubscribeToStream(req *CatchUpSubscribeToStreamRequest, stream EventStore_CatchUpSubscribeToStreamServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	g, gctx := errgroup.WithContext(ctx)

	messageHandler := globalCommon.NewMessageHandler[Event](gctx)

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
					time.Sleep(100 * time.Millisecond)
					continue
				}

				select {
				case <-gctx.Done():
					s.logger.Info("Context cancelled, not sending event to stream")
					return gctx.Err()
				default:
					if err := stream.Send(event); err != nil {
						s.logger.Errorf("Failed to send event: %v", err)
						time.Sleep(100 * time.Millisecond)
						continue
					}
				}
			}
		}
	})

	// Subscription worker: runs the catch-up subscription and feeds the handler
	g.Go(func() error {
		return s.SubscribeToStream(
			gctx,
			req.Boundary,
			req.SubscriberName,
			req.Query,
			req.Stream,
			req.AfterPosition,
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

	if req.Stream == nil {
		return status.Error(codes.InvalidArgument, "Invalid request: missing stream to save events to")
	}

	return nil
}

func (s *EventStore) eventMatchesQueryCriteria(event *Event, criteria *Query, stream *string) bool {
	if stream != nil && strings.TrimSpace(*stream) != event.StreamId {
		return false
	}

	if criteria == nil || len(criteria.Criteria) == 0 {
		return true
	}

	unmarshaledData := map[string]any{}
	json.Unmarshal([]byte(event.Data), &unmarshaledData)

	// For multiple criteria groups, ANY group matching is sufficient (OR logic)
	for _, criteriaGroup := range criteria.Criteria {
		allTagsMatch := true

		// Within a group, ALL tags must match (AND logic)
		for _, criteriaTag := range criteriaGroup.Tags {
			tagFound := false

			for key, eventTag := range unmarshaledData {
				// More robust comparison
				if key == criteriaTag.Key && reflect.DeepEqual(eventTag, criteriaTag.Value) {
					tagFound = true
					break
				}
			}
			if !tagFound {
				allTagsMatch = false
				break
			}
		}
		// If all tags in this group matched, we can return true
		if allTagsMatch {
			return true
		}
	}

	// No criteria group fully matched
	return false
}

func parseInt64(s string) int64 {
	var i int64
	fmt.Sscanf(s, "%d", &i)
	return i
}

func GetEventNatsMessageId(preparePosition int64, commitPosition int64) string {
	return fmt.Sprintf("%d%d", preparePosition, commitPosition)
}
