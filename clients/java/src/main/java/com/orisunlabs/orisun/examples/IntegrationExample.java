package com.orisunlabs.orisun.examples;

import com.orisunlabs.orisun.client.OrisunClient;
import com.orisunlabs.orisun.client.DefaultLogger;
import com.orisunlabs.orisun.client.EventSubscription;
import com.orisun.eventstore.Eventstore;

import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/**
 * Integration example showing real-world usage patterns
 */
public class IntegrationExample {

    public static void main(String[] args) throws Exception {
        // Create client with production-like configuration

        try (OrisunClient client = OrisunClient.newBuilder()
                .withServer("localhost", 5005)
                .withBasicAuth("admin", "changeit")
                .withLogging(true)
                .withLogLevel(DefaultLogger.LogLevel.INFO)
                .withKeepAliveTime(30000)
                .withKeepAliveTimeout(10000)
                .withKeepAlivePermitWithoutCalls(true)
                .withTimeout(30)
                .build()) {
            System.out.println("🚀 Starting integration example...");

            // Test connection
            if (!client.healthCheck("orisun_test_1")) {
                System.err.println("❌ Failed to connect to event store");
                return;
            }
            System.out.println("✅ Connection established successfully!");

            String boundary = "orisun_test_1";
            String orderStreamName = "order-" + System.currentTimeMillis();

            // Demonstrate order processing workflow
            demonstrateOrderProcessing(client, boundary, orderStreamName);

            // Demonstrate event subscription and processing
            demonstrateEventProcessing(client, boundary);

        } finally {
            System.out.println("🏁 Integration example completed!");
        }
    }

    private static void demonstrateOrderProcessing(OrisunClient client, String boundary, String orderStreamName) throws Exception {
        System.out.println("\n📦 Demonstrating order processing workflow...");

        // Create order events
        Eventstore.SaveEventsRequest orderRequest = Eventstore.SaveEventsRequest.newBuilder()
                .setBoundary(boundary)
                .setQuery(Eventstore.SaveQuery.newBuilder()
                        .setExpectedPosition(Eventstore.Position.newBuilder()
                                .setCommitPosition(-1)
                                .setPreparePosition(-1))
                        .build())
                .addEvents(createOrderCreatedEvent())
                .addEvents(createPaymentProcessedEvent())
                .addEvents(createOrderShippedEvent())
                .build();

        // Save order events
        Eventstore.WriteResult result = client.saveEvents(orderRequest);
        System.out.println("✅ Order workflow saved at position: " + result.getLogPosition().getCommitPosition());

        // Read back the order events
        Eventstore.GetEventsRequest readRequest = Eventstore.GetEventsRequest.newBuilder()
                .setBoundary(boundary)
                .setCount(10)
                .build();

        Eventstore.GetEventsResponse response = client.getEvents(readRequest);
        System.out.println("📋 Retrieved " + response.getEventsCount() + " order events:");

        for (int i = 0; i < response.getEventsCount(); i++) {
            Eventstore.Event event = response.getEvents(i);
            System.out.println("  " + (i + 1) + ". " + event.getEventType() + ": " + event.getData());
        }
    }

    private static void demonstrateEventProcessing(OrisunClient client, String boundary) throws Exception {
        System.out.println("\n🔄 Demonstrating event processing with subscription...");

        CountDownLatch eventLatch = new CountDownLatch(5);

        // Subscribe to all events in the boundary
        final var subscribeRequest = Eventstore.CatchUpSubscribeToEventStoreRequest.newBuilder()
                .setBoundary(boundary)
                .setSubscriberName("integration-processor")
                .setAfterPosition(Eventstore.Position.newBuilder().setCommitPosition(-1).setPreparePosition(-1))
                .build();

        EventSubscription subscription = client.subscribeToEvents(subscribeRequest,
                new EventSubscription.EventHandler() {
                    @Override
                    public void onEvent(Eventstore.Event event) {
                        processEvent(event);
                        eventLatch.countDown();
                    }

                    @Override
                    public void onError(Throwable error) {
                        System.err.println("❌ Event processing error: " + error.getMessage());
                        eventLatch.countDown();
                    }

                    @Override
                    public void onCompleted() {
                        System.out.println("🔚 Event subscription completed");
                    }
                });

        // Add a new event to trigger the subscription
        Thread.sleep(1000);
        addNewOrderEvent(client, boundary);

        // Wait for events or timeout
        boolean received = eventLatch.await(10, TimeUnit.SECONDS);
        if (received) {
            System.out.println("✅ Event processing completed successfully");
        } else {
            System.out.println("⏰ Event processing timed out");
        }

        subscription.close();
    }

    private static void processEvent(Eventstore.Event event) {
        System.out.println("📨 Processing event: " + event.getEventType());

        try {
            switch (event.getEventType()) {
                case "OrderCreated":
                    System.out.println("  → Processing new order");
                    break;
                case "PaymentProcessed":
                    System.out.println("  → Processing payment");
                    break;
                case "OrderShipped":
                    System.out.println("  → Processing shipment");
                    break;
                case "OrderDelivered":
                    System.out.println("  → Processing delivery");
                    break;
                default:
                    System.out.println("  → Unknown event type: " + event.getEventType());
            }
        } catch (Exception e) {
            System.err.println("  ❌ Error processing event: " + e.getMessage());
        }
    }

    private static void addNewOrderEvent(OrisunClient client, String boundary) throws Exception {
        String streamName = "order-" + System.currentTimeMillis();

        Eventstore.SaveEventsRequest request = Eventstore.SaveEventsRequest.newBuilder()
                .setBoundary(boundary)
                .setQuery(Eventstore.SaveQuery.newBuilder()
                        .setExpectedPosition(Eventstore.Position.newBuilder()
                                .setCommitPosition(-1)
                                .setPreparePosition(-1))
                        .build())
                .addEvents(createOrderDeliveredEvent())
                .build();

        client.saveEvents(request);
    }

    private static Eventstore.EventToSave createOrderCreatedEvent() {
        return Eventstore.EventToSave.newBuilder()
                .setEventId(UUID.randomUUID().toString())
                .setEventType("OrderCreated")
                .setData("{\"orderId\":\"order-123\",\"customerId\":\"customer-456\",\"items\":[{\"productId\":\"prod-1\",\"quantity\":2,\"price\":29.99}],\"totalAmount\":59.98}")
                .setMetadata("{\"source\":\"order-service\",\"correlationId\":\"corr-" + System.currentTimeMillis() + "\"}")
                .build();
    }

    private static Eventstore.EventToSave createPaymentProcessedEvent() {
        return Eventstore.EventToSave.newBuilder()
                .setEventId(UUID.randomUUID().toString())
                .setEventType("PaymentProcessed")
                .setData("{\"orderId\":\"order-123\",\"paymentId\":\"payment-789\",\"amount\":59.98,\"method\":\"credit_card\",\"status\":\"completed\"}")
                .setMetadata("{\"source\":\"payment-service\",\"correlationId\":\"corr-" + System.currentTimeMillis() + "\"}")
                .build();
    }

    private static Eventstore.EventToSave createOrderShippedEvent() {
        return Eventstore.EventToSave.newBuilder()
                .setEventId(UUID.randomUUID().toString())
                .setEventType("OrderShipped")
                .setData("{\"orderId\":\"order-123\",\"trackingNumber\":\"TRK123456\",\"carrier\":\"FastShip\",\"estimatedDelivery\":\"2024-01-30T00:00:00Z\"}")
                .setMetadata("{\"source\":\"shipping-service\",\"correlationId\":\"corr-" + System.currentTimeMillis() + "\"}")
                .build();
    }

    private static Eventstore.EventToSave createOrderDeliveredEvent() {
        return Eventstore.EventToSave.newBuilder()
                .setEventId(UUID.randomUUID().toString())
                .setEventType("OrderDelivered")
                .setData("{\"orderId\":\"order-123\",\"deliveredAt\":\"2024-01-29T15:30:00Z\",\"signedBy\":\"John Doe\"}")
                .setMetadata("{\"source\":\"delivery-service\",\"correlationId\":\"corr-" + System.currentTimeMillis() + "\"}")
                .build();
    }
}