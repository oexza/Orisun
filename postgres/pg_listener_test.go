package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/OrisunLabs/Orisun/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testLogger struct{}

func (testLogger) IsDebugEnabled() bool  { return false }
func (testLogger) Debug(...any)          {}
func (testLogger) Debugf(string, ...any) {}
func (testLogger) Info(...any)           {}
func (testLogger) Infof(string, ...any)  {}
func (testLogger) Warn(...any)           {}
func (testLogger) Warnf(string, ...any)  {}
func (testLogger) Error(...any)          {}
func (testLogger) Errorf(string, ...any) {}
func (testLogger) Fatal(...any)          {}
func (testLogger) Fatalf(string, ...any) {}

func newTestListener(boundaries ...string) *PGNotifyListener {
	mapping := make(map[string]config.BoundaryToPostgresSchemaMapping, len(boundaries))
	signals := make(map[string]chan struct{}, len(boundaries))
	channelSignals := make(map[string]chan struct{}, len(boundaries))
	for _, b := range boundaries {
		mapping[b] = config.BoundaryToPostgresSchemaMapping{}
		signal := make(chan struct{}, 1)
		signals[b] = signal
		channelSignals[pgNotifyChannelForBoundary(b)] = signal
	}
	return &PGNotifyListener{
		boundarySchemaMapping: mapping,
		logger:                testLogger{},
		signals:               signals,
		channelSignals:        channelSignals,
		done:                  make(chan struct{}),
	}
}

func TestPGNotifyChannelForBoundary(t *testing.T) {
	channel := pgNotifyChannelForBoundary("VeryLongBoundaryNameThatStillPassesValidation_ABCDEFGHIJKLMNOPQRSTUVWXYZ")

	assert.LessOrEqual(t, len(channel), 63)
	assert.Equal(t, channel, pgNotifyChannelForBoundary("VeryLongBoundaryNameThatStillPassesValidation_ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	assert.NotEqual(t, pgNotifyChannelForBoundary("Orders"), pgNotifyChannelForBoundary("orders"))
	assert.Contains(t, channel, "orisun_events_")
}

func TestPGNotifySignal_Wait(t *testing.T) {
	t.Run("wakes on notify channel", func(t *testing.T) {
		ch := make(chan struct{}, 1)
		s := &pgNotifySignal{notifyCh: ch, catchupTicker: time.NewTicker(time.Hour)}
		defer s.Stop()
		ch <- struct{}{}
		require.NoError(t, s.Wait(context.Background()))
	})

	t.Run("wakes on catch-up tick", func(t *testing.T) {
		s := &pgNotifySignal{notifyCh: make(chan struct{}, 1), catchupTicker: time.NewTicker(5 * time.Millisecond)}
		defer s.Stop()
		require.NoError(t, s.Wait(context.Background()))
	})

	t.Run("returns on canceled context", func(t *testing.T) {
		s := &pgNotifySignal{notifyCh: make(chan struct{}, 1), catchupTicker: time.NewTicker(time.Hour)}
		defer s.Stop()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.ErrorIs(t, s.Wait(ctx), context.Canceled)
	})
}

func TestPGNotifyListener_Dispatch(t *testing.T) {
	t.Run("routes notification to the right boundary", func(t *testing.T) {
		l := newTestListener("b1", "b2")
		l.dispatch(&pgconn.Notification{Channel: pgNotifyChannelForBoundary("b1")})

		select {
		case <-l.signals["b1"]:
		default:
			t.Fatal("expected b1 signal to fire")
		}
		select {
		case <-l.signals["b2"]:
			t.Fatal("b2 must not fire for a b1 notification")
		default:
		}
	})

	t.Run("coalesces repeated notifications", func(t *testing.T) {
		l := newTestListener("b1")
		l.dispatch(&pgconn.Notification{Channel: pgNotifyChannelForBoundary("b1")})
		l.dispatch(&pgconn.Notification{Channel: pgNotifyChannelForBoundary("b1")})
		l.dispatch(&pgconn.Notification{Channel: pgNotifyChannelForBoundary("b1")})

		<-l.signals["b1"] // exactly one token despite three dispatches
		select {
		case <-l.signals["b1"]:
			t.Fatal("notifications must coalesce into a single pending token")
		default:
		}
	})

	t.Run("ignores unknown boundary", func(t *testing.T) {
		l := newTestListener("b1")
		l.dispatch(&pgconn.Notification{Channel: pgNotifyChannelForBoundary("unknown")})
		select {
		case <-l.signals["b1"]:
			t.Fatal("unknown boundary must not fire b1")
		default:
		}
	})

	t.Run("ignores malformed channel", func(t *testing.T) {
		l := newTestListener("b1")
		// Channel equal to the prefix has no boundary suffix — must be a no-op.
		assert.NotPanics(t, func() {
			l.dispatch(&pgconn.Notification{Channel: "orisun_events_"})
			l.dispatch(&pgconn.Notification{Channel: "other"})
		})
	})
}

func TestPGNotifyListener_Signal(t *testing.T) {
	t.Run("known boundary wakes on dispatch", func(t *testing.T) {
		l := newTestListener("b1")
		sig := l.Signal("b1", time.Hour)
		defer sig.Stop()

		l.dispatch(&pgconn.Notification{Channel: pgNotifyChannelForBoundary("b1")})
		require.NoError(t, sig.Wait(context.Background()))
	})

	t.Run("unknown boundary falls back to polling", func(t *testing.T) {
		l := newTestListener("b1")
		sig := l.Signal("nope", 5*time.Millisecond)
		defer sig.Stop()
		// Polling fallback fires purely on its own interval (no dispatch).
		require.NoError(t, sig.Wait(context.Background()))
	})
}

// --- integration: real Postgres LISTEN/NOTIFY -----------------------------

func TestPGNotifyListener_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping container-based integration test in short mode")
	}

	container, err := setupTestContainer(t)
	require.NoError(t, err)
	defer func() {
		if err := container.container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}()

	connStr := fmt.Sprintf(
		"host=%s port=%s user=test password=test dbname=testdb sslmode=disable",
		container.host, container.port,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mapping := map[string]config.BoundaryToPostgresSchemaMapping{
		"b1": {}, "b2": {},
	}
	listener, err := NewPGNotifyListener(ctx, connStr, mapping, testLogger{})
	require.NoError(t, err)
	// Close waits for Start to stop, so the context must be cancelled first.
	defer func() {
		cancel()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		listener.Close(closeCtx)
	}()

	go listener.Start(ctx)

	sigB1 := listener.Signal("b1", time.Hour) // catch-up disabled so only NOTIFY can wake it
	defer sigB1.Stop()
	sigB2 := listener.Signal("b2", time.Hour)
	defer sigB2.Stop()

	// Separate connection emits the NOTIFY, mirroring the pg_notify in the
	// insert function.
	notifier, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer notifier.Close(ctx)

	time.Sleep(200 * time.Millisecond) // let LISTEN register

	// Build the channel with Postgres' own md5() exactly as the insert function
	// does. This asserts the Go-side hash (LISTEN) agrees with the SQL-side hash
	// (NOTIFY) — the actual cross-language contract.
	_, err = notifier.Exec(ctx, "SELECT pg_notify('orisun_events_' || md5($1), '7')", "b1")
	require.NoError(t, err)

	waitCtx, waitCancel := context.WithTimeout(ctx, 3*time.Second)
	defer waitCancel()
	require.NoError(t, sigB1.Wait(waitCtx), "b1 must wake on its NOTIFY")

	// b2 received no NOTIFY and has catch-up disabled — it must not wake.
	b2Ctx, b2Cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer b2Cancel()
	assert.ErrorIs(t, sigB2.Wait(b2Ctx), context.DeadlineExceeded, "b2 must stay asleep without a NOTIFY")
}
