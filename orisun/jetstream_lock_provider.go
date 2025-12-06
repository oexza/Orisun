package orisun

import (
	"context"
	"errors"
	"fmt"
	logging "github.com/oexza/Orisun/logging"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	lockBucketName = "ORISUN_LOCKS"
	lockRetryDelay = 100 * time.Millisecond
	maxRetries     = 3
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
// The lock is automatically released when the context is done
func (p *JetStreamLockProvider) Lock(ctx context.Context, lockName string) error {
	lockID := fmt.Sprint(lockName)
	p.logger.Infof("Locking: %s", lockID)
	// Try to acquire the lock with retries
	for range maxRetries {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Try to create the key (which will fail if it already exists)
		revision, err := p.bucket.Create(ctx, lockID, []byte("locked"))

		if err == nil {
			// Lock acquired successfully
			p.logger.Infof("Lock acquired: %s (revision: %d)", lockID, revision)
			// Start goroutine to automatically unlock when context is done
			go func() {
				<-ctx.Done()
				// Delete the key to release the lock
				err := p.bucket.Delete(context.Background(), lockID, jetstream.LastRevision(revision))
				if err != nil {
					p.logger.Warnf("Failed to release lock %s: %v", lockID, err)
				} else {
					p.logger.Infof("Lock released: %s", lockID)
				}
			}()

			return nil
		}
		p.logger.Errorf("Failed to acquire lock: %v", err)
		// If the error is not because the key already exists, return it
		if !errors.Is(err, jetstream.ErrKeyExists) {
			return fmt.Errorf("failed to acquire lock: %w", err)
		}

		// Lock is already held, wait and retry
		p.logger.Infof("Lock %s already held, retrying in %v", lockID, lockRetryDelay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockRetryDelay):
			// Continue to next retry
		}
	}

	return fmt.Errorf("failed to acquire lock after %d retries", maxRetries)
}
