package com.orisunlabs.orisun.client;

import com.orisun.eventstore.EventStoreGrpc;
import com.orisun.eventstore.Eventstore;
import com.orisun.eventstore.Eventstore.*;

import io.grpc.ServerBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcCleanupRule;
import org.junit.Rule;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.net.ServerSocket;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;

class OrisunClientTest {
    @Rule
    public final GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    private MockEventStoreService mockService;
    private OrisunClient client;

    @BeforeEach
    void setUp() throws Exception {
        // Choose a free port
        ServerSocket socket = new ServerSocket(8087);
        int port = socket.getLocalPort();
        socket.close();

        mockService = new MockEventStoreService();

        // Create and start the server on a port
        final var server = ServerBuilder.forPort(port)
                .addService(mockService)
                .build()
                .start();
        grpcCleanup.register(server);

        // Create the client using the port
        client = OrisunClient
                .newBuilder()
                .withServer("localhost", port)
                .build();
    }

    @Test
    void testSaveEvents() throws Exception {
        // Prepare test data
        String eventId = UUID.randomUUID().toString();
        Eventstore.SaveEventsRequest request = Eventstore.SaveEventsRequest.newBuilder()
                .setBoundary("users")
                .addEvents(Eventstore.EventToSave
                        .newBuilder()
                        .setEventId(eventId)
                        .setEventType("UserCreated")
                        .setData("{\"username\":\"test\"}")
                        .build())
                .setStream(
                        Eventstore.SaveStreamQuery
                                .newBuilder()
                                .setName("user-123")
                                .build())
                .build();

        // Configure mock response
        mockService.setNextWriteResult(WriteResult.newBuilder()
                .setLogPosition(Eventstore.Position.newBuilder()
                        .setCommitPosition(1)
                        .setPreparePosition(1)
                        .build())
                .build());

        // Execute test
        WriteResult result = client.saveEvents(request);

        // Verify results
        assertNotNull(result);
        assertEquals(1, result.getLogPosition().getCommitPosition());
        assertEquals(1, result.getLogPosition().getPreparePosition());
        assertEquals(request, mockService.getLastSaveEventsRequest());
    }

    @Test
    void testSubscribeToEvents() throws Exception {
        CountDownLatch eventLatch = new CountDownLatch(1);
        List<Event> receivedEvents = new ArrayList<>();

        // Prepare subscription request
        final var request = CatchUpSubscribeToEventStoreRequest.newBuilder()
                .setBoundary("users")
                .build();

        // Set up subscription
        try (final var subscription = client.subscribeToEvents(request,
                new EventSubscription.EventHandler() {
                    @Override
                    public void onEvent(Event event) {
                        receivedEvents.add(event);
                        eventLatch.countDown();
                    }

                    @Override
                    public void onError(Throwable error) {
                        fail("Unexpected error: " + error);
                    }

                    @Override
                    public void onCompleted() {
                        // Not expected in this test
                    }
                })) {

            // Simulate server sending an event
            mockService.sendEvent(
                    Event.newBuilder()
                            .setEventId(UUID.randomUUID().toString())
                            .setEventType("UserCreated")
                            .setData("{\"username\":\"test\"}")
                            .build()
            );

            // Wait for event to be received
            assertTrue(eventLatch.await(5, TimeUnit.SECONDS));
            assertEquals(1, receivedEvents.size());
            assertEquals("UserCreated", receivedEvents.get(0).getEventType());
        }
    }

    // Mock service implementation
    private static class MockEventStoreService extends EventStoreGrpc.EventStoreImplBase {
        private WriteResult nextWriteResult;
        private SaveEventsRequest lastSaveEventsRequest;
        private StreamObserver<Event> eventObserver;

        void setNextWriteResult(WriteResult result) {
            this.nextWriteResult = result;
        }

        Eventstore.SaveEventsRequest getLastSaveEventsRequest() {
            return lastSaveEventsRequest;
        }

        void sendEvent(Eventstore.Event event) {
            if (eventObserver != null) {
                eventObserver.onNext(event);
            }
        }

        @Override
        public void saveEvents(Eventstore.SaveEventsRequest request, StreamObserver<WriteResult> responseObserver) {
            lastSaveEventsRequest = request;
            responseObserver.onNext(nextWriteResult);
            responseObserver.onCompleted();
        }

        @Override
        public void catchUpSubscribeToEvents(Eventstore.CatchUpSubscribeToEventStoreRequest request,
                                             StreamObserver<Eventstore.Event> responseObserver) {
            this.eventObserver = responseObserver;
        }
    }
}
