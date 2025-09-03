package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"sync"

	"github.com/goccy/go-json"

	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	logger "log"
	pb "orisun/eventstore"
	"runtime/debug"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	c "orisun/config"
	l "orisun/logging"
	postgres "orisun/postgres"

	admin "orisun/admin"
	changepassword "orisun/admin/slices/change_password"
	common "orisun/admin/slices/common"
	"orisun/admin/slices/create_user"
	"orisun/admin/slices/dashboard"
	"orisun/admin/slices/delete_user"
	"orisun/admin/slices/login"
	"orisun/admin/slices/users_page"
	up "orisun/admin/slices/users_projection"
	globalCommon "orisun/common"

	event_count "orisun/admin/slices/dashboard/event_count"
	user_count "orisun/admin/slices/dashboard/user_count"

	"github.com/nats-io/nats-server/v2/server"
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

func main() {
	defer logger.Println("Server shutting down")

	// Load configuration and initialize logger
	config := initializeConfig()

	// Initialize logger
	AppLogger := initializeLogger(config)
	AppLogger.Debugf("config: %v", config)
	// Create a context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize NATS
	js, nc, ns := initializeNATS(ctx, config, AppLogger)
	defer nc.Close()
	defer ns.Shutdown()

	// time.Sleep(60 * time.Second)

	// Initialize database
	saveEvents, getEvents, lockProvider, adminDB, eventPublishing := initializeDatabase(ctx, config, js, AppLogger)

	// Initialize EventStore
	eventStore := initializeEventStore(
		ctx,
		config,
		saveEvents,
		getEvents,
		lockProvider,
		js,
		AppLogger,
	)

	// Start polling events from the event store and publish them to NATS jetstream
	startEventPolling(ctx, config, lockProvider, getEvents, js, eventPublishing, AppLogger)

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
			handler globalCommon.MessageHandler[pb.Event]) error {
			return eventStore.SubscribeToAllEvents(
				ctx, boundary, subscriberName, pos, query, handler,
			)
		},
		getUserCount,
		jetStreamPublishFunction,
		adminDB.SaveUsersCount,
		AppLogger,
	)

	boundariesArray := []string{}
	for _, boundary := range *config.GetBoundaries() {
		boundariesArray = append(boundariesArray, boundary.Name)
	}
	startEventCountProjector(
		ctx,
		boundariesArray,
		adminDB.GetProjectorLastPosition,
		adminDB.UpdateProjectorPosition,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler globalCommon.MessageHandler[pb.Event]) error {
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
		adminDB.CreateNewUser,
		adminDB.DeleteUser,
		func(
			ctx context.Context,
			boundary string,
			subscriberName string,
			pos *pb.Position,
			query *pb.Query,
			handler globalCommon.MessageHandler[pb.Event]) error {
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
			handler globalCommon.MessageHandler[pb.Event]) error {
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
		boundariesArray,
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
	)

	loginHandler := login.NewLoginHandler(
		AppLogger,
		config.Admin.Boundary,
		authenticator,
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
			handler globalCommon.MessageHandler[pb.Event]) error {
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

func initializeConfig() c.AppConfig {
	config, err := c.LoadConfig()
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}
	return config
}

func initializeLogger(config c.AppConfig) l.Logger {
	// Initialize logger
	logr, err := l.ZapLogger(config.Logging.Level)
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	return logr
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
func initializeDatabase(
	ctx context.Context,
	config c.AppConfig,
	js jetstream.JetStream,
	logger l.Logger,
) (pb.EventstoreSaveEvents, pb.EventstoreGetEvents, pb.LockProvider, common.DB, common.EventPublishing) {
	// Create database connection string
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.Postgres.Host,
		config.Postgres.Port,
		config.Postgres.User,
		config.Postgres.Password,
		config.Postgres.Name,
	)

	// Create write database pool (optimized for write operations)
	writeDB, err := sql.Open("postgres", connStr)
	if err != nil {
		logger.Fatalf("Failed to connect to write database: %v", err)
	}

	// Configure write pool - fewer connections, shorter lifetimes for consistency
	writeDB.SetMaxOpenConns(config.Postgres.WriteMaxOpenConns)
	writeDB.SetMaxIdleConns(config.Postgres.WriteMaxIdleConns)
	writeDB.SetConnMaxIdleTime(config.Postgres.WriteConnMaxIdleTime)
	writeDB.SetConnMaxLifetime(config.Postgres.WriteConnMaxLifetime)

	// Create read database pool (optimized for read operations)
	readDB, err := sql.Open("postgres", connStr)
	if err != nil {
		logger.Fatalf("Failed to connect to read database: %v", err)
	}

	// Configure read pool - more connections, longer lifetimes for performance
	readDB.SetMaxOpenConns(config.Postgres.ReadMaxOpenConns)
	readDB.SetMaxIdleConns(config.Postgres.ReadMaxIdleConns)
	readDB.SetConnMaxIdleTime(config.Postgres.ReadConnMaxIdleTime)
	readDB.SetConnMaxLifetime(config.Postgres.ReadConnMaxLifetime)

	// Create admin database pool (optimized for admin operations)
	adminDBPool, err := sql.Open("postgres", connStr)
	if err != nil {
		logger.Fatalf("Failed to connect to admin database: %v", err)
	}

	// Configure admin pool - moderate connections, longer lifetimes for admin tasks
	adminDBPool.SetMaxOpenConns(config.Postgres.AdminMaxOpenConns)
	adminDBPool.SetMaxIdleConns(config.Postgres.AdminMaxIdleConns)
	adminDBPool.SetConnMaxIdleTime(config.Postgres.AdminConnMaxIdleTime)
	adminDBPool.SetConnMaxLifetime(config.Postgres.AdminConnMaxLifetime)

	// Test all connections
	if err = writeDB.PingContext(ctx); err != nil {
		logger.Fatalf("Failed to ping write database: %v", err)
	}
	if err = readDB.PingContext(ctx); err != nil {
		logger.Fatalf("Failed to ping read database: %v", err)
	}
	if err = adminDBPool.PingContext(ctx); err != nil {
		logger.Fatalf("Failed to ping admin database: %v", err)
	}

	logger.Info("Database connections established with separate read/write/admin pools")
	logger.Infof("Write pool: %d max open, %d max idle connections", 
		config.Postgres.WriteMaxOpenConns, config.Postgres.WriteMaxIdleConns)
	logger.Infof("Read pool: %d max open, %d max idle connections", 
		config.Postgres.ReadMaxOpenConns, config.Postgres.ReadMaxIdleConns)
	logger.Infof("Admin pool: %d max open, %d max idle connections", 
		config.Postgres.AdminMaxOpenConns, config.Postgres.AdminMaxIdleConns)

	go func() {
		<-ctx.Done()
		logger.Info("Shutting down database connections")
		writeDB.Close()
		readDB.Close()
		adminDBPool.Close()
	}()

	postgesBoundarySchemaMappings := config.Postgres.GetSchemaMapping()
	for _, schema := range postgesBoundarySchemaMappings {
		isAdminBoundary := schema.Boundary == config.Admin.Boundary
		// Use write pool for database migrations (schema changes)
		if err = postgres.RunDbScripts(writeDB, schema.Schema, isAdminBoundary, ctx); err != nil {
			logger.Fatalf("Failed to run database migrations for schema %s: %v", schema, err)
		}
		logger.Infof("Database migrations for schema %s completed successfully", schema)
	}

	// Use write pool for save operations and read pool for get operations
	saveEvents := postgres.NewPostgresSaveEvents(ctx, writeDB, logger, postgesBoundarySchemaMappings)
	getEvents := postgres.NewPostgresGetEvents(readDB, logger, postgesBoundarySchemaMappings)
	lockProvider, err := pb.NewJetStreamLockProvider(ctx, js, logger)
	if err != nil {
		logger.Fatalf("Failed to create lock provider: %v", err)
	}
	// lockProvider := postgres.NewPGLockProvider(db, AppLogger)
	adminSchema := postgesBoundarySchemaMappings[config.Admin.Boundary]
	if (adminSchema == c.BoundaryToPostgresSchemaMapping{}) {
		logger.Fatalf("No schema specified for admin boundary", err)
	}

	// Use admin pool for admin operations (user management)
	adminDB := postgres.NewPostgresAdminDB(
		adminDBPool,
		logger,
		adminSchema.Schema,
		postgesBoundarySchemaMappings,
	)

	// Use write pool for event publishing operations
	eventPublishing := postgres.NewPostgresEventPublishing(
		writeDB,
		logger,
		postgesBoundarySchemaMappings,
	)

	return saveEvents, getEvents, lockProvider, adminDB, eventPublishing
}

func initializeNATS(ctx context.Context, config c.AppConfig, logger l.Logger) (jetstream.JetStream, *nats.Conn, *server.Server) {
	natsOptions := createNATSOptions(config, logger)
	natsServer := startNATSServer(natsOptions, config, logger)

	// Connect to NATS
	nc, err := nats.Connect("", nats.InProcessServer(natsServer))
	if err != nil {
		logger.Fatalf("Failed to connect to NATS: %v", err)
	}

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		logger.Fatalf("Failed to create JetStream context: %v", err)
	}
	// time.Sleep(30 * time.Second)
	waitForJetStream(ctx, js, logger)
	return js, nc, natsServer
}

func createNATSOptions(config c.AppConfig, logger l.Logger) server.Options {
	options := server.Options{
		ServerName: config.Nats.ServerName,
		Port:       config.Nats.Port,
		MaxPayload: config.Nats.MaxPayload,
		JetStream:  true,
		StoreDir:   config.Nats.StoreDir,
	}

	if config.Nats.Cluster.Enabled {
		options.Cluster = server.ClusterOpts{
			Name: config.Nats.Cluster.Name,
			Host: config.Nats.Cluster.Host,
			Port: config.Nats.Cluster.Port,
		}
		options.Routes = convertToURLSlice(config.Nats.Cluster.GetRoutes(), logger)
		logger.Info("Nats cluster is enabled, running in clustered mode")
		logger.Info(
			"Cluster configuration: Name=%v, Host=%v, Port=%v, Routes=%v",
			config.Nats.Cluster.Name,
			config.Nats.Cluster.Host,
			config.Nats.Cluster.Port,
			config.Nats.Cluster.Routes,
		)
	} else {
		logger.Info("Nats cluster is disabled, running in standalone mode")
	}

	return options
}

func startNATSServer(options server.Options, config c.AppConfig, logger l.Logger) *server.Server {
	natsServer, err := server.NewServer(&options)
	if err != nil {
		logger.Fatalf("Failed to create NATS server: %v", err)
	}

	natsServer.ConfigureLogger()
	go natsServer.Start()
	if !natsServer.ReadyForConnections(config.Nats.Cluster.Timeout) {
		logger.Fatal("NATS server failed to start")
	}
	logger.Info("NATS server started on ", natsServer.ClientURL())

	return natsServer
}

func waitForJetStream(ctx context.Context, js jetstream.JetStream, logger l.Logger) {
	jetStreamTestDone := make(chan struct{})

	go func() {
		defer close(jetStreamTestDone)

		for {
			streamName := "test_jetstream"
			_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
				Name: streamName,
				Subjects: []string{
					streamName + ".test",
				},
				MaxMsgs: 1,
			})

			if err != nil {
				logger.Warnf("failed to add stream: %v %v", streamName, err)
				time.Sleep(5 * time.Second)
				continue
			}

			r, err := js.Publish(ctx, streamName+".test", []byte("test"))
			if err != nil {
				logger.Warnf("Failed to publish to JetStream, retrying in 5 second: %v", err)
				time.Sleep(5 * time.Second)
			} else {
				logger.Infof("Published to JetStream: %v", r)
				break
			}
		}
	}()

	select {
	case <-jetStreamTestDone:
		logger.Info("JetStream system is available")
	}
}

func initializeEventStore(
	ctx context.Context,
	config c.AppConfig,
	saveEvents pb.EventstoreSaveEvents,
	getEvents pb.EventstoreGetEvents,
	lockProvider pb.LockProvider,
	js jetstream.JetStream,
	logger l.Logger) *pb.EventStore {

	logger.Info("Initializing EventStore")
	eventStore := pb.NewEventStoreServer(
		ctx,
		js,
		saveEvents,
		getEvents,
		lockProvider,
		getBoundaryNames((config.GetBoundaries())),
		logger,
	)
	logger.Info("EventStore initialized")

	return eventStore
}

func startEventPolling(
	ctx context.Context,
	config c.AppConfig,
	lockProvider pb.LockProvider,
	getEvents pb.EventstoreGetEvents,
	js jetstream.JetStream,
	eventPublishing common.EventPublishing,
	logger l.Logger) {
	for _, schema := range config.Postgres.GetSchemaMapping() {
		// Start a goroutine for each boundary that continuously tries to acquire lock and poll
		go func(boundary c.BoundaryToPostgresSchemaMapping) {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Try to acquire lock for this boundary
					err := lockProvider.Lock(ctx, boundary.Boundary)
					if err != nil {
						// If lock acquisition fails, wait a bit and try again
						logger.Warnf("Failed to acquire lock for boundary %s: %v - will retry", boundary.Boundary, err)
						time.Sleep(5 * time.Second)
						continue
					}

					logger.Infof("Successfully acquired polling lock for boundary %v", boundary.Boundary)

					// Get last published position
					lastPosition, err := eventPublishing.GetLastPublishedEventPosition(ctx, boundary.Boundary)
					if err != nil {
						logger.Errorf("Failed to get last published position for boundary %s: %v", boundary.Boundary, err)
						time.Sleep(5 * time.Second)
						continue
					}
					logger.Infof("Last published position for boundary %v: %v", boundary.Boundary, &lastPosition)

					// Start polling - this will block until context is cancelled or error occurs
					err = PollEventsFromDatabaseToNats(
						ctx,
						js,
						getEvents,
						config.PollingPublisher.BatchSize,
						&lastPosition,
						boundary.Boundary,
						eventPublishing,
						boundary.Schema,
						logger,
					)

					if err != nil {
						logger.Errorf("Polling stopped for boundary %s: %v - will retry", boundary.Boundary, err)
						time.Sleep(5 * time.Second)
					}
					// Loop will continue to try acquiring lock again
				}
			}
		}(schema)
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
			logger.Errorf("Admin server error: %v", err)
		}
		logger.Infof("Admin server started on port %s", config.Admin.Port)
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

func startUserProjector(
	ctx context.Context,
	adminBoundary string,
	getProjectorLastPosition common.GetProjectorLastPositionType,
	updateProjectorPosition common.UpdateProjectorPositionType,
	createNewUser up.CreateNewUserType,
	deleteUser up.DeleteUserType,
	subscribeToEvents common.SubscribeToEventStoreType,
	saveEvents common.SaveEventsType,
	getEvents common.GetEventsType,
	logger l.Logger) {
	go func() {
		userProjector := up.NewUserProjector(
			getProjectorLastPosition,
			updateProjectorPosition,
			createNewUser,
			deleteUser,
			saveEvents,
			getEvents,
			logger,
			adminBoundary,
			subscribeToEvents,
		)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err := userProjector.Start(ctx)

				if err != nil {
					logger.Debugf("Failed to start user projection (likely due to lock contention): %v - will retry", err)
					time.Sleep(5 * time.Second)
					continue
				}
				logger.Info("User projector started")
				// Projector will run until context is cancelled or error occurs
				return
			}
		}
	}()
}

func startAuthUserProjector(
	ctx context.Context,
	adminBoundary string,
	subscribeToEvents common.SubscribeToEventStoreType,
	logger l.Logger) {
	go func() {
		userProjector := admin.NewAuthUserProjector(
			logger,
			subscribeToEvents,
			adminBoundary,
		)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err := userProjector.Start(ctx)

				if err != nil {
					logger.Debugf("Failed to start auth user projection (likely due to lock contention): %v - will retry", err)
					time.Sleep(5 * time.Second)
					continue
				}
				logger.Info("Auth user projector started")
				// Projector will run until context is cancelled or error occurs
				return
			}
		}
	}()
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

	go func() {
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
			case <-ctx.Done():
				return
			default:
				err := userProjector.Start(ctx)

				if err != nil {
					logger.Debugf("Failed to start user count projection (likely due to lock contention): %v - will retry", err)
					time.Sleep(5 * time.Second)
					continue
				}
				logger.Info("User count projector started")
				// Projector will run until context is cancelled or error occurs
				return
			}
		}
	}()

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
	for _, boundary := range boundaries {
		go func(boundary string) {
			eventProjector := event_count.NewEventCountProjection(
				boundary,
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
				case <-ctx.Done():
					return
				default:
					err := eventProjector.Start(ctx)

					if err != nil {
						logger.Debugf("Failed to start event count projection for boundary %s (likely due to lock contention): %v - will retry", boundary, err)
						time.Sleep(5 * time.Second)
						continue
					}
					logger.Infof("Event count projector started for boundary %s", boundary)
					// Projector will run until context is cancelled or error occurs
					return
				}
			}
		}(boundary)
	}

}

var mutex sync.RWMutex

func PollEventsFromDatabaseToNats(
	ctx context.Context,
	js jetstream.JetStream,
	eventStore pb.EventstoreGetEvents,
	batchSize uint32,
	lastPosition *pb.Position,
	boundary string,
	db common.EventPublishing,
	schema string,
	logger l.Logger,
) error {
	// Start polling loop
	for {
		if ctx.Err() != nil {
			logger.Error("Context cancelled, stopping polling")
			return ctx.Err()
		}

		logger.Debugf("Polling for boundary: %v", boundary)
		req := &pb.GetEventsRequest{
			FromPosition: &pb.Position{
				CommitPosition:  lastPosition.CommitPosition,
				PreparePosition: lastPosition.PreparePosition + 1, // we start from the next position since the underlying database is assumed to be position inclusive in its query handling.
			},
			Count:     batchSize,
			Direction: pb.Direction_ASC,
			Boundary:  boundary,
		}
		resp, err := eventStore.Get(ctx, req)
		if err != nil {
			// return fmt.Errorf("failed to get events: %v", err)
			logger.Fatalf("Failed to get events: %v", err)
		}

		logger.Debugf("Got %d events for boundary %v", len(resp.Events), boundary)

		for _, event := range resp.Events {
			subjectName := pb.GetEventJetstreamSubjectName(
				boundary,
				event.StreamId,
				&pb.Position{
					CommitPosition:  event.Position.CommitPosition,
					PreparePosition: event.Position.PreparePosition,
				},
			)
			logger.Debugf("Subject name is: %s", subjectName)
			eventData, err := json.Marshal(event)
			if err != nil {
				logger.Errorf("Failed to marshal event: %v", err)
				panic(err)
			}
			publishEventWithRetry(
				ctx,
				js,
				eventData,
				subjectName,
				event.Position.PreparePosition,
				event.Position.CommitPosition,
				logger,
			)
			lastPosition = event.Position
		}
		if len(resp.Events) > 0 {
			go func() {
				mutex.Lock()
				defer mutex.Unlock()
				err = db.InsertLastPublishedEvent(
					ctx,
					boundary,
					lastPosition.CommitPosition,
					lastPosition.PreparePosition,
				)
				if err != nil {
					panic(err)
				}
			}()
		}

		if len(resp.Events) > 0 {
			lastPosition = resp.Events[len(resp.Events)-1].Position
		}
		logger.Debugf(":%v Sleeping.....", boundary)
		time.Sleep(500 * time.Millisecond) // Polling interval
	}
}

func publishEventWithRetry(
	ctx context.Context,
	js jetstream.JetStream,
	eventData []byte,
	subjectName string, preparePosition int64,
	commitPosition int64, logger l.Logger) {

	messageIdOpts := jetstream.PublishOpt(
		jetstream.WithMsgID(pb.GetEventNatsMessageId(int64(preparePosition), int64(commitPosition))),
	)
	retryOpts := jetstream.WithRetryAttempts(5)

	_, err := js.Publish(ctx, subjectName, eventData, messageIdOpts, retryOpts)
	if err == nil {
		logger.Debugf("Successfully published event to jetstream")
		return
	}

	logger.Errorf("Failed to publish event: %v, this should not happen.", err)
	panic(err)
}

func getBoundaryNames(boundary *[]c.Boundary) *[]string {
	var names []string
	for _, boundary := range *boundary {
		names = append(names, boundary.Name)
	}
	return &names
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
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("Panic in %s: %v\nStack Trace:\n%s", info.FullMethod, r, debug.Stack())
				err = status.Errorf(codes.Internal, "Internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

func convertToURLSlice(routes []string, logger l.Logger) []*url.URL {
	var urls []*url.URL
	for _, route := range routes {
		u, err := url.Parse(route)
		if err != nil {
			logger.Fatalf("Warning: invalid route URL %q: %v", route, err)
			continue
		}
		urls = append(urls, u)
	}
	return urls
}

func createBasicAuthHeader(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}

func getAuthenticatedContext(username, password string) context.Context {
	// Create Basic Auth header
	authHeader := createBasicAuthHeader(username, password)

	// Create metadata with the Authorization header
	md := metadata.New(map[string]string{
		"Authorization": authHeader,
	})

	// Attach metadata to the context
	return metadata.NewOutgoingContext(context.Background(), md)
}
