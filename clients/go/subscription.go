package orisun

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	eventstore "github.com/orisunlabs/orisun-go-client/eventstore"
)

// EventHandler defines the interface for handling events from a subscription
type EventHandler interface {
	// OnEvent is called when an event is received
	OnEvent(event *eventstore.Event) error
	
	// OnError is called when an error occurs
	OnError(err error)
	
	// OnCompleted is called when the subscription completes
	OnCompleted()
}

// EventSubscription represents a subscription to events
type EventSubscription struct {
	stream   grpc.ClientStream
	handler  EventHandler
	logger   Logger
	closed   bool
	mu       sync.RWMutex
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewEventSubscription creates a new EventSubscription
func NewEventSubscription(
	stream grpc.ClientStream,
	handler EventHandler,
	logger Logger,
	cancel context.CancelFunc,
) *EventSubscription {
	if logger == nil {
		logger = NewNoOpLogger()
	}
	
	subscription := &EventSubscription{
		stream:  stream,
		handler: handler,
		logger:  logger,
		closed:  false,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	
	// Start the event processing goroutine
	go subscription.processEvents()
	
	return subscription
}

// processEvents processes events from the stream
func (es *EventSubscription) processEvents() {
	defer close(es.done)
	
	for {
		es.mu.RLock()
		closed := es.closed
		es.mu.RUnlock()
		
		if closed {
			return
		}
		
		// Receive the actual event from the stream
		event := &eventstore.Event{}
		err := es.stream.RecvMsg(event)
		if err != nil {
			es.mu.RLock()
			if !es.closed {
				es.logger.Errorf("Subscription error: %v", err)
				es.handler.OnError(err)
			}
			es.mu.RUnlock()
			return
		}
		
		es.mu.RLock()
		if !es.closed {
			es.logger.Debug("Received event from stream: %s", event.EventId)
			if handlerErr := es.handler.OnEvent(event); handlerErr != nil {
				es.logger.Errorf("Handler error: %v", handlerErr)
			}
		}
		es.mu.RUnlock()
	}
}

// Close closes the subscription
func (es *EventSubscription) Close() error {
	es.mu.Lock()
	defer es.mu.Unlock()
	
	if es.closed {
		es.logger.Debug("Subscription already closed")
		return nil
	}
	
	es.logger.Debug("Closing subscription")
	es.closed = true
	
	// Cancel the context
	if es.cancel != nil {
		es.cancel()
	}
	
	// Call the completion handler
	es.handler.OnCompleted()
	
	// Wait for the processing goroutine to finish with timeout
	select {
	case <-es.done:
		// Processing goroutine finished normally
	case <-time.After(5 * time.Second):
		// Timeout reached, log and continue
		es.logger.Warn("Timeout waiting for subscription processing goroutine to finish")
	}
	
	return nil
}

// IsClosed returns true if the subscription is closed
func (es *EventSubscription) IsClosed() bool {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.closed
}

// EventSubscriptionBuilder helps build event subscriptions
type EventSubscriptionBuilder struct {
	stream  grpc.ClientStream
	handler EventHandler
	logger  Logger
	cancel  context.CancelFunc
}

// NewEventSubscriptionBuilder creates a new EventSubscriptionBuilder
func NewEventSubscriptionBuilder() *EventSubscriptionBuilder {
	return &EventSubscriptionBuilder{}
}

// WithStream sets the stream for the subscription
func (b *EventSubscriptionBuilder) WithStream(stream grpc.ClientStream) *EventSubscriptionBuilder {
	b.stream = stream
	return b
}

// WithHandler sets the event handler for the subscription
func (b *EventSubscriptionBuilder) WithHandler(handler EventHandler) *EventSubscriptionBuilder {
	b.handler = handler
	return b
}

// WithLogger sets the logger for the subscription
func (b *EventSubscriptionBuilder) WithLogger(logger Logger) *EventSubscriptionBuilder {
	b.logger = logger
	return b
}

// WithCancel sets the cancel function for the subscription
func (b *EventSubscriptionBuilder) WithCancel(cancel context.CancelFunc) *EventSubscriptionBuilder {
	b.cancel = cancel
	return b
}

// Build creates the EventSubscription
func (b *EventSubscriptionBuilder) Build() *EventSubscription {
	return NewEventSubscription(b.stream, b.handler, b.logger, b.cancel)
}

// SimpleEventHandler provides a simple implementation of EventHandler
type SimpleEventHandler struct {
	onEventFunc    func(event *eventstore.Event) error
	onErrorFunc    func(err error)
	onCompletedFunc func()
}

// NewSimpleEventHandler creates a new SimpleEventHandler
func NewSimpleEventHandler() *SimpleEventHandler {
	return &SimpleEventHandler{}
}

// WithOnEvent sets the OnEvent function
func (h *SimpleEventHandler) WithOnEvent(fn func(event *eventstore.Event) error) *SimpleEventHandler {
	h.onEventFunc = fn
	return h
}

// WithOnError sets the OnError function
func (h *SimpleEventHandler) WithOnError(fn func(err error)) *SimpleEventHandler {
	h.onErrorFunc = fn
	return h
}

// WithOnCompleted sets the OnCompleted function
func (h *SimpleEventHandler) WithOnCompleted(fn func()) *SimpleEventHandler {
	h.onCompletedFunc = fn
	return h
}

// OnEvent implements EventHandler
func (h *SimpleEventHandler) OnEvent(event *eventstore.Event) error {
	if h.onEventFunc != nil {
		return h.onEventFunc(event)
	}
	return nil
}

// OnError implements EventHandler
func (h *SimpleEventHandler) OnError(err error) {
	if h.onErrorFunc != nil {
		h.onErrorFunc(err)
	}
}

// OnCompleted implements EventHandler
func (h *SimpleEventHandler) OnCompleted() {
	if h.onCompletedFunc != nil {
		h.onCompletedFunc()
	}
}