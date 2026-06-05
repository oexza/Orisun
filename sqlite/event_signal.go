package sqlite

import (
	"context"
	"sync"
	"time"

	eventstore "github.com/oexza/Orisun/orisun"
)

type SqliteEventNotifier struct {
	interval time.Duration
	mu       sync.Mutex
	signals  map[string]map[*sqliteEventSignal]struct{}
}

func NewSqliteEventNotifier(interval time.Duration) *SqliteEventNotifier {
	if interval <= 0 {
		interval = time.Second
	}
	return &SqliteEventNotifier{
		interval: interval,
		signals:  make(map[string]map[*sqliteEventSignal]struct{}),
	}
}

func (n *SqliteEventNotifier) Signal(boundary string) eventstore.EventSignal {
	signal := &sqliteEventSignal{
		notifier: n,
		boundary: boundary,
		ch:       make(chan struct{}, 1),
		ticker:   time.NewTicker(n.interval),
	}
	n.mu.Lock()
	if n.signals[boundary] == nil {
		n.signals[boundary] = make(map[*sqliteEventSignal]struct{})
	}
	n.signals[boundary][signal] = struct{}{}
	n.mu.Unlock()
	return signal
}

func (n *SqliteEventNotifier) Notify(boundary string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for signal := range n.signals[boundary] {
		select {
		case signal.ch <- struct{}{}:
		default:
		}
	}
}

func (n *SqliteEventNotifier) unregister(signal *sqliteEventSignal) {
	n.mu.Lock()
	defer n.mu.Unlock()
	signals := n.signals[signal.boundary]
	delete(signals, signal)
	if len(signals) == 0 {
		delete(n.signals, signal.boundary)
	}
}

type sqliteEventSignal struct {
	notifier *SqliteEventNotifier
	boundary string
	ch       chan struct{}
	ticker   *time.Ticker
	stopOnce sync.Once
}

func (s *sqliteEventSignal) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ch:
		return nil
	case <-s.ticker.C:
		return nil
	}
}

func (s *sqliteEventSignal) Stop() {
	s.stopOnce.Do(func() {
		s.ticker.Stop()
		s.notifier.unregister(s)
	})
}
