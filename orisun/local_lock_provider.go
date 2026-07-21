package orisun

import (
	"context"
	"fmt"
	"sync"
)

// LocalLockProvider provides named, process-local leases. It is intended for
// single-process embedded runtimes where distributed lock coordination is not
// required, such as an on-device SQLite store.
type LocalLockProvider struct {
	mu    sync.Mutex
	locks map[string]*localLockLease
}

// NewLocalLockProvider creates an empty process-local lock provider.
func NewLocalLockProvider() *LocalLockProvider {
	return &LocalLockProvider{locks: make(map[string]*localLockLease)}
}

// Lock implements LockProvider. The lock remains held until ctx is cancelled.
// Callers that need explicit release should use AcquireLock.
func (p *LocalLockProvider) Lock(ctx context.Context, lockName string) error {
	_, err := p.AcquireLock(ctx, lockName)
	return err
}

// AcquireLock claims lockName until the returned lease is released or its
// parent context is cancelled. Acquisition is deliberately non-blocking to
// match subscription semantics: a duplicate subscriber name is rejected.
func (p *LocalLockProvider) AcquireLock(ctx context.Context, lockName string) (LockLease, error) {
	if p == nil {
		return nil, fmt.Errorf("local lock provider is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locks == nil {
		p.locks = make(map[string]*localLockLease)
	}
	if _, exists := p.locks[lockName]; exists {
		return nil, fmt.Errorf("lock %q is already held", lockName)
	}

	leaseCtx, cancel := context.WithCancel(ctx)
	lease := &localLockLease{
		provider: p,
		name:     lockName,
		ctx:      leaseCtx,
		cancel:   cancel,
	}
	p.locks[lockName] = lease
	go func() {
		<-leaseCtx.Done()
		lease.Release()
	}()
	return lease, nil
}

type localLockLease struct {
	provider *LocalLockProvider
	name     string
	ctx      context.Context
	cancel   context.CancelFunc
	once     sync.Once
}

func (l *localLockLease) Context() context.Context {
	return l.ctx
}

func (l *localLockLease) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.ctx.Err()
}

func (l *localLockLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.cancel()
		l.provider.mu.Lock()
		if l.provider.locks[l.name] == l {
			delete(l.provider.locks, l.name)
		}
		l.provider.mu.Unlock()
	})
}
