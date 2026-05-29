package sqlite

import (
	"context"
	"fmt"
	"time"

	c "github.com/oexza/Orisun/config"
	l "github.com/oexza/Orisun/logging"
	natsruntime "github.com/oexza/Orisun/nats"
	"github.com/oexza/Orisun/orisun"
	sqlitebackend "github.com/oexza/Orisun/sqlite"
)

type Store struct {
	*orisun.OrisunServer

	cancel     context.CancelFunc
	natsServer interface{ Shutdown() }
	natsConn   interface{ Close() }
}

func Start(ctx context.Context, config c.AppConfig, logger l.Logger) (*Store, error) {
	config.Backend.Type = "sqlite"
	if config.Nats.Cluster.Enabled {
		return nil, fmt.Errorf("embedded sqlite does not support NATS clustering")
	}
	if config.Sqlite.Dir == "" {
		return nil, fmt.Errorf("embedded sqlite requires ORISUN_SQLITE_DIR")
	}

	runCtx, cancel := context.WithCancel(ctx)
	js, nc, ns := natsruntime.InitializeNATS(runCtx, config.Nats, logger)
	saveEvents, getEvents, lockProvider, _, eventPublishing, err := sqlitebackend.InitializeSqliteDatabase(
		runCtx,
		config.Sqlite,
		config.Admin,
		config.GetBoundaryNames(),
		js,
		logger,
	)
	if err != nil {
		cancel()
		nc.Close()
		ns.Shutdown()
		return nil, err
	}

	store, err := orisun.NewOrisunServer(runCtx, saveEvents, getEvents, lockProvider, js, config.GetBoundaryNames(), logger)
	if err != nil {
		cancel()
		nc.Close()
		ns.Shutdown()
		return nil, err
	}

	signalProvider := func(boundary string) orisun.EventSignal {
		return orisun.NewPollingSignal(time.Second)
	}
	orisun.StartEventPolling(runCtx, config, lockProvider, getEvents, js, eventPublishing, signalProvider, logger)

	return &Store{
		OrisunServer: store,
		cancel:       cancel,
		natsServer:   ns,
		natsConn:     nc,
	}, nil
}

func (s *Store) Close() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	if s.natsServer != nil {
		s.natsServer.Shutdown()
	}
}
