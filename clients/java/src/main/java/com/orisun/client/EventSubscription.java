package io.orisun.client;

import com.orisun.eventstore.EventStoreGrpc;
import com.orisun.eventstore.Eventstore;
import io.grpc.stub.StreamObserver;

import java.util.concurrent.TimeUnit;

public class EventSubscription implements AutoCloseable {
    private final StreamObserver<Eventstore.Event> observer;
    private volatile boolean closed = false;

    public interface EventHandler {
        void onEvent(Eventstore.Event event);

        void onError(Throwable error);

        void onCompleted();
    }

    EventSubscription(EventStoreGrpc.EventStoreStub stub,
                      Eventstore.CatchUpSubscribeToEventStoreRequest request,
                      EventHandler handler,
                      int timeoutSeconds) {
        this.observer = new StreamObserver<>() {
            @Override
            public void onNext(Eventstore.Event event) {
                if (!closed) {
                    handler.onEvent(event);
                }
            }

            @Override
            public void onError(Throwable t) {
                if (!closed) {
                    handler.onError(t);
                }
            }

            @Override
            public void onCompleted() {
                if (!closed) {
                    handler.onCompleted();
                }
            }
        };
        
        stub
                .withDeadlineAfter(timeoutSeconds, TimeUnit.SECONDS)
                .catchUpSubscribeToEvents(request, observer);
    }

    @Override
    public void close() {
        closed = true;
        observer.onCompleted();
    }
}
