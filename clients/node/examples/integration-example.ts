import { EventStoreClient, EventToSave } from '../src';

/**
 * Integration example showing how to use the Node.js client
 * with a running Orisun Event Store server
 */
async function integrationExample() {
  const client = new EventStoreClient({
    host: 'localhost',
    port: 5005,
    username: 'admin',
    password: 'changeit'
  });

  try {
    console.log('🔌 Connecting to Orisun Event Store...');
    
    // Test connection
    const isConnected = await client.healthCheck();
    if (!isConnected) {
      console.error('❌ Failed to connect to event store');
      console.log('Make sure the Orisun Event Store server is running on localhost:5005');
      return;
    }
    console.log('✅ Connected successfully!');

    const boundary = 'demo-tenant';
    const streamName = `order-${Date.now()}`;

    // Create some events for an order processing scenario
    const orderEvents: EventToSave[] = [
      {
        eventId: `order-created-${Date.now()}`,
        eventType: 'OrderCreated',
        data: {
          orderId: streamName,
          customerId: 'customer-123',
          items: [
            { productId: 'prod-1', quantity: 2, price: 29.99 },
            { productId: 'prod-2', quantity: 1, price: 49.99 }
          ],
          totalAmount: 109.97
        },
        metadata: {
          source: 'order-service',
          correlationId: `corr-${Date.now()}`,
          userId: 'user-456'
        }
      },
      {
        eventId: `payment-processed-${Date.now()}`,
        eventType: 'PaymentProcessed',
        data: {
          orderId: streamName,
          paymentId: `payment-${Date.now()}`,
          amount: 109.97,
          method: 'credit_card',
          status: 'completed'
        },
        metadata: {
          source: 'payment-service',
          correlationId: `corr-${Date.now()}`
        }
      },
      {
        eventId: `order-shipped-${Date.now()}`,
        eventType: 'OrderShipped',
        data: {
          orderId: streamName,
          trackingNumber: `TRK${Date.now()}`,
          carrier: 'FastShip',
          estimatedDelivery: new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString()
        },
        metadata: {
          source: 'shipping-service',
          correlationId: `corr-${Date.now()}`
        }
      }
    ];

    // Save events to the stream
    console.log(`📝 Saving ${orderEvents.length} events to stream: ${streamName}`);
    await client.saveEvents({
      boundary,
      stream: {
        name: streamName,
        expectedVersion: 0 // New stream
      },
      events: orderEvents
    });
    console.log('✅ Events saved successfully!');

    // Read events back from the stream
    console.log('📖 Reading events from stream...');
    const retrievedEvents = await client.getEvents({
      boundary,
      stream: {
        name: streamName
      },
      count: 10
    });
    
    console.log(`📋 Retrieved ${retrievedEvents.length} events:`);
    retrievedEvents.forEach((event, index) => {
      console.log(`\n  Event ${index + 1}:`);
      console.log(`    ID: ${event.eventId}`);
      console.log(`    Type: ${event.eventType}`);
      console.log(`    Version: ${event.version}`);
      console.log(`    Data:`, JSON.stringify(event.data, null, 6));
      console.log(`    Metadata:`, JSON.stringify(event.metadata, null, 6));
    });

    // Demonstrate subscription to the stream
    console.log('\n🔔 Setting up subscription to stream events...');
    const subscription = client.subscribeToEvents(
      {
        stream: streamName,
        subscriberName: 'integration-example',
        boundary,
        afterVersion: 0
      },
      (event) => {
        console.log(`\n📨 Received event via subscription:`);
        console.log(`    Type: ${event.eventType}`);
        console.log(`    Stream: ${event.streamId}`);
        console.log(`    Version: ${event.version}`);
        console.log(`    Data:`, JSON.stringify(event.data, null, 6));
      },
      (error) => {
        console.error('❌ Subscription error:', error);
      }
    );

    // Add one more event to trigger the subscription
    setTimeout(async () => {
      console.log('\n📝 Adding one more event to trigger subscription...');
      await client.saveEvents({
        boundary,
        stream: {
          name: streamName,
          expectedVersion: 3 // We already have 3 events
        },
        events: [{
          eventId: `order-delivered-${Date.now()}`,
          eventType: 'OrderDelivered',
          data: {
            orderId: streamName,
            deliveredAt: new Date().toISOString(),
            signedBy: 'John Doe'
          },
          metadata: {
            source: 'delivery-service',
            correlationId: `corr-${Date.now()}`
          }
        }]
      });
    }, 2000);

    // Let the subscription run for a few seconds
    setTimeout(() => {
      console.log('\n🔌 Closing subscription and connection...');
      subscription.cancel();
      client.close();
      console.log('✅ Integration example completed!');
    }, 5000);

  } catch (error) {
    console.error('❌ Error during integration example:', error);
    client.close();
  }
}

// Run the example if this file is executed directly
if (typeof require !== 'undefined' && require.main === module) {
  integrationExample().catch(console.error);
}

export { integrationExample };