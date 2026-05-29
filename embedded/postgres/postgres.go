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

	cancel     context.CancelFunc
	natsServer interface{ Shutdown() }
	natsConn   interface{ Close() }
	closePG    func(context.Context)
}

func Start(ctx context.Context, config c.AppConfig, logger l.Logger) (*Store, error) {
	config.Backend.Type = "postgres"
	runCtx, cancel := context.WithCancel(ctx)

	js, nc, ns := natsruntime.InitializeNATS(runCtx, config.Nats, logger)
	saveEvents, getEvents, lockProvider, _, eventPublishing, pgListener := pg.InitializePostgresDatabase(runCtx, config.Postgres, config.Admin, js, logger)

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
		cancel:       cancel,
		natsServer:   ns,
		natsConn:     nc,
		closePG:      closePG,
	}, nil
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
