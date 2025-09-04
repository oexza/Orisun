# Orisun Event Store - Node.js Client

A Node.js client library for interacting with the Orisun Event Store, providing a simple and intuitive API for event sourcing operations.

## Features

- **Event Storage**: Save events to streams with optimistic concurrency control
- **Event Retrieval**: Read events from streams with version filtering
- **Event Subscriptions**: Subscribe to real-time event streams
- **Multi-tenancy**: Support for boundary-based tenant isolation
- **TypeScript Support**: Full TypeScript definitions included
- **gRPC Communication**: High-performance gRPC protocol
- **Authentication**: Built-in support for basic authentication
- **Load Balancing**: Support for multiple hosts and DNS-based load balancing
- **Resilience**: Configurable retry policies for handling transient failures
- **Logging**: Configurable logging for debugging and monitoring

## Installation

```bash
npm install orisun-eventstore-client
```

## Quick Start

```typescript
import { EventStoreClient } from 'orisun-eventstore-client';

// Create a client
const client = new EventStoreClient({
  // Connection options (choose one approach)
  
  // Option 1: Single server
  host: 'localhost',
  port: 5005,
  
  // Option 2: Multiple hosts for load balancing
  // host: 'eventstore1.example.com,eventstore2.example.com,eventstore3.example.com',
  // port: 5005,
  
  // Option 3: DNS-based load balancing
  // target: 'dns:///eventstore.example.com:5005',
  
  // Authentication
  username: 'admin',
  password: 'changeit',
  
  // Load balancing configuration
  loadBalancingPolicy: 'round_robin', // or 'pick_first'
  
  // Retry configuration for resilience
  enableRetries: true, // Enable automatic retries for failed requests
  retryPolicy: {
    maxAttempts: 5, // Maximum number of retry attempts
    initialBackoff: '0.1s', // Initial backoff time
    maxBackoff: '10s', // Maximum backoff time
    backoffMultiplier: 2, // Backoff multiplier for exponential backoff
    retryableStatusCodes: ['UNAVAILABLE'] // Status codes that trigger retries
  },
  

  
  // Logging configuration
  enableLogging: true, // Enable or disable logging
  logger: console, // Custom logger (must implement debug, info, warn, error methods)
  

  
  // Keep-alive options for long-lived connections
  keepaliveTimeMs: 30000, // Send ping every 30 seconds
  keepaliveTimeoutMs: 10000, // Wait 10 seconds for ping response
  keepalivePermitWithoutCalls: true // Allow pings when idle
});

// Save events
await client.saveEvents({
  streamId: 'user-123',
  expectedVersion: 0,
  events: [{
    id: 'event-1',
    type: 'UserCreated',
    data: { userId: 'user-123', email: 'john@example.com' }
  }],
  boundary: 'tenant-1'
});

// Read events
const events = await client.getEvents({
  streamId: 'user-123',
  boundary: 'tenant-1'
});

// Subscribe to events
const subscription = client.subscribeToEvents(
  { streamId: 'user-123', boundary: 'tenant-1' },
  (event) => console.log('Received:', event)
);
```

## API Reference

### EventStoreClient

#### Constructor

```typescript
new EventStoreClient(options?: EventStoreClientOptions)
```

**Options:**
- `host` (string): Server hostname or comma-separated list of hosts for load balancing (default: 'localhost')
- `port` (number): Server port (default: 5005)
- `target` (string): Alternative to host:port. A fully qualified gRPC target string (e.g., 'dns:///eventstore.example.com:5005')
- `credentials` (grpc.ChannelCredentials): gRPC credentials (default: insecure)
- `username` (string): Authentication username (default: 'admin')
- `password` (string): Authentication password (default: 'changeit')
- `loadBalancingPolicy` (string): Load balancing strategy - 'round_robin' or 'pick_first' (default: 'round_robin')
- `enableRetries` (boolean): Enable automatic retries for failed requests (default: true)
- `retryPolicy` (object): Configuration for retry behavior
  - `maxAttempts` (number): Maximum number of retry attempts (default: 5)
  - `initialBackoff` (string): Initial backoff time (default: '0.1s')
  - `maxBackoff` (string): Maximum backoff time (default: '10s')
  - `backoffMultiplier` (number): Backoff multiplier for exponential backoff (default: 2)
  - `retryableStatusCodes` (string[]): Status codes that trigger retries (default: ['UNAVAILABLE'])

- `enableLogging` (boolean): Enable or disable logging (default: true)
- `logger` (object): Custom logger implementation (default: console)
  - Must implement `debug`, `info`, `warn`, and `error` methods

- `keepaliveTimeMs` (number): Time in milliseconds between keep-alive pings (default: 30000)
- `keepaliveTimeoutMs` (number): Time in milliseconds to wait for ping response (default: 10000)
- `keepalivePermitWithoutCalls` (boolean): Allow keep-alive pings when there are no active calls (default: true)

#### Methods

##### saveEvents(request: SaveEventsRequest): Promise<void>

Save events to a stream with optimistic concurrency control.

```typescript
await client.saveEvents({
  streamId: 'order-456',
  expectedVersion: 2, // Expected current version of the stream
  events: [
    {
      id: 'event-3',
      type: 'OrderShipped',
      data: { orderId: 'order-456', trackingNumber: 'TRK123' },
      metadata: { source: 'shipping-service' }
    }
  ],
  boundary: 'tenant-1'
});
```

##### getEvents(request: GetEventsRequest): Promise<Event[]>

Retrieve events from a stream.

```typescript
// Get all events
const allEvents = await client.getEvents({
  streamId: 'order-456',
  boundary: 'tenant-1'
});

// Get events from specific version range
const recentEvents = await client.getEvents({
  streamId: 'order-456',
  fromVersion: 5,
  toVersion: 10,
  boundary: 'tenant-1'
});
```

##### subscribeToEvents(request: SubscribeRequest, onEvent: (event: Event) => void, onError?: (error: Error) => void): grpc.ClientReadableStream

Subscribe to real-time events from a stream or all streams.

```typescript
// Subscribe to specific stream
const subscription = client.subscribeToEvents(
  {
    streamId: 'user-123',
    boundary: 'tenant-1'
  },
  (event) => {
    console.log('New event:', event.type, event.data);
  },
  (error) => {
    console.error('Subscription error:', error);
  }
);

// Subscribe to all streams from a specific position
const allStreamsSubscription = client.subscribeToEvents(
  {
    fromPosition: 1000,
    boundary: 'tenant-1'
  },
  (event) => {
    console.log('Event from any stream:', event);
  }
);

// Cancel subscription
subscription.cancel();
```

##### healthCheck(): Promise<boolean>

Check if the client can connect to the event store.

```typescript
const isHealthy = await client.healthCheck();
if (!isHealthy) {
  console.error('Event store is not accessible');
}
```

##### close(): void

Close the client connection.

```typescript
client.close();
```

### Types

#### Event

```typescript
interface Event {
  id: string;
  type: string;
  data: any;
  metadata?: Record<string, string>;
  streamId: string;
  streamVersion: number;
  position: number;
  timestamp: string;
}
```

#### SaveEventsRequest

```typescript
interface SaveEventsRequest {
  streamId: string;
  expectedVersion: number;
  events: Omit<Event, 'streamId' | 'streamVersion' | 'position' | 'timestamp'>[];
  boundary: string;
}
```

#### GetEventsRequest

```typescript
interface GetEventsRequest {
  streamId: string;
  fromVersion?: number;
  toVersion?: number;
  boundary: string;
}
```

#### SubscribeRequest

```typescript
interface SubscribeRequest {
  streamId?: string;
  fromPosition?: number;
  boundary: string;
}
```

## Examples

### Basic Event Sourcing

```typescript
import { EventStoreClient } from 'orisun-eventstore-client';

class UserAggregate {
  private client: EventStoreClient;
  private streamId: string;
  private version: number = 0;
  private boundary: string;

  constructor(userId: string, boundary: string) {
    this.client = new EventStoreClient();
    this.streamId = `user-${userId}`;
    this.boundary = boundary;
  }

  async createUser(email: string, name: string) {
    await this.client.saveEvents({
      streamId: this.streamId,
      expectedVersion: this.version,
      events: [{
        id: `user-created-${Date.now()}`,
        type: 'UserCreated',
        data: { email, name }
      }],
      boundary: this.boundary
    });
    this.version++;
  }

  async updateEmail(newEmail: string) {
    await this.client.saveEvents({
      streamId: this.streamId,
      expectedVersion: this.version,
      events: [{
        id: `email-updated-${Date.now()}`,
        type: 'EmailUpdated',
        data: { newEmail }
      }],
      boundary: this.boundary
    });
    this.version++;
  }

  async loadFromHistory() {
    const events = await this.client.getEvents({
      streamId: this.streamId,
      boundary: this.boundary
    });
    
    // Apply events to rebuild state
    events.forEach(event => {
      switch (event.type) {
        case 'UserCreated':
          // Apply user creation logic
          break;
        case 'EmailUpdated':
          // Apply email update logic
          break;
      }
    });
    
    this.version = events.length;
  }
}
```

### Event Processing with Subscriptions

```typescript
import { EventStoreClient, Event } from 'orisun-eventstore-client';

class EventProcessor {
  private client: EventStoreClient;

  constructor() {
    this.client = new EventStoreClient();
  }

  startProcessing(boundary: string) {
    // Subscribe to all events in the boundary
    this.client.subscribeToEvents(
      { boundary },
      this.handleEvent.bind(this),
      this.handleError.bind(this)
    );
  }

  private async handleEvent(event: Event) {
    console.log(`Processing event: ${event.type}`);
    
    switch (event.type) {
      case 'UserCreated':
        await this.sendWelcomeEmail(event.data);
        break;
      case 'OrderPlaced':
        await this.processPayment(event.data);
        break;
      default:
        console.log(`Unknown event type: ${event.type}`);
    }
  }

  private handleError(error: Error) {
    console.error('Event processing error:', error);
    // Implement retry logic or error handling
  }

  private async sendWelcomeEmail(userData: any) {
    // Email sending logic
    console.log(`Sending welcome email to ${userData.email}`);
  }

  private async processPayment(orderData: any) {
    // Payment processing logic
    console.log(`Processing payment for order ${orderData.orderId}`);
  }
}
```

## Error Handling

The client throws standard JavaScript errors for various failure scenarios:

```typescript
try {
  await client.saveEvents({
    streamId: 'user-123',
    expectedVersion: 5, // Wrong expected version
    events: [/* events */],
    boundary: 'tenant-1'
  });
} catch (error) {
  if (error.message.includes('version')) {
    console.error('Concurrency conflict - stream was modified');
    // Handle optimistic concurrency failure
  } else {
    console.error('Unexpected error:', error);
  }
}
```

## Development

### Building

```bash
npm run build
```

### Running Examples

```bash
npm run example
```

### Testing

```bash
npm test
```

### Generating Protocol Buffers

If you need to regenerate the protocol buffer definitions:

```bash
npm run generate-proto
```

## License

MIT