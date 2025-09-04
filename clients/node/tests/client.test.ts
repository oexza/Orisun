import { EventStoreClient, Event, EventToSave } from '../src';
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
          id: 'test-event-1',
          type: 'TestEvent',
          data: JSON.stringify({ test: 'data' }),
          metadata: JSON.stringify({ source: 'test' }),
          streamId: 'test-stream',
          streamVersion: 1,
          position: 1,
          timestamp: '2024-01-01T00:00:00Z'
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
      const defaultClient = new EventStoreClient();
      expect(defaultClient).toBeInstanceOf(EventStoreClient);
      defaultClient.close();
    });

    it('should create client with custom options', () => {
      expect(client).toBeInstanceOf(EventStoreClient);
    });
  });

  describe('saveEvents', () => {
    it('should save events successfully', async () => {
      const request = {
        stream: {
          name: 'test-stream',
          expected_version: 0
        },
        events: [
          {
            event_id: 'test-event-1',
            event_type: 'TestEvent',
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
        streamId: 'test-stream',
        boundary: 'test-boundary'
      };

      const events = await client.getEvents(request);
      
      expect(events).toHaveLength(1);
      expect(events[0]).toEqual({
        id: 'test-event-1',
        type: 'TestEvent',
        data: { test: 'data' },
        metadata: { source: 'test' },
        streamId: 'test-stream',
        streamVersion: 1,
        position: 1,
        timestamp: '2024-01-01T00:00:00Z'
      });
    });

    it('should handle version range parameters', async () => {
      const request = {
        query: {
          stream: {
            name: 'test-stream',
            from_version: 1
          }
        },
        from_position: 1,
        count: 5,
        direction: 0,
        boundary: 'test-boundary'
      };

      const events = await client.getEvents(request);
      expect(events).toHaveLength(1);
    });
  });

  describe('subscribeToEvents', () => {
    it('should create subscription successfully', () => {
      const request = {
        streamId: 'test-stream',
        boundary: 'test-boundary'
      };

      const onEvent = jest.fn();
      const onError = jest.fn();

      const subscription = client.subscribeToEvents({
        subscriberName: 'test-subscriber',
        stream: { name: 'test-stream' },
        boundary: 'test-boundary'
      }, onEvent, onError);
      
      expect(subscription).toBeDefined();
      expect(subscription.on).toBeDefined();
      expect(subscription.cancel).toBeDefined();
    });

    it('should handle subscription without streamId', () => {
      const request = {
        fromPosition: 100,
        boundary: 'test-boundary'
      };

      const onEvent = jest.fn();
      const subscription = client.subscribeToEvents({
        subscriberName: 'test-subscriber',
        fromPosition: 100,
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