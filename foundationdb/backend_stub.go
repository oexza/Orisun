//go:build !foundationdb

package foundationdb

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	common "github.com/oexza/Orisun/admin/slices/common"
	config "github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	eventstore "github.com/oexza/Orisun/orisun"
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
