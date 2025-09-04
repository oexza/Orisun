import { EventStoreClient, Logger } from '../src';

/**
 * Example of implementing a custom logger for the EventStoreClient
 */

// Custom logger implementation
class CustomLogger implements Logger {
  private logLevel: 'debug' | 'info' | 'warn' | 'error';
  private logPrefix: string;

  constructor(options: { logLevel?: 'debug' | 'info' | 'warn' | 'error'; logPrefix?: string } = {}) {
    this.logLevel = options.logLevel || 'info';
    this.logPrefix = options.logPrefix || 'EventStore';
  }

  private getTimestamp(): string {
    return new Date().toISOString();
  }

  private shouldLog(level: 'debug' | 'info' | 'warn' | 'error'): boolean {
    const levels = { debug: 0, info: 1, warn: 2, error: 3 };
    return levels[level] >= levels[this.logLevel];
  }

  private formatArgs(args: any[]): string {
    return args.map(arg => {
      if (typeof arg === 'object') {
        try {
          return JSON.stringify(arg);
        } catch (e) {
          return String(arg);
        }
      }
      return String(arg);
    }).join(' ');
  }

  debug(message: string, ...args: any[]): void {
    if (this.shouldLog('debug')) {
      console.log(`${this.getTimestamp()} [${this.logPrefix}] [DEBUG] ${message} ${this.formatArgs(args)}`);
    }
  }

  info(message: string, ...args: any[]): void {
    if (this.shouldLog('info')) {
      console.log(`${this.getTimestamp()} [${this.logPrefix}] [INFO] ${message} ${this.formatArgs(args)}`);
    }
  }

  warn(message: string, ...args: any[]): void {
    if (this.shouldLog('warn')) {
      console.warn(`${this.getTimestamp()} [${this.logPrefix}] [WARN] ${message} ${this.formatArgs(args)}`);
    }
  }

  error(message: string, ...args: any[]): void {
    if (this.shouldLog('error')) {
      console.error(`${this.getTimestamp()} [${this.logPrefix}] [ERROR] ${message} ${this.formatArgs(args)}`);
    }
  }
}

async function customLoggerExample() {
  // Create a custom logger
  const logger = new CustomLogger({
    logLevel: 'debug', // Set minimum log level
    logPrefix: 'OrisunClient' // Custom prefix for log messages
  });

  // Create a client with the custom logger
  const client = new EventStoreClient({
    host: 'localhost',
    port: 5005,
    username: 'admin',
    password: 'changeit',
    
    // Use the custom logger
    logger: logger,
    enableLogging: true,
    

  });

  try {
    // The custom logger will be used for all client operations
    console.log('Testing custom logger...');
    
    // Test connection
    const isConnected = await client.healthCheck();
    if (!isConnected) {
      console.error('Failed to connect to event store');
      return;
    }

    // Save an event (this will use the custom logger)
    await client.saveEvents({
      boundary: 'tenant-1',
      stream: {
        name: 'test-stream',
        expectedVersion: 0 // Adding the required expectedVersion property
      },
      events: [{
        eventId: `event-${Date.now()}`,
        eventType: 'TestEvent',
        data: { message: 'Testing custom logger' },
        metadata: { source: 'custom-logger-example' }
      }]
    });

    // Get events (this will use the custom logger)
    const events = await client.getEvents({
      boundary: 'tenant-1',
      stream: {
        name: 'test-stream'
      }
    });

    console.log(`Retrieved ${events.length} events`);
  } catch (error) {
    console.error('Error in custom logger example:', error);
  } finally {
    // Close the client
    client.close();
  }
}

// Run the example
customLoggerExample().catch(console.error);