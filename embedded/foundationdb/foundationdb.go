package foundationdb

import (
	"context"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	c "github.com/oexza/Orisun/config"
	fdbbackend "github.com/oexza/Orisun/foundationdb"
	l "github.com/oexza/Orisun/logging"
	natsruntime "github.com/oexza/Orisun/nats"
	"github.com/oexza/Orisun/orisun"
)

type Store struct {
	*orisun.OrisunServer

	indexManager orisun.BoundaryIndexManager
	cancel       context.CancelFunc
	natsRuntime  *natsruntime.Runtime
	closeFDB     func(context.Context)
}

type StartOption func(*startOptions)

type startOptions struct {
	natsOptions []natsruntime.Option
}

func WithNATSURL(url string, opts ...natsgo.Option) StartOption {
	return func(o *startOptions) {
		o.natsOptions = append(o.natsOptions, natsruntime.WithURL(url, opts...))
	}
}

func WithNATSConnection(conn *natsgo.Conn, opts ...jetstream.JetStreamOpt) StartOption {
	return func(o *startOptions) {
		o.natsOptions = append(o.natsOptions, natsruntime.WithConnection(conn, opts...))
	}
}

func WithJetStream(js jetstream.JetStream) StartOption {
	return func(o *startOptions) {
		o.natsOptions = append(o.natsOptions, natsruntime.WithJetStream(js))
	}
}

func Start(ctx context.Context, config c.AppConfig, logger l.Logger, opts ...StartOption) (*Store, error) {
	config.Backend.Type = "foundationdb"
	runCtx, cancel := context.WithCancel(ctx)

	startOpts := startOptions{}
	for _, apply := range opts {
		apply(&startOpts)
	}

	natsRuntime, err := natsruntime.Start(runCtx, config.Nats, logger, startOpts.natsOptions...)
	if err != nil {
		cancel()
		return nil, err
	}
	js := natsRuntime.JetStream

	saveEvents, getEvents, lockProvider, adminDB, eventPublishing, signalProvider, closeFn, err := fdbbackend.InitializeFoundationDB(
		runCtx,
		config.FoundationDB,
		config.Admin,
		config.GetBoundaryNames(),
		js,
		logger,
	)
	if err != nil {
		cancel()
		natsRuntime.Close()
		return nil, err
	}

	store, err := orisun.NewOrisunServer(runCtx, saveEvents, getEvents, lockProvider, js, config.GetBoundaryNames(), logger)
	if err != nil {
		cancel()
		natsRuntime.Close()
		if closeFn != nil {
			closeFn(context.Background())
		}
		return nil, err
	}

	orisun.StartEventPolling(runCtx, config, lockProvider, getEvents, js, eventPublishing, signalProvider, logger)

	return &Store{
		OrisunServer: store,
		indexManager: adminDB,
		cancel:       cancel,
		natsRuntime:  natsRuntime,
		closeFDB:     closeFn,
	}, nil
}

func (s *Store) CreateBoundaryIndex(ctx context.Context, boundary, name string, fields []orisun.BoundaryIndexField, conditions []orisun.BoundaryIndexCondition, combinator string) error {
	return s.indexManager.CreateBoundaryIndex(ctx, boundary, name, fields, conditions, combinator)
}

func (s *Store) DropBoundaryIndex(ctx context.Context, boundary, name string) error {
	return s.indexManager.DropBoundaryIndex(ctx, boundary, name)
}

func (s *Store) NATSConnection() *natsgo.Conn {
	if s == nil || s.natsRuntime == nil {
		return nil
	}
	return s.natsRuntime.Conn
}

func (s *Store) JetStream() jetstream.JetStream {
	if s == nil || s.natsRuntime == nil {
		return nil
	}
	return s.natsRuntime.JetStream
}

func (s *Store) Close(ctx context.Context) {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.closeFDB != nil {
		s.closeFDB(ctx)
	}
	if s.natsRuntime != nil {
		s.natsRuntime.Close()
	}
}
