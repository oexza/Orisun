package io.orisun.client;

import io.grpc.*;
import io.grpc.stub.StreamObserver;
import com.orisun.eventstore.*;

import java.util.concurrent.TimeUnit;
import java.util.concurrent.CompletableFuture;
import java.util.List;
import java.util.ArrayList;

public class OrisunClient implements AutoCloseable {
    private final ManagedChannel channel;
    private final EventStoreGrpc.EventStoreBlockingStub blockingStub;
    private final EventStoreGrpc.EventStoreStub asyncStub;
    private final int defaultTimeoutSeconds;

    public static class Builder {
        private List<ServerAddress> servers = new ArrayList<>();
        private int timeoutSeconds = 30;
        private boolean useTls = false;
        private ManagedChannel channel;
        private String loadBalancingPolicy = "round_robin";

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
        
        private boolean useDnsResolver = true;
        
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

        public OrisunClient build() {
            if (channel == null) {
                if (servers.isEmpty()) {
                    // Default to localhost if no servers specified
                    servers.add(new ServerAddress("localhost", 50051));
                }

                // Create channel with load balancing
                ManagedChannelBuilder<?> channelBuilder;
                
                if (servers.size() == 1) {
                    // Single server case
                    ServerAddress server = servers.get(0);
                    channelBuilder = ManagedChannelBuilder.forAddress(server.host, server.port);
                } else {
                    // Multiple servers case - use name resolver and load balancing
                    String target = createTargetString(servers);
                    channelBuilder = ManagedChannelBuilder.forTarget(target)
                            .defaultLoadBalancingPolicy(loadBalancingPolicy);
                }

                if (!useTls) {
                    channelBuilder.usePlaintext();
                }

                channel = channelBuilder.build();
            }

            return new OrisunClient(channel, timeoutSeconds);
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

    private OrisunClient(ManagedChannel channel, int timeoutSeconds) {
        this.channel = channel;
        this.defaultTimeoutSeconds = timeoutSeconds;
        this.blockingStub = EventStoreGrpc.newBlockingStub(channel);
        this.asyncStub = EventStoreGrpc.newStub(channel);
    }

    // Synchronous methods
    public Eventstore.WriteResult saveEvents(final Eventstore.SaveEventsRequest request) throws OrisunException {
        try {
            return blockingStub
                    .withDeadlineAfter(defaultTimeoutSeconds, TimeUnit.SECONDS)
                    .saveEvents(request);
        } catch (StatusRuntimeException e) {
            throw new OrisunException("Failed to save events", e);
        }
    }

    public Eventstore.GetEventsResponse getEvents(Eventstore.GetEventsRequest request) throws OrisunException {
        try {
            return blockingStub
                    .withDeadlineAfter(defaultTimeoutSeconds, TimeUnit.SECONDS)
                    .getEvents(request);
        } catch (StatusRuntimeException e) {
            throw new OrisunException("Failed to get events", e);
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
        return new EventSubscription(asyncStub, request, handler, defaultTimeoutSeconds);
    }

    public PubSubSubscription subscribeToPubSub(Eventstore.SubscribeRequest request,
                                                PubSubSubscription.MessageHandler handler) {
        return new PubSubSubscription(asyncStub, request, handler, defaultTimeoutSeconds);
    }

    public void publishToPubSub(Eventstore.PublishRequest request) throws OrisunException {
        try {
            blockingStub
                    .withDeadlineAfter(defaultTimeoutSeconds, TimeUnit.SECONDS)
                    .publishToPubSub(request);
        } catch (StatusRuntimeException e) {
            throw new OrisunException("Failed to publish message", e);
        }
    }

    @Override
    public void close() {
        if (channel != null && !channel.isShutdown()) {
            try {
                channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
    }
}