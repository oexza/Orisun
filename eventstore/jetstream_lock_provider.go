package eventstore

import (
	"context"
	"errors"
	"fmt"
	logging "orisun/logging"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	lockBucketName = "ORISUN_LOCKS"
	lockTTL        = 30 * time.Second
	lockRetryDelay = 500 * time.Millisecond
	maxRetries     = 10
)

// JetStreamLockProvider implements the LockProvider interface using NATS JetStream
type JetStreamLockProvider struct {
	js     jetstream.JetStream
	bucket jetstream.KeyValue
	logger logging.Logger
}

// NewJetStreamLockProvider creates a new JetStreamLockProvider
func NewJetStreamLockProvider(ctx context.Context, js jetstream.JetStream, logger logging.Logger) (*JetStreamLockProvider, error) {

	// Create or get the KV bucket for locks
	bucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      lockBucketName,
		Description: "Distributed locks for Orisun",
		// TTL:         lockTTL,
		Storage:     jetstream.MemoryStorage,
		Replicas:    1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create lock bucket: %w", err)
	}

	return &JetStreamLockProvider{
		js:     js,
		bucket: bucket,
		logger: logger,
	}, nil
}

// Lock acquires a distributed lock with the given name
func (p *JetStreamLockProvider) Lock(ctx context.Context, lockName string) (UnlockFunc, error) {
	lockID := fmt.Sprintf("%s", lockName)
	
	// Try to acquire the lock with retries
	for i := 0; i < maxRetries; i++ {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Try to create the key (which will fail if it already exists)
		revision, err := p.bucket.Create(ctx, lockID, []byte("locked"))
		
		if err == nil {
			// Lock acquired successfully
			p.logger.Debugf("Lock acquired: %s (revision: %d)", lockID, revision)
			
			// Return unlock function
			return func() error {
				// Delete the key to release the lock
				err := p.bucket.Delete(context.Background(), lockID, jetstream.LastRevision(revision))
				if err != nil {
					p.logger.Warnf("Failed to release lock %s: %v", lockID, err)
					return fmt.Errorf("failed to release lock: %w", err)
				}
				p.logger.Infof("Lock released: %s", lockID)
				return nil
			}, nil
		}

		// If the error is not because the key already exists, return it
		if !errors.Is(err, jetstream.ErrKeyExists) {
			return nil, fmt.Errorf("failed to acquire lock: %w", err)
		}

		// Lock is already held, wait and retry
		p.logger.Debugf("Lock %s already held, retrying in %v", lockID, lockRetryDelay)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetryDelay):
			// Continue to next retry
		}
	}

	return nil, fmt.Errorf("failed to acquire lock after %d retries", maxRetries)
}