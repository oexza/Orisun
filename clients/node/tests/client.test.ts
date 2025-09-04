import { EventStoreClient, Event, EventToSave, Position } from '../src';
import * as grpc from '@grpc/grpc-js';

// Mock gRPC and protobuf modules
const mockSaveEvents = jest.fn();
const mockGetEvents = jest.fn();
const mockCatchUpSubscribeToEvents = jest.fn();
const mockCatchUpSubscribeToStream = jest.fn();

const mockEventStoreClient = {
  saveEvents: mockSaveEvents,
  getEvents: mockGetEvents,
  catchUpSubscribeToEvents: mockCatchUpSubscribeToEvents,
  catchUpSubscribeToStream: mockCatchUpSubscribeToStream,
};

const mockClient = jest.fn().mockImplementation(() => mockEventStoreClient);

jest.mock('@grpc/grpc-js', () => ({
  credentials: {
    createInsecure: jest.fn(() => 'mock-credentials')
  },
  loadPackageDefinition: jest.fn(() => ({
    eventstore: {
      EventStore: mockClient
    }
  })),
  Metadata: jest.fn().mockImplementation(() => ({
    add: jest.fn()
  }))
}));

jest.mock('@grpc/proto-loader', () => ({
  loadSync: jest.fn(() => 'mock-package-definition')
}));

// Setup mock implementations
beforeEach(() => {
  mockSaveEvents.mockImplementation((request, metadata, callback) => {
    callback(null, {});
  });
  
  mockGetEvents.mockImplementation((request, metadata, callback) => {
    callback(null, {
      events: [
        {
          event_id: 'test-event-1',
          event_type: 'TestEvent',
          data: JSON.stringify({ test: 'data' }),
          metadata: JSON.stringify({ source: 'test' }),
          stream_id: 'test-stream',
          version: 1,
          position: { commitPosition: 1, preparePosition: 1 },
          date_created: '2024-01-01T00:00:00Z'
        }
      ]
    });
  });
  
  mockCatchUpSubscribeToEvents.mockReturnValue({
    on: jest.fn(),
    cancel: jest.fn()
  });
  
  mockCatchUpSubscribeToStream.mockReturnValue({
    on: jest.fn(),
    cancel: jest.fn()
  });
});

describe('EventStoreClient', () => {
  let client: EventStoreClient;

  beforeEach(() => {
    client = new EventStoreClient({
      host: 'localhost',
      port: 5005,
      username: 'test',
      password: 'test'
    });
  });

  afterEach(() => {
    client.close();
  });

  describe('constructor', () => {
    it('should create client with default options', () => {
      const defaultClient = new EventStoreClient({ host: 'localhost', port: 5005 });
      expect(defaultClient).toBeInstanceOf(EventStoreClient);
      defaultClient.close();
    });

    it('should create client with custom options', () => {
      expect(client).toBeInstanceOf(EventStoreClient);
    });
    
    it('should create client with keep-alive options', () => {
      const clientWithKeepalive = new EventStoreClient({
        host: 'localhost',
        port: 5005,
        username: 'test',
        password: 'test',
        keepaliveTimeMs: 60000,
        keepaliveTimeoutMs: 20000,
        keepalivePermitWithoutCalls: false
      });
      expect(clientWithKeepalive).toBeInstanceOf(EventStoreClient);
      clientWithKeepalive.close();
    });
    
    it('should create client with load balancing options', () => {
      const clientWithLoadBalancing = new EventStoreClient({
        host: 'localhost',
        port: 5005,
        username: 'test',
        password: 'test',
        loadBalancingPolicy: 'round_robin'
      });
      expect(clientWithLoadBalancing).toBeInstanceOf(EventStoreClient);
      clientWithLoadBalancing.close();
    });
    
    it('should create client with comma-separated hosts for load balancing', () => {
      const clientWithMultipleHosts = new EventStoreClient({
        host: 'host1.example.com,host2.example.com,host3.example.com',
        port: 5005,
        username: 'test',
        password: 'test'
      });
      expect(clientWithMultipleHosts).toBeInstanceOf(EventStoreClient);
      clientWithMultipleHosts.close();
    });
    
    it('should create client with target string for DNS-based load balancing', () => {
      const clientWithTarget = new EventStoreClient({
        target: 'dns:///eventstore.example.com:5005',
        username: 'test',
        password: 'test'
      });
      expect(clientWithTarget).toBeInstanceOf(EventStoreClient);
      clientWithTarget.close();
    });
    
    it('should create client with custom retry policy', () => {
      const clientWithRetryPolicy = new EventStoreClient({
        host: 'localhost',
        port: 5005,
        username: 'test',
        password: 'test',
        enableRetries: true,
        retryPolicy: {
          maxAttempts: 10,
          initialBackoff: '0.5s',
          maxBackoff: '30s',
          backoffMultiplier: 1.5,
          retryableStatusCodes: ['UNAVAILABLE', 'RESOURCE_EXHAUSTED']
        }
      });
      expect(clientWithRetryPolicy).toBeInstanceOf(EventStoreClient);
      clientWithRetryPolicy.close();
    });
    
    it('should create client with retries disabled', () => {
      const clientWithoutRetries = new EventStoreClient({
        host: 'localhost',
        port: 5005,
        username: 'test',
        password: 'test',
        enableRetries: false
      });
      expect(clientWithoutRetries).toBeInstanceOf(EventStoreClient);
      clientWithoutRetries.close();
    });
  });

  describe('saveEvents', () => {
    it('should save events successfully', async () => {
      const request = {
        stream: {
          name: 'test-stream',
          expectedVersion: 0
        },
        events: [
          {
            eventId: 'test-event-1',
            eventType: 'TestEvent',
            data: { test: 'data' },
            metadata: { source: 'test' }
          }
        ],
        boundary: 'test-boundary'
      };

      await expect(client.saveEvents(request)).resolves.toBeUndefined();
    });
  });

  describe('getEvents', () => {
    it('should retrieve events successfully', async () => {
      const request = {
        stream: {
          name: 'test-stream'
        },
        boundary: 'test-boundary'
      };

      const events = await client.getEvents(request);
      
      expect(events).toHaveLength(1);
      expect(events[0]).toEqual({
        eventId: 'test-event-1',
        eventType: 'TestEvent',
        data: { test: 'data' },
        metadata: { source: 'test' },
        streamId: 'test-stream',
        version: 1,
        position: { commitPosition: 1, preparePosition: 1 },
        dateCreated: '2024-01-01T00:00:00Z'
      });
    });

    it('should handle version range parameters', async () => {
      const request = {
        stream: {
          name: 'test-stream',
          fromVersion: 1
        },
        fromPosition: { commitPosition: 1, preparePosition: 1 } as Position,
        count: 5,
        direction: 'ASC' as 'ASC',
        boundary: 'test-boundary'
      };

      const events = await client.getEvents(request);
      expect(events).toHaveLength(1);
    });
  });

  describe('subscribeToEvents', () => {
    it('should create subscription successfully', () => {
      const request = {
        subscriberName: 'test-subscriber',
        stream: 'test-stream',
        boundary: 'test-boundary'
      };

      const onEvent = jest.fn();
      const onError = jest.fn();

      const subscription = client.subscribeToEvents({
        subscriberName: 'test-subscriber',
        stream: 'test-stream',
        boundary: 'test-boundary'
      }, onEvent, onError);
      
      expect(subscription).toBeDefined();
      expect(subscription.on).toBeDefined();
      expect(subscription.cancel).toBeDefined();
    });

    it('should handle subscription without stream', () => {
      const request = {
        afterPosition: { commitPosition: 100, preparePosition: 100 } as Position,
        boundary: 'test-boundary'
      };

      const onEvent = jest.fn();
      const subscription = client.subscribeToEvents({
        subscriberName: 'test-subscriber',
        afterPosition: { commitPosition: 100, preparePosition: 100 } as Position,
        boundary: 'test-boundary'
      }, onEvent);
      
      expect(subscription).toBeDefined();
    });
  });

  describe('healthCheck', () => {
    it('should return true for successful connection', async () => {
      const isHealthy = await client.healthCheck();
      expect(isHealthy).toBe(true);
    });
  });

  describe('close', () => {
    it('should close client connection', () => {
      expect(() => client.close()).not.toThrow();
    });
  });
});