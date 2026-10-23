package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/internal/groupcommit"
	"github.com/OrisunLabs/Orisun/internal/statuscode"
	eventstore "github.com/OrisunLabs/Orisun/orisun"
)

// PostgreSQL group commit coalesces concurrent SaveEvents calls per boundary.
// Every flush uses one canonical SQL implementation and one transaction.
// Malformed requests are rejected before constructing the SQL batch.
// Every CCC request is evaluated in queue order, so later checks observe
// earlier accepted writes in the same flush. The SQL function's advisory lock
// remains the cross-process serialization boundary.
const (
	postgresGroupCommitMaxBatchRequests = 512
	postgresGroupCommitMaxBatchEvents   = 1024
	postgresGroupCommitMaxDelay         = 0
	postgresGroupCommitMaxPending       = 4096
	postgresGroupCommitFlushTimeout     = 30 * time.Second
)

func normalizePostgresGroupCommitConfig(cfg config.PostgresGroupCommitConfig) (config.PostgresGroupCommitConfig, error) {
	if cfg.MaxBatchRequests == 0 {
		cfg.MaxBatchRequests = postgresGroupCommitMaxBatchRequests
	}
	if cfg.MaxBatchEvents == 0 {
		cfg.MaxBatchEvents = postgresGroupCommitMaxBatchEvents
	}
	if cfg.MaxPending == 0 {
		cfg.MaxPending = postgresGroupCommitMaxPending
	}
	if cfg.FlushTimeout == 0 {
		cfg.FlushTimeout = postgresGroupCommitFlushTimeout
	}
	switch {
	case cfg.MaxBatchRequests < 0:
		return cfg, fmt.Errorf("postgres group commit maxBatchRequests must be >= 0, got %d", cfg.MaxBatchRequests)
	case cfg.MaxBatchEvents < 0:
		return cfg, fmt.Errorf("postgres group commit maxBatchEvents must be >= 0, got %d", cfg.MaxBatchEvents)
	case cfg.MaxDelay < 0:
		return cfg, fmt.Errorf("postgres group commit maxDelay must be >= 0, got %s", cfg.MaxDelay)
	case cfg.MaxPending < 0:
		return cfg, fmt.Errorf("postgres group commit maxPending must be >= 0, got %d", cfg.MaxPending)
	case cfg.FlushTimeout < 0:
		return cfg, fmt.Errorf("postgres group commit flushTimeout must be >= 0, got %s", cfg.FlushTimeout)
	}
	return cfg, nil
}

type postgresSaveResult struct {
	transactionID string
	globalID      int64
	err           error
}

var errPostgresSaverClosed = statuscode.New(statuscode.Unavailable, "postgres event saver is shut down")

func (s *PostgresSaveEvents) initGroupCommit(cfg config.PostgresGroupCommitConfig) {
	s.gc = groupcommit.New(groupcommit.Config{
		MaxBatchRequests: cfg.MaxBatchRequests, MaxBatchEvents: cfg.MaxBatchEvents,
		MaxPending: cfg.MaxPending, MaxDelay: cfg.MaxDelay, FlushTimeout: cfg.FlushTimeout,
	}, s.executeFlush)
}

func (s *PostgresSaveEvents) enqueue(ctx context.Context, boundary string, events eventstore.PreparedEventBatch, consistency []eventstore.ConsistencyCheck) (string, int64, error) {
	if s.gc.Closed() {
		return "", 0, errPostgresSaverClosed
	}
	prepared, err := s.prepareSave(events, consistency)
	if err != nil {
		return "", 0, err
	}
	result, err := s.gc.Submit(ctx, boundary, prepared, prepared.eventCount)
	if err != nil {
		if errors.Is(err, groupcommit.ErrClosed) {
			return "", 0, errPostgresSaverClosed
		}
		if ctx.Err() != nil {
			return "", 0, statuscode.FromContextError(ctx.Err())
		}
		return "", 0, statuscode.Errorf(statuscode.Internal, "%v", err)
	}
	return result.transactionID, result.globalID, result.err
}

func (s *PostgresSaveEvents) executeFlush(ctx context.Context, boundary string, prepared []preparedPostgresSave) ([]postgresSaveResult, error) {
	if s.testFlushHook != nil {
		s.testFlushHook(len(prepared))
	}
	start := time.Now()
	outcomes, err := s.executeBatch(ctx, boundary, prepared)
	if err != nil {
		return nil, err
	}
	if s.logger.IsDebugEnabled() {
		accepted := 0
		for _, outcome := range outcomes {
			if outcome.err == nil {
				accepted++
			}
		}
		s.logger.Debugf("postgres group commit: boundary=%s requests=%d accepted=%d rejected=%d duration=%s", boundary, len(prepared), accepted, len(prepared)-accepted, time.Since(start))
	}
	return outcomes, nil
}

func (s *PostgresSaveEvents) mapSaveError(err error) error {
	if code, _, ok := statuscode.FromError(err); ok && code != statuscode.Unknown {
		return err
	}
	if strings.Contains(err.Error(), "OptimisticConcurrencyException") {
		return statuscode.New(statuscode.AlreadyExists, err.Error())
	}
	s.logger.Errorf("Error inserting events: %v", err)
	return statuscode.Errorf(statuscode.Internal, "failed to insert events: %v", err)
}

func (s *PostgresSaveEvents) close() { s.gc.Close() }
