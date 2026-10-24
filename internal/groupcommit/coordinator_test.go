package groupcommit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testConfig() Config {
	return Config{MaxBatchRequests: 3, MaxBatchEvents: 4, MaxPending: 1, FlushTimeout: time.Second}
}
func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for coordinator")
		var zero T
		return zero
	}
}
func submit(c *Coordinator[int, int], ctx context.Context, boundary string, value int) <-chan completion[int] {
	result := make(chan completion[int], 1)
	go func() { v, err := c.Submit(ctx, boundary, value, 1); result <- completion[int]{value: v, err: err} }()
	return result
}

func TestBatchOrderAndLimits(t *testing.T) {
	for _, counts := range [][]int{{1, 1, 1, 1}, {3, 2, 1}, {5, 1}} {
		c := New(testConfig(), func(context.Context, string, []int) ([]int, error) { panic("unused") })
		queue := make(chan *request[int, int], len(counts))
		for i, count := range counts {
			queue <- &request[int, int]{prepared: i, events: count}
		}
		first := <-queue
		var got []int
		for first != nil {
			batch, carry := c.drainBatch(queue, first)
			require.LessOrEqual(t, len(batch), c.maxBatchRequests)
			total := 0
			for _, req := range batch {
				total += req.events
				got = append(got, req.prepared)
			}
			require.True(t, total <= c.maxBatchEvents || len(batch) == 1)
			first = carry
			if first == nil && len(queue) > 0 {
				first = <-queue
			}
		}
		for i := range counts {
			require.Equal(t, i, got[i])
		}
		require.Len(t, got, len(counts))
		c.Close()
	}
}

func TestFlushValidatesWholeOutcomeSetAndRecoversPanic(t *testing.T) {
	for _, mode := range []string{"short", "long", "error", "panic", "success"} {
		t.Run(mode, func(t *testing.T) {
			c := New(testConfig(), func(_ context.Context, _ string, values []int) ([]int, error) {
				switch mode {
				case "short":
					return []int{10}, nil
				case "long":
					return []int{10, 20, 30}, nil
				case "error":
					return []int{10, 20}, errors.New("commit failed")
				case "panic":
					panic("executor failed")
				}
				return []int{10, 20}, nil
			})
			defer c.Close()
			batch := []*request[int, int]{{ctx: t.Context(), prepared: 1, result: make(chan completion[int], 1)}, {ctx: t.Context(), prepared: 2, result: make(chan completion[int], 1)}}
			c.runFlush("a", batch)
			for i, req := range batch {
				result := await(t, req.result)
				if mode == "success" {
					require.NoError(t, result.err)
					require.Equal(t, (i+1)*10, result.value)
				} else {
					require.Error(t, result.err)
					require.Zero(t, result.value)
				}
				require.Empty(t, req.result)
			}
		})
	}
}

func TestCallerCancellationDoesNotCancelSharedFlush(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	c := New(testConfig(), func(ctx context.Context, _ string, values []int) ([]int, error) {
		started <- ctx
		<-release
		return values, nil
	})
	defer c.Close()
	ctx, cancel := context.WithCancel(t.Context())
	result := submit(c, ctx, "a", 1)
	flushCtx := await(t, started)
	cancel()
	require.ErrorIs(t, await(t, result).err, context.Canceled)
	require.NoError(t, flushCtx.Err())
	close(release)
}

func TestShutdownUnblocksAdmissionAndRejectsQueuedRequests(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(testConfig(), func(_ context.Context, _ string, values []int) ([]int, error) {
		started <- struct{}{}
		<-release
		return values, nil
	})
	first := submit(c, t.Context(), "a", 1)
	await(t, started)
	queued := submit(c, t.Context(), "a", 2)
	require.Eventually(t, func() bool { return c.Pending("a") == 1 }, time.Second, time.Millisecond)
	blocked := submit(c, t.Context(), "a", 3)
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	require.ErrorIs(t, await(t, blocked).err, ErrClosed)
	select {
	case <-closed:
		t.Fatal("close returned while execution was active")
	default:
	}
	close(release)
	require.NoError(t, await(t, first).err)
	require.ErrorIs(t, await(t, queued).err, ErrClosed)
	await(t, closed)
	c.Close()
	_, err := c.Submit(t.Context(), "a", 4, 1)
	require.ErrorIs(t, err, ErrClosed)
}

func TestCancelledQueuedRequestNeverReachesExecutor(t *testing.T) {
	started := make(chan int, 2)
	release := make(chan struct{})
	c := New(testConfig(), func(_ context.Context, _ string, values []int) ([]int, error) {
		started <- values[0]
		if values[0] == 1 {
			<-release
		}
		return values, nil
	})
	defer c.Close()
	first := submit(c, t.Context(), "a", 1)
	require.Equal(t, 1, await(t, started))
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := submit(c, ctx, "a", 2)
	require.Eventually(t, func() bool { return c.Pending("a") == 1 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, await(t, cancelled).err, context.Canceled)
	close(release)
	require.NoError(t, await(t, first).err)
	last := submit(c, t.Context(), "a", 3)
	require.Equal(t, 3, await(t, started))
	require.NoError(t, await(t, last).err)
}

func TestBoundariesExecuteIndependently(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(testConfig(), func(_ context.Context, boundary string, values []int) ([]int, error) {
		if boundary == "a" {
			started <- struct{}{}
			<-release
		}
		return values, nil
	})
	defer c.Close()
	a := submit(c, t.Context(), "a", 1)
	await(t, started)
	require.Equal(t, completion[int]{value: 2}, await(t, submit(c, t.Context(), "b", 2)))
	close(release)
	require.NoError(t, await(t, a).err)
}

func TestFlushTimeoutAndSubsequentRecovery(t *testing.T) {
	cfg := testConfig()
	cfg.FlushTimeout = 10 * time.Millisecond
	c := New(cfg, func(ctx context.Context, _ string, values []int) ([]int, error) {
		if values[0] == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return values, nil
	})
	defer c.Close()
	require.ErrorIs(t, await(t, submit(c, t.Context(), "a", 1)).err, context.DeadlineExceeded)
	require.Equal(t, completion[int]{value: 2}, await(t, submit(c, t.Context(), "a", 2)))
}

func TestFullBoundaryDoesNotBlockAnotherBoundaryAdmission(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(testConfig(), func(_ context.Context, boundary string, values []int) ([]int, error) {
		if boundary == "a" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		}
		return values, nil
	})
	defer c.Close()
	first := submit(c, t.Context(), "a", 1)
	await(t, started)
	second := submit(c, t.Context(), "a", 2)
	require.Eventually(t, func() bool { return c.Pending("a") == 1 }, time.Second, time.Millisecond)
	third := submit(c, t.Context(), "a", 3)
	// Even registration of a new boundary must be independent of a full queue.
	other := submit(c, t.Context(), "b", 4)
	got := await(t, other)
	close(release)
	require.Equal(t, completion[int]{value: 4}, got)
	for _, result := range []<-chan completion[int]{first, second, third} {
		require.NoError(t, await(t, result).err)
	}
}

func TestWorkerContinuesAfterExecutorPanic(t *testing.T) {
	c := New(testConfig(), func(_ context.Context, _ string, values []int) ([]int, error) {
		if values[0] == 1 {
			panic("injected")
		}
		return values, nil
	})
	defer c.Close()
	require.ErrorContains(t, await(t, submit(c, t.Context(), "a", 1)).err, "injected")
	require.Equal(t, completion[int]{value: 2}, await(t, submit(c, t.Context(), "a", 2)))
}
