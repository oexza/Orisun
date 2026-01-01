package com.orisunlabs.orisun.client;

import io.grpc.*;
import io.grpc.stub.StreamObserver;
import com.orisun.eventstore.*;

import java.util.*;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.CompletableFuture;

public class OrisunClient implements AutoCloseable {
    private final ManagedChannel channel;
    private final EventStoreGrpc.EventStoreBlockingStub blockingStub;
    private final EventStoreGrpc.EventStoreStub asyncStub;
    private final int defaultTimeoutSeconds;
    private final Logger logger;
    private final TokenCache tokenCache;
    private final boolean disposed = false;
    private final String username;
    private final String password;

    public static class Builder {
        private List<ServerAddress> servers = new ArrayList<>();
        private int timeoutSeconds = 30;
        private boolean useTls = false;
        private ManagedChannel channel;
        private String loadBalancingPolicy = "round_robin";
        private String username;
        private String password;
        private Logger logger;
        private boolean enableLogging = false;
        private DefaultLogger.LogLevel logLevel = DefaultLogger.LogLevel.INFO;
        private boolean useDnsResolver = true;

        // Keep-alive settings
        private long keepAliveTimeMs = 30000;
        private long keepAliveTimeoutMs = 10000;
        private boolean keepAlivePermitWithoutCalls = true;

        // DNS and static target settings
        private String dnsTarget;
        private String staticTarget;

        // Keep the original methods for backward compatibility
        public Builder withHost(String host) {
            return withServer(host, 50051);
        }

        public Builder withPort(int port) {
            if (servers.isEmpty()) {
                servers.add(new ServerAddress("localhost", port));
            } else {
                // Update the last added server's port
                ServerAddress lastServer = servers.get(servers.size() - 1);
                servers.set(servers.size() - 1, new ServerAddress(lastServer.host, port));
            }
            return this;
        }

        // New methods for multiple servers
        public Builder withServer(String host, int port) {
            servers.add(new ServerAddress(host, port));
            return this;
        }

        public Builder withServers(List<ServerAddress> servers) {
            this.servers.addAll(servers);
            return this;
        }

        public Builder withLoadBalancingPolicy(String policy) {
            this.loadBalancingPolicy = policy;
            return this;
        }

        // DNS-based load balancing
        public Builder withDnsTarget(String dnsTarget) {
            this.dnsTarget = dnsTarget;
            return this;
        }

        public Builder withStaticTarget(String staticTarget) {
            this.staticTarget = staticTarget;
            return this;
        }

        public Builder withDnsResolver(boolean useDns) {
            this.useDnsResolver = useDns;
            return this;
        }

        public Builder withTimeout(int seconds) {
            this.timeoutSeconds = seconds;
            return this;
        }

        public Builder withTls(boolean useTls) {
            this.useTls = useTls;
            return this;
        }

        public Builder withChannel(ManagedChannel channel) {
            this.channel = channel;
            return this;
        }

        // Add authentication methods
        public Builder withBasicAuth(String username, String password) {
            this.username = username;
            this.password = password;
            return this;
        }

        // Add logging methods
        public Builder withLogger(Logger logger) {
            this.logger = logger;
            return this;
        }

        public Builder withLogging(boolean enableLogging) {
            this.enableLogging = enableLogging;
            return this;
        }

        public Builder withLogLevel(DefaultLogger.LogLevel level) {
            this.logLevel = level;
            return this;
        }

        // Add keep-alive methods
        public Builder withKeepAliveTime(long keepAliveTimeMs) {
            this.keepAliveTimeMs = keepAliveTimeMs;
            return this;
        }

        public Builder withKeepAliveTimeout(long keepAliveTimeoutMs) {
            this.keepAliveTimeoutMs = keepAliveTimeoutMs;
            return this;
        }

        public Builder withKeepAlivePermitWithoutCalls(boolean permitWithoutCalls) {
            this.keepAlivePermitWithoutCalls = permitWithoutCalls;
            return this;
        }

        public OrisunClient build() {
            // Initialize logger
            Logger clientLogger;
            // Minimal logging by default
            if (enableLogging && logger == null) {
                clientLogger = new DefaultLogger(logLevel);
            } else
                clientLogger = Objects.requireNonNullElseGet(logger,
                        () -> new DefaultLogger(DefaultLogger.LogLevel.WARN));

            // Initialize token cache
            TokenCache clientTokenCache = new TokenCache(clientLogger);

            if (this.channel == null) {
                ManagedChannelBuilder<?> channelBuilder;

                // Check for DNS or static targets first
                if (dnsTarget != null && !dnsTarget.trim().isEmpty()) {
                    // DNS-based load balancing
                    String target = dnsTarget.startsWith("dns:///") ? dnsTarget : "dns:///" + dnsTarget;
                    channelBuilder = ManagedChannelBuilder.forTarget(target)
                            .defaultLoadBalancingPolicy(loadBalancingPolicy)
                            .keepAliveTime(keepAliveTimeMs, TimeUnit.MILLISECONDS)
                            .keepAliveTimeout(keepAliveTimeoutMs, TimeUnit.MILLISECONDS)
                            .keepAliveWithoutCalls(keepAlivePermitWithoutCalls);

                    if (!useTls) {
                        channelBuilder.usePlaintext();
                    }

                    this.channel = channelBuilder.build();
                } else if (staticTarget != null && !staticTarget.trim().isEmpty()) {
                    // Static-based load balancing
                    String target = staticTarget.startsWith("static:///") ? staticTarget : "static:///" + staticTarget;
                    channelBuilder = ManagedChannelBuilder.forTarget(target)
                            .defaultLoadBalancingPolicy(loadBalancingPolicy)
                            .keepAliveTime(keepAliveTimeMs, TimeUnit.MILLISECONDS)
                            .keepAliveTimeout(keepAliveTimeoutMs, TimeUnit.MILLISECONDS)
                            .keepAliveWithoutCalls(keepAlivePermitWithoutCalls);

                    if (!useTls) {
                        channelBuilder.usePlaintext();
                    }

                    this.channel = channelBuilder.build();
                } else {
                    // Traditional server-based load balancing
                    if (servers.isEmpty()) {
                        // Default to localhost if no servers specified
                        servers.add(new ServerAddress("localhost", 5005));
                    }

                    // Create channel with load balancing
                    if (servers.size() == 1) {
                        // Single server case
                        ServerAddress server = servers.getFirst();
                        channelBuilder = ManagedChannelBuilder.forAddress(server.host, server.port);
                    } else {
                        // Multiple servers case - check for comma-separated hosts and use name resolver and load balancing
                        String target;

                        // Check if any host contains commas for manual load balancing
                        boolean hasCommaSeparatedHosts = false;
                        for (ServerAddress server : servers) {
                            if (server.host.contains(",")) {
                                hasCommaSeparatedHosts = true;
                                break;
                            }
                        }

                        if (hasCommaSeparatedHosts) {
                            // Handle comma-separated list of hosts for manual load balancing
                            StringBuilder hostsBuilder = new StringBuilder();
                            for (ServerAddress server : servers) {
                                if (!hostsBuilder.isEmpty()) {
                                    hostsBuilder.append(",");
                                }
                                hostsBuilder.append(server.host).append(":").append(server.port);
                            }
                            target = hostsBuilder.toString();
                        } else {
                            // Use DNS or static resolver
                            target = createTargetString(servers);
                        }

                        channelBuilder = ManagedChannelBuilder.forTarget(target)
                                .defaultLoadBalancingPolicy(loadBalancingPolicy);
                    }

                    if (!useTls) {
                        channelBuilder.usePlaintext();
                    }

                    // Apply keep-alive settings
                    channelBuilder.keepAliveTime(keepAliveTimeMs, TimeUnit.MILLISECONDS)
                            .keepAliveTimeout(keepAliveTimeoutMs, TimeUnit.MILLISECONDS)
                            .keepAliveWithoutCalls(keepAlivePermitWithoutCalls);

                    channelBuilder.intercept(new ClientInterceptor() {
                        @Override
                        public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
                                MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {

                            return new ForwardingClientCall.SimpleForwardingClientCall<>(
                                    next.newCall(method, callOptions)) {

                                @Override
                                public void start(Listener<RespT> responseListener, Metadata headers) {
                                    final var metadata = clientTokenCache.createAuthMetadata(() ->
                                            username != null && password != null ? "Basic " + java.util.Base64.getEncoder()
                                                    .encodeToString((username + ":" + password).getBytes()) : null);
                                    // Copy metadata from our prepared metadata
                                    metadata.keys().forEach(key -> {
                                        for (String value : Objects.requireNonNull(metadata
                                                .getAll(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER)))) {
                                            headers.put(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER), value);
                                        }
                                    });
                                    super.start(new Listener<>() {
                                                    @Override
                                                    public void onHeaders(Metadata headers) {
                                                        // Extract and cache token from response headers
                                                        clientTokenCache.extractAndCacheToken(headers);

                                                        responseListener.onHeaders(headers);
                                                    }

                                                    @Override
                                                    public void onMessage(RespT message) {
                                                        responseListener.onMessage(message);
                                                    }

                                                    @Override
                                                    public void onClose(Status status, Metadata trailers) {
                                                        responseListener.onClose(status, trailers);
                                                    }

                                                    @Override
                                                    public void onReady() {
                                                        responseListener.onReady();
                                                    }
                                                },
                                            headers
                                    );
                                }
                            };
                        }
                    });

                    this.channel = channelBuilder.build();
                }
            }

            return new OrisunClient(this.channel, timeoutSeconds, clientLogger, clientTokenCache, username, password);
        }

        private String createTargetString(List<ServerAddress> servers) {
            // Choose between DNS and static resolution based on configuration
            StringBuilder sb = new StringBuilder(useDnsResolver ? "dns:///" : "static:///");
            boolean first = true;
            for (ServerAddress server : servers) {
                if (!first) {
                    sb.append(",");
                }
                sb.append(server.host).append(":").append(server.port);
                first = false;
            }
            return sb.toString();
        }
    }

    public static class ServerAddress {
        private final String host;
        private final int port;

        public ServerAddress(String host, int port) {
            this.host = host;
            this.port = port;
        }

        public String getHost() {
            return host;
        }

        public int getPort() {
            return port;
        }
    }

    public static Builder newBuilder() {
        return new Builder();
    }

    private OrisunClient(ManagedChannel channel, int timeoutSeconds, Logger logger, TokenCache tokenCache,
                         String username, String password) {
        this.channel = channel;
        this.defaultTimeoutSeconds = timeoutSeconds;
        this.logger = logger;
        this.tokenCache = tokenCache;
        this.username = username;
        this.password = password;
        this.blockingStub = EventStoreGrpc.newBlockingStub(channel);
        this.asyncStub = EventStoreGrpc.newStub(channel);

        this.logger.info("OrisunClient initialized with timeout: {} seconds", timeoutSeconds);
    }

    // Synchronous methods
    public Eventstore.WriteResult saveEvents(final Eventstore.SaveEventsRequest request) throws Exception {
        // Validate request
        RequestValidator.validateSaveEventsRequest(request);

        logger.debug("Saving {} events in boundary '{}'",
                request.getEventsCount(), request.getBoundary());

        try {
            Eventstore.WriteResult result = blockingStub
                    .saveEvents(request);

            logger.info("Successfully saved {} events'",
                    request.getEventsCount());
            return result;

        } catch (StatusRuntimeException e) {
            throw handleSaveException(e);
        }
    }

    private Exception handleSaveException(StatusRuntimeException e) {
        Map<String, Object> context = new HashMap<>();
        context.put("operation", "saveEvents");
        context.put("statusCode", e.getStatus().getCode().name());
        context.put("statusDescription", e.getStatus().getDescription());

        if (e.getStatus().getCode() == Status.Code.ALREADY_EXISTS) {
            final var versions = Utils.extractVersionNumbers(e.getStatus().getDescription());
            context.put("expectedVersion", versions[0]);
            context.put("actualVersion", versions[1]);
            return new OptimisticConcurrencyException(
                    e.getStatus().getDescription(), versions[0], versions[1]);
        }

        return new OrisunException("Failed to save events", e, context);
    }

    public Eventstore.GetEventsResponse getEvents(Eventstore.GetEventsRequest request) throws OrisunException {
        // Validate request
        RequestValidator.validateGetEventsRequest(request);

        logger.debug("Getting events from boundary: {}", request.getBoundary());

        try {
            final var response = blockingStub
                    .withDeadlineAfter(defaultTimeoutSeconds, TimeUnit.SECONDS)
                    .getEvents(request);

            logger.debug("Successfully retrieved {} events", response.getEventsCount());
            return response;

        } catch (StatusRuntimeException e) {
            Map<String, Object> context = new HashMap<>();
            context.put("operation", "getEvents");
            context.put("boundary", request.getBoundary());
            context.put("statusCode", e.getStatus().getCode().name());

            throw new OrisunException("Failed to get events", e, context);
        }
    }

    // Asynchronous methods
    public CompletableFuture<Eventstore.WriteResult> saveEventsAsync(Eventstore.SaveEventsRequest request) {
        CompletableFuture<Eventstore.WriteResult> future = new CompletableFuture<>();

        asyncStub
                .withDeadlineAfter(defaultTimeoutSeconds, TimeUnit.SECONDS)
                .saveEvents(request, new StreamObserver<>() {
                    @Override
                    public void onNext(Eventstore.WriteResult result) {
                        future.complete(result);
                    }

                    @Override
                    public void onError(Throwable t) {
                        if (t instanceof StatusRuntimeException e) {
                            future.completeExceptionally(handleSaveException(e));
                        } else
                            future.completeExceptionally(new OrisunException("Failed to save events", t));
                    }

                    @Override
                    public void onCompleted() {
                        // Already completed in onNext
                    }
                });

        return future;
    }

    // Streaming methods
    public EventSubscription subscribeToEvents(Eventstore.CatchUpSubscribeToEventStoreRequest request,
                                               EventSubscription.EventHandler handler) {
        // Validate request
        RequestValidator.validateSubscribeRequest(request);

        logger.debug("Subscribing to events in boundary '{}' with subscriber '{}'",
                request.getBoundary(), request.getSubscriberName());

        return new EventSubscription(asyncStub, request, handler, defaultTimeoutSeconds, logger, tokenCache, username,
                password);
    }

    /**
     * Ping the server to check connectivity
     *
     * @throws OrisunException if the ping fails
     */
    public void ping() throws OrisunException {
        logger.debug("Pinging server");

        try {
            final var request = Eventstore.PingRequest.newBuilder().build();

            blockingStub.ping(request);

            logger.debug("Ping successful");

        } catch (StatusRuntimeException e) {
            Map<String, Object> context = new HashMap<>();
            context.put("operation", "ping");
            context.put("statusCode", e.getStatus().getCode().name());

            throw new OrisunException("Ping failed", e, context);
        }
    }

    /**
     * Check if the client is connected to the server
     *
     * @return true if connected, false otherwise
     */
    public boolean healthCheck(final String boundary) throws OrisunException {
        logger.debug("Performing health check");

        // Try to ping the server
        ping();

        // Try to make a simple call to test connectivity
        getEvents(Eventstore.GetEventsRequest.newBuilder()
                .setBoundary(boundary)
                .setCount(1)
                .build());

        logger.debug("Health check successful");
        return true;
    }

    @Override
    public void close() {
        if (disposed) {
            logger.debug("OrisunClient already disposed");
            return;
        }

        logger.debug("Closing OrisunClient connection");

        if (channel != null && !channel.isShutdown()) {
            try {
                channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
                logger.info("OrisunClient connection closed successfully");
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                logger.error("Interrupted while closing OrisunClient connection", e);
            }
        }
    }
}