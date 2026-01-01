package com.orisunlabs.orisun.client;

import com.orisun.eventstore.EventStoreGrpc;
import com.orisun.eventstore.Eventstore;
import io.grpc.stub.StreamObserver;
import io.grpc.ClientInterceptor;
import io.grpc.ForwardingClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Channel;

import java.util.Objects;
import java.util.concurrent.TimeUnit;

public class EventSubscription implements AutoCloseable {
    private final StreamObserver<Eventstore.Event> observer;
    private volatile boolean closed = false;
    private final Logger logger;

    public interface EventHandler {
        void onEvent(Eventstore.Event event);

        void onError(Throwable error);

        void onCompleted();
    }

    EventSubscription(EventStoreGrpc.EventStoreStub stub,
                      Eventstore.CatchUpSubscribeToEventStoreRequest request,
                      EventHandler handler,
                      int timeoutSeconds,
                      Logger logger,
                      TokenCache tokenCache,
                      String username,
                      String password) {
        this.logger = logger != null ? logger : new DefaultLogger(DefaultLogger.LogLevel.WARN);

        // Create metadata with authentication
        Metadata metadata = tokenCache != null ? tokenCache.createAuthMetadata(
                username != null && password != null ?
                        () -> "Basic " + java.util.Base64.getEncoder().encodeToString((username + ":" + password).getBytes()) : null
        ) : new Metadata();

        this.observer = new StreamObserver<>() {
            @Override
            public void onNext(Eventstore.Event event) {
                if (!closed) {
                    logger.debug("Received event: {}", event.getEventType());
                    handler.onEvent(event);
                }
            }

            @Override
            public void onError(Throwable t) {
                if (!closed) {
                    logger.error("Subscription error: {}", t.getMessage(), t);
                    handler.onError(t);
                }
            }

            @Override
            public void onCompleted() {
                if (!closed) {
                    logger.debug("Subscription completed");
                    handler.onCompleted();
                }
            }
        };

        EventStoreGrpc.EventStoreStub enhancedStub = stub.withInterceptors(new ClientInterceptor() {
            @Override
            public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
                    MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {

                return new ForwardingClientCall.SimpleForwardingClientCall<>(
                        next.newCall(method, callOptions)) {

                    @Override
                    public void start(Listener<RespT> responseListener, Metadata headers) {
                        // Copy metadata from our prepared metadata
                        metadata.keys().forEach(key -> {
                            for (String value : Objects.requireNonNull(metadata.getAll(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER)))) {
                                headers.put(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER), value);
                            }
                        });
                        super.start(responseListener, headers);
                    }
                };
            }
        });

        enhancedStub
                .withDeadlineAfter(timeoutSeconds, TimeUnit.SECONDS)
                .catchUpSubscribeToEvents(request, observer);
    }

    // Constructor for stream subscription
    EventSubscription(EventStoreGrpc.EventStoreStub stub,
                      EventHandler handler,
                      int timeoutSeconds,
                      Logger logger,
                      TokenCache tokenCache,
                      String username,
                      String password) {
        this.logger = logger != null ? logger : new DefaultLogger(DefaultLogger.LogLevel.WARN);

        // Create metadata with authentication
        Metadata metadata = tokenCache != null ? tokenCache.createAuthMetadata(
                username != null && password != null ?
                        () -> "Basic " + java.util.Base64.getEncoder().encodeToString((username + ":" + password).getBytes()) : null
        ) : new Metadata();

        this.observer = new StreamObserver<>() {
            @Override
            public void onNext(Eventstore.Event event) {
                if (!closed) {
                    logger.debug("Received event: {}", event.getEventType());
                    handler.onEvent(event);
                }
            }

            @Override
            public void onError(Throwable t) {
                if (!closed) {
                    logger.error("Subscription error: {}", t.getMessage(), t);
                    handler.onError(t);
                }
            }

            @Override
            public void onCompleted() {
                if (!closed) {
                    logger.debug("Subscription completed");
                    handler.onCompleted();
                }
            }
        };

        EventStoreGrpc.EventStoreStub enhancedStub = stub.withInterceptors(new ClientInterceptor() {
            @Override
            public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
                    MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {

                return new ForwardingClientCall.SimpleForwardingClientCall<>(
                        next.newCall(method, callOptions)) {

                    @Override
                    public void start(Listener<RespT> responseListener, Metadata headers) {
                        // Copy metadata from our prepared metadata
                        metadata.keys().forEach(key -> {
                            for (String value : Objects.requireNonNull(metadata.getAll(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER)))) {
                                headers.put(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER), value);
                            }
                        });
                        super.start(responseListener, headers);
                    }
                };
            }
        });

        enhancedStub
                .withDeadlineAfter(timeoutSeconds, TimeUnit.SECONDS);
    }

    @Override
    public void close() {
        if (!closed) {
            closed = true;
            logger.debug("Closing subscription");
            observer.onCompleted();
        }
    }
}
