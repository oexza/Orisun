// Package mobile exposes the NATS-free embedded SQLite store through a small
// API compatible with gomobile bindings. Structured inputs and outputs cross
// the language boundary as JSON so callers never need Go contexts, maps,
// protobuf values, channels, or generic types.
package mobile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"

	localstore "github.com/OrisunLabs/Orisun/embedded/sqlite/local"
	"github.com/OrisunLabs/Orisun/orisun"
)

// EventListener receives serialized event envelopes. Binding consumers should
// return quickly from callbacks and dispatch UI work onto their platform's main
// thread when necessary.
type EventListener interface {
	OnEvent(eventJSON string)
	OnError(message string)
}

// Store owns one local SQLite runtime and all subscriptions created from it.
type Store struct {
	inner  *localstore.Store
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

// Open creates a NATS-free embedded store. boundariesJSON must be a non-empty
// JSON string array, for example ["accounts","payments"].
func Open(dataDirectory string, boundariesJSON string) (*Store, error) {
	var boundaries []string
	if err := json.Unmarshal([]byte(boundariesJSON), &boundaries); err != nil {
		return nil, fmt.Errorf("decode boundaries: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	inner, err := localstore.Open(ctx, dataDirectory, boundaries, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	return &Store{inner: inner, ctx: ctx, cancel: cancel}, nil
}

// SaveEvents appends the JSON event array and returns its committed position as
// JSON. Empty expectedPositionJSON or queryJSON means no CCC condition.
func (s *Store) SaveEvents(boundary, eventsJSON, expectedPositionJSON, queryJSON string) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	events, err := decodeEvents(eventsJSON)
	if err != nil {
		return "", err
	}
	expectedPosition, err := decodePosition(expectedPositionJSON)
	if err != nil {
		return "", fmt.Errorf("decode expected position: %w", err)
	}
	query, err := decodeQuery(queryJSON)
	if err != nil {
		return "", fmt.Errorf("decode query: %w", err)
	}
	position, err := s.inner.SaveEvents(s.ctx, events, boundary, expectedPosition, query)
	if err != nil {
		return "", err
	}
	return marshalJSON(positionFromProto(position))
}

// GetEvents reads a page from SQLite and returns a JSON event array. A blank
// fromPositionJSON starts at the beginning for ascending reads and at the end
// for descending reads.
func (s *Store) GetEvents(boundary, fromPositionJSON, queryJSON string, count int, descending bool) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	if count < 0 {
		return "", errors.New("event count must be >= 0")
	}
	if count > int(orisun.MaxReadBatchSize) {
		return "", fmt.Errorf("event count must be <= %d", orisun.MaxReadBatchSize)
	}
	position, err := decodePosition(fromPositionJSON)
	if err != nil {
		return "", fmt.Errorf("decode from position: %w", err)
	}
	query, err := decodeQuery(queryJSON)
	if err != nil {
		return "", fmt.Errorf("decode query: %w", err)
	}
	direction := orisun.Direction_ASC
	if descending {
		direction = orisun.Direction_DESC
	}
	batch, err := s.inner.GetEvents(s.ctx, &orisun.GetEventsRequest{
		Boundary:     boundary,
		FromPosition: position,
		Query:        query,
		Count:        uint32(count),
		Direction:    direction,
	})
	if err != nil {
		return "", err
	}
	return marshalJSON(eventsFromBatch(batch))
}

// GetLatestByCriteria returns one match per input criterion plus the combined
// CCC context position. queryJSON has the same {"criteria":[...]} shape used by
// SaveEvents.
func (s *Store) GetLatestByCriteria(boundary, queryJSON string) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	query, err := decodeReadQuery(boundary, queryJSON)
	if err != nil {
		return "", fmt.Errorf("decode query: %w", err)
	}
	batch, err := s.inner.GetLatestByCriteria(s.ctx, query)
	if err != nil {
		return "", err
	}
	response := latestResponse{
		Matches: make([]latestMatch, len(batch.Matches)),
		ContextPosition: positionJSON{
			CommitPosition:  batch.ContextCommitPosition,
			PreparePosition: batch.ContextPreparePosition,
		},
	}
	for i := range batch.Matches {
		response.Matches[i].Found = batch.Matches[i].Found
		if batch.Matches[i].Found {
			event := eventFromRead(batch.Matches[i].Event)
			response.Matches[i].Event = &event
		}
	}
	return marshalJSON(response)
}

// Subscribe starts an asynchronous local subscription. A blank
// afterPositionJSON starts at the current end; {"commit_position":-1,
// "prepare_position":-1} replays from the beginning.
func (s *Store) Subscribe(boundary, subscriberName, afterPositionJSON, queryJSON string, listener EventListener) (*Subscription, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if listener == nil {
		return nil, errors.New("event listener is nil")
	}
	afterPosition, err := decodePosition(afterPositionJSON)
	if err != nil {
		return nil, fmt.Errorf("decode after position: %w", err)
	}
	query, err := decodeQuery(queryJSON)
	if err != nil {
		return nil, fmt.Errorf("decode query: %w", err)
	}
	// Resolve "start at the current end" before returning the subscription
	// handle. This makes an event saved immediately after Subscribe returns
	// observable even if the worker goroutine has not registered its notifier
	// yet; its first SQLite catch-up read will find the event.
	if afterPosition == nil {
		latest, err := s.inner.GetEvents(s.ctx, &orisun.GetEventsRequest{
			Boundary:  boundary,
			Count:     1,
			Direction: orisun.Direction_DESC,
		})
		if err != nil {
			return nil, err
		}
		afterPosition = &orisun.Position{CommitPosition: -1, PreparePosition: -1}
		if len(latest) > 0 {
			afterPosition.CommitPosition = latest[0].CommitPosition
			afterPosition.PreparePosition = latest[0].PreparePosition
		}
	}

	ctx, cancel := context.WithCancel(s.ctx)
	subscription := &Subscription{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(subscription.done)
		err := s.inner.Subscribe(ctx, boundary, subscriberName, afterPosition, query, func(event orisun.ReadEvent) error {
			encoded, err := marshalJSON(eventFromRead(event))
			if err != nil {
				return err
			}
			listener.OnEvent(encoded)
			return nil
		})
		if err != nil && ctx.Err() == nil {
			listener.OnError(err.Error())
		}
	}()
	return subscription, nil
}

// CreateBoundaryIndex creates an SQLite expression index. fieldsJSON and
// conditionsJSON are arrays matching BoundaryIndexField and
// BoundaryIndexCondition JSON field names.
func (s *Store) CreateBoundaryIndex(boundary, name, fieldsJSON, conditionsJSON, combinator string) error {
	if err := s.ready(); err != nil {
		return err
	}
	var inputFields []indexFieldJSON
	if err := decodeOptionalJSON(fieldsJSON, &inputFields); err != nil {
		return fmt.Errorf("decode index fields: %w", err)
	}
	fields := make([]orisun.BoundaryIndexField, len(inputFields))
	for i := range inputFields {
		fields[i] = orisun.BoundaryIndexField{JsonKey: inputFields[i].JSONKey, ValueType: inputFields[i].ValueType}
	}
	var inputConditions []indexConditionJSON
	if err := decodeOptionalJSON(conditionsJSON, &inputConditions); err != nil {
		return fmt.Errorf("decode index conditions: %w", err)
	}
	conditions := make([]orisun.BoundaryIndexCondition, len(inputConditions))
	for i := range inputConditions {
		conditions[i] = orisun.BoundaryIndexCondition{
			Key:      inputConditions[i].Key,
			Operator: inputConditions[i].Operator,
			Value:    inputConditions[i].Value,
		}
	}
	return s.inner.CreateBoundaryIndex(s.ctx, boundary, name, fields, conditions, combinator)
}

func (s *Store) DropBoundaryIndex(boundary, name string) error {
	if err := s.ready(); err != nil {
		return err
	}
	return s.inner.DropBoundaryIndex(s.ctx, boundary, name)
}

// Close stops the store and its subscriptions. It is safe to call repeatedly.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.inner != nil {
			s.inner.Close()
		}
	})
}

func (s *Store) ready() error {
	if s == nil || s.inner == nil || s.ctx == nil {
		return errors.New("mobile sqlite store is not open")
	}
	if err := s.ctx.Err(); err != nil {
		return errors.New("mobile sqlite store is closed")
	}
	return nil
}

// Subscription is a cancellable asynchronous subscription handle.
type Subscription struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// Stop requests subscription cancellation and returns immediately.
func (s *Subscription) Stop() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.once.Do(s.cancel)
	}
}

// IsDone reports whether the subscription goroutine has stopped.
func (s *Subscription) IsDone() bool {
	if s == nil || s.done == nil {
		return true
	}
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// AwaitDone blocks until the subscription worker exits. Call it from a background
// thread when a platform binding needs a completion signal.
func (s *Subscription) AwaitDone() {
	if s == nil || s.done == nil {
		return
	}
	<-s.done
}

type eventInput struct {
	EventID   string          `json:"event_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	Metadata  json.RawMessage `json:"metadata"`
}

type positionJSON struct {
	CommitPosition  int64 `json:"commit_position"`
	PreparePosition int64 `json:"prepare_position"`
}

type tagJSON struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type criterionJSON struct {
	Tags []tagJSON `json:"tags"`
}

type queryJSON struct {
	Criteria []criterionJSON `json:"criteria"`
}

type eventJSON struct {
	EventID     string          `json:"event_id"`
	EventType   string          `json:"event_type"`
	Data        json.RawMessage `json:"data"`
	Metadata    json.RawMessage `json:"metadata"`
	Position    positionJSON    `json:"position"`
	DateCreated string          `json:"date_created"`
}

type latestMatch struct {
	Found bool       `json:"found"`
	Event *eventJSON `json:"event,omitempty"`
}

type latestResponse struct {
	Matches         []latestMatch `json:"matches"`
	ContextPosition positionJSON  `json:"context_position"`
}

type indexFieldJSON struct {
	JSONKey   string `json:"json_key"`
	ValueType string `json:"value_type"`
}

type indexConditionJSON struct {
	Key      string `json:"key"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

func decodeEvents(encoded string) ([]orisun.EventWithMapTags, error) {
	var input []eventInput
	if err := json.Unmarshal([]byte(encoded), &input); err != nil {
		return nil, fmt.Errorf("decode events: %w", err)
	}
	events := make([]orisun.EventWithMapTags, len(input))
	for i := range input {
		data := strings.TrimSpace(string(input[i].Data))
		if data == "" || data == "null" {
			data = "{}"
		}
		metadata := strings.TrimSpace(string(input[i].Metadata))
		if metadata == "" {
			metadata = "null"
		}
		events[i] = orisun.EventWithMapTags{
			EventId:   input[i].EventID,
			EventType: input[i].EventType,
			Data:      data,
			Metadata:  metadata,
		}
	}
	return events, nil
}

func decodePosition(encoded string) (*orisun.Position, error) {
	if optionalJSONEmpty(encoded) {
		return nil, nil
	}
	var position positionJSON
	if err := json.Unmarshal([]byte(encoded), &position); err != nil {
		return nil, err
	}
	return &orisun.Position{
		CommitPosition:  position.CommitPosition,
		PreparePosition: position.PreparePosition,
	}, nil
}

func decodeQuery(encoded string) (*orisun.Query, error) {
	if optionalJSONEmpty(encoded) {
		return nil, nil
	}
	var input queryJSON
	if err := json.Unmarshal([]byte(encoded), &input); err != nil {
		return nil, err
	}
	query := &orisun.Query{Criteria: make([]*orisun.Criterion, len(input.Criteria))}
	for i := range input.Criteria {
		criterion := &orisun.Criterion{Tags: make([]*orisun.Tag, len(input.Criteria[i].Tags))}
		for j := range input.Criteria[i].Tags {
			criterion.Tags[j] = &orisun.Tag{Key: input.Criteria[i].Tags[j].Key, Value: input.Criteria[i].Tags[j].Value}
		}
		query.Criteria[i] = criterion
	}
	return query, nil
}

func decodeReadQuery(boundary, encoded string) (orisun.LatestByCriteriaQuery, error) {
	query, err := decodeQuery(encoded)
	if err != nil {
		return orisun.LatestByCriteriaQuery{}, err
	}
	if query == nil {
		return orisun.LatestByCriteriaQuery{Boundary: boundary}, nil
	}
	criteria := make([]orisun.ReadCriterion, len(query.Criteria))
	for i := range query.Criteria {
		criteria[i].Tags = make([]orisun.ReadTag, len(query.Criteria[i].Tags))
		for j := range query.Criteria[i].Tags {
			criteria[i].Tags[j] = orisun.ReadTag{Key: query.Criteria[i].Tags[j].Key, Value: query.Criteria[i].Tags[j].Value}
		}
	}
	return orisun.LatestByCriteriaQuery{Boundary: boundary, Criteria: criteria}, nil
}

func decodeOptionalJSON(encoded string, target any) error {
	if optionalJSONEmpty(encoded) {
		return nil
	}
	return json.Unmarshal([]byte(encoded), target)
}

func optionalJSONEmpty(encoded string) bool {
	trimmed := strings.TrimSpace(encoded)
	return trimmed == "" || trimmed == "null"
}

func eventsFromBatch(batch orisun.ReadEventBatch) []eventJSON {
	events := make([]eventJSON, len(batch))
	for i := range batch {
		events[i] = eventFromRead(batch[i])
	}
	return events
}

func eventFromRead(event orisun.ReadEvent) eventJSON {
	return eventJSON{
		EventID:     event.EventId,
		EventType:   event.EventType,
		Data:        json.RawMessage(event.Data),
		Metadata:    json.RawMessage(event.Metadata),
		Position:    positionJSON{CommitPosition: event.CommitPosition, PreparePosition: event.PreparePosition},
		DateCreated: event.DateCreated.UTC().Format(time.RFC3339Nano),
	}
}

func positionFromProto(position *orisun.Position) positionJSON {
	return positionJSON{CommitPosition: position.CommitPosition, PreparePosition: position.PreparePosition}
}

func marshalJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
