package main

import (
	"context"
	"errors"
	"github.com/goccy/go-json"
	nats2 "github.com/oexza/Orisun/nats"

	"fmt"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oexza/Orisun/admin"
	changepassword "github.com/oexza/Orisun/admin/slices/change_password"
	common "github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/admin/slices/create_user"
	"github.com/oexza/Orisun/admin/slices/dashboard"
	"github.com/oexza/Orisun/admin/slices/dashboard/event_count"
	"github.com/oexza/Orisun/admin/slices/dashboard/user_count"
	"github.com/oexza/Orisun/admin/slices/delete_user"
	"github.com/oexza/Orisun/admin/slices/login"
	"github.com/oexza/Orisun/admin/slices/users_page"
	up "github.com/oexza/Orisun/admin/slices/users_projection"
	globalCommon "github.com/oexza/Orisun/common"
	c "github.com/oexza/Orisun/config"
	pb "github.com/oexza/Orisun/eventstore"
	l "github.com/oexza/Orisun/logging"
	pg "github.com/oexza/Orisun/postgres"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	logger "log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"runtime/debug"
	"time"
)

// LoginAuthenticatorAdapter bridges admin.Authenticator to login.Authenticator
type LoginAuthenticatorAdapter struct {
	authenticator *admin.Authenticator
	logger        l.Logger
}

// ValidateCredentials implements the login.Authenticator interface
func (adapter *LoginAuthenticatorAdapter) ValidateCredentials(ctx context.Context, username string, password string) (globalCommon.User, error) {
	user, _, err := adapter.authenticator.ValidateCredentials(ctx, username, password)
	if err != nil {
		return globalCommon.User{}, err
	}
	return user, nil
}

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
func setupJetStreamConsumer(ctx context.Context, js jetstream.JetStream, streamName string, consumerName string, subject string, handler *globalCommon.MessageHandler[common.PublishRequest], logger l.Logger) error {
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

// getUserByIdWrapper implements the up.GetUserById interface using adminDB
type getUserByIdWrapper struct {
	adminDB common.DB
}

func (w getUserByIdWrapper) Get(userId string) (globalCommon.User, error) {
	return w.adminDB.GetUserById(userId)
}

func main() {
	defer logger.Println("Server shutting down")

	// Display version information
	fmt.Printf("Starting Orisun %s (Go %s)\n", globalCommon.GetVersion(), runtime.Version())

	// Load configuration and initialize logger
	config := c.InitializeConfig()

	// Initialize logger
	AppLogger := l.InitializeDefaultLogger(config.Logging)
	AppLogger.Debugf("config: %v", config)

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

	// Create a context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize NATS
	js, nc, ns := nats2.InitializeNATS(ctx, config.Nats, AppLogger)
	defer nc.Close()
	defer ns.Shutdown()

	// Initialize database
	saveEvents, getEvents, lockProvider, adminDB, eventPublishing := pg.InitializePostgresDatabase(ctx, config.Postgres, config.Admin, js, AppLogger)

	// Initialize EventStore
	eventStore := pb.InitializeEventStore(
		ctx,
		config,
		saveEvents,
		getEvents,
		lockProvider,
		js,
		AppLogger,
	)

	// Start polling events from the event store and publish them to NATS jetstream
	pb.StartEventPolling(ctx, config, lockProvider, getEvents, js, eventPublishing, AppLogger)

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
		count, err := adminDB.GetUsersCount()
		if err != nil {
			return user_count.UserCountReadModel{}, err
		}
		return user_count.UserCountReadModel{
			Count: count,
		}, nil
	}

	getEventCount := func(boundary string) (event_count.EventCountReadModel, error) {
		count, err := adminDB.GetEventsCount(boundary)
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
		adminDB.GetProjectorLastPosition,
		adminDB.UpdateProjectorPosition,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *globalCommon.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		getUserCount,
		jetStreamPublishFunction,
		adminDB.SaveUsersCount,
		AppLogger,
	)

	startEventCountProjector(
		ctx,
		config.GetBoundaryNames(),
		adminDB.GetProjectorLastPosition,
		adminDB.UpdateProjectorPosition,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *globalCommon.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		getEventCount,
		jetStreamPublishFunction,
		adminDB.SaveEventCount,
		AppLogger,
	)

	startUserProjector(
		ctx,
		config.Admin.Boundary,
		adminDB.GetProjectorLastPosition,
		adminDB.UpdateProjectorPosition,
		adminDB.UpsertUser,
		adminDB.DeleteUser,
		adminDB.GetUserById,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *globalCommon.MessageHandler[pb.Event]) error {
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
			handler *globalCommon.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		AppLogger,
	)

	//create default user
	err := createDefaultUser(
		ctx,
		config.Admin.Boundary,
		*eventStore,
		AppLogger,
	)
	if err != nil {
		AppLogger.Infof("%v", err)
	}

	// Start admin server
	createUserCommandHandler := create_user.NewCreateUserHandler(
		AppLogger,
		config.Admin.Boundary,
		eventStore.SaveEvents,
		eventStore.GetEvents,
	)
	dashboardHandler := dashboard.NewDashboardHandler(
		AppLogger,
		config.GetBoundaryNames(),
		getUserCount,
		func(consumerName string, ctx context.Context, messageHandler *globalCommon.MessageHandler[user_count.UserCountReadModel]) error {
			handler := globalCommon.NewMessageHandler[common.PublishRequest](ctx)
			go func() {
				for {
					select {
					case <-ctx.Done():
						AppLogger.Debug("Context done, stopping...")
						return // Exit the goroutine completely
					default:
						// Only try to receive if context is not done
						event, err := handler.Recv()
						if err != nil {
							AppLogger.Errorf("Error receiving: %v", err)
							return
						}
						userReadModel := user_count.UserCountReadModel{}
						json.Unmarshal(event.Data, &userReadModel)
						AppLogger.Debugf("User count updated is: %v", string(event.Data))
						messageHandler.Send(&userReadModel)
					}
				}
			}()

			count, err := getUserCount()
			if err != nil && count != (user_count.UserCountReadModel{}) {
				messageHandler.Send(&count)
			}
			AppLogger.Infof("Subscribe To user count called with subject: %s, consumer_name: %s", user_count.UserCountPubSubscription, consumerName)

			err = setupJetStreamConsumer(ctx, js, pubsubStreamName, consumerName, user_count.UserCountPubSubscription, handler, AppLogger)
			if err != nil {
				return fmt.Errorf("failed to subscribe: %v", err)
			}
			AppLogger.Infof("setupJetStreamConsumer called with subject: %s, consumer_name: %s", user_count.UserCountPubSubscription, consumerName)
			return nil
		},
		getEventCount,
		func(consumerName string, boundary string, ctx context.Context, messageHandler *globalCommon.MessageHandler[event_count.EventCountReadModel]) error {
			handler := globalCommon.NewMessageHandler[common.PublishRequest](ctx)
			go func(boundary string, logger l.Logger) {
				for {
					select {
					case <-ctx.Done():
						logger.Debug("Context done, stopping...")
						return // Exit the goroutine completely
					default:
						// Only try to receive if context is not done
						event, err := handler.Recv()
						if err != nil {
							if ctx.Err() != nil {
								// Context is done, exit gracefully
								return
							}
							logger.Errorf("Error receiving: %v", err)
							// Add a small sleep to prevent CPU spinning on persistent errors
							time.Sleep(100 * time.Millisecond)
							continue
						}
						eventReadModel := event_count.EventCountReadModel{}
						json.Unmarshal(event.Data, &eventReadModel)

						//Check if the boundary from the event is the same with the current boundary
						if eventReadModel.Boundary == boundary {
							messageHandler.Send(&eventReadModel)
							AppLogger.Debugf("Event count updated is: %v", string(event.Data))

						}
					}
				}
			}(boundary, AppLogger)

			count, err := getEventCount(boundary)
			if err != nil && count != (event_count.EventCountReadModel{}) {
				messageHandler.Send(&count)
			}
			AppLogger.Infof("Subscribe To event count called with subject: %s, consumer_name: %s", event_count.EventCountPubSubscription, consumerName)

			err = setupJetStreamConsumer(ctx, js, pubsubStreamName, consumerName, event_count.EventCountPubSubscription, handler, AppLogger)
			if err != nil {
				return fmt.Errorf("failed to subscribe: %v", err)
			}
			AppLogger.Infof("setupJetStreamConsumer called with subject: %s, consumer_name: %s", event_count.EventCountPubSubscription, consumerName)
			return nil
		},
	)

	authenticator := admin.NewAuthenticator(
		eventStore.GetEvents,
		AppLogger,
		config.Admin.Boundary,
		adminDB.GetUserByUsername,
	)

	// Create an adapter to bridge admin.Authenticator to login.Authenticator
	loginAuthenticator := &LoginAuthenticatorAdapter{
		authenticator: authenticator,
		logger:        AppLogger,
	}

	loginHandler := login.NewLoginHandler(
		AppLogger,
		config.Admin.Boundary,
		loginAuthenticator,
	)

	deleteUserHandler := delete_user.NewDeleteUserHandler(
		AppLogger,
		eventStore.SaveEvents,
		eventStore.GetEvents,
		config.Admin.Boundary,
	)

	usersPageHandler := users_page.NewUsersPageHandler(
		AppLogger,
		config.Admin.Boundary,
		adminDB.ListAdminUsers,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler *globalCommon.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
	)

	changePasswordHandler := changepassword.NewChangePasswordHandler(
		AppLogger,
		config.Admin.Boundary,
		eventStore.SaveEvents,
		eventStore.GetEvents,
	)

	startAdminServer(
		config,
		createUserCommandHandler,
		dashboardHandler,
		loginHandler,
		deleteUserHandler,
		usersPageHandler,
		changePasswordHandler,
		AppLogger,
	)

	// Start gRPC server
	startGRPCServer(config, eventStore, authenticator, AppLogger)
}

func createDefaultUser(ctx context.Context, adminBoundary string, eventstore pb.EventStore, logger l.Logger) error {
	var userExistsError create_user.UserExistsError
	if _, err := create_user.CreateUser(
		ctx,
		"admin",
		"admin",
		"changeit",
		[]globalCommon.Role{globalCommon.RoleAdmin},
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
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			default:
				err := userProjector.Start(groupCtx)

				if err != nil {
					logger.Debugf("Failed to start user projection (likely due to lock contention): %v - will retry", err)
					time.Sleep(5 * time.Second)
					continue
				}
				logger.Info("User projector started")
				// Projector will run until context is cancelled or error occurs
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
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			default:
				err := userProjector.Start(groupCtx)

				if err != nil {
					logger.Debugf("Failed to start auth user projection (likely due to lock contention): %v - will retry", err)
					time.Sleep(5 * time.Second)
					continue
				}
				logger.Info("Auth user projector started")
				// Projector will run until context is cancelled or error occurs
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
		for {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			default:
				err := userProjector.Start(groupCtx)

				if err != nil {
					logger.Debugf("Failed to start user count projection (likely due to lock contention): %v - will retry", err)
					time.Sleep(5 * time.Second)
					continue
				}
				logger.Info("User count projector started")
				// Projector will run until context is cancelled or error occurs
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
			for {
				select {
				case <-groupCtx.Done():
					return groupCtx.Err()
				default:
					err := eventProjector.Start(groupCtx)

					if err != nil {
						logger.Debugf("Failed to start event count projection for boundary %s (likely due to lock contention): %v - will retry", b, err)
						time.Sleep(5 * time.Second)
						continue
					}
					logger.Infof("Event count projector started for boundary %s", b)
					// Projector will run until context is cancelled or error occurs
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

func startAdminServer(
	config c.AppConfig,
	createUserHandler *create_user.CreateUserHandler,
	dashboardHandler *dashboard.DashboardHandler,
	loginHandler *login.LoginHandler,
	deleteUserHandler *delete_user.DeleteUserHandler,
	usersHandler *users_page.UsersPageHandler,
	changePasswordHandler *changepassword.ChangePasswordHandler,
	logger l.Logger) {
	go func() {
		adminServer, err := admin.NewAdminServer(
			logger,
			createUserHandler,
			dashboardHandler,
			loginHandler,
			deleteUserHandler,
			usersHandler,
			changePasswordHandler,
		)
		if err != nil {
			logger.Fatalf("Could not start admin server %v", err)
		}
		httpServer := &http.Server{
			Addr:    fmt.Sprintf(":%s", config.Admin.Port),
			Handler: adminServer,
		}

		logger.Infof("Starting admin server on port %s", config.Admin.Port)
		if err := httpServer.ListenAndServe(); err != nil {
			logger.Errorf("AdminConfig server error: %v", err)
		}
		logger.Infof("AdminConfig server started on port %s", config.Admin.Port)
	}()
}

func startGRPCServer(config c.AppConfig, eventStore pb.EventStoreServer,
	authenticator *admin.Authenticator, logger l.Logger) {
	grpcServer := grpc.NewServer(
		// grpc.ChainUnaryInterceptor(admin.UnaryPerformanceInterceptor()),
		grpc.UnaryInterceptor(admin.UnaryAuthInterceptor(authenticator, logger)),
		grpc.StreamInterceptor(admin.StreamAuthInterceptor(authenticator, logger)),
		grpc.ChainUnaryInterceptor(recoveryInterceptor(logger)),
		grpc.ChainStreamInterceptor(streamErrorInterceptor(logger)),
		grpc.ConnectionTimeout(config.Grpc.ConnectionTimeout),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    config.Grpc.KeepAliveTime,
			Timeout: config.Grpc.KeepAliveTimeout,
		}),
		grpc.MaxConcurrentStreams(config.Grpc.MaxConcurrentStreams),
	)
	pb.RegisterEventStoreServer(grpcServer, eventStore)
	if config.Grpc.EnableReflection {
		logger.Infof("Enabling gRPC server reflection")
		reflection.Register(grpcServer)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", config.Grpc.Port))
	if err != nil {
		logger.Fatalf("Failed to listen: %v", err)
	}

	logger.Infof("Grpc Server listening on port %s", config.Grpc.Port)
	if err := grpcServer.Serve(lis); err != nil {
		logger.Fatalf("Failed to serve: %v", err)
	}
}
