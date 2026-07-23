package foundationdb

import (
	"context"
	"fmt"

	boundarycatalog "github.com/OrisunLabs/Orisun/admin/slices/boundary_catalog"
	boundaryprovisioning "github.com/OrisunLabs/Orisun/admin/slices/boundary_provisioning"
	createboundary "github.com/OrisunLabs/Orisun/admin/slices/create_boundary"
	importboundary "github.com/OrisunLabs/Orisun/admin/slices/import_boundary"
	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	c "github.com/OrisunLabs/Orisun/config"
	fdbbackend "github.com/OrisunLabs/Orisun/foundationdb"
	"github.com/OrisunLabs/Orisun/internal/eventstoreadapter"
	l "github.com/OrisunLabs/Orisun/logging"
	natsruntime "github.com/OrisunLabs/Orisun/nats"
	"github.com/OrisunLabs/Orisun/orisun"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Store struct {
	*orisun.OrisunServer

	indexManager   orisun.BoundaryIndexManager
	adminBoundary  string
	boundaryEvents *eventstoreadapter.Adapter
	cancel         context.CancelFunc
	natsRuntime    *natsruntime.Runtime
	closeFDB       func(context.Context)
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

	runtime, err := fdbbackend.InitializeFoundationDBRuntime(
		runCtx,
		config.FoundationDB,
		config.Admin,
		[]string{config.Admin.Boundary},
		js,
		logger,
	)
	if err != nil {
		cancel()
		natsRuntime.Close()
		return nil, err
	}

	store, err := orisun.NewOrisunServer(
		runCtx, runtime.SaveEvents, runtime.GetEvents, runtime.LockProvider,
		js, runtime.InitialBoundaries, logger,
	)
	if err != nil {
		cancel()
		natsRuntime.Close()
		if runtime.Close != nil {
			runtime.Close(context.Background())
		}
		return nil, err
	}
	if err := store.EnableBoundaryActivationGate(config.Admin.Boundary); err != nil {
		cancel()
		natsRuntime.Close()
		if runtime.Close != nil {
			runtime.Close(context.Background())
		}
		return nil, err
	}
	boundaryEvents := eventstoreadapter.New(runtime.SaveEvents, runtime.GetEvents, store.SubscribeToEvents)

	pollingManager := orisun.StartEventPolling(
		runCtx, config, runtime.InitialBoundaries, runtime.LockProvider,
		runtime.GetEvents, js, runtime.EventPublishing, runtime.SignalProvider, logger,
	)
	provisionBoundary := func(provisionCtx context.Context, definition boundarymodel.Definition) error {
		if err := runtime.ProvisionBoundary(provisionCtx, definition); err != nil {
			return err
		}
		if err := store.EnsureBoundary(provisionCtx, definition.Name); err != nil {
			return err
		}
		return pollingManager.StartBoundary(definition.Name)
	}
	handler := boundaryprovisioning.NewBoundaryProvisioningEventHandler(
		config.Admin.Boundary, boundaryEvents, boundaryEvents, provisionBoundary, store.ActivateBoundary,
	)
	reconciliation, err := importboundary.ReconcileLegacyBoundaries(
		runCtx,
		fdbbackend.LegacyBoundaryDefinitions(runtime.InitialBoundaries, runtime.BoundaryNamespace),
		config.Admin.Boundary,
		boundaryEvents,
		boundaryEvents,
	)
	if err != nil {
		cancel()
		natsRuntime.Close()
		if runtime.Close != nil {
			runtime.Close(context.Background())
		}
		return nil, fmt.Errorf("migrate FoundationDB boundaries into catalog: %w", err)
	}
	logger.Infof(
		"Boundary catalog migration completed: imported=%d existing=%d",
		len(reconciliation.Imported), len(reconciliation.Existing),
	)
	subscriber := boundaryprovisioning.NewBoundaryProvisioningSubscriber(
		config.Admin.Boundary,
		boundaryEvents,
		boundaryEvents.Subscribe,
		handler.Handle,
		logger,
	)
	if err := subscriber.Replay(runCtx); err != nil {
		cancel()
		natsRuntime.Close()
		if runtime.Close != nil {
			runtime.Close(context.Background())
		}
		return nil, fmt.Errorf("replay boundary catalog into embedded FoundationDB runtime: %w", err)
	}
	go subscriber.Run(runCtx)

	return &Store{
		OrisunServer:   store,
		indexManager:   runtime.AdminDB,
		adminBoundary:  config.Admin.Boundary,
		boundaryEvents: boundaryEvents,
		cancel:         cancel,
		natsRuntime:    natsRuntime,
		closeFDB:       runtime.Close,
	}, nil
}

func (s *Store) CreateBoundary(ctx context.Context, definition boundarymodel.Definition) (boundarymodel.Boundary, error) {
	result, err := createboundary.CreateBoundaryCommandHandler(
		ctx,
		createboundary.CreateBoundaryCommand{
			Name: definition.Name, Description: definition.Description, Placement: definition.Placement,
			Metadata: createboundary.CommandMetadata{"source": "embedded_foundationdb", "operation": "create_boundary"},
		},
		s.adminBoundary, s.boundaryEvents, s.boundaryEvents,
	)
	return result.Boundary, err
}

func (s *Store) ImportBoundary(ctx context.Context, definition boundarymodel.Definition) (boundarymodel.Boundary, error) {
	result, err := importboundary.ImportBoundaryCommandHandler(
		ctx,
		importboundary.ImportBoundaryCommand{
			Name: definition.Name, Description: definition.Description, Placement: definition.Placement,
			Metadata: importboundary.CommandMetadata{"source": "embedded_foundationdb", "operation": "import_boundary"},
		},
		s.adminBoundary, s.boundaryEvents, s.boundaryEvents,
	)
	return result.Boundary, err
}

func (s *Store) ListBoundaries(ctx context.Context) ([]boundarymodel.Boundary, error) {
	return boundarycatalog.ListBoundariesQueryHandler(
		ctx, boundarycatalog.ListBoundariesQuery{}, s.adminBoundary, s.boundaryEvents,
	)
}

func (s *Store) GetBoundary(ctx context.Context, name string) (boundarymodel.Boundary, error) {
	return boundarycatalog.GetBoundaryQueryHandler(
		ctx, boundarycatalog.GetBoundaryQuery{Name: name}, s.adminBoundary, s.boundaryEvents,
	)
}

func (s *Store) CreateBoundaryIndex(ctx context.Context, boundary, name string, fields []orisun.BoundaryIndexField, conditions []orisun.BoundaryIndexCondition, combinator string) error {
	if err := s.RequireBoundaryActive(boundary); err != nil {
		return err
	}
	return s.indexManager.CreateBoundaryIndex(ctx, boundary, name, fields, conditions, combinator)
}

func (s *Store) DropBoundaryIndex(ctx context.Context, boundary, name string) error {
	if err := s.RequireBoundaryActive(boundary); err != nil {
		return err
	}
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
