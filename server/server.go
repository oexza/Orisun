package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oexza/Orisun/admin"
	common "github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/admin/slices/create_user"
	"github.com/oexza/Orisun/admin/slices/dashboard/event_count"
	"github.com/oexza/Orisun/admin/slices/dashboard/user_count"
	up "github.com/oexza/Orisun/admin/slices/users_projection"
	c "github.com/oexza/Orisun/config"
	l "github.com/oexza/Orisun/logging"
	nats2 "github.com/oexza/Orisun/nats"
	"github.com/oexza/Orisun/orisun"
	pb "github.com/oexza/Orisun/orisun"
	_ "go.uber.org/automaxprocs" // auto-set GOMAXPROCS from cgroup CPU limit
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// ensureJetStreamStreamIsProperlySetup ensures that a JetStream stream with the given name exists.
// If the stream doesn't exist, it creates a new one with default configuration.
func ensureJetStreamStreamIsProperlySetup(ctx context.Context, js jetstream.JetStream, streamName string, logger l.Logger) (jetstream.Stream, error) {
	natsStream, err := js.Stream(ctx, streamName)

	if err != nil && err.Error() != jetstream.ErrStreamNotFound.Error() {
		return nil, fmt.Errorf("failed to subscribe: %v", err)
	}

	if natsStream == nil {
		natsStream, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:              streamName,
			Subjects:          []string{streamName + ".>"},
			Storage:           jetstream.MemoryStorage,
			MaxConsumers:      -1, // Allow unlimited consumers
			MaxAge:            24 * time.Second,
			MaxMsgsPerSubject: 10,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add stream: %v", err)
		}
		logger.Debugf("stream info: %v", natsStream)
	}

	return natsStream, nil
}

// setupJetStreamConsumer sets up a JetStream consumer for the given stream and handles message consumption.
// It creates or updates the consumer, sets up message handling with retry logic, and cleans up resources when done.
func setupJetStreamConsumer(ctx context.Context, js jetstream.JetStream, streamName string, consumerName string, subject string, handler *orisun.MessageHandler[common.PublishRequest], logger l.Logger) error {
	jetStreamStream, err := ensureJetStreamStreamIsProperlySetup(ctx, js, streamName, logger)
	if err != nil {
		return err
	}

	consumer, err := jetStreamStream.CreateOrUpdateConsumer(
		ctx,
		jetstream.ConsumerConfig{
			Name:          consumerName,
			DeliverPolicy: jetstream.DeliverNewPolicy,
			AckPolicy:     jetstream.AckNonePolicy,
			MaxAckPending: 100,
			FilterSubjects: []string{
				streamName + "." + subject,
			},
		},
	)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to subscribe: %v", err)
	}

	consumption, err := consumer.Consume(func(natsNsg jetstream.Msg) {
		// Try to send the message to the handler
		for {
			logger.Debugf("Consuming message: %v", string(natsNsg.Data()))
			if ctx.Err() != nil {
				return
			}
			message := common.PublishRequest{}
			json.Unmarshal(natsNsg.Data(), &message)
			logger.Debugf("message: %v", message)
			err = handler.Send(&message)

			if err == nil {
				// Message sent successfully, break the retry loop
				natsNsg.Ack()
				break
			}

			if ctx.Err() != nil {
				// Context is done, exit handler
				logger.Infof("Context done, stopping message handling: %v", ctx.Err())
				return
			}

			// Log the error and retry
			logger.Errorf("Error handling message: %v. Retrying...", err)
			// Add a short delay before retrying
			time.Sleep(time.Millisecond * 100)
		}
	})

	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		logger.Infof("Context done, stopping message consumption: %v", ctx.Err())
		consumption.Stop()
		js.DeleteConsumer(ctx, streamName, consumerName)
	}()

	return nil
}

// getUserByIdWrapper implements the up.GetUserById interface using adminDB.
type getUserByIdWrapper struct {
	adminDB common.DB
}

func (w getUserByIdWrapper) Get(userId string) (orisun.User, error) {
	return w.adminDB.GetUserById(userId)
}

type Backend struct {
	SaveEvents      orisun.EventsSaver
	GetEvents       orisun.EventsRetriever
	LockProvider    orisun.LockProvider
	AdminDB         common.DB
	EventPublishing orisun.EventPublishingTracker
	SignalProvider  func(string) orisun.EventSignal
	Start           func(context.Context)
	Close           func(context.Context)
}

type BackendInitializer func(context.Context, c.AppConfig, jetstream.JetStream, l.Logger) (Backend, error)

func Run(ctx context.Context, config c.AppConfig, AppLogger l.Logger, initializeBackend BackendInitializer) {
	AppLogger.Debugf("config: %v", config)

	// Log Go runtime tuning. GOMAXPROCS auto-set from cgroup via automaxprocs side-effect import.
	// GOMEMLIMIT/GOGC honor the standard Go env vars; tune in container/orchestrator manifest.
	memLimit := debug.SetMemoryLimit(-1)
	gcPercent := debug.SetGCPercent(-1)
	debug.SetGCPercent(gcPercent) // restore after read
	AppLogger.Infof("runtime: GOMAXPROCS=%d GOMEMLIMIT=%d GOGC=%d", runtime.GOMAXPROCS(0), memLimit, gcPercent)

	// Start pprof server if enabled
	if config.Pprof.Enabled {
		addr := fmt.Sprintf(":%s", config.Pprof.Port)
		go func() {
			AppLogger.Infof("pprof listening on %s", addr)
			if err := http.ListenAndServe(addr, nil); err != nil {
				AppLogger.Errorf("pprof server error: %v", err)
			}
		}()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Initialize NATS
	js, nc, ns := nats2.InitializeNATS(ctx, config.Nats, AppLogger)
	defer nc.Close()
	defer ns.Shutdown()

	backend, err := initializeBackend(ctx, config, js, AppLogger)
	if err != nil {
		AppLogger.Fatalf("Failed to initialize backend: %v", err)
	}
	if backend.Close != nil {
		defer backend.Close(context.Background())
	}
	if backend.Start != nil {
		backend.Start(ctx)
	}

	// Initialize EventStore
	eventStore := pb.InitializeEventStore(
		ctx,
		config,
		backend.SaveEvents,
		backend.GetEvents,
		backend.LockProvider,
		backend.AdminDB,
		js,
		AppLogger,
	)

	signalProvider := backend.SignalProvider
	if signalProvider == nil {
		signalProvider = func(boundary string) orisun.EventSignal {
			return orisun.NewPollingSignal(1 * time.Second)
		}
	}
	pb.StartEventPolling(ctx, config, backend.LockProvider, backend.GetEvents, js, backend.EventPublishing, signalProvider, AppLogger)

	// Start projectors
	pubsubStreamName := "ORISUN-ADMIN"
	var jetStreamPublishFunction = func(ctx context.Context, req *common.PublishRequest) error {
		AppLogger.Debugf("Publishing to jetstream: %v", req)

		_, err := ensureJetStreamStreamIsProperlySetup(ctx, js, pubsubStreamName, AppLogger)
		if err != nil {
			return err
		}

		data, err := json.Marshal(req)
		if err != nil {
			return err
		}

		res, err := js.Publish(ctx, pubsubStreamName+"."+req.Subject, data)
		if err != nil {
			AppLogger.Errorf("Failed to publish to pubsub: %v", err)
			return err
		}

		AppLogger.Debugf("Published to jetstream: %v", res)
		return nil
	}

	getUserCount := func() (user_count.UserCountReadModel, error) {
		count, err := backend.AdminDB.GetUsersCount()
		if err != nil {
			return user_count.UserCountReadModel{}, err
		}
		return user_count.UserCountReadModel{
			Count: count,
		}, nil
	}

	getEventCount := func(boundary string) (event_count.EventCountReadModel, error) {
		count, err := backend.AdminDB.GetEventsCount(boundary)
		if err != nil {
			return event_count.EventCountReadModel{}, err
		}
		return event_count.EventCountReadModel{
			Count: count,
		}, nil
	}

	startUserCountProjector(
		ctx,
		config.Admin.Boundary,
		backend.AdminDB.GetProjectorLastPosition,
		backend.AdminDB.UpdateProjectorPosition,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *orisun.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		getUserCount,
		jetStreamPublishFunction,
		backend.AdminDB.SaveUsersCount,
		AppLogger,
	)

	startEventCountProjector(
		ctx,
		config.GetBoundaryNames(),
		backend.AdminDB.GetProjectorLastPosition,
		backend.AdminDB.UpdateProjectorPosition,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *orisun.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		getEventCount,
		jetStreamPublishFunction,
		backend.AdminDB.SaveEventCount,
		AppLogger,
	)

	startUserProjector(
		ctx,
		config.Admin.Boundary,
		backend.AdminDB.GetProjectorLastPosition,
		backend.AdminDB.UpdateProjectorPosition,
		backend.AdminDB.UpsertUser,
		backend.AdminDB.DeleteUser,
		backend.AdminDB.GetUserById,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *orisun.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		eventStore.SaveEvents,
		eventStore.GetEvents,
		AppLogger,
	)

	startAuthUserProjector(
		ctx,
		config.Admin.Boundary,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *orisun.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		AppLogger,
	)

	// Create default user
	err = createDefaultUser(
		ctx,
		config.Admin.Boundary,
		*eventStore,
		AppLogger,
	)
	if err != nil {
		AppLogger.Infof("%v", err)
	}

	// Create authenticator for gRPC authentication
	authenticator := admin.NewAuthenticator(
		eventStore.GetEvents,
		AppLogger,
		config.Admin.Boundary,
		backend.AdminDB.GetUserByUsername,
	)

	// Start gRPC server
	startGRPCServer(config, eventStore, authenticator, backend.AdminDB, AppLogger)
}

func createDefaultUser(ctx context.Context, adminBoundary string, eventstore pb.EventStore, logger l.Logger) error {
	var userExistsError create_user.UserExistsError
	if _, err := create_user.CreateUser(
		ctx,
		"admin",
		"admin",
		"changeit",
		[]orisun.Role{orisun.RoleAdmin},
		adminBoundary,
		eventstore.SaveEvents,
		eventstore.GetEvents,
		logger,
		nil,
	); err != nil && !errors.As(err, &userExistsError) {
		return err
	}
	return nil
}

func startUserProjector(
	ctx context.Context,
	adminBoundary string,
	getProjectorLastPosition common.GetProjectorLastPositionType,
	updateProjectorPosition common.UpdateProjectorPositionType,
	createNewUser up.CreateNewUserType,
	deleteUser up.DeleteUserType,
	getUserById up.GetUserById,
	subscribeToEvents common.SubscribeToEventStoreType,
	saveEvents common.SaveEventsType,
	getEvents common.GetEventsType,
	logger l.Logger) {
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		userProjector := up.NewUserProjector(
			getProjectorLastPosition,
			updateProjectorPosition,
			createNewUser,
			deleteUser,
			getUserById,
			saveEvents,
			getEvents,
			logger,
			adminBoundary,
			subscribeToEvents,
		)
		backoff := orisun.Backoff{Base: 100 * time.Millisecond, Max: 5 * time.Second}
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			default:
				err := userProjector.Start(groupCtx)

				if err != nil {
					logger.Debugf("Failed to start user projection (likely due to lock contention): %v - will retry", err)
					if waitErr := backoff.Wait(groupCtx); waitErr != nil {
						return waitErr
					}
					continue
				}
				logger.Info("User projector started")
				return nil
			}
		}
	})
}

func startAuthUserProjector(
	ctx context.Context,
	adminBoundary string,
	subscribeToEvents common.SubscribeToEventStoreType,
	logger l.Logger) {
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		userProjector := admin.NewAuthUserProjector(
			logger,
			subscribeToEvents,
			adminBoundary,
		)
		backoff := orisun.Backoff{Base: 100 * time.Millisecond, Max: 5 * time.Second}
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			default:
				err := userProjector.Start(groupCtx)

				if err != nil {
					logger.Debugf("Failed to start auth user projection (likely due to lock contention): %v - will retry", err)
					if waitErr := backoff.Wait(groupCtx); waitErr != nil {
						return waitErr
					}
					continue
				}
				logger.Info("Auth user projector started")
				return nil
			}
		}
	})
}

func startUserCountProjector(
	ctx context.Context,
	adminBoundary string,
	getProjectorLastPosition common.GetProjectorLastPositionType,
	updateProjectorPosition common.UpdateProjectorPositionType,
	subscribeToEvents common.SubscribeToEventStoreType,
	getUserCount user_count.GetUserCount,
	publishToPubSub common.PublishToPubSubType,
	saveUserCount user_count.SaveUserCount,
	logger l.Logger) {

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		userProjector := user_count.NewUserCountProjection(
			adminBoundary,
			getProjectorLastPosition,
			publishToPubSub,
			getUserCount,
			saveUserCount,
			subscribeToEvents,
			updateProjectorPosition,
			logger,
		)
		backoff := orisun.Backoff{Base: 100 * time.Millisecond, Max: 5 * time.Second}
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			default:
				err := userProjector.Start(groupCtx)

				if err != nil {
					logger.Debugf("Failed to start user count projection (likely due to lock contention): %v - will retry", err)
					if waitErr := backoff.Wait(groupCtx); waitErr != nil {
						return waitErr
					}
					continue
				}
				logger.Info("User count projector started")
				return nil
			}
		}
	})

}

func startEventCountProjector(
	ctx context.Context,
	boundaries []string,
	getProjectorLastPosition common.GetProjectorLastPositionType,
	updateProjectorPosition common.UpdateProjectorPositionType,
	subscribeToEvents common.SubscribeToEventStoreType,
	getEventCount event_count.GetEventCount,
	publishToPubSub common.PublishToPubSubType,
	saveEventCount event_count.SaveEventCount,
	logger l.Logger) {
	group, groupCtx := errgroup.WithContext(ctx)
	for _, boundary := range boundaries {
		b := boundary
		group.Go(func() error {
			eventProjector := event_count.NewEventCountProjection(
				b,
				getProjectorLastPosition,
				publishToPubSub,
				getEventCount,
				saveEventCount,
				subscribeToEvents,
				updateProjectorPosition,
				logger,
			)
			backoff := orisun.Backoff{Base: 100 * time.Millisecond, Max: 5 * time.Second}
			for {
				select {
				case <-groupCtx.Done():
					return groupCtx.Err()
				default:
					err := eventProjector.Start(groupCtx)

					if err != nil {
						logger.Debugf("Failed to start event count projection for boundary %s (likely due to lock contention): %v - will retry", b, err)
						if waitErr := backoff.Wait(groupCtx); waitErr != nil {
							return waitErr
						}
						continue
					}
					logger.Infof("Event count projector started for boundary %s", b)
					return nil
				}
			}
		})
	}

}

func streamErrorInterceptor(logger l.Logger) grpc.StreamServerInterceptor {
	return func(srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler) error {
		err := handler(srv, ss)
		if err != nil {
			logger.Errorf("Error in streaming RPC %s: %v", info.FullMethod, err)
			return status.Errorf(codes.Internal, "Error: %v", err)
		}

		return nil
	}
}

func recoveryInterceptor(logger l.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("Panic in %s: %v\nStack Trace:\n%s", info.FullMethod, r, debug.Stack())
				err = status.Errorf(codes.Internal, "Internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// clientAcceptsGzip returns true when the inbound RPC advertises gzip in grpc-accept-encoding.
// Server only enables compression when the client explicitly supports it (older clients
// without the gzip codec registered keep working uncompressed).
func clientAcceptsGzip(ctx context.Context) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}
	for _, ae := range md.Get("grpc-accept-encoding") {
		for _, enc := range strings.Split(ae, ",") {
			if strings.TrimSpace(enc) == "gzip" {
				return true
			}
		}
	}
	return false
}

func compressionUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if clientAcceptsGzip(ctx) {
			_ = grpc.SetSendCompressor(ctx, "gzip")
		}
		return handler(ctx, req)
	}
}

func compressionStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if clientAcceptsGzip(ss.Context()) {
			_ = grpc.SetSendCompressor(ss.Context(), "gzip")
		}
		return handler(srv, ss)
	}
}

// LoadTLSCredentials creates TLS credentials from the configured certificate files.
func LoadTLSCredentials(config c.AppConfig, logger l.Logger) (credentials.TransportCredentials, error) {
	// Load server certificate and key
	cert, err := tls.LoadX509KeyPair(config.Grpc.TLS.CertFile, config.Grpc.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// If client authentication is required, load CA certificate
	if config.Grpc.TLS.ClientAuthRequired && config.Grpc.TLS.CAFile != "" {
		caCert, err := os.ReadFile(config.Grpc.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	// Create credentials from TLS config
	creds := credentials.NewTLS(tlsConfig)

	logger.Infof("TLS enabled - Cert: %s, Key: %s, ClientAuth: %v",
		config.Grpc.TLS.CertFile,
		config.Grpc.TLS.KeyFile,
		config.Grpc.TLS.ClientAuthRequired,
	)

	return creds, nil
}

func startGRPCServer(config c.AppConfig, eventStore pb.EventStoreServer,
	authenticator *admin.Authenticator, adminDB common.DB, logger l.Logger) {
	// Initialize OpenTelemetry
	var otelShutdown func(context.Context) error
	if config.OpenTelemetry.Enabled {
		serviceName := config.OpenTelemetry.ServiceName
		if serviceName == "" {
			serviceName = "orisun"
		}
		var err error
		otelShutdown, err = admin.InitTracer(serviceName, config.OpenTelemetry.Endpoint, logger)
		if err != nil {
			logger.Errorf("Failed to initialize OpenTelemetry: %v", err)
		}
		if otelShutdown != nil {
			defer func() {
				if err := otelShutdown(context.Background()); err != nil {
					logger.Errorf("Failed to shutdown OpenTelemetry: %v", err)
				}
			}()
		}
	}

	// Prepare gRPC server options
	var serverOpts []grpc.ServerOption

	// Add TLS credentials if enabled
	if config.Grpc.TLS.Enabled {
		tlsCreds, err := LoadTLSCredentials(config, logger)
		if err != nil {
			logger.Fatalf("Failed to load TLS credentials: %v", err)
		}
		serverOpts = append(serverOpts, grpc.Creds(tlsCreds))
		logger.Infof("gRPC server running in TLS mode")
	} else {
		logger.Infof("gRPC server running in insecure mode (TLS disabled)")
	}

	// Add interceptors and other options
	serverOpts = append(serverOpts,
		grpc.ChainUnaryInterceptor(
			compressionUnaryInterceptor(),
			admin.UnaryTracingInterceptor(logger),
			admin.UnaryAuthInterceptor(authenticator, logger),
			recoveryInterceptor(logger),
		),
		grpc.ChainStreamInterceptor(
			compressionStreamInterceptor(),
			admin.StreamTracingInterceptor(logger),
			admin.StreamAuthInterceptor(authenticator, logger),
			streamErrorInterceptor(logger),
		),
		grpc.ConnectionTimeout(config.Grpc.ConnectionTimeout),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    config.Grpc.KeepAliveTime,
			Timeout: config.Grpc.KeepAliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             config.Grpc.KeepaliveMinTime,
			PermitWithoutStream: config.Grpc.KeepalivePermitWithoutStream,
		}),
		grpc.MaxConcurrentStreams(config.Grpc.MaxConcurrentStreams),
		grpc.MaxRecvMsgSize(config.Grpc.MaxReceiveMessageSize),
		grpc.MaxSendMsgSize(config.Grpc.MaxSendMessageSize),
		grpc.InitialWindowSize(config.Grpc.InitialWindowSize),
		grpc.InitialConnWindowSize(config.Grpc.InitialConnWindowSize),
		grpc.WriteBufferSize(config.Grpc.WriteBufferSize),
		grpc.ReadBufferSize(config.Grpc.ReadBufferSize),
	)

	grpcServer := grpc.NewServer(serverOpts...)
	pb.RegisterEventStoreServer(grpcServer, eventStore)

	// Register Admin service
	grpcAdminServer := admin.NewGRPCAdminServer(
		logger,
		config.Admin.Boundary,
		eventStore.GetEvents,
		eventStore.SaveEvents,
		adminDB.ListAdminUsers,
		authenticator,
		adminDB.CreateBoundaryIndex,
		adminDB.DropBoundaryIndex,
	)
	pb.RegisterAdminServer(grpcServer, grpcAdminServer)

	if config.Grpc.EnableReflection {
		logger.Infof("Enabling gRPC server reflection")
		reflection.Register(grpcServer)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", config.Grpc.Port))
	if err != nil {
		logger.Fatalf("Failed to listen: %v", err)
	}

	logger.Infof("gRPC server listening on port %s (TLS: %v)", config.Grpc.Port, config.Grpc.TLS.Enabled)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Fatalf("Failed to serve: %v", err)
	}
}
