package postgres

import (
	"context"
	"time"

	c "github.com/oexza/Orisun/config"
	l "github.com/oexza/Orisun/logging"
	natsruntime "github.com/oexza/Orisun/nats"
	"github.com/oexza/Orisun/orisun"
	pg "github.com/oexza/Orisun/postgres"
)

type Store struct {
	*orisun.OrisunServer

	indexManager orisun.BoundaryIndexManager
	cancel       context.CancelFunc
	natsServer   interface{ Shutdown() }
	natsConn     interface{ Close() }
	closePG      func(context.Context)
}

func Start(ctx context.Context, config c.AppConfig, logger l.Logger) (*Store, error) {
	config.Backend.Type = "postgres"
	runCtx, cancel := context.WithCancel(ctx)

	js, nc, ns := natsruntime.InitializeNATS(runCtx, config.Nats, logger)
	saveEvents, getEvents, lockProvider, adminDB, eventPublishing, pgListener := pg.InitializePostgresDatabase(runCtx, config.Postgres, config.Admin, js, logger)

	store, err := orisun.NewOrisunServer(runCtx, saveEvents, getEvents, lockProvider, js, config.GetBoundaryNames(), logger)
	if err != nil {
		cancel()
		nc.Close()
		ns.Shutdown()
		return nil, err
	}

	var signalProvider func(string) orisun.EventSignal
	var stopListener context.CancelFunc
	var closePG func(context.Context)
	if pgListener != nil {
		listenerCtx, stop := context.WithCancel(runCtx)
		stopListener = stop
		go pgListener.Start(listenerCtx)
		signalProvider = func(boundary string) orisun.EventSignal {
			return pgListener.Signal(boundary, 30*time.Second)
		}
		closePG = func(ctx context.Context) {
			if stopListener != nil {
				stopListener()
			}
			waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
			defer waitCancel()
			pgListener.Close(waitCtx)
		}
	} else {
		signalProvider = func(boundary string) orisun.EventSignal {
			return orisun.NewPollingSignal(time.Second)
		}
	}

	orisun.StartEventPolling(runCtx, config, lockProvider, getEvents, js, eventPublishing, signalProvider, logger)

	return &Store{
		OrisunServer: store,
		indexManager: adminDB,
		cancel:       cancel,
		natsServer:   ns,
		natsConn:     nc,
		closePG:      closePG,
	}, nil
}

func (s *Store) CreateBoundaryIndex(ctx context.Context, boundary, name string, fields []orisun.BoundaryIndexField, conditions []orisun.BoundaryIndexCondition, combinator string) error {
	return s.indexManager.CreateBoundaryIndex(ctx, boundary, name, fields, conditions, combinator)
}

func (s *Store) DropBoundaryIndex(ctx context.Context, boundary, name string) error {
	return s.indexManager.DropBoundaryIndex(ctx, boundary, name)
}

func (s *Store) Close(ctx context.Context) {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.closePG != nil {
		s.closePG(ctx)
	}
	if s.natsConn != nil {
		s.natsConn.Close()
	}
	if s.natsServer != nil {
		s.natsServer.Shutdown()
	}
}
