package orisun

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
)

type contextOnlyLockProvider struct {
	acquired chan struct{}
	released chan struct{}
	once     sync.Once
}

func newContextOnlyLockProvider() *contextOnlyLockProvider {
	return &contextOnlyLockProvider{
		acquired: make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (p *contextOnlyLockProvider) Lock(ctx context.Context, _ string) error {
	p.once.Do(func() {
		close(p.acquired)
		go func() {
			<-ctx.Done()
			close(p.released)
		}()
	})
	return nil
}

func TestSubscribeToAllEventsCancelsLockContextOnEarlyError(t *testing.T) {
	lockProvider := newContextOnlyLockProvider()
	retriever := &fakeRetriever{errOnce: true}
	store := &EventStore{
		getEventsFn:  retriever,
		lockProvider: lockProvider,
		logger:       noopLogger{},
	}
	handlerCtx, cancelHandler := context.WithCancel(context.Background())
	defer cancelHandler()

	err := store.SubscribeToAllEvents(
		context.Background(),
		"orders",
		"subscriber",
		&Position{},
		nil,
		NewMessageHandler[Event](handlerCtx),
	)
	if err == nil {
		t.Fatal("expected catch-up read error")
	}

	select {
	case <-lockProvider.released:
	case <-time.After(time.Second):
		t.Fatal("lock context was not cancelled after an early subscription error")
	}
}

type recordingLeaseProvider struct {
	lease *recordingLease
}

func (p *recordingLeaseProvider) Lock(ctx context.Context, lockName string) error {
	_, err := p.AcquireLock(ctx, lockName)
	return err
}

func (p *recordingLeaseProvider) AcquireLock(ctx context.Context, _ string) (LockLease, error) {
	leaseCtx, cancel := context.WithCancel(ctx)
	p.lease = &recordingLease{ctx: leaseCtx, cancel: cancel}
	return p.lease, nil
}

type recordingLease struct {
	ctx      context.Context
	cancel   context.CancelFunc
	released atomic.Bool
}

func (l *recordingLease) Context() context.Context { return l.ctx }
func (l *recordingLease) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.ctx.Err()
}
func (l *recordingLease) Release() {
	l.released.Store(true)
	l.cancel()
}

type failingSendSubscriptionStream struct {
	grpc.ServerStream
	ctx   context.Context
	sends atomic.Int32
}

func (s *failingSendSubscriptionStream) Context() context.Context {
	return s.ctx
}

func (s *failingSendSubscriptionStream) Send(*Event) error {
	s.sends.Add(1)
	return errors.New("injected transport failure")
}

type blockingAfterFirstBatchRetriever struct {
	batch ReadEventBatch
	calls atomic.Int32
}

func (r *blockingAfterFirstBatchRetriever) GetBatch(ctx context.Context, _ *GetEventsRequest) (ReadEventBatch, error) {
	if r.calls.Add(1) == 1 {
		return r.batch, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *blockingAfterFirstBatchRetriever) GetLatestByCriteria(
	context.Context,
	LatestByCriteriaQuery,
) (LatestByCriteriaBatch, error) {
	return LatestByCriteriaBatch{}, errors.New("not implemented")
}

func TestCatchUpSubscriptionSendFailureTerminatesAndReleasesLease(t *testing.T) {
	retriever := &blockingAfterFirstBatchRetriever{}
	for i := 1; i <= 100; i++ {
		retriever.batch = append(retriever.batch, ReadEvent{
			EventId:         "event",
			EventType:       "TestEvent",
			Data:            "{}",
			Metadata:        "{}",
			CommitPosition:  int64(i),
			PreparePosition: int64(i),
			DateCreated:     time.Now(),
		})
	}
	lockProvider := &recordingLeaseProvider{}
	store := &EventStore{
		getEventsFn:  retriever,
		lockProvider: lockProvider,
		logger:       noopLogger{},
	}
	stream := &failingSendSubscriptionStream{ctx: context.Background()}

	done := make(chan error, 1)
	go func() {
		done <- store.CatchUpSubscribeToEvents(&CatchUpSubscribeToEventStoreRequest{
			Boundary:       "orders",
			SubscriberName: "subscriber",
			AfterPosition:  &Position{},
		}, stream)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected stream send error")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not terminate after stream send failure")
	}
	if stream.sends.Load() != 1 {
		t.Fatalf("stream send attempts = %d, want 1", stream.sends.Load())
	}
	if lockProvider.lease == nil || !lockProvider.lease.released.Load() {
		t.Fatal("subscription lease was not released")
	}
}
