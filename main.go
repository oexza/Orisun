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

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	logger "log"
	pb "orisun/eventstore"
	"runtime/debug"

	c "orisun/config"
	l "orisun/logging"
	postgres "orisun/postgres"

	admin "orisun/admin"
	common "orisun/admin/slices/common"
	"orisun/admin/slices/create_user"
	"orisun/admin/slices/dashboard"
	"orisun/admin/slices/delete_user"
	"orisun/admin/slices/login"
	"orisun/admin/slices/users_page"
	up "orisun/admin/slices/users_projection"
	globalCommon "orisun/common"

	user_count "orisun/admin/slices/dashboard/user_count"

	"github.com/nats-io/nats-server/v2/server"
)

var AppLogger l.Logger

func main() {
	defer logger.Println("Server shutting down")

	// Load configuration and initialize logger
	config := initializeConfig()
	// Create a context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize NATS
	js, nc, ns := initializeNATS(ctx, config)
	defer nc.Close()
	defer ns.Shutdown()

	// time.Sleep(60 * time.Second)

	// Initialize database
	saveEvents, getEvents, lockProvider, adminDB, eventPublishing := initializeDatabase(ctx, config, js)

	// Initialize EventStore
	eventStore := initializeEventStore(
		ctx,
		config,
		saveEvents,
		getEvents,
		lockProvider,
		js,
	)

	// Start polling events
	startEventPolling(ctx, config, lockProvider, getEvents, js, eventPublishing)

	// Start projectors
	var pubSubFunction = func(ctx context.Context, req *pb.PublishRequest) error {
		_, err := eventStore.PublishToPubSub(ctx, req)
		if err != nil {
			return err
		}
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
	startUserCountProjector(
		ctx,
		config.Admin.Boundary,
		adminDB.GetProjectorLastPosition,
		adminDB.UpdateProjectorPosition,
		eventStore.SubscribeToEvents,
		getUserCount,
		pubSubFunction,
		adminDB.SaveUsersCount,
	)
	startUserProjector(
		ctx,
		config.Admin.Boundary,
		adminDB.GetProjectorLastPosition,
		adminDB.UpdateProjectorPosition,
		adminDB.CreateNewUser,
		adminDB.DeleteUser,
		eventStore.SubscribeToEvents,
		eventStore.SaveEvents,
		eventStore.GetEvents,
	)

	//create default user
	createDefaultUser(
		ctx,
		config.Admin.Boundary, *eventStore,
	)

	// Start admin server
	createUserCommandHandler := create_user.NewCreateUserHandler(
		AppLogger,
		config.Admin.Boundary,
		eventStore.SaveEvents,
		eventStore.GetEvents,
	)
	dashboardHandler := dashboard.NewDashboardHandler(
		AppLogger,
		config.Admin.Boundary,
		getUserCount,
		func(consumerName string, ctx context.Context, messageHandler *globalCommon.MessageHandler[user_count.UserCountReadModel]) error {
			handler := globalCommon.NewMessageHandler[pb.Message](ctx)
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
							if ctx.Err() != nil {
								// Context is done, exit gracefully
								return
							}
							AppLogger.Error("Error receiving: %v", err)
							// Add a small sleep to prevent CPU spinning on persistent errors
							time.Sleep(100 * time.Millisecond)
							continue
						}
						userReadModel := user_count.UserCountReadModel{}
						json.Unmarshal(event.Data, &userReadModel)
						AppLogger.Debugf("User count updated is: %v", string(event.Data))
						messageHandler.Send(&userReadModel)
					}
				}
			}()
			eventStore.SubscribeToPubSubGeneric(
				ctx,
				user_count.UserCountPubSubscription,
				consumerName,
				handler,
			)
			return nil
		},
	)

	authenticator := admin.NewAuthenticator(
		func(username string) (globalCommon.User, error) {
			user, err := adminDB.GetUserByUsername(username)
			if err != nil {
				return globalCommon.User{}, err
			}
			return user, nil
		},
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
	)

	startAdminServer(
		config,
		createUserCommandHandler,
		dashboardHandler,
		loginHandler,
		deleteUserHandler,
		usersPageHandler,
		authenticator,
	)

	// Start gRPC server
	startGRPCServer(config, eventStore, authenticator)
}

func initializeConfig() *c.AppConfig {
	config, err := c.LoadConfig()
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logr, err := l.ZapLogger(config.Logging.Level)
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	AppLogger = logr
	return config
}

func createDefaultUser(ctx context.Context, adminBoundary string, eventstore pb.EventStore) error {
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
	); err != nil && !errors.As(err, &userExistsError) {
		return err
	}
	return nil
}
func initializeDatabase(ctx context.Context, config *c.AppConfig, js jetstream.JetStream) (pb.ImplementerSaveEvents,
	pb.ImplementerGetEvents, pb.LockProvider, common.DB, common.EventPublishing) {
	db, err := sql.Open(
		"postgres",
		fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			config.Postgres.Host,
			config.Postgres.Port,
			config.Postgres.User,
			config.Postgres.Password,
			config.Postgres.Name,
		),
	)
	if err != nil {
		AppLogger.Fatalf("Failed to connect to database: %v", err)
	}
	go func() {
		<-ctx.Done()
		AppLogger.Info("Shutting down database connection")
		db.Close()
	}()

	postgesBoundarySchemaMappings := config.Postgres.GetSchemaMapping()
	for _, schema := range postgesBoundarySchemaMappings {
		isAdminBoundary := schema.Boundary == config.Admin.Boundary
		if err := postgres.RunDbScripts(db, schema.Schema, isAdminBoundary, ctx); err != nil {
			AppLogger.Fatalf("Failed to run database migrations for schema %s: %v", schema, err)
		}
		AppLogger.Info("Database migrations for schema %s completed successfully", schema)
	}

	saveEvents := postgres.NewPostgresSaveEvents(db, &AppLogger, postgesBoundarySchemaMappings)
	getEvents := postgres.NewPostgresGetEvents(db, &AppLogger, postgesBoundarySchemaMappings)
	lockProvider, err := pb.NewJetStreamLockProvider(ctx, js)
	if err != nil {
		AppLogger.Fatalf("Failed to create lock provider: %v", err)
	}
	// lockProvider := postgres.NewPGLockProvider(db, AppLogger)
	adminDB := postgres.NewPostgresAdminDB(
		db, AppLogger, postgesBoundarySchemaMappings[config.Admin.Boundary].Schema,
	)
	eventPublishing := postgres.NewPostgresEventPublishing(
		db,
		AppLogger,
		postgesBoundarySchemaMappings,
	)

	return saveEvents, getEvents, lockProvider, adminDB, eventPublishing
}

func initializeNATS(ctx context.Context, config *c.AppConfig) (jetstream.JetStream, *nats.Conn, *server.Server) {
	natsOptions := createNATSOptions(config)
	natsServer := startNATSServer(natsOptions, config)

	// Connect to NATS
	nc, err := nats.Connect("", nats.InProcessServer(natsServer))
	if err != nil {
		AppLogger.Fatalf("Failed to connect to NATS: %v", err)
	}

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		AppLogger.Fatalf("Failed to create JetStream context: %v", err)
	}
	// time.Sleep(30 * time.Second)
	waitForJetStream(ctx, js)
	return js, nc, natsServer
}

func createNATSOptions(config *c.AppConfig) *server.Options {
	options := &server.Options{
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
		options.Routes = convertToURLSlice(config.Nats.Cluster.GetRoutes())
		AppLogger.Info("Nats cluster is enabled, running in clustered mode")
		AppLogger.Info(
			"Cluster configuration: Name=%v, Host=%v, Port=%v, Routes=%v",
			config.Nats.Cluster.Name,
			config.Nats.Cluster.Host,
			config.Nats.Cluster.Port,
			config.Nats.Cluster.Routes,
		)
	} else {
		AppLogger.Info("Nats cluster is disabled, running in standalone mode")
	}

	return options
}

func startNATSServer(options *server.Options, config *c.AppConfig) *server.Server {
	natsServer, err := server.NewServer(options)
	if err != nil {
		AppLogger.Fatalf("Failed to create NATS server: %v", err)
	}

	natsServer.ConfigureLogger()
	go natsServer.Start()
	if !natsServer.ReadyForConnections(config.Nats.Cluster.Timeout) {
		AppLogger.Fatal("NATS server failed to start")
	}
	AppLogger.Info("NATS server started on ", natsServer.ClientURL())

	return natsServer
}

func waitForJetStream(ctx context.Context, js jetstream.JetStream) {
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
				AppLogger.Warnf("failed to add stream: %v %v", streamName, err)
				time.Sleep(5 * time.Second)
				continue
			}

			r, err := js.Publish(ctx, streamName+".test", []byte("test"))
			if err != nil {
				AppLogger.Warnf("Failed to publish to JetStream, retrying in 5 second: %v", err)
				time.Sleep(5 * time.Second)
			} else {
				AppLogger.Infof("Published to JetStream: %v", r)
				break
			}
		}
	}()

	select {
	case <-jetStreamTestDone:
		AppLogger.Info("JetStream system is available")
	}
}

func initializeEventStore(
	ctx context.Context,
	config *c.AppConfig,
	saveEvents pb.ImplementerSaveEvents,
	getEvents pb.ImplementerGetEvents,
	lockProvider pb.LockProvider,
	js jetstream.JetStream) *pb.EventStore {

	AppLogger.Info("Initializing EventStore")
	eventStore := pb.NewEventStoreServer(
		ctx,
		js,
		saveEvents,
		getEvents,
		lockProvider,
		getBoundaryNames(&config.Boundaries),
	)
	AppLogger.Info("EventStore initialized")

	return eventStore
}

func startEventPolling(
	ctx context.Context,
	config *c.AppConfig,
	lockProvider pb.LockProvider,
	getEvents pb.ImplementerGetEvents,
	js jetstream.JetStream,
	eventPublishing common.EventPublishing) {
	for _, schema := range config.Postgres.GetSchemaMapping() {
		unlock, err := lockProvider.Lock(ctx, schema.Boundary)
		if err != nil {
			AppLogger.Fatalf("Failed to acquire lock: %v", err)
		}

		AppLogger.Infof("Successfully acquired polling lock for %v", schema.Schema)

		// Get last published position
		lastPosition, err := eventPublishing.GetLastPublishedEventPosition(ctx, schema.Boundary)
		if err != nil {
			AppLogger.Fatalf("Failed to get last published position: %v", err)
		}
		AppLogger.Info("Last published position for schema %v: %v", schema, &lastPosition)

		go func(boundary c.BoundaryToPostgresSchemaMapping) {
			defer unlock()
			PollEventsFromDatabaseToNats(
				ctx,
				js,
				getEvents,
				config.PollingPublisher.BatchSize,
				&lastPosition,
				boundary.Boundary,
				eventPublishing,
				schema.Schema,
			)
		}(schema)
	}
}

func startAdminServer(
	config *c.AppConfig,
	createUserHandler *create_user.CreateUserHandler,
	dashboardHandler *dashboard.DashboardHandler,
	loginHandler *login.LoginHandler,
	deleteUserHandler *delete_user.DeleteUserHandler,
	usersHandler *users_page.UsersPageHandler,
	authenticator *admin.Authenticator) {
	go func() {
		adminServer, err := admin.NewAdminServer(
			AppLogger,
			createUserHandler,
			dashboardHandler,
			loginHandler,
			deleteUserHandler,
			usersHandler,
		)
		if err != nil {
			AppLogger.Fatalf("Could not start admin server %v", err)
		}
		httpServer := &http.Server{
			Addr:    fmt.Sprintf(":%s", config.Admin.Port),
			Handler: adminServer,
		}

		AppLogger.Info("Starting admin server on port %s", config.Admin.Port)
		if err := httpServer.ListenAndServe(); err != nil {
			AppLogger.Errorf("Admin server error: %v", err)
		}
		AppLogger.Info("Admin server started on port %s", config.Admin.Port)
	}()
}

func startGRPCServer(config *c.AppConfig, eventStore pb.EventStoreServer, authenticator *admin.Authenticator) {
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(admin.UnaryAuthInterceptor(authenticator)),
		grpc.StreamInterceptor(admin.StreamAuthInterceptor(authenticator)),
		grpc.ChainUnaryInterceptor(recoveryInterceptor),
		grpc.ChainStreamInterceptor(streamErrorInterceptor),
	)
	pb.RegisterEventStoreServer(grpcServer, eventStore)

	if config.Grpc.EnableReflection {
		AppLogger.Info("Enabling gRPC server reflection")
		reflection.Register(grpcServer)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", config.Grpc.Port))
	if err != nil {
		AppLogger.Fatalf("Failed to listen: %v", err)
	}

	AppLogger.Info("Grpc Server listening on port %s", config.Grpc.Port)
	if err := grpcServer.Serve(lis); err != nil {
		AppLogger.Fatalf("Failed to serve: %v", err)
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
	getEvents common.GetEventsType) {
	go func() {
		userProjector := up.NewUserProjector(
			getProjectorLastPosition,
			updateProjectorPosition,
			createNewUser,
			deleteUser,
			saveEvents,
			getEvents,
			AppLogger,
			adminBoundary,
			subscribeToEvents,
		)
		err := userProjector.Start(ctx)

		if err != nil {
			AppLogger.Fatalf("Failed to start projection %v", err)
		}
		AppLogger.Info("User projector started")
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
	saveUserCount user_count.SaveUserCount) {
	go func() {
		userProjector := user_count.NewUserCountProjection(
			adminBoundary,
			getProjectorLastPosition,
			publishToPubSub,
			getUserCount,
			saveUserCount,
			subscribeToEvents,
			updateProjectorPosition,
			AppLogger,
		)
		err := userProjector.Start(ctx)

		if err != nil {
			AppLogger.Fatalf("Failed to start projection %v", err)
		}
		AppLogger.Info("User count projector started")
	}()
}

var mutex sync.RWMutex

func PollEventsFromDatabaseToNats(
	ctx context.Context,
	js jetstream.JetStream,
	eventStore pb.ImplementerGetEvents,
	batchSize uint32,
	lastPosition *pb.Position,
	boundary string,
	db common.EventPublishing,
	schema string,
) error {
	// Start polling loop
	for {
		if ctx.Err() != nil {
			AppLogger.Error("Context cancelled, stopping polling")
			return ctx.Err()
		}

		AppLogger.Debugf("Polling for boundary: %v", boundary)
		req := &pb.GetEventsRequest{
			FromPosition: lastPosition,
			Count:        batchSize,
			Direction:    pb.Direction_ASC,
			Boundary:     boundary,
		}
		resp, err := eventStore.Get(ctx, req)
		if err != nil {
			AppLogger.Errorf("Error retrieving events: %v", err)
			return fmt.Errorf("failed to get events: %v", err)
		}

		AppLogger.Debugf("Got %d events for boundary %v", len(resp.Events), boundary)

		for _, event := range resp.Events {
			subjectName := pb.GetEventSubjectName(
				boundary,
				&pb.Position{
					CommitPosition:  event.Position.CommitPosition,
					PreparePosition: event.Position.PreparePosition,
				},
			)
			AppLogger.Debugf("Subject name is: %s", subjectName)
			eventData, err := json.Marshal(event)
			if err != nil {
				AppLogger.Errorf("Failed to marshal event: %v", err)
				continue
			}
			publishEventWithRetry(
				ctx,
				js,
				eventData,
				subjectName,
				event.Position.PreparePosition,
				event.Position.CommitPosition,
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
		AppLogger.Debugf(":%v Sleeping.....", boundary)
		time.Sleep(500 * time.Millisecond) // Polling interval
	}
}

func publishEventWithRetry(ctx context.Context, js jetstream.JetStream, eventData []byte,
	subjectName string, preparePosition uint64, commitPosition uint64) {

	attempt := 1

	messageIdOpts := jetstream.PublishOpt(
		jetstream.WithMsgID(pb.GetEventNatsMessageId(int64(preparePosition), int64(commitPosition))),
	)
	retryOpts := jetstream.WithRetryAttempts(999999999999)

	_, err := js.Publish(ctx, subjectName, eventData, messageIdOpts, retryOpts)
	if err == nil {
		AppLogger.Debugf("Successfully published event after %d attempts", attempt)
		return
	}

	AppLogger.Errorf("Failed to publish event (attempt %d): ", err)

	publishEventWithRetry(ctx, js, eventData, subjectName, preparePosition, commitPosition)
}

func getBoundaryNames(boundary *[]c.Boundary) *[]string {
	var names []string
	for _, boundary := range *boundary {
		names = append(names, boundary.Name)
	}
	return &names
}

func streamErrorInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	err := handler(srv, ss)
	if err != nil {
		AppLogger.Errorf("Error in streaming RPC %s: %v", info.FullMethod, err)
		return status.Errorf(codes.Internal, "Error: %v", err)
	}
	return nil
}

func recoveryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			AppLogger.Errorf("Panic in %s: %v\nStack Trace:\n%s", info.FullMethod, r, debug.Stack())
			err = status.Errorf(codes.Internal, "Internal server error")
		}
	}()
	return handler(ctx, req)
}

func convertToURLSlice(routes []string) []*url.URL {
	var urls []*url.URL
	for _, route := range routes {
		u, err := url.Parse(route)
		if err != nil {
			AppLogger.Fatalf("Warning: invalid route URL %q: %v", route, err)
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
