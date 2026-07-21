// Package fluttermobile exposes the desktop JSON bridge using only types that
// gomobile can bind. Flutter's Android and iOS plugins use this facade so the
// Dart API has the same handles, response envelopes, and error semantics on
// every native platform.
package fluttermobile

import (
	"time"

	desktopbridge "github.com/OrisunLabs/Orisun/embedded/sqlite/desktop"
)

// Bridge owns all embedded stores and subscriptions opened by one Flutter
// engine. Its methods intentionally return JSON response envelopes instead of
// Go errors so Android, iOS, and desktop decode exactly the same protocol.
type Bridge struct {
	inner *desktopbridge.Bridge
}

func NewBridge() *Bridge {
	return &Bridge{inner: desktopbridge.NewBridge()}
}

func (b *Bridge) ABIVersion() int {
	return desktopbridge.ABIVersion
}

func (b *Bridge) Open(dataDirectory, boundariesJSON string) string {
	return b.bridge().Open(dataDirectory, boundariesJSON)
}

func (b *Bridge) Close(storeHandle int64) string {
	return b.bridge().Close(uint64(storeHandle))
}

func (b *Bridge) Shutdown() {
	b.bridge().Shutdown()
}

func (b *Bridge) SaveEvents(storeHandle int64, boundary, eventsJSON, expectedPositionJSON, queryJSON string) string {
	return b.bridge().SaveEvents(uint64(storeHandle), boundary, eventsJSON, expectedPositionJSON, queryJSON)
}

func (b *Bridge) GetEvents(storeHandle int64, boundary, fromPositionJSON, queryJSON string, count int, descending bool) string {
	return b.bridge().GetEvents(uint64(storeHandle), boundary, fromPositionJSON, queryJSON, count, descending)
}

func (b *Bridge) GetLatestByCriteria(storeHandle int64, boundary, queryJSON string) string {
	return b.bridge().GetLatestByCriteria(uint64(storeHandle), boundary, queryJSON)
}

func (b *Bridge) CreateBoundaryIndex(storeHandle int64, boundary, name, fieldsJSON, conditionsJSON, combinator string) string {
	return b.bridge().CreateBoundaryIndex(uint64(storeHandle), boundary, name, fieldsJSON, conditionsJSON, combinator)
}

func (b *Bridge) DropBoundaryIndex(storeHandle int64, boundary, name string) string {
	return b.bridge().DropBoundaryIndex(uint64(storeHandle), boundary, name)
}

func (b *Bridge) Subscribe(storeHandle int64, boundary, subscriberName, afterPositionJSON, queryJSON string) string {
	return b.bridge().Subscribe(uint64(storeHandle), boundary, subscriberName, afterPositionJSON, queryJSON)
}

// SubscriptionNext waits up to timeoutMillis for a message. Platform plugins
// call it on a background task queue, never the Android or iOS UI thread.
func (b *Bridge) SubscriptionNext(subscriptionHandle, timeoutMillis int64) string {
	if timeoutMillis < 0 {
		timeoutMillis = 0
	}
	return b.bridge().NextSubscriptionMessage(
		uint64(subscriptionHandle),
		time.Duration(timeoutMillis)*time.Millisecond,
	)
}

func (b *Bridge) SubscriptionStop(subscriptionHandle int64) string {
	return b.bridge().StopSubscription(uint64(subscriptionHandle))
}

func (b *Bridge) bridge() *desktopbridge.Bridge {
	if b.inner == nil {
		b.inner = desktopbridge.NewBridge()
	}
	return b.inner
}
