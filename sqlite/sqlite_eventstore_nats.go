//go:build !orisun_embedded

package sqlite

import (
	"context"

	common "github.com/OrisunLabs/Orisun/admin/slices/common"
	config "github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/logging"
	eventstore "github.com/OrisunLabs/Orisun/orisun"
	"github.com/nats-io/nats.go/jetstream"
)

// InitializeSqliteDatabase opens per-boundary SQLite pools and constructs the
// backend interfaces used by the server runtime. Embedded builds inject a
// process-local lock provider instead and exclude this file entirely.
func InitializeSqliteDatabase(
	ctx context.Context,
	sqliteCfg config.SqliteConfig,
	adminCfg config.AdminConfig,
	boundaries []string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, common.DB, eventstore.EventPublishingTracker, func(string) eventstore.EventSignal, error) {
	return initializeSqliteDatabase(
		ctx,
		sqliteCfg,
		adminCfg,
		boundaries,
		nil,
		func() (eventstore.LockProvider, error) {
			return eventstore.NewJetStreamLockProvider(ctx, js, logger)
		},
		nil,
		logger,
	)
}
