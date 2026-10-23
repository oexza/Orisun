package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/internal/groupcommit"
	"github.com/OrisunLabs/Orisun/internal/statuscode"
	eventstore "github.com/OrisunLabs/Orisun/orisun"
)

// Group commit coalesces concurrent Save calls per boundary into one SQLite
// transaction per flush. Every request uses queue-ordered CCC checks and a
// savepoint to isolate its result, including batches of one.
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

var errSaverClosed = statuscode.New(statuscode.Unavailable, "sqlite event saver is shut down")

func (s *SqliteSaveEvents) initGroupCommit(cfg config.SqliteGroupCommitConfig) {
	s.gc = groupcommit.New(groupcommit.Config{MaxBatchRequests: cfg.MaxBatchRequests, MaxBatchEvents: cfg.MaxBatchEvents, MaxPending: cfg.MaxPending, MaxDelay: cfg.MaxDelay, FlushTimeout: cfg.FlushTimeout}, s.executeBatch)
}

func (s *SqliteSaveEvents) enqueue(ctx context.Context, boundary string, inserts eventstore.PreparedEventBatch, consistency []eventstore.ConsistencyCheck) (string, int64, error) {
	prepared, err := prepareSQLiteSave(ctx, inserts, consistency)
	if err != nil {
		return "", 0, err
	}
	result, err := s.gc.Submit(ctx, boundary, prepared, len(prepared.inserts))
	if err != nil {
		if errors.Is(err, groupcommit.ErrClosed) {
			return "", 0, errSaverClosed
		}
		if ctx.Err() != nil {
			return "", 0, statuscode.FromContextError(ctx.Err())
		}
		return "", 0, statuscode.Errorf(statuscode.Internal, "%v", err)
	}
	return result.transactionID, result.globalID, result.err
}
