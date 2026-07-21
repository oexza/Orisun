// Package local provides a NATS-free, in-process Orisun runtime backed by
// SQLite. It is intended for single-process applications such as mobile apps
// and desktop clients.
package local

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	config "github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	sqlitebackend "github.com/OrisunLabs/Orisun/sqlite"
)

// Store is a single-process embedded event store. SQLite is the durable source
// of truth; live subscriptions wake on in-process commit notifications and
// always catch up from SQLite by position.
type Store struct {
	saver          orisun.EventsSaver
	retriever      orisun.EventsRetriever
	indexManager   orisun.BoundaryIndexManager
	lockProvider   orisun.LockProvider
	signalProvider func(string) orisun.EventSignal

	ctx       context.Context
	cancel    context.CancelFunc
	shutdown  <-chan struct{}
	closeOnce sync.Once
}

// Open starts a local store with standard SQLite defaults. The first boundary
// is used as the admin metadata boundary.
func Open(ctx context.Context, dir string, boundaries []string, logger logging.Logger) (*Store, error) {
	if len(boundaries) == 0 {
		return nil, errors.New("local sqlite requires at least one boundary")
	}
	cfg := config.AppConfig{}
	cfg.Sqlite.Dir = dir
	cfg.Admin.Boundary = boundaries[0]
	return start(ctx, cfg, boundaries, logger)
}

// Start starts a local store from an AppConfig. NATS settings are ignored.
// Callers constructing AppConfig directly must call ParseBoundaries first, or
// use Open when only a directory and boundary list are needed.
func Start(ctx context.Context, cfg config.AppConfig, logger logging.Logger) (*Store, error) {
	return start(ctx, cfg, cfg.GetBoundaryNames(), logger)
}

func start(ctx context.Context, cfg config.AppConfig, boundaries []string, logger logging.Logger) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("local sqlite context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Sqlite.Dir == "" {
		return nil, errors.New("local sqlite requires a data directory")
	}
	if len(boundaries) == 0 {
		return nil, errors.New("local sqlite requires at least one boundary")
	}
	seen := make(map[string]struct{}, len(boundaries))
	for _, boundary := range boundaries {
		if _, exists := seen[boundary]; exists {
			return nil, fmt.Errorf("duplicate boundary %q", boundary)
		}
		seen[boundary] = struct{}{}
	}
	if logger == nil {
		logger = discardLogger{}
	}
	if cfg.Admin.Boundary == "" {
		cfg.Admin.Boundary = boundaries[0]
	}

	runCtx, cancel := context.WithCancel(ctx)
	localLockProvider := orisun.NewLocalLockProvider()
	saver, retriever, lockProvider, adminDB, _, signalProvider, shutdown, err := sqlitebackend.InitializeSqliteDatabaseWithLockProviderAndShutdown(
		runCtx,
		cfg.Sqlite,
		cfg.Admin,
		boundaries,
		localLockProvider,
		logger,
	)
	if err != nil {
		cancel()
		return nil, err
	}

	return &Store{
		saver:          saver,
		retriever:      retriever,
		indexManager:   adminDB,
		lockProvider:   lockProvider,
		signalProvider: signalProvider,
		ctx:            runCtx,
		cancel:         cancel,
		shutdown:       shutdown,
	}, nil
}

// SaveEvents appends events after atomically re-checking the supplied CCC
// context inside the SQLite write transaction.
func (s *Store) SaveEvents(
	ctx context.Context,
	events []orisun.EventWithMapTags,
	boundary string,
	expectedPosition *orisun.Position,
	query *orisun.Query,
) (*orisun.Position, error) {
	if s == nil || s.saver == nil {
		return nil, errors.New("local sqlite store is not open")
	}
	prepared, err := orisun.PrepareEventsForSave(events)
	if err != nil {
		return nil, fmt.Errorf("prepare events: %w", err)
	}
	transactionID, globalID, err := s.saver.SavePrepared(ctx, prepared, boundary, expectedPosition, query)
	if err != nil {
		return nil, err
	}
	commitPosition, err := strconv.ParseInt(transactionID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse transaction id: %w", err)
	}
	return &orisun.Position{CommitPosition: commitPosition, PreparePosition: globalID}, nil
}

// GetEvents reads an ordered batch directly from SQLite.
func (s *Store) GetEvents(ctx context.Context, req *orisun.GetEventsRequest) (orisun.ReadEventBatch, error) {
	if s == nil || s.retriever == nil {
		return nil, errors.New("local sqlite store is not open")
	}
	if req == nil {
		return nil, errors.New("get events request is nil")
	}
	return s.retriever.GetBatch(ctx, req)
}

// GetLatestByCriteria reads all requested carried-state values from one SQLite
// snapshot and returns the position suitable for the next CCC write.
func (s *Store) GetLatestByCriteria(ctx context.Context, query orisun.LatestByCriteriaQuery) (orisun.LatestByCriteriaBatch, error) {
	if s == nil || s.retriever == nil {
		return orisun.LatestByCriteriaBatch{}, errors.New("local sqlite store is not open")
	}
	return s.retriever.GetLatestByCriteria(ctx, query)
}

// Subscribe calls deliver for events committed after afterPosition. Passing nil
// starts at the current end of the store; pass orisun.NotExistsPosition() to
// replay from the beginning. Delivery is ordered and at-least-once across
// caller-managed restarts when the last handled position is persisted.
func (s *Store) Subscribe(
	ctx context.Context,
	boundary string,
	subscriberName string,
	afterPosition *orisun.Position,
	query *orisun.Query,
	deliver func(orisun.ReadEvent) error,
) error {
	if s == nil || s.retriever == nil || s.signalProvider == nil {
		return errors.New("local sqlite store is not open")
	}
	if ctx == nil {
		return errors.New("subscription context is nil")
	}
	if deliver == nil {
		return errors.New("subscription deliver function is nil")
	}

	subscriptionCtx, cancel := context.WithCancel(ctx)
	stopStoreCancellation := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopStoreCancellation()
		cancel()
	}()

	leaseProvider, ok := s.lockProvider.(orisun.LockLeaseProvider)
	if !ok {
		return errors.New("local sqlite lock provider does not support leases")
	}
	lease, err := leaseProvider.AcquireLock(subscriptionCtx, boundary+"__"+subscriberName)
	if err != nil {
		return fmt.Errorf("acquire subscription lock: %w", err)
	}
	defer lease.Release()

	signal := s.signalProvider(boundary)
	defer signal.Stop()

	cursorCommit := int64(-1)
	cursorPrepare := int64(-1)
	if afterPosition == nil {
		latest, err := s.retriever.GetBatch(lease.Context(), &orisun.GetEventsRequest{
			Boundary:  boundary,
			Count:     1,
			Direction: orisun.Direction_DESC,
		})
		if err != nil {
			if ctxErr := lease.Context().Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}
		if len(latest) > 0 {
			cursorCommit = latest[0].CommitPosition
			cursorPrepare = latest[0].PreparePosition
		}
	} else {
		cursorCommit = afterPosition.CommitPosition
		cursorPrepare = afterPosition.PreparePosition
	}

	for {
		if err := lease.Check(lease.Context()); err != nil {
			return err
		}
		batch, err := s.retriever.GetBatch(lease.Context(), &orisun.GetEventsRequest{
			Boundary:  boundary,
			Count:     orisun.DefaultReadBatchSize,
			Direction: orisun.Direction_ASC,
			FromPosition: &orisun.Position{
				CommitPosition:  cursorCommit,
				PreparePosition: cursorPrepare,
			},
			Query: query,
		})
		if err != nil {
			if ctxErr := lease.Context().Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}

		delivered := 0
		for i := range batch {
			event := batch[i]
			if !positionAfter(event.CommitPosition, event.PreparePosition, cursorCommit, cursorPrepare) {
				continue
			}
			if err := lease.Check(lease.Context()); err != nil {
				return err
			}
			if err := deliver(event); err != nil {
				if ctxErr := lease.Context().Err(); ctxErr != nil {
					return ctxErr
				}
				return err
			}
			cursorCommit = event.CommitPosition
			cursorPrepare = event.PreparePosition
			delivered++
		}

		// A full page may have more rows immediately available. Otherwise wait
		// for a commit notification (with the notifier's periodic poll as a
		// no-miss fallback).
		if len(batch) == int(orisun.DefaultReadBatchSize) && delivered > 0 {
			continue
		}
		if err := signal.Wait(lease.Context()); err != nil {
			return err
		}
	}
}

// SubscribeToEvents adapts Subscribe to the existing in-process message
// handler API without introducing gRPC or JetStream.
func (s *Store) SubscribeToEvents(
	ctx context.Context,
	boundary string,
	subscriberName string,
	afterPosition *orisun.Position,
	query *orisun.Query,
	handler *orisun.MessageHandler[orisun.Event],
) error {
	if handler == nil {
		return errors.New("subscription handler is nil")
	}
	return s.Subscribe(ctx, boundary, subscriberName, afterPosition, query, func(event orisun.ReadEvent) error {
		return handler.Send(event.ProtoEvent())
	})
}

func (s *Store) CreateBoundaryIndex(ctx context.Context, boundary, name string, fields []orisun.BoundaryIndexField, conditions []orisun.BoundaryIndexCondition, combinator string) error {
	if s == nil || s.indexManager == nil {
		return errors.New("local sqlite store is not open")
	}
	return s.indexManager.CreateBoundaryIndex(ctx, boundary, name, fields, conditions, combinator)
}

func (s *Store) DropBoundaryIndex(ctx context.Context, boundary, name string) error {
	if s == nil || s.indexManager == nil {
		return errors.New("local sqlite store is not open")
	}
	return s.indexManager.DropBoundaryIndex(ctx, boundary, name)
}

// Close stops subscriptions and SQLite workers, then waits until all database
// pools have been released. It is safe to call more than once.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.shutdown != nil {
			<-s.shutdown
		}
	})
}

func positionAfter(commit, prepare, cursorCommit, cursorPrepare int64) bool {
	return commit > cursorCommit || (commit == cursorCommit && prepare > cursorPrepare)
}

type discardLogger struct{}

func (discardLogger) IsDebugEnabled() bool  { return false }
func (discardLogger) Debug(...any)          {}
func (discardLogger) Debugf(string, ...any) {}
func (discardLogger) Info(...any)           {}
func (discardLogger) Infof(string, ...any)  {}
func (discardLogger) Warn(...any)           {}
func (discardLogger) Warnf(string, ...any)  {}
func (discardLogger) Error(...any)          {}
func (discardLogger) Errorf(string, ...any) {}
func (discardLogger) Fatal(...any)          {}
func (discardLogger) Fatalf(string, ...any) {}
