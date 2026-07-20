package orisun

import (
	"context"
	"testing"
	"time"
)

func TestLocalLockProviderRejectsDuplicateAndReleases(t *testing.T) {
	provider := NewLocalLockProvider()
	lease, err := provider.AcquireLock(context.Background(), "mobile-subscription")
	if err != nil {
		t.Fatalf("AcquireLock() returned error: %v", err)
	}
	if _, err := provider.AcquireLock(context.Background(), "mobile-subscription"); err == nil {
		t.Fatal("duplicate AcquireLock() succeeded")
	}

	lease.Release()
	replacement, err := provider.AcquireLock(context.Background(), "mobile-subscription")
	if err != nil {
		t.Fatalf("AcquireLock() after release returned error: %v", err)
	}
	replacement.Release()
}

func TestLocalLockProviderContextCancellationReleases(t *testing.T) {
	provider := NewLocalLockProvider()
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := provider.AcquireLock(ctx, "mobile-subscription")
	if err != nil {
		t.Fatalf("AcquireLock() returned error: %v", err)
	}
	cancel()

	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("lease context was not cancelled")
	}

	deadline := time.Now().Add(time.Second)
	for {
		replacement, err := provider.AcquireLock(context.Background(), "mobile-subscription")
		if err == nil {
			replacement.Release()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock was not released after context cancellation: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLocalLockProviderDifferentNamesAreIndependent(t *testing.T) {
	provider := NewLocalLockProvider()
	first, err := provider.AcquireLock(context.Background(), "first")
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer first.Release()
	second, err := provider.AcquireLock(context.Background(), "second")
	if err != nil {
		t.Fatalf("acquire second lock: %v", err)
	}
	second.Release()
}
