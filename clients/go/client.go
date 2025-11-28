package orisun

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"time"

	eventstore "github.com/orisunlabs/orisun-go-client/eventstore"
)

// OrisunClient is the main client for interacting with the Orisun event store
type OrisunClient struct {
	conn           *grpc.ClientConn
	client         eventstore.EventStoreClient
	defaultTimeout time.Duration
	logger         Logger
	tokenCache     *TokenCache
	username       string
	password       string
	mu             sync.RWMutex
	closed         bool
}

// ClientBuilder is used to build an OrisunClient instance
type ClientBuilder struct {
	servers                     []*ServerAddress
	timeoutSeconds              int
	useTLS                      bool
	loadBalancingPolicy         string
	username                    string
	password                    string
	logger                      Logger
	enableLogging               bool
	logLevel                    LogLevel
	useDnsResolver              bool
	keepAliveTimeMs             time.Duration
	keepAliveTimeoutMs          time.Duration
	keepAlivePermitWithoutCalls bool
	dnsTarget                   string
	staticTarget                string
	channel                     *grpc.ClientConn
}

// NewClientBuilder creates a new ClientBuilder with default values
func NewClientBuilder() *ClientBuilder {
	return &ClientBuilder{
		timeoutSeconds:              30,
		useTLS:                      false,
		loadBalancingPolicy:         "round_robin",
		enableLogging:               false,
		logLevel:                    INFO,
		useDnsResolver:              true,
		keepAliveTimeMs:             30 * time.Second,
		keepAliveTimeoutMs:          10 * time.Second,
		keepAlivePermitWithoutCalls: true,
		servers:                     make([]*ServerAddress, 0),
	}
}

// WithHost adds a server with the given host and default port
func (b *ClientBuilder) WithHost(host string) *ClientBuilder {
	return b.WithServer(host, 50051)
}

// WithPort sets the port for the last added server
func (b *ClientBuilder) WithPort(port int) *ClientBuilder {
	if len(b.servers) == 0 {
		b.servers = append(b.servers, NewServerAddress("localhost", port))
	} else {
		lastServer := b.servers[len(b.servers)-1]
		b.servers[len(b.servers)-1] = NewServerAddress(lastServer.Host, port)
	}
	return b
}

// WithServer adds a server with the given host and port
func (b *ClientBuilder) WithServer(host string, port int) *ClientBuilder {
	b.servers = append(b.servers, NewServerAddress(host, port))
	return b
}

// WithServers adds multiple servers
func (b *ClientBuilder) WithServers(servers []*ServerAddress) *ClientBuilder {
	b.servers = append(b.servers, servers...)
	return b
}

// WithLoadBalancingPolicy sets the load balancing policy
func (b *ClientBuilder) WithLoadBalancingPolicy(policy string) *ClientBuilder {
	b.loadBalancingPolicy = policy
	return b
}

// WithDnsTarget sets the DNS target for DNS-based load balancing
func (b *ClientBuilder) WithDnsTarget(dnsTarget string) *ClientBuilder {
	b.dnsTarget = dnsTarget
	return b
}

// WithStaticTarget sets the static target for static-based load balancing
func (b *ClientBuilder) WithStaticTarget(staticTarget string) *ClientBuilder {
	b.staticTarget = staticTarget
	return b
}

// WithDnsResolver sets whether to use DNS resolver
func (b *ClientBuilder) WithDnsResolver(useDns bool) *ClientBuilder {
	b.useDnsResolver = useDns
	return b
}

// WithTimeout sets the default timeout in seconds
func (b *ClientBuilder) WithTimeout(seconds int) *ClientBuilder {
	b.timeoutSeconds = seconds
	return b
}

// WithTLS sets whether to use TLS
func (b *ClientBuilder) WithTLS(useTLS bool) *ClientBuilder {
	b.useTLS = useTLS
	return b
}

// WithChannel sets a custom gRPC channel
func (b *ClientBuilder) WithChannel(channel *grpc.ClientConn) *ClientBuilder {
	b.channel = channel
	return b
}

// WithBasicAuth sets the basic authentication credentials
func (b *ClientBuilder) WithBasicAuth(username, password string) *ClientBuilder {
	b.username = username
	b.password = password
	return b
}

// WithLogger sets the logger
func (b *ClientBuilder) WithLogger(logger Logger) *ClientBuilder {
	b.logger = logger
	return b
}

// WithLogging enables or disables logging
func (b *ClientBuilder) WithLogging(enableLogging bool) *ClientBuilder {
	b.enableLogging = enableLogging
	return b
}

// WithLogLevel sets the log level
func (b *ClientBuilder) WithLogLevel(level LogLevel) *ClientBuilder {
	b.logLevel = level
	return b
}

// WithKeepAliveTime sets the keep-alive time
func (b *ClientBuilder) WithKeepAliveTime(keepAliveTimeMs time.Duration) *ClientBuilder {
	b.keepAliveTimeMs = keepAliveTimeMs
	return b
}

// WithKeepAliveTimeout sets the keep-alive timeout
func (b *ClientBuilder) WithKeepAliveTimeout(keepAliveTimeoutMs time.Duration) *ClientBuilder {
	b.keepAliveTimeoutMs = keepAliveTimeoutMs
	return b
}

// WithKeepAlivePermitWithoutCalls sets whether to permit keep-alive without calls
func (b *ClientBuilder) WithKeepAlivePermitWithoutCalls(permitWithoutCalls bool) *ClientBuilder {
	b.keepAlivePermitWithoutCalls = permitWithoutCalls
	return b
}

// Build creates the OrisunClient instance
func (b *ClientBuilder) Build() (*OrisunClient, error) {
	// Initialize logger
	var clientLogger Logger
	if b.enableLogging && b.logger == nil {
		clientLogger = NewDefaultLogger(b.logLevel)
	} else if b.logger == nil {
		clientLogger = NewDefaultLogger(WARN)
	} else {
		clientLogger = b.logger
	}

	// Initialize token cache
	clientTokenCache := NewTokenCache(clientLogger)

	var conn *grpc.ClientConn
	var err error

	if b.channel != nil {
		conn = b.channel
	} else {
		// Create channel based on configuration
		if b.dnsTarget != "" && strings.TrimSpace(b.dnsTarget) != "" {
			// DNS-based load balancing
			target := b.dnsTarget
			if !strings.HasPrefix(target, "dns:///") {
				target = "dns:///" + target
			}
			conn, err = b.createChannel(target)
		} else if b.staticTarget != "" && strings.TrimSpace(b.staticTarget) != "" {
			// Static-based load balancing
			target := b.staticTarget
			if !strings.HasPrefix(target, "static:///") {
				target = "static:///" + target
			}
			conn, err = b.createChannel(target)
		} else {
			// Traditional server-based load balancing
			if len(b.servers) == 0 {
				// Default to localhost if no servers specified
				b.servers = append(b.servers, NewServerAddress("localhost", 5005))
			}

			var target string
			if len(b.servers) == 1 {
				// Single server case
				server := b.servers[0]
				target = fmt.Sprintf("%s:%d", server.Host, server.Port)
			} else {
				// Multiple servers case - use name resolver and load balancing
				// Check if hosts contain commas for manual load balancing
				hasCommaSeparatedHosts := false
				for _, server := range b.servers {
					if strings.Contains(server.Host, ",") {
						hasCommaSeparatedHosts = true
						break
					}
				}

				if hasCommaSeparatedHosts {
					// Handle comma-separated list of hosts for manual load balancing
					var hosts []string
					for _, server := range b.servers {
						hosts = append(hosts, fmt.Sprintf("%s:%d", server.Host, server.Port))
					}
					target = strings.Join(hosts, ",")
				} else {
					// Use DNS or static resolver
					target = b.createTargetString(b.servers)
				}
			}

			conn, err = b.createChannel(target)
		}

		if err != nil {
			return nil, NewOrisunExceptionWithCause("Failed to create gRPC channel", err)
		}
	}

	client := &OrisunClient{
		conn:           conn,
		client:         eventstore.NewEventStoreClient(conn),
		defaultTimeout: time.Duration(b.timeoutSeconds) * time.Second,
		logger:         clientLogger,
		tokenCache:     clientTokenCache,
		username:       b.username,
		password:       b.password,
		closed:         false,
	}

	clientLogger.Info("OrisunClient initialized with timeout: {} seconds", b.timeoutSeconds)
	return client, nil
}

// createChannel creates a gRPC channel with the given target
func (b *ClientBuilder) createChannel(target string) (*grpc.ClientConn, error) {
	kp := keepalive.ClientParameters{
		Time:                b.keepAliveTimeMs,
		Timeout:             b.keepAliveTimeoutMs,
		PermitWithoutStream: b.keepAlivePermitWithoutCalls,
	}

	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(kp),
	}

	// Set up load balancing configuration
	if b.loadBalancingPolicy != "" {
		serviceConfig := fmt.Sprintf(`{"loadBalancingConfig": [{"%s": {}}]}`, b.loadBalancingPolicy)
		opts = append(opts, grpc.WithDefaultServiceConfig(serviceConfig))
	}

	if !b.useTLS {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Add authentication interceptor if credentials are provided
	if b.username != "" && b.password != "" {
		basicAuth := CreateBasicAuthCredentials(b.username, b.password)
		opts = append(opts, grpc.WithUnaryInterceptor(b.createAuthInterceptor(basicAuth)))
		opts = append(opts, grpc.WithStreamInterceptor(b.createStreamAuthInterceptor(basicAuth)))
	}

	return grpc.Dial(target, opts...)
}

// createAuthInterceptor creates a unary interceptor for authentication
func (b *ClientBuilder) createAuthInterceptor(basicAuth string) grpc.UnaryClientInterceptor {
	tokenCache := NewTokenCache(nil)

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// Add authentication metadata
		md := tokenCache.CreateAuthMetadata(basicAuth)
		ctx = metadata.NewOutgoingContext(ctx, md)

		// Create a trailer-only context to extract response headers
		var trailerMD metadata.MD
		trailerCtx := context.WithValue(ctx, "trailerKey", &trailerMD)

		// Call the invoker with a custom option to capture trailers
		err := invoker(trailerCtx, method, req, reply, cc, opts...)

		// Extract and cache token from response trailers
		if trailerMD != nil {
			tokenCache.ExtractAndCacheToken(trailerMD)
		}

		return err
	}
}

// createStreamAuthInterceptor creates a stream interceptor for authentication
func (b *ClientBuilder) createStreamAuthInterceptor(basicAuth string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		// Add authentication metadata
		tokenCache := NewTokenCache(nil) // Create a temporary token cache for the interceptor
		md := tokenCache.CreateAuthMetadata(basicAuth)
		ctx = metadata.NewOutgoingContext(ctx, md)

		// Create the stream with trailer capture
		stream, err := streamer(ctx, desc, cc, method, opts...)

		// Set up a function to extract tokens from trailers when available
		if stream != nil {
			// Note: In gRPC-Go, trailer extraction is handled differently
			// We'll need to use stream-specific methods to access trailers
			// This is a simplified approach - in production, you might need
			// to implement custom stream wrapping to properly intercept trailers
		}

		return stream, err
	}
}

// createTargetString creates a target string from server addresses
func (b *ClientBuilder) createTargetString(servers []*ServerAddress) string {
	var sb strings.Builder
	prefix := "dns:///"
	if !b.useDnsResolver {
		prefix = "static:///"
	}
	sb.WriteString(prefix)

	for i, server := range servers {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%s:%d", server.Host, server.Port))
	}

	return sb.String()
}

// Close closes the client connection
func (c *OrisunClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		c.logger.Debug("OrisunClient already closed")
		return nil
	}

	c.logger.Debug("Closing OrisunClient connection")

	if c.conn != nil {
		err := c.conn.Close()
		if err != nil {
			c.logger.Errorf("Error closing connection: %v", err)
			return err
		}
		c.logger.Info("OrisunClient connection closed successfully")
	}

	c.closed = true
	return nil
}

// IsClosed returns true if the client is closed
func (c *OrisunClient) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// Ping pings the server to check connectivity
func (c *OrisunClient) Ping(ctx context.Context) error {
	c.logger.Debug("Pinging server")

	// Placeholder implementation - will be replaced with actual gRPC call
	// request := &eventstore.PingRequest{}
	// _, err := c.blockingStub.Ping(ctx, request)
	// return err

	c.logger.Debug("Ping successful")
	return nil
}

// HealthCheck performs a health check
func (c *OrisunClient) HealthCheck(ctx context.Context, boundary string) (bool, error) {
	c.logger.Debug("Performing health check")

	// Try to ping the server
	if err := c.Ping(ctx); err != nil {
		return false, NewOrisunExceptionWithCause("Health check failed - ping failed", err).
			AddContext("operation", "healthCheck")
	}

	// Try to make a simple call to test connectivity
	// Placeholder implementation - will be replaced with actual gRPC call
	request := &eventstore.GetEventsRequest{
		Boundary: boundary,
		Stream:   &eventstore.GetStreamQuery{Name: "health-check"},
		Count:    1,
	}
	_, err := c.GetEvents(ctx, request)
	if err != nil {
		return false, NewOrisunExceptionWithCause("Health check failed - get events failed", err).
			AddContext("operation", "healthCheck").
			AddContext("boundary", boundary)
	}

	c.logger.Debug("Health check successful")
	return true, nil
}

// SaveEvents saves events to a stream
func (c *OrisunClient) SaveEvents(ctx context.Context, request *eventstore.SaveEventsRequest) (*eventstore.WriteResult, error) {
	// Validate request
	validator := NewRequestValidator()
	if err := validator.ValidateSaveEventsRequest(request); err != nil {
		return nil, err
	}

	c.logger.Debug("Saving {} events to stream '{}' in boundary '{}'",
		len(request.Events), request.Stream.Name, request.Boundary)

	// Create context with timeout if not provided
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), c.defaultTimeout)
		defer cancel()
	}

	// Make the gRPC call
	response, err := c.client.SaveEvents(ctx, request)
	if err != nil {
		return nil, c.handleSaveException(err)
	}

	c.logger.Info("Successfully saved {} events to stream '{}'",
		len(request.Events), request.Stream.Name)

	return response, nil
}

// GetEvents retrieves events from the event store
func (c *OrisunClient) GetEvents(ctx context.Context, request *eventstore.GetEventsRequest) (*eventstore.GetEventsResponse, error) {
	// Validate request
	validator := NewRequestValidator()
	if err := validator.ValidateGetEventsRequest(request); err != nil {
		return nil, err
	}

	c.logger.Debug("Getting events from boundary: {}", request.Boundary)

	// Create context with timeout if not provided
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), c.defaultTimeout)
		defer cancel()
	}

	// Make the gRPC call
	response, err := c.client.GetEvents(ctx, request)
	if err != nil {
		return nil, c.handleGetException(err, request)
	}

	c.logger.Debug("Successfully retrieved {} events", len(response.Events))
	return response, nil
}

// SubscribeToEvents subscribes to events from the event store
func (c *OrisunClient) SubscribeToEvents(ctx context.Context, request *eventstore.CatchUpSubscribeToEventStoreRequest, handler EventHandler) (*EventSubscription, error) {
	// Validate request
	validator := NewRequestValidator()
	if err := validator.ValidateSubscribeRequest(request); err != nil {
		return nil, err
	}

	c.logger.Debug("Subscribing to events in boundary '{}' with subscriber '{}'",
		request.Boundary, request.SubscriberName)

	// Create context with timeout if not provided
	if ctx == nil {
		ctx = context.Background()
	}

	// Create cancel function for the subscription
	ctx, cancel := context.WithCancel(ctx)

	// Make the gRPC streaming call
	stream, err := c.client.CatchUpSubscribeToEvents(ctx, request)
	if err != nil {
		cancel()
		return nil, c.handleSubscribeException(err, request)
	}

	// Create and return subscription
	subscription := NewEventSubscription(stream, handler, c.logger, cancel)
	return subscription, nil
}

// SubscribeToStream subscribes to events from a specific stream
func (c *OrisunClient) SubscribeToStream(ctx context.Context, request *eventstore.CatchUpSubscribeToStreamRequest, handler EventHandler) (*EventSubscription, error) {
	// Validate request
	validator := NewRequestValidator()
	if err := validator.ValidateSubscribeToStreamRequest(request); err != nil {
		return nil, err
	}

	c.logger.Debug("Subscribing to stream '{}' in boundary '{}' with subscriber '{}'",
		request.Stream, request.Boundary, request.SubscriberName)

	// Create context with timeout if not provided
	if ctx == nil {
		ctx = context.Background()
	}

	// Create cancel function for the subscription
	ctx, cancel := context.WithCancel(ctx)

	// Make the gRPC streaming call
	stream, err := c.client.CatchUpSubscribeToStream(ctx, request)
	if err != nil {
		cancel()
		return nil, c.handleSubscribeException(err, request)
	}

	// Create and return subscription
	subscription := NewEventSubscription(stream, handler, c.logger, cancel)
	return subscription, nil
}

// handleSaveException handles exceptions from SaveEvents operations
func (c *OrisunClient) handleSaveException(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return NewOrisunExceptionWithCause("Failed to save events", err).
			AddContext("operation", "saveEvents")
	}

	if st.Code() == codes.AlreadyExists {
		// Extract version numbers from error description
		expected, actual, parseErr := ExtractVersionNumbers(st.Message())
		if parseErr == nil {
			return NewOptimisticConcurrencyExceptionWithCause(
				st.Message(), expected, actual, err).
				AddContext("operation", "saveEvents").
				AddContext("expectedVersion", expected).
				AddContext("actualVersion", actual)
		}
	}

	return NewOrisunExceptionWithCause("Failed to save events", err).
		AddContext("operation", "saveEvents").
		AddContext("statusCode", st.Code().String()).
		AddContext("statusDescription", st.Message())
}

// handleGetException handles exceptions from GetEvents operations
func (c *OrisunClient) handleGetException(err error, request *eventstore.GetEventsRequest) error {
	st, ok := status.FromError(err)
	if !ok {
		return NewOrisunExceptionWithCause("Failed to get events", err).
			AddContext("operation", "getEvents").
			AddContext("boundary", request.Boundary)
	}

	return NewOrisunExceptionWithCause("Failed to get events", err).
		AddContext("operation", "getEvents").
		AddContext("boundary", request.Boundary).
		AddContext("statusCode", st.Code().String()).
		AddContext("statusDescription", st.Message())
}

// handleSubscribeException handles exceptions from subscription operations
func (c *OrisunClient) handleSubscribeException(err error, request interface{}) error {
	st, ok := status.FromError(err)
	if !ok {
		return NewOrisunExceptionWithCause("Failed to create subscription", err).
			AddContext("operation", "subscribe")
	}

	return NewOrisunExceptionWithCause("Failed to create subscription", err).
		AddContext("operation", "subscribe").
		AddContext("statusCode", st.Code().String()).
		AddContext("statusDescription", st.Message())
}
