package sqlite

import (
	"context"
	"sync/atomic"

	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
)

// QueuedEventStore wraps SQLiteSaveEvents with a request queue to serialize writes
// This eliminates write lock contention by processing requests sequentially
type QueuedEventStore struct {
	saveEvents   *SQLiteSaveEvents
	requestQueue chan saveRequest
	workerDone   chan struct{}
	started      atomic.Value
	logger       logging.Logger
}

type saveRequest struct {
	ctx                        context.Context
	events                     []orisun.EventWithMapTags
	boundary                   string
	expectedPosition           *orisun.Position
	streamConsistencyCondition *orisun.Query
	result                     chan saveResult
}

type saveResult struct {
	transactionID string
	globalID      int64
	err           error
}

// NewQueuedEventStore creates a new queued event store with the given queue size
func NewQueuedEventStore(saveEvents *SQLiteSaveEvents, queueSize int, logger logging.Logger) *QueuedEventStore {
	q := &QueuedEventStore{
		saveEvents:   saveEvents,
		requestQueue: make(chan saveRequest, queueSize),
		workerDone:   make(chan struct{}),
		logger:       logger,
	}
	q.started.Store(false)
	return q
}

// Start begins the background worker that processes queued requests
func (q *QueuedEventStore) Start() {
	if q.started.Load().(bool) {
		return
	}
	q.started.Store(true)
	go q.worker()
}

// Stop gracefully shuts down the worker after processing all queued requests
func (q *QueuedEventStore) Stop() {
	if !q.started.Load().(bool) {
		return
	}
	close(q.requestQueue)
	<-q.workerDone
	q.started.Store(false)
}

// Save queues a save request and waits for the result
func (q *QueuedEventStore) Save(
	ctx context.Context,
	events []orisun.EventWithMapTags,
	boundary string,
	expectedPosition *orisun.Position,
	streamConsistencyCondition *orisun.Query,
) (transactionID string, globalID int64, err error) {
	// Ensure worker is started
	if !q.started.Load().(bool) {
		q.logger.Warn("QueuedEventStore not started, starting automatically")
		q.Start()
	}

	// Create result channel with buffer to avoid deadlock if worker fails
	resultChan := make(chan saveResult, 1)

	// Create and queue the request
	req := saveRequest{
		ctx:                        ctx,
		events:                     events,
		boundary:                   boundary,
		expectedPosition:           expectedPosition,
		streamConsistencyCondition: streamConsistencyCondition,
		result:                     resultChan,
	}

	select {
	case q.requestQueue <- req:
		// Request queued successfully, wait for result
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}

	// Wait for result or context cancellation
	select {
	case result := <-resultChan:
		return result.transactionID, result.globalID, result.err
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}
}

// worker processes save requests sequentially
func (q *QueuedEventStore) worker() {
	defer close(q.workerDone)

	q.logger.Info("QueuedEventStore worker started")

	for req := range q.requestQueue {
		// Check if context is already cancelled
		if err := req.ctx.Err(); err != nil {
			req.result <- saveResult{"", 0, err}
			continue
		}

		// Process the save request
		txID, gid, err := q.saveEvents.Save(
			req.ctx,
			req.events,
			req.boundary,
			req.expectedPosition,
			req.streamConsistencyCondition,
		)

		// Send result (with select to avoid blocking if result channel is closed)
		select {
		case req.result <- saveResult{txID, gid, err}:
		default:
			q.logger.Warn("Failed to send save result: channel closed or full")
		}
	}

	q.logger.Info("QueuedEventStore worker stopped")
}

// GetQueueSize returns the current number of queued requests
func (q *QueuedEventStore) GetQueueSize() int {
	return len(q.requestQueue)
}

// GetQueueCapacity returns the maximum queue size
func (q *QueuedEventStore) GetQueueCapacity() int {
	return cap(q.requestQueue)
}
