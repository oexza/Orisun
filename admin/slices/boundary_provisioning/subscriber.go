package boundary_provisioning

import (
	"context"
	"fmt"
	"sync"
	"time"

	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/OrisunLabs/Orisun/logging"
	"github.com/google/uuid"
)

const (
	boundaryProvisioningSubscriberName = "boundary-provisioning"
	boundaryDefinitionReplayBatchSize  = uint32(100)
)

type DefinitionEventsRetriever interface {
	Read(ctx context.Context, request coreeventstore.ReadRequest) (coreeventstore.ReadEventBatch, error)
}

type SubscribeToBoundaryEvents func(
	ctx context.Context,
	request coreeventstore.SubscribeRequest,
	handle coreeventstore.EventHandler,
) error
type HandleBoundaryEvent func(ctx context.Context, event coreeventstore.ReadEvent) error

// BoundaryProvisioningSubscriber projects durable boundary-definition events
// into one process's local runtime. Its cursor is intentionally process-local:
// backend registries, NATS streams, and polling loops must be installed on
// every server node, not just whichever node advanced a shared projector row.
type BoundaryProvisioningSubscriber struct {
	adminBoundary  string
	subscriberName string
	retriever      DefinitionEventsRetriever
	subscribe      SubscribeToBoundaryEvents
	handle         HandleBoundaryEvent
	logger         logging.Logger

	positionMu sync.RWMutex
	commit     int64
	prepare    int64

	retryMu  sync.Mutex
	retrying map[string]struct{}
}

func NewBoundaryProvisioningSubscriber(
	adminBoundary string,
	retriever DefinitionEventsRetriever,
	subscribe SubscribeToBoundaryEvents,
	handle HandleBoundaryEvent,
	logger logging.Logger,
) *BoundaryProvisioningSubscriber {
	return &BoundaryProvisioningSubscriber{
		adminBoundary:  adminBoundary,
		subscriberName: boundaryProvisioningSubscriberName + "-" + uuid.NewString(),
		retriever:      retriever,
		subscribe:      subscribe,
		handle:         handle,
		logger:         logger,
		commit:         -1,
		prepare:        -1,
		retrying:       make(map[string]struct{}),
	}
}

// Replay synchronously installs every definition visible at startup. Normal
// successful (including already-active) boundaries are therefore usable before
// the composition root exposes its API. A definition whose provisioning fails
// is retried independently so it cannot block later boundaries.
func (s *BoundaryProvisioningSubscriber) Replay(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	cursor := s.currentPosition()
	for {
		batch, err := s.retriever.Read(ctx, coreeventstore.ReadRequest{
			Boundary:     s.adminBoundary,
			Direction:    coreeventstore.DirectionAscending,
			Count:        boundaryDefinitionReplayBatchSize,
			FromPosition: cursor,
			Query:        boundaryDefinitionQuery(),
		})
		if err != nil {
			return fmt.Errorf("replay boundary definitions: %w", err)
		}
		advanced := false
		for _, event := range batch {
			if !event.Position.After(*cursor) {
				continue
			}
			s.handleOrRetry(ctx, event)
			cursor = &coreeventstore.Position{
				CommitPosition:  event.Position.CommitPosition,
				PreparePosition: event.Position.PreparePosition,
			}
			s.setPosition(event.Position.CommitPosition, event.Position.PreparePosition)
			advanced = true
		}
		if len(batch) < int(boundaryDefinitionReplayBatchSize) || !advanced {
			return nil
		}
	}
}

// Start follows definitions newer than the process-local replay cursor.
func (s *BoundaryProvisioningSubscriber) Start(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	return s.subscribe(ctx, coreeventstore.SubscribeRequest{
		Boundary:       s.adminBoundary,
		SubscriberName: s.subscriberName,
		AfterPosition:  s.currentPosition(),
		Query:          boundaryDefinitionQuery(),
	}, func(_ context.Context, event coreeventstore.ReadEvent) error {
		s.handleOrRetry(ctx, event)
		s.setPosition(event.Position.CommitPosition, event.Position.PreparePosition)
		return nil
	})
}

func (s *BoundaryProvisioningSubscriber) validate() error {
	if s == nil || s.adminBoundary == "" || s.subscriberName == "" || s.retriever == nil || s.subscribe == nil || s.handle == nil || s.logger == nil {
		return fmt.Errorf("boundary provisioning subscriber is not configured")
	}
	return nil
}

func (s *BoundaryProvisioningSubscriber) handleOrRetry(ctx context.Context, event coreeventstore.ReadEvent) {
	if err := s.handle(ctx, event); err != nil {
		s.logger.Errorf("Boundary provisioning failed for %s at %d/%d: %v - will retry", event.EventType, event.Position.CommitPosition, event.Position.PreparePosition, err)
		s.scheduleRetry(ctx, event)
	}
}

// scheduleRetry gives each failed definition its own retry loop. The global
// definition cursor can keep advancing, so one invalid or temporarily
// unavailable boundary never starves boundaries defined after it.
func (s *BoundaryProvisioningSubscriber) scheduleRetry(ctx context.Context, event coreeventstore.ReadEvent) {
	key := event.EventID
	if key == "" {
		key = fmt.Sprintf("%s:%d:%d", event.EventType, event.Position.CommitPosition, event.Position.PreparePosition)
	}
	s.retryMu.Lock()
	if _, exists := s.retrying[key]; exists {
		s.retryMu.Unlock()
		return
	}
	s.retrying[key] = struct{}{}
	s.retryMu.Unlock()

	go func() {
		defer func() {
			s.retryMu.Lock()
			delete(s.retrying, key)
			s.retryMu.Unlock()
		}()
		backoff := 100 * time.Millisecond
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if err := s.handle(ctx, event); err == nil {
				return
			} else {
				s.logger.Errorf("Boundary provisioning retry failed for %s at %d/%d: %v", event.EventType, event.Position.CommitPosition, event.Position.PreparePosition, err)
			}
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}()
}

// Run performs a fresh durable replay after every subscription interruption,
// then follows live definitions. This closes any NATS retention or reconnect
// gap without relying on a shared projector checkpoint.
func (s *BoundaryProvisioningSubscriber) Run(ctx context.Context) {
	backoff := 100 * time.Millisecond
	for ctx.Err() == nil {
		if err := s.Replay(ctx); err == nil {
			err = s.Start(ctx)
			if err == nil || ctx.Err() != nil {
				return
			}
			s.logger.Errorf("Boundary provisioning subscriber stopped: %v - will retry", err)
		} else {
			s.logger.Errorf("Boundary provisioning replay stopped: %v - will retry", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
	}
}

func (s *BoundaryProvisioningSubscriber) currentPosition() *coreeventstore.Position {
	s.positionMu.RLock()
	defer s.positionMu.RUnlock()
	return &coreeventstore.Position{CommitPosition: s.commit, PreparePosition: s.prepare}
}

func (s *BoundaryProvisioningSubscriber) setPosition(commit, prepare int64) {
	s.positionMu.Lock()
	if positionAfter(commit, prepare, s.commit, s.prepare) {
		s.commit = commit
		s.prepare = prepare
	}
	s.positionMu.Unlock()
}

func positionAfter(commit, prepare, cursorCommit, cursorPrepare int64) bool {
	return commit > cursorCommit || commit == cursorCommit && prepare > cursorPrepare
}

func boundaryDefinitionQuery() coreeventstore.Query {
	return coreeventstore.Query{Criteria: []coreeventstore.Criterion{
		{Tags: []coreeventstore.Tag{{Key: "eventType", Value: adminevents.EventTypeBoundaryCreated}}},
		{Tags: []coreeventstore.Tag{{Key: "eventType", Value: adminevents.EventTypeBoundaryImported}}},
	}}
}
