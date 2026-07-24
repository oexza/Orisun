//go:build !foundationdb

package foundationdb

import (
	"context"
	"fmt"

	common "github.com/OrisunLabs/Orisun/admin/slices/common"
	config "github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/logging"
	eventstore "github.com/OrisunLabs/Orisun/orisun"
	"github.com/nats-io/nats.go/jetstream"
)

func InitializeFoundationDB(
	ctx context.Context,
	fdbCfg config.FoundationDBConfig,
	adminCfg config.AdminConfig,
	boundaries []string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, common.DB, eventstore.EventPublishingTracker, func(string) eventstore.EventSignal, func(context.Context), error) {
	return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("foundationdb backend requires building with -tags foundationdb and installed FoundationDB client libraries")
}

func InitializeFoundationDBRuntime(
	ctx context.Context,
	fdbCfg config.FoundationDBConfig,
	adminCfg config.AdminConfig,
	boundaries []string,
	js jetstream.JetStream,
	logger logging.Logger,
) (*DatabaseRuntime, error) {
	return nil, fmt.Errorf("foundationdb backend requires building with -tags foundationdb and installed FoundationDB client libraries")
}
