package orisun

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles ---------------------------------------------------------

type noopLogger struct{}

func (noopLogger) Debug(...any)          {}
func (noopLogger) Debugf(string, ...any) {}
func (noopLogger) Info(...any)           {}
func (noopLogger) Infof(string, ...any)  {}
func (noopLogger) Warn(...any)           {}
func (noopLogger) Warnf(string, ...any)  {}
func (noopLogger) Error(...any)          {}
func (noopLogger) Errorf(string, ...any) {}
func (noopLogger) Fatal(...any)          {}
func (noopLogger) Fatalf(string, ...any) {}

// fakeRetriever serves events with PreparePosition >= req.FromPosition, capped
// at req.Count, mimicking the position-inclusive paginated Get.
type fakeRetriever struct {
	mu      sync.Mutex
	events  []*Event
	calls   int
	errOnce bool
}

func (r *fakeRetriever) add(events ...*Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
}

func (r *fakeRetriever) Get(ctx context.Context, req *GetEventsRequest) (*GetEventsResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.errOnce {
		r.errOnce = false
		return nil, errors.New("transient get error")
	}
	from := req.FromPosition.PreparePosition
	var out []*Event
	for _, e := range r.events {
		if e.Position == nil || e.Position.PreparePosition >= from {
			out = append(out, e)
			if uint32(len(out)) >= req.Count {
				break
			}
		}
	}
	return &GetEventsResponse{Events: out}, nil
}

// fakeJS embeds jetstream.JetStream so only Publish needs an implementation;
// any other method call would nil-panic, which is fine — the loop only Publishes.
type fakeJS struct {
	jetstream.JetStream
	mu        sync.Mutex
	published [][]byte
	attempts  int
	failFirst int
}

func (f *fakeJS) Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.failFirst > 0 {
		f.failFirst--
		return nil, errors.New("nats unavailable")
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.published = append(f.published, cp)
	return &jetstream.PubAck{}, nil
}

func (f *fakeJS) publishedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.published))
	for _, p := range f.published {
		var e struct {
			EventID string `json:"event_id"`
		}
		_ = json.Unmarshal(p, &e)
		ids = append(ids, e.EventID)
	}
	return ids
}

func (f *fakeJS) publishedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.published)
}

type fakeTracker struct {
	mu        sync.Mutex
	startTx   int64
	startPrep int64
	inserts   []Position
}

func (t *fakeTracker) GetLastPublishedEventPosition(ctx context.Context, boundary string) (Position, error) {
	return Position{CommitPosition: t.startTx, PreparePosition: t.startPrep}, nil
}

func (t *fakeTracker) InsertLastPublishedEvent(ctx context.Context, boundary string, transactionId int64, globalId int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inserts = append(t.inserts, Position{CommitPosition: transactionId, PreparePosition: globalId})
	return nil
}

func (t *fakeTracker) insertCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.inserts)
}

// pulseSignal wakes the loop repeatedly, simulating polling/NOTIFY ticks.
type pulseSignal struct {
	interval time.Duration
	stopped  atomic.Bool
}

func (s *pulseSignal) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.interval):
		return nil
	}
}

func (s *pulseSignal) Stop() { s.stopped.Store(true) }

func makeEvent(i int) *Event {
	return &Event{
		EventId:   "e" + string(rune('0'+i)),
		EventType: "TestEvent",
		Data:      "{}",
		Position:  &Position{CommitPosition: int64(i), PreparePosition: int64(i)},
	}
}

// --- tests ----------------------------------------------------------------

func TestPublishEventsLoop_DrainsInOrder(t *testing.T) {
	retriever := &fakeRetriever{}
	for i := 1; i <= 3; i++ {
		retriever.add(makeEvent(i))
	}
	js := &fakeJS{}
	tracker := &fakeTracker{}
	signal := &pulseSignal{interval: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- publishEventsLoop(ctx, js, retriever, 10, &Position{}, "b", tracker, signal, noopLogger{})
	}()

	require.Eventually(t, func() bool { return js.publishedCount() == 3 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	assert.ErrorIs(t, <-errCh, context.Canceled)

	assert.Equal(t, []string{"e1", "e2", "e3"}, js.publishedIDs(), "events must publish in position order")
	assert.Equal(t, 3, tracker.insertCount(), "each published event records its position")
	assert.True(t, signal.stopped.Load(), "signal.Stop must be called on exit")
}

func TestPublishEventsLoop_Paginates(t *testing.T) {
	retriever := &fakeRetriever{}
	for i := 1; i <= 5; i++ {
		retriever.add(makeEvent(i))
	}
	js := &fakeJS{}
	tracker := &fakeTracker{}
	signal := &pulseSignal{interval: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- publishEventsLoop(ctx, js, retriever, 2, &Position{}, "b", tracker, signal, noopLogger{})
	}()

	require.Eventually(t, func() bool { return js.publishedCount() == 5 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-errCh

	assert.Equal(t, []string{"e1", "e2", "e3", "e4", "e5"}, js.publishedIDs())
	retriever.mu.Lock()
	calls := retriever.calls
	retriever.mu.Unlock()
	assert.GreaterOrEqual(t, calls, 3, "batchSize=2 over 5 events needs multiple paginated Gets")
}

func TestPublishEventsLoop_RetriesFailedPublish(t *testing.T) {
	retriever := &fakeRetriever{}
	retriever.add(makeEvent(1))
	js := &fakeJS{failFirst: 2}
	tracker := &fakeTracker{}
	signal := &pulseSignal{interval: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- publishEventsLoop(ctx, js, retriever, 10, &Position{}, "b", tracker, signal, noopLogger{})
	}()

	require.Eventually(t, func() bool { return js.publishedCount() == 1 }, 3*time.Second, 10*time.Millisecond)
	cancel()
	<-errCh

	js.mu.Lock()
	attempts := js.attempts
	js.mu.Unlock()
	assert.GreaterOrEqual(t, attempts, 3, "2 failures then success = >=3 publish attempts")
	assert.Equal(t, 1, tracker.insertCount(), "position recorded only after successful publish")
}

func TestPublishEventsLoop_RecoversFromGetError(t *testing.T) {
	retriever := &fakeRetriever{errOnce: true}
	retriever.add(makeEvent(1), makeEvent(2))
	js := &fakeJS{}
	tracker := &fakeTracker{}
	signal := &pulseSignal{interval: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- publishEventsLoop(ctx, js, retriever, 10, &Position{}, "b", tracker, signal, noopLogger{})
	}()

	require.Eventually(t, func() bool { return js.publishedCount() == 2 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-errCh

	assert.Equal(t, []string{"e1", "e2"}, js.publishedIDs(), "loop continues after a transient Get error")
}

func TestPublishEventsLoop_ResumesFromLastPosition(t *testing.T) {
	retriever := &fakeRetriever{}
	for i := 1; i <= 3; i++ {
		retriever.add(makeEvent(i))
	}
	js := &fakeJS{}
	tracker := &fakeTracker{}
	signal := &pulseSignal{interval: time.Millisecond}

	// Already published up to position 2 — only event 3 should publish.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- publishEventsLoop(ctx, js, retriever, 10, &Position{CommitPosition: 2, PreparePosition: 2}, "b", tracker, signal, noopLogger{})
	}()

	require.Eventually(t, func() bool { return js.publishedCount() == 1 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-errCh

	assert.Equal(t, []string{"e3"}, js.publishedIDs(), "must resume after the last published position")
}

func TestPublishEventsLoop_RejectsOutOfOrderBatchBeforePublishing(t *testing.T) {
	retriever := &fakeRetriever{}
	retriever.add(makeEvent(2), makeEvent(1))
	js := &fakeJS{}
	tracker := &fakeTracker{}
	signal := &pulseSignal{interval: time.Millisecond}

	err := publishEventsLoop(context.Background(), js, retriever, 10, &Position{}, "b", tracker, signal, noopLogger{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not after cursor")
	assert.Equal(t, 0, js.publishedCount(), "invalid batches must not publish a partial prefix")
	assert.Equal(t, 0, tracker.insertCount(), "invalid batches must not advance the checkpoint")
	assert.True(t, signal.stopped.Load())
}

func TestPublishEventsLoop_RejectsNilPositionBeforePublishing(t *testing.T) {
	retriever := &fakeRetriever{}
	event := makeEvent(1)
	event.Position = nil
	retriever.add(event)
	js := &fakeJS{}
	tracker := &fakeTracker{}
	signal := &pulseSignal{interval: time.Millisecond}

	err := publishEventsLoop(context.Background(), js, retriever, 10, &Position{}, "b", tracker, signal, noopLogger{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil position")
	assert.Equal(t, 0, js.publishedCount())
	assert.Equal(t, 0, tracker.insertCount())
	assert.True(t, signal.stopped.Load())
}

func TestPublishEventsLoop_ExitsOnCanceledContext(t *testing.T) {
	js := &fakeJS{}
	signal := &pulseSignal{interval: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := publishEventsLoop(ctx, js, &fakeRetriever{}, 10, &Position{}, "b", &fakeTracker{}, signal, noopLogger{})
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, signal.stopped.Load())
}

func TestPollingSignal(t *testing.T) {
	t.Run("fires on tick", func(t *testing.T) {
		s := NewPollingSignal(5 * time.Millisecond)
		defer s.Stop()
		require.NoError(t, s.Wait(context.Background()))
	})

	t.Run("returns on canceled context", func(t *testing.T) {
		s := NewPollingSignal(time.Hour)
		defer s.Stop()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.ErrorIs(t, s.Wait(ctx), context.Canceled)
	})
}
