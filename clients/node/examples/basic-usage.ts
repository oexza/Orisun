import { EventStoreClient, Event } from '../src';

async function basicUsageExample() {
  // Create a client instance with keep-alive options and load balancing
  const client = new EventStoreClient({
    // Option 1: Single host and port
    host: 'localhost',
    port: 5005,
    
    // Option 2: Multiple hosts for load balancing
    // host: 'eventstore1.example.com,eventstore2.example.com,eventstore3.example.com',
    // port: 5005,
    
    // Option 3: DNS-based load balancing
    // target: 'dns:///eventstore.example.com:5005',
    
    username: 'admin',
    password: 'changeit',
    
    // Load balancing policy
    loadBalancingPolicy: 'round_robin', // or 'pick_first'
    
    // Retry configuration
    enableRetries: true, // Enable automatic retries for failed requests
    retryPolicy: {
      maxAttempts: 5, // Maximum number of retry attempts
      initialBackoff: '0.1s', // Initial backoff time
      maxBackoff: '10s', // Maximum backoff time
      backoffMultiplier: 2, // Backoff multiplier for exponential backoff
      retryableStatusCodes: ['UNAVAILABLE'] // Status codes that trigger retries
    },
    
    // Logging configuration
    enableLogging: true, // Enable logging (set to false in production if you want to minimize output)
    logger: console, // Use the default console logger (you can provide a custom logger)
    

    
    // Keep-alive options for maintaining long-lived connections
    keepaliveTimeMs: 30000, // Send keep-alive ping every 30 seconds
    keepaliveTimeoutMs: 10000, // Wait 10 seconds for ping response before considering connection dead
    keepalivePermitWithoutCalls: true // Allow keep-alive pings even when there are no active calls
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