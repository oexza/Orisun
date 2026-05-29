package postgres

import (
	"context"
	"crypto/md5"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	orisun "github.com/oexza/Orisun/orisun"
)

type PGNotifyListener struct {
	conn                  *pgx.Conn
	boundarySchemaMapping map[string]config.BoundaryToPostgresSchemaMapping
	logger                logging.Logger

	mu             sync.Mutex
	signals        map[string]chan struct{}
	channelSignals map[string]chan struct{}
	done           chan struct{}
}

const pgNotifyChannelPrefix = "orisun_events_"

func pgNotifyChannelForBoundary(boundary string) string {
	return fmt.Sprintf("%s%x", pgNotifyChannelPrefix, md5.Sum([]byte(boundary)))
}

func NewPGNotifyListener(
	ctx context.Context,
	connStr string,
	boundarySchemaMapping map[string]config.BoundaryToPostgresSchemaMapping,
	logger logging.Logger,
) (*PGNotifyListener, error) {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("pg_notify listener connect: %w", err)
	}

	signals := make(map[string]chan struct{}, len(boundarySchemaMapping))
	channelSignals := make(map[string]chan struct{}, len(boundarySchemaMapping))
	for boundary := range boundarySchemaMapping {
		channel := pgNotifyChannelForBoundary(boundary)
		if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
			conn.Close(ctx)
			return nil, fmt.Errorf("LISTEN %s: %w", channel, err)
		}
		signal := make(chan struct{}, 1)
		signals[boundary] = signal
		channelSignals[channel] = signal
	}

	return &PGNotifyListener{
		conn:                  conn,
		boundarySchemaMapping: boundarySchemaMapping,
		logger:                logger,
		signals:               signals,
		channelSignals:        channelSignals,
		done:                  make(chan struct{}),
	}, nil
}

// Start owns l.conn exclusively for the lifetime of the goroutine: it is the
// only place the connection is read, swapped (via reconnect), or closed. No
// other goroutine may touch l.conn — pgx.Conn is not safe for concurrent use.
func (l *PGNotifyListener) Start(ctx context.Context) {
	defer close(l.done)
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.conn.Close(closeCtx)
	}()

	for {
		notification, err := l.conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			l.logger.Errorf("PG LISTEN connection error: %v — reconnecting", err)
			if reconnectErr := l.reconnect(ctx); reconnectErr != nil {
				if ctx.Err() != nil {
					return
				}
				l.logger.Errorf("PG LISTEN reconnect failed: %v", reconnectErr)
				return
			}
			continue
		}

		l.dispatch(notification)
	}
}

func (l *PGNotifyListener) dispatch(notification *pgconn.Notification) {
	l.mu.Lock()
	ch, ok := l.channelSignals[notification.Channel]
	l.mu.Unlock()

	if !ok {
		return
	}

	// Non-blocking send — coalesce if a signal is already pending.
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (l *PGNotifyListener) reconnect(ctx context.Context) error {
	backoff := orisun.Backoff{Base: 500 * time.Millisecond, Max: 10 * time.Second}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := backoff.Wait(ctx); err != nil {
			return err
		}

		conn, err := pgx.Connect(ctx, l.conn.Config().ConnString())
		if err != nil {
			l.logger.Errorf("PG LISTEN reconnect attempt failed: %v", err)
			continue
		}

		// Re-issue all LISTEN commands on the new connection.
		allOk := true
		for boundary := range l.boundarySchemaMapping {
			channel := pgNotifyChannelForBoundary(boundary)
			if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
				l.logger.Errorf("Re-LISTEN %s failed: %v", channel, err)
				allOk = false
				break
			}
		}
		if !allOk {
			conn.Close(ctx)
			continue
		}

		// Swap the old connection for the new one. Safe without locking: only
		// the Start goroutine touches l.conn.
		oldConn := l.conn
		l.conn = conn
		oldConn.Close(ctx)

		l.logger.Info("PG LISTEN reconnected and re-listened on all channels")
		return nil
	}
}

func (l *PGNotifyListener) Signal(boundary string, catchupInterval time.Duration) orisun.EventSignal {
	l.mu.Lock()
	ch, ok := l.signals[boundary]
	l.mu.Unlock()

	if !ok {
		// Unknown boundary — fall back to polling.
		return orisun.NewPollingSignal(catchupInterval)
	}

	return &pgNotifySignal{
		notifyCh:      ch,
		catchupTicker: time.NewTicker(catchupInterval),
	}
}

// Close waits for the Start goroutine to stop (it closes the connection on
// exit). The caller must cancel the context passed to Start to trigger
// shutdown; Close itself returns once Start has finished or ctx is done.
func (l *PGNotifyListener) Close(ctx context.Context) {
	select {
	case <-l.done:
	case <-ctx.Done():
	}
}

type pgNotifySignal struct {
	notifyCh      chan struct{}
	catchupTicker *time.Ticker
}

func (s *pgNotifySignal) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.notifyCh:
		return nil
	case <-s.catchupTicker.C:
		return nil
	}
}

func (s *pgNotifySignal) Stop() {
	s.catchupTicker.Stop()
}
