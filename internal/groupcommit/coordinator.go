// Package groupcommit owns bounded admission and ordered batch lifecycles.
// Backends own prepared input, transactional execution, and per-request outcomes.
package groupcommit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrClosed = errors.New("group commit coordinator is shut down")

type Config struct {
	MaxBatchRequests int
	MaxBatchEvents   int
	MaxPending       int
	MaxDelay         time.Duration
	FlushTimeout     time.Duration
}

// Executor must return exactly one outcome per input in input order, only after
// transaction completion. A returned error invalidates the entire outcome set.
// A caller cancellation never cancels the shared transaction.
type Executor[P, R any] func(context.Context, string, []P) ([]R, error)

type Coordinator[P, R any] struct {
	maxBatchRequests, maxBatchEvents, maxPending int
	maxDelay, flushTimeout                       time.Duration
	execute                                      Executor[P, R]
	queues                                       map[string]chan *request[P, R]
	closed                                       chan struct{}
	closeOnce                                    sync.Once
	enqueueMu                                    sync.RWMutex
	senders                                      sync.WaitGroup
	workerWG                                     sync.WaitGroup
}

type request[P, R any] struct {
	ctx       context.Context
	prepared  P
	events    int
	result    chan completion[R]
	delivered bool
}
type completion[R any] struct {
	value R
	err   error
}

func (r *request[P, R]) deliver(result completion[R]) {
	if r.delivered {
		return
	}
	r.delivered = true
	r.result <- result
}
func fail[P, R any](batch []*request[P, R], err error) {
	for _, req := range batch {
		req.deliver(completion[R]{err: err})
	}
}

// New requires normalized positive bounds; backend configuration owns defaults.
func New[P, R any](cfg Config, execute Executor[P, R]) *Coordinator[P, R] {
	if cfg.MaxBatchRequests <= 0 || cfg.MaxBatchEvents <= 0 || cfg.MaxPending <= 0 || cfg.FlushTimeout <= 0 || cfg.MaxDelay < 0 || execute == nil {
		panic("groupcommit: invalid configuration")
	}
	return &Coordinator[P, R]{maxBatchRequests: cfg.MaxBatchRequests, maxBatchEvents: cfg.MaxBatchEvents, maxPending: cfg.MaxPending, maxDelay: cfg.MaxDelay, flushTimeout: cfg.FlushTimeout, execute: execute, queues: make(map[string]chan *request[P, R]), closed: make(chan struct{})}
}

// Pending reports queued requests, excluding the batch currently executing.
func (s *Coordinator[P, R]) Pending(boundary string) int {
	s.enqueueMu.RLock()
	defer s.enqueueMu.RUnlock()
	return len(s.queues[boundary])
}

func (s *Coordinator[P, R]) Submit(ctx context.Context, boundary string, prepared P, events int) (R, error) {
	var zero R
	if s.Closed() {
		return zero, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if events <= 0 {
		return zero, errors.New("groupcommit: request must contain events")
	}
	s.enqueueMu.Lock()
	if s.Closed() {
		s.enqueueMu.Unlock()
		return zero, ErrClosed
	}
	queue := s.queues[boundary]
	if queue == nil {
		queue = make(chan *request[P, R], s.maxPending)
		s.queues[boundary] = queue
		s.workerWG.Add(1)
		go s.runWorker(boundary, queue)
	}
	s.senders.Add(1)
	s.enqueueMu.Unlock()
	req := &request[P, R]{ctx: ctx, prepared: prepared, events: events, result: make(chan completion[R], 1)}
	// Admission is registered before releasing the registry lock. Waiting for
	// queue space holds no registry lock, so a full boundary cannot block others.
	select {
	case queue <- req:
		s.senders.Done()
	case <-ctx.Done():
		s.senders.Done()
		return zero, ctx.Err()
	case <-s.closed:
		s.senders.Done()
		return zero, ErrClosed
	}

	select {
	case result := <-req.result:
		return result.value, result.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func (s *Coordinator[P, R]) runWorker(boundary string, queue chan *request[P, R]) {
	defer s.workerWG.Done()

	var carry *request[P, R]
	for {
		req := carry
		carry = nil
		if req == nil {
			select {
			case <-s.closed:
				s.failFast(queue)
				return
			case next, ok := <-queue:
				if !ok {
					return
				}
				req = next
			}
		}
		if s.Closed() {
			req.deliver(completion[R]{err: ErrClosed})
			s.failFast(queue)
			return
		}

		var batch []*request[P, R]
		batch, carry = s.drainBatch(queue, req)
		if s.Closed() {
			fail(batch, ErrClosed)
			if carry != nil {
				carry.deliver(completion[R]{err: ErrClosed})
			}
			s.failFast(queue)
			return
		}
		s.runFlush(boundary, batch)
		if s.Closed() {
			if carry != nil {
				carry.deliver(completion[R]{err: ErrClosed})
			}
			s.failFast(queue)
			return
		}
	}
}

func (s *Coordinator[P, R]) failFast(queue chan *request[P, R]) {
	for req := range queue {
		req.deliver(completion[R]{err: ErrClosed})
	}
}

func (s *Coordinator[P, R]) drainBatch(
	queue chan *request[P, R],
	first *request[P, R],
) ([]*request[P, R], *request[P, R]) {
	batch := []*request[P, R]{first}
	events := first.events

	var delay <-chan time.Time
	if s.maxDelay > 0 {
		timer := time.NewTimer(s.maxDelay)
		defer timer.Stop()
		delay = timer.C
	}

	for len(batch) < s.maxBatchRequests && events < s.maxBatchEvents {
		if delay == nil {
			select {
			case req, ok := <-queue:
				if !ok {
					return batch, nil
				}
				if events+req.events > s.maxBatchEvents {
					return batch, req
				}
				batch = append(batch, req)
				events += req.events
			default:
				return batch, nil
			}
			continue
		}
		select {
		case req, ok := <-queue:
			if !ok {
				return batch, nil
			}
			if events+req.events > s.maxBatchEvents {
				return batch, req
			}
			batch = append(batch, req)
			events += req.events
		case <-delay:
			return batch, nil
		case <-s.closed:
			return batch, nil
		}
	}
	return batch, nil
}

func (s *Coordinator[P, R]) runFlush(boundary string, batch []*request[P, R]) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fail(batch, fmt.Errorf("group commit flush panic: %v", recovered))
		}
	}()
	live := make([]*request[P, R], 0, len(batch))
	prepared := make([]P, 0, len(batch))
	for _, req := range batch {
		if err := req.ctx.Err(); err != nil {
			req.deliver(completion[R]{err: err})
			continue
		}
		live = append(live, req)
		prepared = append(prepared, req.prepared)
	}
	if len(live) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.flushTimeout)
	defer cancel()
	outcomes, err := s.execute(ctx, boundary, prepared)
	if err != nil {
		fail(live, fmt.Errorf("group commit flush: %w", err))
		return
	}
	// Validate the entire result before completing any caller successfully.
	if len(outcomes) != len(live) {
		fail(live, fmt.Errorf("group commit returned %d outcomes for %d requests", len(outcomes), len(live)))
		return
	}
	for i, outcome := range outcomes {
		live[i].deliver(completion[R]{value: outcome})
	}
}

func (s *Coordinator[P, R]) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)

		s.enqueueMu.Lock()
		queues := make([]chan *request[P, R], 0, len(s.queues))
		for _, queue := range s.queues {
			queues = append(queues, queue)
		}
		s.enqueueMu.Unlock()

		// No sender can register after closed is signalled and the registry
		// lock has been acquired. Settle existing admissions before closing.
		s.senders.Wait()
		for _, queue := range queues {
			close(queue)
		}
		s.workerWG.Wait()
	})
}

func (s *Coordinator[P, R]) Closed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// Config returns a copy of the immutable batching policy.
func (s *Coordinator[P, R]) Config() Config {
	return Config{MaxBatchRequests: s.maxBatchRequests, MaxBatchEvents: s.maxBatchEvents, MaxPending: s.maxPending, MaxDelay: s.maxDelay, FlushTimeout: s.flushTimeout}
}
