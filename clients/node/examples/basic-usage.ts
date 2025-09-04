import { EventStoreClient, Event } from '../src';

async function basicUsageExample() {
  // Create a client instance
  const client = new EventStoreClient({
    host: 'localhost',
    port: 5005,
    username: 'admin',
    password: 'changeit'
  });

  try {
    console.log('Connecting to Orisun Event Store...');
    
    // Test connection
    const isConnected = await client.healthCheck();
    if (!isConnected) {
      console.error('Failed to connect to event store');
      return;
    }
    console.log('Connected successfully!');

    // Define some sample events
    const events = [
      {
        eventId: 'event-1',
        eventType: 'UserCreated',
        data: {
          userId: 'user-123',
          email: 'john.doe@example.com',
          name: 'John Doe'
        },
        metadata: {
          source: 'user-service',
          version: '1.0'
        }
      },
      {
        eventId: 'event-2',
        eventType: 'UserEmailUpdated',
        data: {
          userId: 'user-123',
          oldEmail: 'john.doe@example.com',
          newEmail: 'john.doe@newdomain.com'
        },
        metadata: {
          source: 'user-service',
          version: '1.0'
        }
      }
    ];

    // Save events to a stream
    console.log('Saving events to stream...');
    await client.saveEvents({
      boundary: 'tenant-1',
      stream: {
        name: 'user-123',
        expectedVersion: 0 // Expecting stream to be new
      },
      events: events
    });
    console.log('Events saved successfully!');

    // Read events from the stream
    console.log('Reading events from stream...');
    const retrievedEvents = await client.getEvents({
      boundary: 'tenant-1',
      stream: {
        name: 'user-123'
      }
    });
    
    console.log(`Retrieved ${retrievedEvents.length} events:`);
    retrievedEvents.forEach((event, index) => {
      console.log(`Event ${index + 1}:`, {
        eventId: event.eventId,
        eventType: event.eventType,
        data: event.data,
        version: event.version,
        dateCreated: event.dateCreated
      });
    });

    // Subscribe to events (this will run indefinitely)
    console.log('\nSubscribing to events...');
    const subscription = client.subscribeToEvents(
      {
        stream: 'user-123',
        subscriberName: 'example-subscriber',
        boundary: 'tenant-1'
      },
      (event: Event) => {
        console.log('Received event:', {
          eventId: event.eventId,
          eventType: event.eventType,
          data: event.data,
          version: event.version
        });
      },
      (error: Error) => {
        console.error('Subscription error:', error);
      }
    );

    // Let the subscription run for a few seconds
    setTimeout(() => {
      console.log('Closing subscription...');
      subscription.cancel();
      client.close();
      console.log('Example completed!');
    }, 5000);

  } catch (error) {
    console.error('Error:', error);
    client.close();
  }
}

// Run the example
if (require.main === module) {
  basicUsageExample().catch(console.error);
}

export { basicUsageExample };