package com.orisunlabs.orisun.examples;

import com.orisunlabs.orisun.client.OrisunClient;
import com.orisunlabs.orisun.client.DefaultLogger;
import com.orisunlabs.orisun.client.EventSubscription;
import com.orisunlabs.orisun.client.OptimisticConcurrencyException;
import com.orisun.eventstore.Eventstore;

import java.util.UUID;

/**
 * Basic usage example for Orisun Java Client
 */
public class BasicUsageExample {

    public static void main(String[] args) throws Exception {
        // Create client with comprehensive configuration

        try (final var client = OrisunClient.newBuilder()
                .withServer("localhost", 5005)
                .withBasicAuth("admin", "changeit")
                .withLogging(true)
                .withLogLevel(DefaultLogger.LogLevel.INFO)
                .withKeepAliveTime(30000)
                .withKeepAliveTimeout(10000)
                .withTimeout(30)
                .build()) {
            System.out.println("🔌 Connecting to Orisun Event Store...");

            // Perform health check
            boolean isHealthy = client.healthCheck("orisun_test_1");
            if (!isHealthy) {
                System.err.println("❌ Failed to connect to event store");
                System.out.println("Make sure Orisun Event Store server is running on localhost:5005");
                return;
            }
            System.out.println("✅ Connected successfully!");

            final var boundary = "orisun_test_1";
            final var streamName = "user-" + System.currentTimeMillis();

            // Create and save events
            saveEventsExample(client, boundary, streamName);

            // Read events back
            readEventsExample(client, boundary);

            // Subscribe to events
            subscribeToEventsExample(client, boundary, streamName);
            System.out.println("✅ Example completed!");

        }
    }

    private static void saveEventsExample(OrisunClient client, String boundary, String streamName) throws Exception {
        System.out.println("📝 Saving events to stream: " + streamName);

        Eventstore.SaveEventsRequest request = Eventstore.SaveEventsRequest.newBuilder()
                .setBoundary(boundary)
                .setQuery(Eventstore.SaveQuery.newBuilder()
                        .setExpectedPosition(Eventstore.Position.newBuilder()
                                .setCommitPosition(-1)
                                .setPreparePosition(-1))
                        .build())
                .addEvents(Eventstore.EventToSave.newBuilder()
                        .setEventId(UUID.randomUUID().toString())
                        .setEventType("UserCreated")
                        .setData("{\"userId\":\"user-123\",\"email\":\"john.doe@example.com\",\"name\":\"John Doe\"}")
                        .setMetadata("{\"source\":\"user-service\",\"version\":\"1.0\"}")
                        .build())
                .addEvents(Eventstore.EventToSave.newBuilder()
                        .setEventId(UUID.randomUUID().toString())
                        .setEventType("UserEmailUpdated")
                        .setData("{\"userId\":\"user-123\",\"oldEmail\":\"john.doe@example.com\",\"newEmail\":\"john.doe@newdomain.com\"}")
                        .setMetadata("{\"source\":\"user-service\",\"version\":\"1.0\"}")
                        .build())
                .build();

        try {
            Eventstore.WriteResult result = client.saveEvents(request);
            System.out.println("✅ Events saved successfully!");
            System.out.println("📍 Log position: " + result.getLogPosition().getCommitPosition());
        } catch (OptimisticConcurrencyException e) {
            System.err.println("⚠️ Concurrency conflict detected:");
            System.err.println("   Expected version: " + e.getExpectedVersion());
            System.err.println("   Actual version: " + e.getActualVersion());
        }
    }

    private static void readEventsExample(OrisunClient client, String boundary) throws Exception {
        System.out.println("📖 Reading events");

        Eventstore.GetEventsRequest request = Eventstore.GetEventsRequest.newBuilder()
                .setBoundary(boundary)
                .setCount(10)
                .build();

        Eventstore.GetEventsResponse response = client.getEvents(request);

        System.out.println("📋 Retrieved " + response.getEventsCount() + " events:");
        for (int i = 0; i < response.getEventsCount(); i++) {
            Eventstore.Event event = response.getEvents(i);
            System.out.println("    Event " + (i + 1) + ":");
            System.out.println("    ID: " + event.getEventId());
            System.out.println("    Type: " + event.getEventType());
            System.out.println("    Data: " + event.getData());
            System.out.println("    Created: " + event.getDateCreated());
        }
    }

    private static void subscribeToEventsExample(OrisunClient client, String boundary, String streamName) throws Exception {
        System.out.println("🔔 Setting up subscription to events...");

        Eventstore.CatchUpSubscribeToEventStoreRequest request =
                Eventstore.CatchUpSubscribeToEventStoreRequest.newBuilder()
                        .setBoundary(boundary)
                        .setSubscriberName("basic-usage-example")
                        .build();

        EventSubscription subscription = client.subscribeToEvents(request,
                new EventSubscription.EventHandler() {
                    @Override
                    public void onEvent(Eventstore.Event event) {
                        System.out.println("📨 Received event via subscription:");
                        System.out.println("    Type: " + event.getEventType());
                        System.out.println("    Data: " + event.getData());
                    }

                    @Override
                    public void onError(Throwable error) {
                        System.err.println("❌ Subscription error: " + error.getMessage());
                    }

                    @Override
                    public void onCompleted() {
                        System.out.println("🔚 Subscription completed");
                    }
                });

        // Let subscription run for a few seconds
        Thread.sleep(3000);

        System.out.println("🔌 Closing subscription...");
        subscription.close();
    }
}