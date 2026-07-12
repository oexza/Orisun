package sqlite

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/oexza/Orisun/config"
	eventstore "github.com/oexza/Orisun/orisun"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Group commit coalesces concurrent Save calls per boundary into one SQLite
// transaction per flush, with a savepoint per request so each request keeps
// its own CCC check and its own result. Design: QUEUED_BATCHED_WRITES_PLAN.md.
//
// Batching is opportunistic (maxDelay = 0): the worker never waits to fill a
// batch. Batches form naturally while a flush holds the single write
// connection and new requests queue behind it.
//
// Defaults; overridable via sqlite.groupCommit in config.yaml
// (ORISUN_SQLITE_GC_* env vars).
const (
	sqliteGroupCommitMaxBatchRequests = 128
	sqliteGroupCommitMaxBatchEvents   = 1024
	sqliteGroupCommitMaxDelay         = 0
	sqliteGroupCommitMaxPending       = 4096
	sqliteGroupCommitFlushTimeout     = 30 * time.Second
)

// normalizeGroupCommitConfig applies package defaults to zero values and
// rejects negatives. MaxDelay 0 is a meaningful setting (opportunistic
// batching), so it has no non-zero default.
func normalizeGroupCommitConfig(cfg config.SqliteGroupCommitConfig) (config.SqliteGroupCommitConfig, error) {
	if cfg.MaxBatchRequests == 0 {
		cfg.MaxBatchRequests = sqliteGroupCommitMaxBatchRequests
	}
	if cfg.MaxBatchEvents == 0 {
		cfg.MaxBatchEvents = sqliteGroupCommitMaxBatchEvents
	}
	if cfg.MaxPending == 0 {
		cfg.MaxPending = sqliteGroupCommitMaxPending
	}
	if cfg.FlushTimeout == 0 {
		cfg.FlushTimeout = sqliteGroupCommitFlushTimeout
	}
	switch {
	case cfg.MaxBatchRequests < 0:
		return cfg, fmt.Errorf("sqlite group commit maxBatchRequests must be >= 0, got %d", cfg.MaxBatchRequests)
	case cfg.MaxBatchEvents < 0:
		return cfg, fmt.Errorf("sqlite group commit maxBatchEvents must be >= 0, got %d", cfg.MaxBatchEvents)
	case cfg.MaxDelay < 0:
		return cfg, fmt.Errorf("sqlite group commit maxDelay must be >= 0, got %s", cfg.MaxDelay)
	case cfg.MaxPending < 0:
		return cfg, fmt.Errorf("sqlite group commit maxPending must be >= 0, got %d", cfg.MaxPending)
	case cfg.FlushTimeout < 0:
		return cfg, fmt.Errorf("sqlite group commit flushTimeout must be >= 0, got %s", cfg.FlushTimeout)
	}
	return cfg, nil
}

type sqliteSaveRequest struct {
	ctx      context.Context
	inserts  []sqliteEventToInsert
	expected *eventstore.Position
	query    *eventstore.Query
	// result has capacity 1 so the worker's send never blocks on a caller
	// that abandoned its Save after inclusion in a flush.
	result chan sqliteSaveResult
	// delivered is touched only by the boundary worker goroutine.
	delivered bool
}

type sqliteSaveResult struct {
	transactionID string
	globalID      int64
	err           error
}

// deliver sends a request's result exactly once. Worker-goroutine only.
func (r *sqliteSaveRequest) deliver(res sqliteSaveResult) {
	if r.delivered {
		return
	}
	r.delivered = true
	r.result <- res
}

var errSaverClosed = status.Error(codes.Unavailable, "sqlite event saver is shut down")

// enqueue hands the request to the boundary worker and waits for its result.
// A context cancellation after the request may already be in a flush carries
// the same ambiguity as cancelling a direct write mid-transaction: the batch
// may still commit.
func (s *SqliteSaveEvents) enqueue(
	ctx context.Context,
	boundary string,
	inserts []sqliteEventToInsert,
	expected *eventstore.Position,
	query *eventstore.Query,
) (string, int64, error) {
	s.enqueueMu.RLock()
	if s.isClosed() {
		s.enqueueMu.RUnlock()
		return "", 0, errSaverClosed
	}
	req := &sqliteSaveRequest{
		ctx:      ctx,
		inserts:  inserts,
		expected: expected,
		query:    query,
		result:   make(chan sqliteSaveResult, 1),
	}
	select {
	case s.queues[boundary] <- req:
		s.enqueueMu.RUnlock()
	case <-ctx.Done():
		s.enqueueMu.RUnlock()
		return "", 0, status.FromContextError(ctx.Err()).Err()
	case <-s.closed:
		s.enqueueMu.RUnlock()
		return "", 0, errSaverClosed
	}
	select {
	case res := <-req.result:
		return res.transactionID, res.globalID, res.err
	case <-ctx.Done():
		return "", 0, status.FromContextError(ctx.Err()).Err()
	}
}

// runWorker is the per-boundary write loop: take one request, drain more up
// to the batch limits, flush, repeat. After close() it fails everything in
// (and arriving on) the queue and parks.
func (s *SqliteSaveEvents) runWorker(boundary string, pool *BoundaryPools, queue chan *sqliteSaveRequest) {
	defer s.workerWG.Done()

	var carry *sqliteSaveRequest
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
		if s.isClosed() {
			req.deliver(sqliteSaveResult{err: errSaverClosed})
			if carry != nil {
				carry.deliver(sqliteSaveResult{err: errSaverClosed})
			}
			s.failFast(queue)
			return
		}

		var batch []*sqliteSaveRequest
		batch, carry = s.drainBatch(queue, req)
		if s.isClosed() {
			failUndelivered(batch, errSaverClosed)
			if carry != nil {
				carry.deliver(sqliteSaveResult{err: errSaverClosed})
			}
			s.failFast(queue)
			return
		}
		s.runFlush(boundary, pool, batch)
		if s.isClosed() {
			if carry != nil {
				carry.deliver(sqliteSaveResult{err: errSaverClosed})
			}
			s.failFast(queue)
			return
		}
	}
}

// failFast serves the queue in shutdown mode: every remaining request fails
// immediately. close() guarantees no further sends and closes the queue, so
// the loop ends once the buffered requests are drained.
func (s *SqliteSaveEvents) failFast(queue chan *sqliteSaveRequest) {
	for req := range queue {
		req.deliver(sqliteSaveResult{err: errSaverClosed})
	}
}

// drainBatch greedily collects queued requests behind first, bounded by the
// request and event limits. With gcMaxDelay > 0 it waits up to that long for
// the batch to fill instead of flushing on the first empty read.
func (s *SqliteSaveEvents) drainBatch(queue chan *sqliteSaveRequest, first *sqliteSaveRequest) ([]*sqliteSaveRequest, *sqliteSaveRequest) {
	batch := []*sqliteSaveRequest{first}
	events := len(first.inserts)

	var delay <-chan time.Time
	if s.gcMaxDelay > 0 {
		timer := time.NewTimer(s.gcMaxDelay)
		defer timer.Stop()
		delay = timer.C
	}

	for len(batch) < s.gcMaxBatchRequests && events < s.gcMaxBatchEvents {
		if delay == nil {
			select {
			case req, ok := <-queue:
				if !ok {
					return batch, nil
				}
				if len(batch) > 0 && events+len(req.inserts) > s.gcMaxBatchEvents {
					return batch, req
				}
				batch = append(batch, req)
				events += len(req.inserts)
			default:
				return batch, nil
			}
		} else {
			select {
			case req, ok := <-queue:
				if !ok {
					return batch, nil
				}
				if len(batch) > 0 && events+len(req.inserts) > s.gcMaxBatchEvents {
					return batch, req
				}
				batch = append(batch, req)
				events += len(req.inserts)
			case <-delay:
				return batch, nil
			case <-s.closed:
				return batch, nil
			}
		}
	}
	return batch, nil
}

// runFlush writes one batch. Requests whose context is already cancelled are
// answered with their context error and excluded. All live requests — a
// single one included — share one transaction with a savepoint per request.
// A panic anywhere in the flush is answered with INTERNAL for every
// undelivered request and the worker keeps serving.
func (s *SqliteSaveEvents) runFlush(boundary string, pool *BoundaryPools, batch []*sqliteSaveRequest) {
	defer func() {
		if p := recover(); p != nil {
			s.logger.Errorf("sqlite group commit: panic during flush for boundary %s: %v", boundary, p)
			failUndelivered(batch, status.Errorf(codes.Internal, "flush panic: %v", p))
		}
	}()

	live := batch[:0]
	for _, req := range batch {
		if ctxErr := req.ctx.Err(); ctxErr != nil {
			req.deliver(sqliteSaveResult{err: status.FromContextError(ctxErr).Err()})
			continue
		}
		live = append(live, req)
	}
	if len(live) == 0 {
		return
	}
	if len(live) == 1 {
		s.gcSingleFlushes.Add(1)
	} else {
		s.gcMultiFlushes.Add(1)
	}

	// The flush must not run under any single caller's context — one
	// cancellation would poison the whole batch. Pool.Take wires the flush
	// context's Done channel in as the connection's interrupt, bounding the
	// transaction by gcFlushTimeout.
	flushCtx, cancel := context.WithTimeout(context.Background(), s.gcFlushTimeout)
	defer cancel()

	conn, takeErr := pool.Write.Take(flushCtx)
	if takeErr != nil {
		err := status.Errorf(codes.Internal, "take write conn: %v", takeErr)
		if s.isClosed() {
			err = errSaverClosed
		}
		failUndelivered(live, err)
		return
	}
	defer pool.Write.Put(conn)

	if s.gcTestFlushHook != nil {
		s.gcTestFlushHook(len(live))
	}

	start := time.Now()
	accepted, flushErr := s.flushTx(conn, pool, boundary, live)
	if flushErr != nil {
		// Begin or commit failed: nothing persisted; every provisionally
		// accepted request reports the error. (endFn inside flushTx already
		// rolled back, so the connection returns to the pool clean.)
		failUndelivered(live, status.Errorf(codes.Internal, "group commit flush: %v", flushErr))
		return
	}
	for _, a := range accepted {
		a.req.deliver(sqliteSaveResult{transactionID: a.transactionID, globalID: a.globalID})
	}
	if len(accepted) > 0 && s.notifier != nil {
		s.notifier.Notify(boundary)
	}
	if s.logger.IsDebugEnabled() {
		s.logger.Debugf("sqlite group commit: boundary=%s drained=%d accepted=%d rejected=%d duration=%s",
			boundary, len(batch), len(accepted), len(live)-len(accepted), time.Since(start))
	}
}

type acceptedSave struct {
	req           *sqliteSaveRequest
	transactionID string
	globalID      int64
}

// flushTx runs one IMMEDIATE transaction over the live requests, one
// savepoint each, in queue order. Per-request failures (CCC conflicts,
// invalid criteria) are delivered inside the loop and roll back only that
// request's savepoint — including its orisun_es_seq update, so rejected
// requests leave no position gaps. The returned flushErr is a whole-batch
// failure: BEGIN or COMMIT failed and nothing was persisted.
//
// endFn is deferred, so it also converts a mid-flush panic into a rollback
// before re-panicking (recovered by runFlush).
func (s *SqliteSaveEvents) flushTx(
	conn *sqlite.Conn,
	pool *BoundaryPools,
	boundary string,
	live []*sqliteSaveRequest,
) (accepted []acceptedSave, flushErr error) {
	endFn, beginErr := sqlitex.ImmediateTransaction(conn)
	if beginErr != nil {
		return nil, beginErr
	}
	defer endFn(&flushErr)

	accepted = make([]acceptedSave, 0, len(live))
	for _, req := range live {
		if ctxErr := req.ctx.Err(); ctxErr != nil {
			req.deliver(sqliteSaveResult{err: status.FromContextError(ctxErr).Err()})
			continue
		}
		var txID string
		var gid int64
		var err error
		if len(live) == 1 {
			// Batch of one: the transaction itself is the rollback boundary,
			// so the savepoint pair is redundant. On error the transaction
			// rolls back whole (via the returned flushErr), which for a single
			// request is exactly the savepoint rollback.
			txID, gid, err = s.saveEventsOnConn(conn, pool, boundary, req.inserts, req.expected, req.query)
			if err != nil {
				req.deliver(sqliteSaveResult{err: err})
				return nil, err
			}
		} else {
			txID, gid, err = s.saveSavepointed(conn, pool, boundary, req)
			if err != nil {
				req.deliver(sqliteSaveResult{err: err})
				continue
			}
		}
		accepted = append(accepted, acceptedSave{req: req, transactionID: txID, globalID: gid})
	}
	return accepted, nil
}

// saveSavepointed wraps one request's CCC check + insert in a savepoint, so
// a rejection rolls back that request without poisoning the transaction.
func (s *SqliteSaveEvents) saveSavepointed(
	conn *sqlite.Conn,
	pool *BoundaryPools,
	boundary string,
	req *sqliteSaveRequest,
) (transactionID string, globalID int64, err error) {
	releaseFn := sqlitex.Save(conn)
	defer releaseFn(&err)
	return s.saveEventsOnConn(conn, pool, boundary, req.inserts, req.expected, req.query)
}

func failUndelivered(batch []*sqliteSaveRequest, err error) {
	for _, req := range batch {
		req.deliver(sqliteSaveResult{err: err})
	}
}
