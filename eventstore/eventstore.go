package eventstore

import (
	"context"
	"fmt"
	reflect "reflect"
	"slices"
	"strings"

	"github.com/goccy/go-json"

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
		streamVersion int64,
		streamSubSet *Query,
	) (transactionID string, globalID int64, newStreamPosition int64, err error)
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

const (
	eventsStreamPrefix = "ORISUN_EVENTS"
	EventsSubjectName  = "events"
)

func GetEventsNatsJetstreamStreamStreamName(boundary string) string {
	return eventsStreamPrefix + "__" + boundary
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
	var newStreamVersion int64

	// Execute the query
	transactionID, globalID, newStreamVersion, err = s.saveEventsFn.Save(
		ctx,
		eventsForMarshaling,
		req.Boundary,
		req.Stream.Name,
		req.Stream.ExpectedVersion,
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
		NewStreamVersion: newStreamVersion,
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
	err := s.lockProvider.Lock(ctx, boundary+"__"+subscriberName)

	if err != nil {
		return status.Errorf(codes.AlreadyExists, "failed to acquire lock: %v", err)
	}

	var now = time.Now()
	var timeToSubscribeFromJetstream time.Time = now

	// first get the latest event in the event store
	latestEventInTheEventstore, err := s.getEventsFn.Get(
		ctx,
		&GetEventsRequest{
			Count:     1,
			Direction: Direction_DESC,
			Boundary:  boundary,
		},
	)

	if err != nil {
		return err
	}

	if afterPosition == nil {
		// client is trying to subscribe from the latest position
		s.logger.Infof("Position is nil, subscribing from the latest position")

		// get the latest event matching the query criteria
		resp, err := s.getEventsFn.Get(ctx, &GetEventsRequest{
			Count:     1,
			Direction: Direction_DESC,
			Boundary:  boundary,
			Query:     query,
		})

		if err != nil {
			return err
		}

		if len(resp.Events) > 0 {
			s.logger.Infof("Found oldest event matching query criteria: %v", resp.Events[0].EventId)
			// set the from position to the last event in the eventstore matching the query condition
			timeToSubscribeFromJetstream = resp.Events[0].DateCreated.AsTime()
		} else {
			// no events match the query criteria, so we default to the latest event in the eventstore
			if len(latestEventInTheEventstore.Events) > 0 {
				timeToSubscribeFromJetstream = latestEventInTheEventstore.Events[0].DateCreated.AsTime()
			}
		}
	} else if afterPosition.CommitPosition == 0 && afterPosition.PreparePosition == 0 {
		// client is subscribing from the beginning of the eventstore
		s.logger.Infof("Position is 0,0")

		//there's a chance for an optimisation here, we fetch the oldest event matching the query criteria
		// and set the timeToSubscribeFromJetstream to that event's time
		oldestMatchingEvent, err := s.GetEvents(
			ctx,
			&GetEventsRequest{
				Query: query,
				FromPosition: &Position{
					CommitPosition:  0,
					PreparePosition: 0,
				},
				Count:     1,
				Direction: Direction_ASC,
				Boundary:  boundary,
			},
		)

		if err != nil {
			return err
		}

		if len(oldestMatchingEvent.Events) > 0 {
			timeToSubscribeFromJetstream = oldestMatchingEvent.Events[0].DateCreated.AsTime()
		} else {
			// no events match the query criteria, so we default to the latest event in the eventstore.
			timeToSubscribeFromJetstream = time.Unix(0, 0)
		}
	} else {
		events, err := s.GetEvents(
			ctx,
			&GetEventsRequest{
				Query: query,
				FromPosition: &Position{
					CommitPosition:  afterPosition.CommitPosition,
					PreparePosition: afterPosition.PreparePosition,
				},
				Count:     1,
				Direction: Direction_DESC,
				Boundary:  boundary,
			},
		)

		if err != nil {
			return status.Errorf(codes.Internal, "failed to get events: %v", err)
		}

		if len(events.Events) > 0 {
			//start from 5 seconds before event time
			timeToSubscribeFromJetstream = events.Events[0].DateCreated.AsTime()
		} else {
			// no events match the query criteria, so we default to the latest event in the eventstore, or now.
			if len(latestEventInTheEventstore.Events) > 0 {
				timeToSubscribeFromJetstream = latestEventInTheEventstore.Events[0].DateCreated.AsTime()
			}
		}
	}

	// Initialize position tracking
	lastProcessedPosition := afterPosition

	// Set up NATS subscription for live events
	subs, err := s.js.Stream(ctx, GetEventsNatsJetstreamStreamStreamName(boundary))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get stream: %v", err)
	}

	consumer, err := subs.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          subscriberName,
		DeliverPolicy: jetstream.DeliverByStartTimePolicy,
		AckPolicy:     jetstream.AckNonePolicy,
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
		OptStartTime:  &timeToSubscribeFromJetstream,
	})

	defer subs.DeleteConsumer(ctx, subscriberName)

	if err != nil {
		return status.Errorf(codes.Internal, "failed to create consumer: %v", err)
	}
	// Ensure the consumer is cleaned up using the same name
	defer subs.DeleteConsumer(ctx, subscriberName)

	// Start consuming messages with a done channel for cleanup
	msgDone := make(chan struct{})
	msgCtx, msgCancel := context.WithCancel(ctx)
	defer msgCancel()

	// Start consuming messages
	msgs, err := consumer.Messages(jetstream.PullMaxMessages(200))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get message iterator: %v", err)
	}

	// Start message processing in a separate goroutine
	go func() {
		defer close(msgDone)
		for {
			select {
			case <-msgCtx.Done():
				s.logger.Info("Message processing stopped")
				return
			default:
				msg, err := msgs.Next()
				if err != nil {
					if msgCtx.Err() != nil {
						s.logger.Info("Context cancelled, stopping message processing")
						return
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

				isNewer := false
				if lastProcessedPosition != nil {
					isNewer = isEventPositionNewerThanPosition(event.Position, lastProcessedPosition)
				} else {
					isNewer = timeToSubscribeFromJetstream.Before(event.DateCreated.AsTime())
				}

				if isNewer && s.eventMatchesQueryCriteria(&event, query, nil) {
					// Check context before attempting to send
					select {
					case <-msgCtx.Done():
						// Context is cancelled, don't try to send
						s.logger.Info("Context cancelled, not sending event")
						msg.Ack() // Acknowledge the message since we're shutting down
						continue
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
					msg.Ack() // Acknowledge messages that don't match criteria
				}
			}
		}
	}()

	// Wait for either context cancellation or message processing completion
	select {
	case <-ctx.Done():
		s.logger.Info("Context cancelled, cleaning up subscription")
		return ctx.Err()
	case <-msgDone:
		s.logger.Info("Message processing completed")
		return nil
	}
}

func (s *EventStore) SubscribeToStream(
	ctx context.Context,
	boundary string,
	subscriberName string,
	query *Query,
	stream string,
	afterStreamVersion *int64,
	handler *globalCommon.MessageHandler[Event],
) error {
	err := s.lockProvider.Lock(ctx, boundary+"__"+subscriberName+"__"+stream)

	if err != nil {
		return status.Errorf(codes.AlreadyExists, "failed to acquire lock: %v", err)
	}

	var now = time.Now()
	var timeToSubscribeFromJetstream time.Time = now

	// first get the latest event in the stream
	latestEventInStream, err := s.getEventsFn.Get(
		ctx,
		&GetEventsRequest{
			Count:     1,
			Direction: Direction_DESC,
			Boundary:  boundary,
			Stream: &GetStreamQuery{
				Name: stream,
			},
		},
	)

	if err != nil {
		return err
	}

	// also get the latest event in the event store
	latestEventInTheEventstore, err := s.getEventsFn.Get(
		ctx,
		&GetEventsRequest{
			Count:     1,
			Direction: Direction_DESC,
			Boundary:  boundary,
		},
	)

	if err != nil {
		return err
	}

	if afterStreamVersion == nil {
		// client is trying to subscribe from the latest version
		s.logger.Infof("Version is nil")

		// get the latest event matching the query criteria
		resp, err := s.getEventsFn.Get(ctx, &GetEventsRequest{
			Count:     1,
			Direction: Direction_DESC,
			Boundary:  boundary,
			Query:     query,
			Stream: &GetStreamQuery{
				Name:        stream,
				FromVersion: 999999999999999999,
			},
		})

		if err != nil {
			return err
		}

		if len(resp.Events) > 0 {
			// set timeToSubscribeFromJetstream to the last event in the stream matching the query condition
			timeToSubscribeFromJetstream = resp.Events[0].DateCreated.AsTime()
		} else {
			if len(latestEventInStream.Events) > 0 {
				// no events match the query criteria, so we default to the latest event in the stream
				timeToSubscribeFromJetstream = latestEventInStream.Events[0].DateCreated.AsTime()
			} else if len(latestEventInTheEventstore.Events) > 0 {
				// no events in the stream, so we default to the latest event in the eventstore
				timeToSubscribeFromJetstream = latestEventInTheEventstore.Events[0].DateCreated.AsTime()
			}
		}
	} else if *afterStreamVersion == -1 {
		// client is subscribing from the beginning of the stream
		s.logger.Infof("Version is -1")

		// there's a chance for an optimisation here, we fetch the oldest event matching the query criteria
		// and set the timeToSubscribeFromJetstream to that event's time
		oldestMatchingEvent, err := s.GetEvents(
			ctx,
			&GetEventsRequest{
				Query:     query,
				Count:     1,
				Direction: Direction_ASC,
				Boundary:  boundary,
				Stream: &GetStreamQuery{
					Name:        stream,
					FromVersion: 0,
				},
			},
		)

		if err != nil {
			return err
		}

		if len(oldestMatchingEvent.Events) > 0 {
			s.logger.Infof("Oldest matching event: %v", oldestMatchingEvent.Events[0])
			timeToSubscribeFromJetstream = oldestMatchingEvent.Events[0].DateCreated.AsTime()
		} else {
			// no events match the query criteria, so we default to the first event in the stream.
			firstEventInStream, err := s.GetEvents(
				ctx,
				&GetEventsRequest{
					Count:     1,
					Direction: Direction_ASC,
					Boundary:  boundary,
					Stream: &GetStreamQuery{
						Name: stream,
					},
				},
			)

			if err != nil {
				return err
			}

			if len(firstEventInStream.Events) > 0 {
				s.logger.Infof("First event in stream: %v", firstEventInStream.Events[0])
				timeToSubscribeFromJetstream = firstEventInStream.Events[0].DateCreated.AsTime()
			} else {
				//stream does not exist we can simply start from the latest event in the eventstore
				s.logger.Infof("Latest event in the eventstore: %v", latestEventInTheEventstore.Events[0])
				if latestEventInTheEventstore.Events[0].DateCreated.AsTime() != time.Unix(0, 0) {
					timeToSubscribeFromJetstream = latestEventInTheEventstore.Events[0].DateCreated.AsTime()
				}
			}
		}
	} else {
		events, err := s.GetEvents(
			ctx,
			&GetEventsRequest{
				Query: query,
				Stream: &GetStreamQuery{
					Name:        stream,
					FromVersion: *afterStreamVersion,
				},
				Count:     1,
				Direction: Direction_DESC,
				Boundary:  boundary,
			},
		)

		if err != nil {
			return status.Errorf(codes.Internal, "failed to get events: %v", err)
		}

		if len(events.Events) > 0 {
			s.logger.Infof("Event: %v", events.Events[0])
			timeToSubscribeFromJetstream = events.Events[0].DateCreated.AsTime()
		} else {
			// no events match the query criteria, so we default to the latest event in the eventstore, or now.
			if len(latestEventInStream.Events) > 0 {
				timeToSubscribeFromJetstream = latestEventInStream.Events[0].DateCreated.AsTime()
			} else if len(latestEventInTheEventstore.Events) > 0 {
				timeToSubscribeFromJetstream = latestEventInTheEventstore.Events[0].DateCreated.AsTime()
			}
		}
	}

	// Initialize position tracking
	var lastProcessedPosition *Position

	// Set up NATS subscription for live events
	subs, err := s.js.Stream(ctx, GetEventsNatsJetstreamStreamStreamName(boundary))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get stream: %v", err)
	}

	consumer, err := subs.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          subscriberName,
		DeliverPolicy: jetstream.DeliverByStartTimePolicy,
		AckPolicy:     jetstream.AckNonePolicy,
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
		OptStartTime:  &timeToSubscribeFromJetstream,
	})

	if err != nil {
		return status.Errorf(codes.Internal, "failed to create consumer: %v", err)
	}
	// Ensure the consumer is cleaned up using the same name
	defer subs.DeleteConsumer(ctx, subscriberName)

	// Start consuming messages with a done channel for cleanup
	msgDone := make(chan struct{})
	msgCtx, msgCancel := context.WithCancel(ctx)
	defer msgCancel()

	// Start consuming messages
	msgs, err := consumer.Messages(jetstream.PullMaxMessages(200))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get message iterator: %v", err)
	}

	// Start message processing in a separate goroutine
	go func() {
		defer close(msgDone)
		for {
			select {
			case <-msgCtx.Done():
				s.logger.Info("Message processing stopped")
				return
			default:
				msg, err := msgs.Next()
				if err != nil {
					if msgCtx.Err() != nil {
						s.logger.Info("Context cancelled, stopping message processing")
						return
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

				isNewer := false
				if lastProcessedPosition == nil {
					if *afterStreamVersion == -1 {
						isNewer = event.DateCreated.AsTime().After(timeToSubscribeFromJetstream) || event.DateCreated.AsTime().Equal(timeToSubscribeFromJetstream)
					} else {
						isNewer = event.DateCreated.AsTime().After(timeToSubscribeFromJetstream)
					}
				} else {
					isNewer = isEventPositionNewerThanPosition(event.Position, lastProcessedPosition)
				}

				if isNewer && s.eventMatchesQueryCriteria(&event, query, &stream) {
					// Check context before attempting to send
					select {
					case <-msgCtx.Done():
						// Context is cancelled, don't try to send
						s.logger.Info("Context cancelled, not sending event")
						msg.Ack() // Acknowledge the message since we're shutting down
						continue
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
					msg.Ack() // Acknowledge messages that don't match criteria
				}
			}
		}
	}()

	// Wait for either context cancellation or message processing completion
	select {
	case <-ctx.Done():
		s.logger.Info("Context cancelled, cleaning up subscription")
		return ctx.Err()
	case <-msgDone:
		s.logger.Info("Message processing completed")
		return nil
	}
}

func (s *EventStore) CatchUpSubscribeToEvents(req *CatchUpSubscribeToEventStoreRequest, stream EventStore_CatchUpSubscribeToEventsServer) error {
	s.logger.Infof("CatchUpSubscribeToEvents: %v", req)
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
			&req.AfterVersion,
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
