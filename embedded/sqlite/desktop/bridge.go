// Package desktop provides a handle-based JSON bridge over the NATS-free
// embedded SQLite runtime. It contains no C or Dart dependencies so its
// lifecycle, error, and subscription semantics can be tested as normal Go.
package desktop

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mobile "github.com/OrisunLabs/Orisun/embedded/sqlite/mobile"
)

const ABIVersion = 1

const maxQueuedSubscriptionMessages = 10_000

type Bridge struct {
	mu            sync.Mutex
	nextHandle    uint64
	stores        map[uint64]*storeEntry
	subscriptions map[uint64]*subscriptionEntry
}

type storeEntry struct {
	store         *mobile.Store
	subscriptions map[uint64]struct{}
}

type subscriptionEntry struct {
	storeHandle  uint64
	subscription *mobile.Subscription
	queue        *subscriptionQueue
}

type subscriptionMessage struct {
	kind  string
	value string
}

type subscriptionQueue struct {
	mu       sync.Mutex
	messages []subscriptionMessage
	notify   chan struct{}
	done     bool
	overflow bool
}

type queueListener struct {
	queue *subscriptionQueue
}

type envelope struct {
	OK     bool            `json:"ok"`
	Handle uint64          `json:"handle,omitempty"`
	Kind   string          `json:"kind,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	Error  *bridgeError    `json:"error,omitempty"`
}

type bridgeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewBridge() *Bridge {
	return &Bridge{
		nextHandle:    1,
		stores:        make(map[uint64]*storeEntry),
		subscriptions: make(map[uint64]*subscriptionEntry),
	}
}

func (b *Bridge) Open(dataDirectory, boundariesJSON string) string {
	store, err := mobile.Open(dataDirectory, boundariesJSON)
	if err != nil {
		return failure(err)
	}
	b.mu.Lock()
	handle := b.allocateHandleLocked()
	b.stores[handle] = &storeEntry{store: store, subscriptions: make(map[uint64]struct{})}
	b.mu.Unlock()
	return successHandle(handle)
}

func (b *Bridge) Close(storeHandle uint64) string {
	b.mu.Lock()
	entry, ok := b.stores[storeHandle]
	if !ok {
		b.mu.Unlock()
		return failure(fmt.Errorf("store handle %d was not found", storeHandle))
	}
	delete(b.stores, storeHandle)
	subscriptions := make([]*subscriptionEntry, 0, len(entry.subscriptions))
	for handle := range entry.subscriptions {
		if subscription, exists := b.subscriptions[handle]; exists {
			delete(b.subscriptions, handle)
			subscriptions = append(subscriptions, subscription)
		}
	}
	b.mu.Unlock()

	for _, subscription := range subscriptions {
		subscription.subscription.Stop()
		subscription.queue.markDone()
	}
	entry.store.Close()
	return success()
}

// Shutdown closes every store owned by the bridge. Platform plugins call this
// when a Flutter engine detaches so hot restarts do not leave database workers
// alive in the host process.
func (b *Bridge) Shutdown() {
	b.mu.Lock()
	handles := make([]uint64, 0, len(b.stores))
	for handle := range b.stores {
		handles = append(handles, handle)
	}
	b.mu.Unlock()
	for _, handle := range handles {
		b.Close(handle)
	}
}

func (b *Bridge) SaveEvents(storeHandle uint64, boundary, eventsJSON, expectedPositionJSON, queryJSON string) string {
	store, err := b.store(storeHandle)
	if err != nil {
		return failure(err)
	}
	value, err := store.SaveEvents(boundary, eventsJSON, expectedPositionJSON, queryJSON)
	if err != nil {
		return failure(err)
	}
	return successValue(value)
}

func (b *Bridge) GetEvents(storeHandle uint64, boundary, fromPositionJSON, queryJSON string, count int, descending bool) string {
	store, err := b.store(storeHandle)
	if err != nil {
		return failure(err)
	}
	value, err := store.GetEvents(boundary, fromPositionJSON, queryJSON, count, descending)
	if err != nil {
		return failure(err)
	}
	return successValue(value)
}

func (b *Bridge) GetLatestByCriteria(storeHandle uint64, boundary, queryJSON string) string {
	store, err := b.store(storeHandle)
	if err != nil {
		return failure(err)
	}
	value, err := store.GetLatestByCriteria(boundary, queryJSON)
	if err != nil {
		return failure(err)
	}
	return successValue(value)
}

func (b *Bridge) CreateBoundaryIndex(storeHandle uint64, boundary, name, fieldsJSON, conditionsJSON, combinator string) string {
	store, err := b.store(storeHandle)
	if err != nil {
		return failure(err)
	}
	if err := store.CreateBoundaryIndex(boundary, name, fieldsJSON, conditionsJSON, combinator); err != nil {
		return failure(err)
	}
	return success()
}

func (b *Bridge) DropBoundaryIndex(storeHandle uint64, boundary, name string) string {
	store, err := b.store(storeHandle)
	if err != nil {
		return failure(err)
	}
	if err := store.DropBoundaryIndex(boundary, name); err != nil {
		return failure(err)
	}
	return success()
}

func (b *Bridge) Subscribe(storeHandle uint64, boundary, subscriberName, afterPositionJSON, queryJSON string) string {
	store, err := b.store(storeHandle)
	if err != nil {
		return failure(err)
	}
	queue := newSubscriptionQueue()
	subscription, err := store.Subscribe(boundary, subscriberName, afterPositionJSON, queryJSON, &queueListener{queue: queue})
	if err != nil {
		return failure(err)
	}

	b.mu.Lock()
	storeEntry, ok := b.stores[storeHandle]
	if !ok {
		b.mu.Unlock()
		subscription.Stop()
		return failure(errors.New("store was closed while creating subscription"))
	}
	handle := b.allocateHandleLocked()
	entry := &subscriptionEntry{storeHandle: storeHandle, subscription: subscription, queue: queue}
	b.subscriptions[handle] = entry
	storeEntry.subscriptions[handle] = struct{}{}
	b.mu.Unlock()

	go func() {
		subscription.AwaitDone()
		queue.markDone()
	}()
	return successHandle(handle)
}

func (b *Bridge) NextSubscriptionMessage(subscriptionHandle uint64, timeout time.Duration) string {
	entry, err := b.subscription(subscriptionHandle)
	if err != nil {
		return failure(err)
	}
	message, state := entry.queue.next(timeout)
	switch state {
	case "event", "error":
		return successKindValue(state, message.value)
	case "done", "timeout":
		return successKind(state)
	default:
		return failure(fmt.Errorf("unknown subscription state %q", state))
	}
}

func (b *Bridge) StopSubscription(subscriptionHandle uint64) string {
	b.mu.Lock()
	entry, ok := b.subscriptions[subscriptionHandle]
	if !ok {
		b.mu.Unlock()
		return failure(fmt.Errorf("subscription handle %d was not found", subscriptionHandle))
	}
	delete(b.subscriptions, subscriptionHandle)
	if store, exists := b.stores[entry.storeHandle]; exists {
		delete(store.subscriptions, subscriptionHandle)
	}
	b.mu.Unlock()
	entry.subscription.Stop()
	entry.queue.markDone()
	return success()
}

func (b *Bridge) store(handle uint64) (*mobile.Store, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.stores[handle]
	if !ok {
		return nil, fmt.Errorf("store handle %d was not found", handle)
	}
	return entry.store, nil
}

func (b *Bridge) subscription(handle uint64) (*subscriptionEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.subscriptions[handle]
	if !ok {
		return nil, fmt.Errorf("subscription handle %d was not found", handle)
	}
	return entry, nil
}

func (b *Bridge) allocateHandleLocked() uint64 {
	handle := b.nextHandle
	b.nextHandle++
	if b.nextHandle == 0 {
		b.nextHandle = 1
	}
	return handle
}

func newSubscriptionQueue() *subscriptionQueue {
	return &subscriptionQueue{notify: make(chan struct{}, 1)}
}

func (q *subscriptionQueue) push(message subscriptionMessage) {
	q.mu.Lock()
	if q.done {
		q.mu.Unlock()
		return
	}
	if len(q.messages) >= maxQueuedSubscriptionMessages {
		if !q.overflow {
			q.overflow = true
			q.messages = append(q.messages, subscriptionMessage{
				kind:  "error",
				value: "subscription queue exceeded 10000 messages; consume events continuously or restart from the last persisted position",
			})
		}
		q.mu.Unlock()
		q.signal()
		return
	}
	q.messages = append(q.messages, message)
	q.mu.Unlock()
	q.signal()
}

func (q *subscriptionQueue) markDone() {
	q.mu.Lock()
	q.done = true
	q.mu.Unlock()
	q.signal()
}

func (q *subscriptionQueue) next(timeout time.Duration) (subscriptionMessage, string) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		q.mu.Lock()
		if len(q.messages) > 0 {
			message := q.messages[0]
			q.messages[0] = subscriptionMessage{}
			q.messages = q.messages[1:]
			q.mu.Unlock()
			return message, message.kind
		}
		if q.done {
			q.mu.Unlock()
			return subscriptionMessage{}, "done"
		}
		q.mu.Unlock()

		if timeout <= 0 {
			return subscriptionMessage{}, "timeout"
		}
		select {
		case <-q.notify:
		case <-deadline.C:
			return subscriptionMessage{}, "timeout"
		}
	}
}

func (q *subscriptionQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (l *queueListener) OnEvent(eventJSON string) {
	l.queue.push(subscriptionMessage{kind: "event", value: eventJSON})
}

func (l *queueListener) OnError(message string) {
	l.queue.push(subscriptionMessage{kind: "error", value: message})
}

func success() string {
	return marshalEnvelope(envelope{OK: true})
}

func successHandle(handle uint64) string {
	return marshalEnvelope(envelope{OK: true, Handle: handle})
}

func successValue(value string) string {
	return marshalEnvelope(envelope{OK: true, Value: json.RawMessage(value)})
}

func successKind(kind string) string {
	return marshalEnvelope(envelope{OK: true, Kind: kind})
}

func successKindValue(kind, value string) string {
	if kind == "error" {
		encoded, _ := json.Marshal(value)
		return marshalEnvelope(envelope{OK: true, Kind: kind, Value: encoded})
	}
	return marshalEnvelope(envelope{OK: true, Kind: kind, Value: json.RawMessage(value)})
}

func failure(err error) string {
	code := status.Code(err)
	message := err.Error()
	if code != codes.Unknown {
		message = status.Convert(err).Message()
	}
	return marshalEnvelope(envelope{OK: false, Error: &bridgeError{Code: errorCode(code), Message: message}})
}

func errorCode(code codes.Code) string {
	switch code {
	case codes.InvalidArgument:
		return "invalid_argument"
	case codes.AlreadyExists:
		return "already_exists"
	case codes.NotFound:
		return "not_found"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.Unauthenticated:
		return "unauthenticated"
	case codes.Unavailable:
		return "unavailable"
	case codes.Unimplemented:
		return "unimplemented"
	case codes.Internal:
		return "internal"
	default:
		return "operation_failed"
	}
}

func marshalEnvelope(value envelope) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `{"ok":false,"error":{"code":"internal","message":"failed to encode native response"}}`
	}
	return string(encoded)
}
