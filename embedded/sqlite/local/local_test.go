package local

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OrisunLabs/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testBoundary = "mobile"

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), t.TempDir(), []string{testBoundary}, nil)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func testEvent(id, value string) orisun.EventWithMapTags {
	return orisun.EventWithMapTags{
		EventId:   id,
		EventType: "ValueSet",
		Data:      map[string]any{"key": "counter", "value": value},
		Metadata:  map[string]any{"source": "mobile"},
	}
}

func TestStoreSavesReadsAndEnforcesCCCWithoutNATS(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	query := &orisun.Query{Criteria: []*orisun.Criterion{{Tags: []*orisun.Tag{{Key: "key", Value: "counter"}}}}}
	notExists := orisun.NotExistsPosition()

	position, err := store.SaveEvents(ctx, []orisun.EventWithMapTags{testEvent("event-1", "one")}, testBoundary, &notExists, query)
	if err != nil {
		t.Fatalf("SaveEvents() returned error: %v", err)
	}
	if position.CommitPosition != 1 || position.PreparePosition != 1 {
		t.Fatalf("unexpected position: %+v", position)
	}

	_, err = store.SaveEvents(ctx, []orisun.EventWithMapTags{testEvent("event-2", "two")}, testBoundary, &notExists, query)
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("stale SaveEvents() code = %v, want %v (err=%v)", status.Code(err), codes.AlreadyExists, err)
	}

	batch, err := store.GetEvents(ctx, &orisun.GetEventsRequest{
		Boundary:  testBoundary,
		Count:     10,
		Direction: orisun.Direction_ASC,
	})
	if err != nil {
		t.Fatalf("GetEvents() returned error: %v", err)
	}
	if len(batch) != 1 || batch[0].EventId != "event-1" {
		t.Fatalf("unexpected events: %+v", batch)
	}
}

func TestStoreSubscriptionCatchesUpFromSQLiteNotifications(t *testing.T) {
	store := openTestStore(t)
	subscriptionCtx, cancelSubscription := context.WithCancel(context.Background())
	defer cancelSubscription()

	events := make(chan orisun.ReadEvent, 1)
	errs := make(chan error, 1)
	after := orisun.NotExistsPosition()
	go func() {
		errs <- store.Subscribe(subscriptionCtx, testBoundary, "ui", &after, nil, func(event orisun.ReadEvent) error {
			events <- event
			return nil
		})
	}()

	if _, err := store.SaveEvents(context.Background(), []orisun.EventWithMapTags{testEvent("event-live", "one")}, testBoundary, nil, nil); err != nil {
		t.Fatalf("SaveEvents() returned error: %v", err)
	}

	select {
	case event := <-events:
		if event.EventId != "event-live" {
			t.Fatalf("event ID = %q, want event-live", event.EventId)
		}
	case err := <-errs:
		t.Fatalf("Subscribe() returned early: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for locally subscribed event")
	}

	cancelSubscription()
	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Subscribe() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not stop after cancellation")
	}
}

func TestStoreSubscriptionRejectsDuplicateName(t *testing.T) {
	store := openTestStore(t)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	started := make(chan struct{})
	firstErr := make(chan error, 1)
	after := orisun.NotExistsPosition()
	go func() {
		firstErr <- store.Subscribe(firstCtx, testBoundary, "same", &after, nil, func(orisun.ReadEvent) error {
			return nil
		})
	}()

	// Wait until the first subscription owns the name by probing the same
	// process-local lease through the store.
	leaseProvider := store.lockProvider.(orisun.LockLeaseProvider)
	deadline := time.Now().Add(time.Second)
	for {
		lease, err := leaseProvider.AcquireLock(context.Background(), testBoundary+"__same")
		if err != nil {
			close(started)
			break
		}
		lease.Release()
		if time.Now().After(deadline) {
			t.Fatal("first subscription did not acquire its lock")
		}
		time.Sleep(time.Millisecond)
	}
	<-started

	err := store.Subscribe(context.Background(), testBoundary, "same", &after, nil, func(orisun.ReadEvent) error {
		return nil
	})
	if err == nil {
		t.Fatal("duplicate Subscribe() succeeded")
	}

	cancelFirst()
	select {
	case <-firstErr:
	case <-time.After(time.Second):
		t.Fatal("first subscription did not stop")
	}
}

func TestStoreCloseStopsSubscription(t *testing.T) {
	store := openTestStore(t)
	errCh := make(chan error, 1)
	after := orisun.NotExistsPosition()
	go func() {
		errCh <- store.Subscribe(context.Background(), testBoundary, "close", &after, nil, func(orisun.ReadEvent) error {
			return nil
		})
	}()

	time.Sleep(10 * time.Millisecond)
	store.Close()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Subscribe() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not stop when store closed")
	}
}
