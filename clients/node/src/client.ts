import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { promisify } from 'util';
import * as path from 'path';

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

export interface RetryPolicy {
  /**
   * Maximum number of retry attempts
   */
  maxAttempts?: number;
  /**
   * Initial backoff time (e.g., '0.1s', '1s')
   */
  initialBackoff?: string;
  /**
   * Maximum backoff time (e.g., '10s', '60s')
   */
  maxBackoff?: string;
  /**
   * Backoff multiplier for exponential backoff
   */
  backoffMultiplier?: number;
  /**
   * Status codes that should trigger a retry
   */
  retryableStatusCodes?: string[];
}



/**
 * Logger interface for client logging
 */
export interface Logger {
  debug(message: string, ...args: any[]): void;
  info(message: string, ...args: any[]): void;
  warn(message: string, ...args: any[]): void;
  error(message: string, ...args: any[]): void;
}



export interface EventStoreClientOptions {
  /**
   * Server hostname or DNS name. Can be a comma-separated list of hosts for load balancing.
   * If using DNS-based load balancing, provide a single DNS name that resolves to multiple IPs.
   */
  host?: string;
  /**
   * Server port. Required when using a single host or comma-separated hosts.
   * Not required when using a target string.
   */
  port?: number;
  /**
   * Alternative to host:port. A fully qualified gRPC target string in the format:
   * dns:[//authority/]host[:port] or ipv4:host:port or ipv6:[host]:port
   * Examples: "dns:///eventstore.example.com:5005", "ipv4:10.0.0.10:5005"
   * When provided, this takes precedence over host and port.
   */
  target?: string;
  /**
   * gRPC credentials for secure connections
   */
  credentials?: grpc.ChannelCredentials;
  /**
   * Authentication username
   */
  username?: string;
  /**
   * Authentication password
   */
  password?: string;
  /**
   * Time in milliseconds between keep-alive pings
   */
  keepaliveTimeMs?: number;
  /**
   * Time in milliseconds to wait for ping response
   */
  keepaliveTimeoutMs?: number;
  /**
   * Allow keep-alive pings when there are no active calls
   */
  keepalivePermitWithoutCalls?: boolean;
  /**
   * Load balancing policy to use. Options: 'round_robin', 'pick_first'
   * - round_robin: Distributes requests across all available servers
   * - pick_first: Connects to the first available server
   */
  loadBalancingPolicy?: 'round_robin' | 'pick_first';
  /**
   * Enable retries for failed requests
   */
  enableRetries?: boolean;
  /**
   * Retry policy configuration for failed requests
   */
  retryPolicy?: RetryPolicy;
  /**
   * Custom logger implementation
   */
  logger?: Logger;
  /**
   * Enable or disable logging
   */
  enableLogging?: boolean;
}

/**
 * Validates client options and throws errors for invalid configurations
 */
function validateClientOptions(options: EventStoreClientOptions): void {
  // Validate connection options
  if (!options.target && !options.host) {
    throw new Error('Either host or target must be provided');
  }
  
  if (options.host && !options.target && !options.port) {
    throw new Error('Port must be provided when using host without target');
  }
  
  if (options.port && (options.port < 1 || options.port > 65535)) {
    throw new Error('Port must be between 1 and 65535');
  }
  
  // Validate keepalive options
  if (options.keepaliveTimeMs && options.keepaliveTimeMs < 0) {
    throw new Error('keepaliveTimeMs must be a positive number');
  }
  
  if (options.keepaliveTimeoutMs && options.keepaliveTimeoutMs < 0) {
    throw new Error('keepaliveTimeoutMs must be a positive number');
  }
  
  // Validate retry policy
  if (options.retryPolicy) {
    if (options.retryPolicy.maxAttempts && options.retryPolicy.maxAttempts < 1) {
      throw new Error('retryPolicy.maxAttempts must be at least 1');
    }
    
    if (options.retryPolicy.backoffMultiplier && options.retryPolicy.backoffMultiplier <= 0) {
      throw new Error('retryPolicy.backoffMultiplier must be greater than 0');
    }
  }
  
  // Validate load balancing policy
  if (options.loadBalancingPolicy && 
      !['round_robin', 'pick_first'].includes(options.loadBalancingPolicy)) {
    throw new Error('loadBalancingPolicy must be either "round_robin" or "pick_first"');
  }
}

/**
 * Default no-op logger that doesn't log anything
 */
class NoopLogger implements Logger {
  debug(message: string, ...args: any[]): void {}
  info(message: string, ...args: any[]): void {}
  warn(message: string, ...args: any[]): void {}
  error(message: string, ...args: any[]): void {}
}

/**
 * Rate limiter implementation for client operations
 */


export class EventStoreClient {
  private client: any;
  private channel!: grpc.Channel;
  private logger: Logger;
  private credentials?: grpc.Metadata;
  private disposed: boolean = false;

  constructor(options: EventStoreClientOptions = {}) {
    // Validate options
    validateClientOptions(options);
    
    const {
      host = 'localhost',
      port = 5005,
      target,
      credentials = grpc.credentials.createInsecure(),
      username = 'admin',
      password = 'changeit',
      keepaliveTimeMs = 30000, // 30 seconds by default
      keepaliveTimeoutMs = 10000, // 10 seconds by default
      keepalivePermitWithoutCalls = true, // Allow pings when there are no calls
      loadBalancingPolicy = 'round_robin',
      enableRetries = true,
      retryPolicy = {
        maxAttempts: 5,
        initialBackoff: '0.1s',
        maxBackoff: '10s',
        backoffMultiplier: 1.5,
        retryableStatusCodes: ['UNAVAILABLE', 'UNKNOWN']
      },
      logger,
      enableLogging = false
    } = options;
    
    // Initialize logger
    this.logger = enableLogging ? (logger || console) : new NoopLogger();
    
    // Rate limiting functionality has been removed
    
    // Circuit breaker functionality has been removed

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
    
    // Create the client with keep-alive options and load balancing
    const channelOptions = {
      'grpc.keepalive_time_ms': keepaliveTimeMs,
      'grpc.keepalive_timeout_ms': keepaliveTimeoutMs,
      'grpc.keepalive_permit_without_calls': keepalivePermitWithoutCalls ? 1 : 0,
      'grpc.http2.min_time_between_pings_ms': keepaliveTimeMs,
      'grpc.http2.max_pings_without_data': 0,
      'grpc.lb_policy_name': loadBalancingPolicy,
      'grpc.enable_retries': enableRetries ? 1 : 0,
      'grpc.service_config': JSON.stringify({
        loadBalancingConfig: [{ [loadBalancingPolicy]: {} }],
        methodConfig: [{
          name: [{ service: 'eventstore.EventStore' }],
          retryPolicy: {
            maxAttempts: retryPolicy.maxAttempts,
            initialBackoff: retryPolicy.initialBackoff,
            maxBackoff: retryPolicy.maxBackoff,
            backoffMultiplier: retryPolicy.backoffMultiplier,
            retryableStatusCodes: retryPolicy.retryableStatusCodes
          }
        }]
      })
    };
    
    // Determine the target string
    let targetString: string;
    
    if (target) {
      // Use the provided target string directly
      targetString = target;
    } else if (host.includes(',')) {
      // Handle comma-separated list of hosts for manual load balancing
      const hosts = host.split(',').map(h => h.trim());
      targetString = hosts.map(h => `${h}:${port}`).join(',');
    } else {
      // Standard host:port format
      targetString = `${host}:${port}`;
    }
    
    this.client = new eventStoreProto.eventstore.EventStore(
      targetString,
      credentials,
      channelOptions
    );

    // Set up authentication metadata
    this.credentials = new grpc.Metadata();
    const auth = Buffer.from(`${username}:${password}`).toString('base64');
    this.credentials.add('authorization', `Basic ${auth}`);
    
    // Set up logger
    this.logger = options.logger || {
      debug: () => {},
      info: () => {},
      warn: () => {},
      error: () => {}
    };
    
    if (options.enableLogging) {
      this.logger.info('EventStoreClient initialized');
    }
  }

  /**
   * Save events to a stream
   * @throws {Error} If the request is invalid or the operation fails
   */
  async saveEvents(request: SaveEventsRequest): Promise<void> {
    // Check if client is disposed
    if (this.disposed) {
      throw new Error('Client has been disposed');
    }
    
    if (!request) {
      throw new Error('SaveEventsRequest cannot be null or undefined');
    }
    
    if (!request.boundary) {
      throw new Error('Boundary is required');
    }
    
    if (!request.stream || !request.stream.name) {
      throw new Error('Stream name is required');
    }
    
    if (!request.events || !Array.isArray(request.events) || request.events.length === 0) {
      throw new Error('At least one event is required');
    }
    
    // Validate each event
    request.events.forEach((event, index) => {
      if (!event.eventId) {
        throw new Error(`Event at index ${index} is missing eventId`);
      }
      if (!event.eventType) {
        throw new Error(`Event at index ${index} is missing eventType`);
      }
      if (event.data === undefined || event.data === null) {
        throw new Error(`Event at index ${index} is missing data`);
      }
    });
    
    this.logger.debug(`Saving ${request.events.length} events to stream '${request.stream.name}'`);
    
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

    try {
      await saveEventsAsync(grpcRequest, this.credentials || new grpc.Metadata());
      this.logger.debug(`Successfully saved events to stream '${request.stream.name}'`);
    } catch (error) {
      this.logger.error(`Failed to save events to stream '${request.stream.name}':`, error);
      
      // Enhance error with context
      const enhancedError = new Error(
        `Failed to save events to stream '${request.stream.name}': ${(error as Error).message}`
      );
      enhancedError.stack = (error as Error).stack;
      (enhancedError as any).originalError = error;
      (enhancedError as any).streamName = request.stream.name;
      (enhancedError as any).eventCount = request.events.length;
      
      throw enhancedError;
    }
  }

  /**
   * Get events from a stream
   * @throws {Error} If the request is invalid or the operation fails
   */
  async getEvents(request: GetEventsRequest): Promise<Event[]> {
    // Check if client is disposed
    if (this.disposed) {
      throw new Error('Client has been disposed');
    }
    
    if (!request) {
      throw new Error('GetEventsRequest cannot be null or undefined');
    }
    
    if (!request.boundary) {
      throw new Error('Boundary is required');
    }
    
    // If stream is specified, validate stream name
    if (request.stream && !request.stream.name) {
      throw new Error('Stream name is required when stream is specified');
    }
    
    // Validate count if provided
    if (request.count !== undefined && request.count <= 0) {
      throw new Error('Count must be greater than 0');
    }
    
    const streamInfo = request.stream ? `stream '${request.stream.name}'` : 'all streams';
    this.logger.debug(`Getting events from ${streamInfo}`);
    
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

    try {
      const response = await getEventsAsync(grpcRequest, this.credentials || new grpc.Metadata());
      
      if (!response || !response.events) {
        this.logger.warn(`No events returned from ${streamInfo}`);
        return [];
      }
      
      const events = response.events.map((event: any) => {
        try {
          return {
            eventId: event.event_id,
            eventType: event.event_type,
            data: JSON.parse(event.data),
            metadata: JSON.parse(event.metadata || '{}'),
            streamId: event.stream_id,
            version: event.version,
            position: event.position,
            dateCreated: event.date_created
          };
        } catch (parseError) {
          this.logger.error(`Failed to parse event data or metadata: ${(parseError as Error).message}`);
          // Return event with unparsed data to avoid losing the event
          return {
            eventId: event.event_id,
            eventType: event.event_type,
            data: event.data, // Raw string
            metadata: event.metadata || '{}', // Raw string
            streamId: event.stream_id,
            version: event.version,
            position: event.position,
            dateCreated: event.date_created
          };
        }
      });
      
      this.logger.debug(`Successfully retrieved ${events.length} events from ${streamInfo}`);
      return events;
    } catch (error) {
      this.logger.error(`Failed to get events from ${streamInfo}:`, error);
      
      // Enhance error with context
      const enhancedError = new Error(
        `Failed to get events from ${streamInfo}: ${(error as Error).message}`
      );
      enhancedError.stack = (error as Error).stack;
      (enhancedError as any).originalError = error;
      (enhancedError as any).streamName = request.stream?.name;
      (enhancedError as any).boundary = request.boundary;
      
      throw enhancedError;
    }
  }

  /**
   * Subscribe to events from a stream or all streams
   * @throws {Error} If the request is invalid
   */
  subscribeToEvents(request: SubscribeRequest, onEvent: (event: Event) => void, onError?: (error: Error) => void): grpc.ClientReadableStream<any> {
    // Check if client is disposed
    if (this.disposed) {
      throw new Error('Client has been disposed');
    }
    
    if (!request) {
      throw new Error('SubscribeRequest cannot be null or undefined');
    }
    
    if (!request.boundary) {
      throw new Error('Boundary is required');
    }
    
    if (!request.subscriberName) {
      throw new Error('Subscriber name is required');
    }
    
    if (!onEvent || typeof onEvent !== 'function') {
      throw new Error('Event handler function is required');
    }
    
    const streamInfo = request.stream ? `stream '${request.stream}'` : 'all streams';
    this.logger.debug(`Subscribing to ${streamInfo} with subscriber '${request.subscriberName}'`);
    
    // No circuit breaker or rate limiting check needed
    
    let stream: grpc.ClientReadableStream<any>;
    
    try {
      if (request.stream) {
        // Subscribe to a specific stream
        const grpcRequest = {
          query: request.query,
          subscriber_name: request.subscriberName,
          boundary: request.boundary,
          stream: request.stream,
          after_version: request.afterVersion || 0
        };
        stream = this.client.catchUpSubscribeToStream(grpcRequest);
      } else {
        // Subscribe to all events
        const grpcRequest = {
          afterPosition: request.afterPosition,
          query: request.query,
          subscriber_name: request.subscriberName,
          boundary: request.boundary
        };
        stream = this.client.catchUpSubscribeToEvents(grpcRequest);
      }
    } catch (error) {
      // Circuit breaker functionality has been removed
      
      this.logger.error(`Failed to create subscription to ${streamInfo}:`, error);
      
      // Enhance error with context
      const enhancedError = new Error(
        `Failed to create subscription to ${streamInfo}: ${(error as Error).message}`
      );
      enhancedError.stack = (error as Error).stack;
      (enhancedError as any).originalError = error;
      (enhancedError as any).streamName = request.stream;
      (enhancedError as any).subscriberName = request.subscriberName;
      
      throw enhancedError;
    }
    
    this.logger.debug(`Successfully subscribed to ${streamInfo}`);
    
    // Handle data events
    stream.on('data', (event: any) => {
      try {
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
      } catch (parseError) {
        this.logger.error(`Failed to parse event data or metadata: ${(parseError as Error).message}`);
        
        // Call error handler with enhanced error
        const enhancedError = new Error(
          `Failed to parse event: ${(parseError as Error).message}`
        );
        enhancedError.stack = (parseError as Error).stack;
        (enhancedError as any).originalError = parseError;
        (enhancedError as any).eventId = event.event_id;
        (enhancedError as any).eventType = event.event_type;
        
        if (onError) {
          onError(enhancedError);
        } else {
          this.logger.error('Subscription parse error:', enhancedError);
        }
      }
    });

    // Handle stream errors
    stream.on('error', (error: Error) => {
      this.logger.error(`Subscription error for ${streamInfo}:`, error);
      
      // Enhance error with context
      const enhancedError = new Error(
        `Subscription error for ${streamInfo}: ${error.message}`
      );
      enhancedError.stack = error.stack;
      (enhancedError as any).originalError = error;
      (enhancedError as any).streamName = request.stream;
      (enhancedError as any).subscriberName = request.subscriberName;
      
      if (onError) {
        onError(enhancedError);
      } else {
        this.logger.error('Unhandled subscription error:', enhancedError);
      }
    });
    
    // Handle end of stream
    stream.on('end', () => {
      this.logger.debug(`Subscription to ${streamInfo} ended`);
    });

    return stream;
  }

  /**
   * Close the client connection and clean up resources
   */
  close(): void {
    if (this.disposed) {
      this.logger.debug('EventStoreClient already disposed');
      return;
    }
    
    this.logger.debug('Closing EventStoreClient connection');
    
    try {
      // No rate limiter or circuit breaker to clean up
      
      // In newer versions of gRPC, we need to close the channel
      if (this.client && typeof this.client.close === 'function') {
        this.client.close();
      } else if (this.client && (this.client as any).channel) {
        // Try to close the underlying channel
        const channel = (this.client as any).channel;
        if (typeof channel.close === 'function') {
          channel.close();
        }
      }
      
      this.disposed = true;
      this.logger.debug('EventStoreClient connection closed successfully');
    } catch (error) {
      this.logger.error('Error closing EventStoreClient connection:', error);
      throw new Error(`Failed to close EventStoreClient: ${(error as Error).message}`);
    }
  }

  /**
   * Check if the client is connected to the server
   * @returns Promise<boolean> True if connected, false otherwise
   */
  async healthCheck(): Promise<boolean> {
    // Check if client is disposed
    if (this.disposed) {
      throw new Error('Client has been disposed');
    }
    
    this.logger.debug('Performing health check');
    
    try {
      // Try to make a simple call to test connectivity
      await this.getEvents({ 
        boundary: 'test',
        stream: { name: 'health-check' },
        count: 1
      });
      
      this.logger.debug('Health check successful');
      return true;
    } catch (error) {
      this.logger.warn('Health check failed:', error);
      return false;
    }
  }
}