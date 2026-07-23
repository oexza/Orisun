package boundary_provisioning

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
)

func TestBoundaryProvisioningSubscriberReplaysIntoLocalRuntime(t *testing.T) {
	event := coreeventstore.ReadEvent{
		EventID: "event-1", EventType: adminevents.EventTypeBoundaryCreated,
		Data:     `{"boundary":"sales"}`,
		Position: coreeventstore.Position{CommitPosition: 8, PreparePosition: 9},
	}
	handled := make(chan coreeventstore.ReadEvent, 1)
	subscriber := NewBoundaryProvisioningSubscriber(
		"orisun_admin",
		&definitionBatchRetriever{events: coreeventstore.ReadEventBatch{event}},
		unusedDefinitionSubscription,
		func(_ context.Context, event coreeventstore.ReadEvent) error {
			handled <- event
			return nil
		},
		subscriberTestLogger{},
	)

	if err := subscriber.Replay(t.Context()); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if got := <-handled; got.EventID != event.EventID {
		t.Fatalf("handled event = %#v", got)
	}
	if got := subscriber.currentPosition(); got.CommitPosition != 8 || got.PreparePosition != 9 {
		t.Fatalf("position = %d/%d", got.CommitPosition, got.PreparePosition)
	}
	if !strings.HasPrefix(subscriber.subscriberName, boundaryProvisioningSubscriberName+"-") {
		t.Fatalf("subscriber name = %q", subscriber.subscriberName)
	}
}

func TestBoundaryProvisioningSubscriberRetriesFailureWithoutBlockingLaterDefinitions(t *testing.T) {
	events := coreeventstore.ReadEventBatch{
		{EventID: "failed", EventType: adminevents.EventTypeBoundaryCreated, Position: coreeventstore.Position{CommitPosition: 1, PreparePosition: 1}},
		{EventID: "later", EventType: adminevents.EventTypeBoundaryCreated, Position: coreeventstore.Position{CommitPosition: 2, PreparePosition: 1}},
	}
	var failedCalls atomic.Int32
	laterHandled := make(chan struct{}, 1)
	retrySucceeded := make(chan struct{}, 1)
	subscriber := NewBoundaryProvisioningSubscriber(
		"orisun_admin",
		&definitionBatchRetriever{events: events},
		unusedDefinitionSubscription,
		func(_ context.Context, event coreeventstore.ReadEvent) error {
			switch event.EventID {
			case "failed":
				if failedCalls.Add(1) == 1 {
					return errors.New("provision failed")
				}
				retrySucceeded <- struct{}{}
			case "later":
				laterHandled <- struct{}{}
			}
			return nil
		},
		subscriberTestLogger{},
	)

	if err := subscriber.Replay(t.Context()); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	select {
	case <-laterHandled:
	case <-time.After(time.Second):
		t.Fatal("later definition was blocked by failed boundary")
	}
	select {
	case <-retrySucceeded:
	case <-time.After(time.Second):
		t.Fatal("failed definition was not retried")
	}
	if got := subscriber.currentPosition(); got.CommitPosition != 2 || got.PreparePosition != 1 {
		t.Fatalf("position = %d/%d", got.CommitPosition, got.PreparePosition)
	}
}

func TestBoundaryProvisioningSubscribersUseIndependentRuntimeIdentities(t *testing.T) {
	first := NewBoundaryProvisioningSubscriber("admin", &definitionBatchRetriever{}, unusedDefinitionSubscription, func(context.Context, coreeventstore.ReadEvent) error { return nil }, subscriberTestLogger{})
	second := NewBoundaryProvisioningSubscriber("admin", &definitionBatchRetriever{}, unusedDefinitionSubscription, func(context.Context, coreeventstore.ReadEvent) error { return nil }, subscriberTestLogger{})
	if first.subscriberName == second.subscriberName {
		t.Fatalf("runtime subscriber names must be unique: %q", first.subscriberName)
	}
}

func TestBoundaryProvisioningRetrySurvivesLiveSubscriptionRestart(t *testing.T) {
	wantSubscriptionErr := errors.New("live subscription disconnected")
	firstAttempt := make(chan struct{}, 1)
	retrySucceeded := make(chan struct{}, 1)
	var calls atomic.Int32
	subscriber := NewBoundaryProvisioningSubscriber(
		"orisun_admin",
		&definitionBatchRetriever{},
		func(ctx context.Context, _ coreeventstore.SubscribeRequest, handle func(context.Context, coreeventstore.ReadEvent) error) error {
			if err := handle(ctx, coreeventstore.ReadEvent{
				EventID: "live-failure", EventType: adminevents.EventTypeBoundaryCreated,
				Position: coreeventstore.Position{CommitPosition: 4, PreparePosition: 2},
			}); err != nil {
				return err
			}
			select {
			case <-firstAttempt:
				return wantSubscriptionErr
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		func(context.Context, coreeventstore.ReadEvent) error {
			if calls.Add(1) == 1 {
				firstAttempt <- struct{}{}
				return errors.New("temporary provisioning failure")
			}
			retrySucceeded <- struct{}{}
			return nil
		},
		subscriberTestLogger{},
	)

	if err := subscriber.Start(t.Context()); !errors.Is(err, wantSubscriptionErr) {
		t.Fatalf("Start() error = %v, want %v", err, wantSubscriptionErr)
	}
	select {
	case <-retrySucceeded:
	case <-time.After(time.Second):
		t.Fatal("provisioning retry was canceled with the live subscription")
	}
}

type definitionBatchRetriever struct {
	events coreeventstore.ReadEventBatch
}

func (r *definitionBatchRetriever) Read(_ context.Context, req coreeventstore.ReadRequest) (coreeventstore.ReadEventBatch, error) {
	result := make(coreeventstore.ReadEventBatch, 0, len(r.events))
	for _, event := range r.events {
		if req.FromPosition == nil || event.Position.After(*req.FromPosition) {
			result = append(result, event)
		}
	}
	return result, nil
}

func unusedDefinitionSubscription(context.Context, coreeventstore.SubscribeRequest, func(context.Context, coreeventstore.ReadEvent) error) error {
	return nil
}

type subscriberTestLogger struct{}

func (subscriberTestLogger) IsDebugEnabled() bool  { return false }
func (subscriberTestLogger) Debug(...any)          {}
func (subscriberTestLogger) Debugf(string, ...any) {}
func (subscriberTestLogger) Info(...any)           {}
func (subscriberTestLogger) Infof(string, ...any)  {}
func (subscriberTestLogger) Warn(...any)           {}
func (subscriberTestLogger) Warnf(string, ...any)  {}
func (subscriberTestLogger) Error(...any)          {}
func (subscriberTestLogger) Errorf(string, ...any) {}
func (subscriberTestLogger) Fatal(...any)          {}
func (subscriberTestLogger) Fatalf(string, ...any) {}
