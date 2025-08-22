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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ImplementerSaveEvents interface {
	Save(ctx context.Context,
		events []EventWithMapTags,
		// indexLockCondition *IndexLockCondition,
		boundary string,
		streamName string,
		streamVersion int64,
		streamSubSet *Query) (transactionID string, globalID int64, err error)
}

type ImplementerGetEvents interface {
	Get(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error)
}

type UnlockFunc func() error

type LockProvider interface {
	Lock(ctx context.Context, lockName string) (UnlockFunc, error)
}

type EventStore struct {
	UnimplementedEventStoreServer
	js           jetstream.JetStream
	saveEventsFn ImplementerSaveEvents
	getEventsFn  ImplementerGetEvents
	lockProvider LockProvider
}

const (
	eventsStreamPrefix = "ORISUN_EVENTS"
	EventsSubjectName  = "events"
)

var logger logging.Logger

func GetEventsStreamName(boundary string) string {
	return eventsStreamPrefix + "__" + boundary
}

func GetEventsSubjectName(boundary string) string {
	return GetEventsStreamName(boundary) + "." + EventsSubjectName + ".*"
}

func GetEventSubjectName(boundary string, position *Position) string {
	return GetEventsStreamName(boundary) + "." + EventsSubjectName + "." + GetEventNatsMessageId(int64(position.PreparePosition), int64(position.CommitPosition))
}

func NewEventStoreServer(
	ctx context.Context,
	js jetstream.JetStream,
	saveEventsFn ImplementerSaveEvents,
	getEventsFn ImplementerGetEvents,
	lockProvider LockProvider,
	boundaries *[]string,
	log logging.Logger,
) *EventStore {

	logger = log
	for _, boundary := range *boundaries {
		streamName := GetEventsStreamName(boundary)
		info, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name: streamName,
			Subjects: []string{
				GetEventsSubjectName(boundary),
			},
			MaxMsgs: -1,
		})

		if err != nil {
			log.Fatalf("failed to add stream: %v %v", streamName, err)
		}

		log.Infof("stream info: %v", info)
	}

	return &EventStore{
		js:           js,
		saveEventsFn: saveEventsFn,
		getEventsFn:  getEventsFn,
		lockProvider: lockProvider,
	}
}

type EventWithMapTags struct {
	EventId   string `json:"event_id"`
	EventType string `json:"event_type"`
	Data      any    `json:"data"`
	Metadata  any    `json:"metadata"`
	// Tags      map[string]interface{} `json:"tags"`
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
	logger.Debugf("SaveEvents called with req: %v", req)

	err = authorizeRequest(ctx, []globalCommon.Role{globalCommon.RoleAdmin, globalCommon.RoleOperations})
	if err != nil {
		return nil, err
	}
	// Defer a recovery function to catch any panics
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("Panic in SaveEvents: %v\nStack Trace:\n%s", r, debug.Stack())
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
			// Tags:      getTagsAsMap(event.Tags, event.EventType),
		}
	}

	var transactionID string
	var globalID int64

	// Execute the query
	transactionID, globalID, err = s.saveEventsFn.Save(
		ctx,
		eventsForMarshaling,
		// req.ConsistencyCondition,
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
	}, nil
}

func (s *EventStore) GetEvents(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error) {
	if req.Count == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Count cannot be 0")
	}
	return s.getEventsFn.Get(ctx, req)
}

func (s *EventStore) SubscribeToEvents(
	ctx context.Context,
	boundary string,
	subscriberName string,
	fromPosition *Position,
	query *Query,
	handler globalCommon.MessageHandler[Event],
) error {
	if fromPosition == nil {
		//get the latest event in the event store
		resp, err := s.getEventsFn.Get(
			ctx,
			&GetEventsRequest{
				Count:     1,
				Direction: Direction_DESC,
				Boundary:  boundary,
				Query:     query,
			},
		)
		if err != nil {
			return err
		}
		if len(resp.Events) > 0 {
			// set the from position to the last event in the stream
			fromPosition = &Position{
				CommitPosition:  resp.Events[0].Position.CommitPosition,
				PreparePosition: resp.Events[0].Position.PreparePosition,
			}
		} else {
			fromPosition = &Position{
				CommitPosition:  0,
				PreparePosition: 0,
			}
		}
	}
	unlockFunc, err := s.lockProvider.Lock(ctx, boundary+"__"+subscriberName)

	if err != nil {
		return status.Errorf(codes.AlreadyExists, "failed to acquire lock: %v", err)
	}

	// Ensure cleanup happens in all cases
	defer unlockFunc()

	// Initialize position tracking
	lastPosition := fromPosition

	// Set up NATS subscription for live events
	subs, err := s.js.Stream(ctx, GetEventsStreamName(boundary))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get stream: %v", err)
	}

	// Check if we need to process historical events by examining the first event in the jetstream
	skipHistorical := false
	info, err := subs.Info(ctx)
	if err == nil && info.State.FirstSeq > 0 {
		firstMsg, err := subs.GetMsg(ctx, info.State.FirstSeq)
		if err == nil {
			var firstEvent Event
			if err := json.Unmarshal(firstMsg.Data, &firstEvent); err == nil {
				// If the first event in the stream is newer than our position, skip historical events
				if !isEventPositionNewerThanPosition(firstEvent.Position, lastPosition) {
					logger.Infof("First event in nats jetstream is older than requested position, skipping historical events")
					skipHistorical = true
					lastPosition = firstEvent.Position
				}
			}
		}
	}

	// Process historical events
	historicalDone := make(chan struct{})
	var historicalErr error
	var lastTime time.Time

	go func() {
		defer close(historicalDone)

		if skipHistorical {
			lastTime = time.Now()
			return
		}

		lastPosition, lastTime, historicalErr = s.sendHistoricalEventsFromDatabaseToNats(
			ctx,
			lastPosition,
			query,
			handler,
			boundary,
		)

		if historicalErr != nil {
			logger.Errorf("Historical events processing failed: %v", historicalErr)
			return
		}

		if (lastTime.Equal(time.Time{})) {
			lastTime = time.Now()
		}

		logger.Infof("Historical events processed up to %v", lastTime)
	}()

	if skipHistorical {
		events, err := s.GetEvents(ctx, &GetEventsRequest{
			Query:        query,
			FromPosition: fromPosition,
			Count:        1,
			Direction:    Direction_DESC,
			Boundary:     boundary,
		})

		if err != nil {
			return status.Errorf(codes.Internal, "failed to get events: %v", err)
		}

		if len(events.Events) > 0 {
			lastPosition = events.Events[0].Position
			//start from 5 seconds before event time
			lastTime = events.Events[0].DateCreated.AsTime().Add(-5 * time.Second)
		} else {
			return status.Errorf(codes.NotFound, "No event with position %v found", fromPosition)
		}
	}

	// Wait for historical processing
	select {
	case <-historicalDone:
		if historicalErr != nil {
			return status.Errorf(codes.Internal, "historical events failed: %v", historicalErr)
		}
	case <-ctx.Done():
		logger.Error("Context cancelled, stopping subscription")
		return ctx.Err()
	}

	consumer, err := subs.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		// Name:          subscriberName,
		DeliverPolicy: jetstream.DeliverByStartTimePolicy,
		AckPolicy:     jetstream.AckNonePolicy,
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
		OptStartTime:  &lastTime,
	})

	if err != nil {
		return status.Errorf(codes.Internal, "failed to create consumer: %v", err)
	}
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
				logger.Info("Message processing stopped")
				return
			default:
				msg, err := msgs.Next()
				if err != nil {
					if msgCtx.Err() != nil {
						logger.Info("Context cancelled, stopping message processing")
						return
					}
					logger.Errorf("Error getting next message: %v", err)
					continue
				}

				var event Event
				if err := json.Unmarshal(msg.Data(), &event); err != nil {
					logger.Errorf("Failed to unmarshal event: %v", err)
					msg.Ack()
					continue
				}

				isNewer := isEventPositionNewerThanPosition(event.Position, fromPosition)

				if isNewer && s.eventMatchesQueryCriteria(&event, query) {
					// Check context before attempting to send
					select {
					case <-msgCtx.Done():
						// Context is cancelled, don't try to send
						logger.Info("Context cancelled, not sending event")
						msg.Ack() // Acknowledge the message since we're shutting down
						continue
					default:
						// Context is still active, proceed with send
						if err := handler.Send(&event); err != nil {
							logger.Errorf("Failed to send event: %v", err)
							msg.Nak() // Negative acknowledgment to retry later
							continue
						}
					}

					lastPosition = event.Position

					if err := msg.Ack(); err != nil {
						logger.Errorf("Failed to acknowledge message: %v", err)
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
		logger.Info("Context cancelled, cleaning up subscription")
		return ctx.Err()
	case <-msgDone:
		logger.Info("Message processing completed")
		return nil
	}
}

func (s *EventStore) CatchUpSubscribeToEvents(req *CatchUpSubscribeToEventStoreRequest, stream EventStore_CatchUpSubscribeToEventsServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	messageHandler := globalCommon.NewMessageHandler[Event](ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("Message processing stopped")
				return

			default:
				event, err := messageHandler.Recv()
				if err != nil {
					if ctx.Err() != nil {
						// Context is cancelled, exit gracefully
						logger.Info("Context cancelled, stopping event processing")
						return
					}
					logger.Errorf("Failed to receive event: %v", err)
					continue
				}

				// Check context before sending to stream
				select {
				case <-ctx.Done():
					// Context is cancelled, don't try to send
					logger.Info("Context cancelled, not sending event to stream")
					return
				default:
					// Context is still active, proceed with send
					if err := stream.Send(event); err != nil {
						logger.Errorf("Failed to send event: %v", err)
						continue
					}
				}
			}
		}
	}()
	return s.SubscribeToEvents(
		ctx,
		req.Boundary,
		req.SubscriberName,
		req.GetPosition(),
		req.Query,
		*messageHandler,
	)
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

func (s *EventStore) sendHistoricalEventsFromDatabaseToNats(
	ctx context.Context,
	fromPosition *Position,
	query *Query,
	stream globalCommon.MessageHandler[Event],
	boundary string) (*Position, time.Time, error) {

	lastPosition := fromPosition
	var lastEventTime time.Time
	batchSize := uint32(100) // Adjust as needed

	for {
		events, err := s.GetEvents(ctx, &GetEventsRequest{
			Query:        query,
			FromPosition: lastPosition,
			Count:        batchSize,
			Direction:    Direction_ASC,
			Boundary:     boundary,
		})

		if err != nil {
			return nil, time.Time{}, status.Errorf(codes.Internal, "failed to fetch historical events: %v", err)
		}

		for _, event := range events.Events {
			if err := stream.Send(event); err != nil {
				return nil, time.Time{}, err
			}
			lastPosition = event.Position
			lastEventTime = event.DateCreated.AsTime()
		}

		if len(events.Events) < int(batchSize) {
			// We've reached the end of historical events
			break
		}
	}

	logger.Debugf("Finished sending historical events %v")

	return lastPosition, lastEventTime, nil
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

func (s *EventStore) eventMatchesQueryCriteria(event *Event, criteria *Query) bool {
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
