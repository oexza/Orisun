import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { promisify } from 'util';
import path from 'path';

// Types for the event store operations
export interface Event {
  eventId: string;
  eventType: string;
  data: any;
  metadata?: Record<string, string>;
  streamId: string;
  version: number;
  position: Position;
  dateCreated: string;
}

export interface EventToSave {
  eventId: string;
  eventType: string;
  data: any;
  metadata?: Record<string, string>;
}

export interface Position {
  commitPosition: number;
  preparePosition: number;
}

export interface Tag {
  key: string;
  value: string;
}

export interface Criterion {
  tags: Tag[];
}

export interface Query {
  criteria: Criterion[];
}

export interface SaveEventsRequest {
  boundary: string;
  stream: {
    name: string;
    expectedVersion: number;
    subsetQuery?: Query;
  };
  events: EventToSave[];
}

export interface GetEventsRequest {
  query?: Query;
  fromPosition?: Position;
  count?: number;
  direction?: 'ASC' | 'DESC';
  boundary: string;
  stream?: {
    name: string;
    fromVersion?: number;
  };
}

export interface SubscribeRequest {
  afterPosition?: Position;
  query?: Query;
  subscriberName: string;
  boundary: string;
  stream?: string;
  afterVersion?: number;
}

export interface EventStoreClientOptions {
  host?: string;
  port?: number;
  credentials?: grpc.ChannelCredentials;
  username?: string;
  password?: string;
}

export class EventStoreClient {
  private client: any;
  private credentials: grpc.Metadata;

  constructor(options: EventStoreClientOptions = {}) {
    const {
      host = 'localhost',
      port = 5005,
      credentials = grpc.credentials.createInsecure(),
      username = 'admin',
      password = 'changeit'
    } = options;

    // Load the protobuf definition
    const PROTO_PATH = path.join(__dirname, '../../../eventstore/eventstore.proto');
    const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
      keepCase: true,
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true,
    });

    const eventStoreProto = grpc.loadPackageDefinition(packageDefinition) as any;
    
    // Create the client
    this.client = new eventStoreProto.eventstore.EventStore(
      `${host}:${port}`,
      credentials
    );

    // Set up authentication metadata
    this.credentials = new grpc.Metadata();
    const auth = Buffer.from(`${username}:${password}`).toString('base64');
    this.credentials.add('authorization', `Basic ${auth}`);
  }

  /**
   * Save events to a stream
   */
  async saveEvents(request: SaveEventsRequest): Promise<void> {
    const saveEventsAsync = promisify(this.client.saveEvents.bind(this.client));
    
    const grpcRequest = {
      boundary: request.boundary,
      stream: {
        name: request.stream.name,
        expected_version: request.stream.expectedVersion,
        subsetQuery: request.stream.subsetQuery
      },
      events: request.events.map(event => ({
        event_id: event.eventId,
        event_type: event.eventType,
        data: JSON.stringify(event.data),
        metadata: JSON.stringify(event.metadata || {})
      }))
    };

    await saveEventsAsync(grpcRequest, this.credentials);
  }

  /**
   * Get events from a stream
   */
  async getEvents(request: GetEventsRequest): Promise<Event[]> {
    const getEventsAsync = promisify(this.client.getEvents.bind(this.client));
    
    const grpcRequest = {
      query: request.query,
      from_position: request.fromPosition,
      count: request.count || 100,
      direction: request.direction === 'DESC' ? 1 : 0,
      boundary: request.boundary,
      stream: request.stream ? {
        name: request.stream.name,
        from_version: request.stream.fromVersion || 0
      } : undefined
    };

    const response = await getEventsAsync(grpcRequest, this.credentials);
    
    return response.events.map((event: any) => ({
      eventId: event.event_id,
      eventType: event.event_type,
      data: JSON.parse(event.data),
      metadata: JSON.parse(event.metadata || '{}'),
      streamId: event.stream_id,
      version: event.version,
      position: event.position,
      dateCreated: event.date_created
    }));
  }

  /**
   * Subscribe to events from a stream or all streams
   */
  subscribeToEvents(request: SubscribeRequest, onEvent: (event: Event) => void, onError?: (error: Error) => void): grpc.ClientReadableStream<any> {
    let stream: grpc.ClientReadableStream<any>;
    
    if (request.stream) {
      // Subscribe to a specific stream
      const grpcRequest = {
        query: request.query,
        subscriber_name: request.subscriberName,
        boundary: request.boundary,
        stream: request.stream,
        after_version: request.afterVersion || 0
      };
      stream = this.client.catchUpSubscribeToStream(grpcRequest, this.credentials);
    } else {
      // Subscribe to all events
      const grpcRequest = {
        afterPosition: request.afterPosition,
        query: request.query,
        subscriber_name: request.subscriberName,
        boundary: request.boundary
      };
      stream = this.client.catchUpSubscribeToEvents(grpcRequest, this.credentials);
    }
    
    stream.on('data', (event: any) => {
      const parsedEvent: Event = {
        eventId: event.event_id,
        eventType: event.event_type,
        data: JSON.parse(event.data),
        metadata: JSON.parse(event.metadata || '{}'),
        streamId: event.stream_id,
        version: event.version,
        position: event.position,
        dateCreated: event.date_created
      };
      onEvent(parsedEvent);
    });

    stream.on('error', (error: Error) => {
      if (onError) {
        onError(error);
      } else {
        console.error('Subscription error:', error);
      }
    });

    return stream;
  }

  /**
   * Close the client connection
   */
  close(): void {
    this.client.close();
  }

  /**
   * Check if the client is connected
   */
  async healthCheck(): Promise<boolean> {
    try {
      // Try to make a simple call to test connectivity
      await this.getEvents({ 
        boundary: 'test',
        stream: { name: 'health-check' },
        count: 1
      });
      return true;
    } catch (error) {
      return false;
    }
  }
}