# Orisun Event Store PHP Client

A production-ready PHP client for Orisun Event Store with Laravel integration.

## Features

- 🚀 **High Performance**: Built on gRPC for fast, efficient communication
- ⚖️ **Load Balancing**: Client-side load balancing with round-robin and pick-first policies
- 🔧 **Laravel Integration**: Service provider, facade, and Artisan commands
- 📝 **Type Safety**: Full PHP type hints and comprehensive error handling
- 🔄 **Event Streaming**: Support for real-time event subscriptions
- 📊 **Logging**: PSR-3 compatible logging integration
- ⚡ **Easy Setup**: Simple configuration and installation

## Installation

### Requirements

- PHP 8.1 or higher
- gRPC PHP extension
- Composer

### Install via Composer

```bash
composer require orisun/php-client
```

### Install gRPC Extension

```bash
# Using PECL
pecl install grpc

# Or using package manager (Ubuntu/Debian)
sudo apt-get install php-grpc

# Or using package manager (macOS with Homebrew)
brew install grpc
```

## Laravel Integration

### 1. Register Service Provider

Add the service provider to your `config/app.php`:

```php
'providers' => [
    // Other providers...
    Orisun\Client\Laravel\OrisunServiceProvider::class,
],
```

### 2. Add Facade (Optional)

Add the facade to your `config/app.php`:

```php
'aliases' => [
    // Other aliases...
    'Orisun' => Orisun\Client\Laravel\Facades\Orisun::class,
],
```

### 3. Publish Configuration

```bash
php artisan vendor:publish --tag=orisun-config
```

### 4. Configure Environment

Add to your `.env` file:

```env
# Single host configuration
ORISUN_HOST=localhost
ORISUN_PORT=50051

# OR multiple hosts for load balancing
# ORISUN_HOSTS=server1:50051,server2:50051,server3:50051

# OR full target string for DNS/IPv4/IPv6
# ORISUN_TARGET=dns:///eventstore.example.com:50051
# ORISUN_TARGET=ipv4:10.0.0.10:50051
# ORISUN_TARGET=ipv6:[::1]:50051

ORISUN_BOUNDARY=default
ORISUN_TLS_ENABLED=false
ORISUN_LOGGING_ENABLED=true
```

## Basic Usage

### Dependency Injection

```php
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;

class EventController extends Controller
{
    public function __construct(
        private OrisunClient $orisun
    ) {}

    public function store(Request $request)
    {
        $event = new Event(
            'user.registered',
            ['user_id' => 123, 'email' => 'user@example.com'],
            ['ip_address' => $request->ip()]
        );

        $result = $this->orisun->saveEvents('user-stream', [$event]);
        
        return response()->json([
            'success' => true,
            'position' => $result->getLogPosition()
        ]);
    }
}
```

### Using Facade

```php
use Orisun\Client\Laravel\Facades\Orisun;
use Orisun\Client\Event;

// Save events
$event = new Event('order.created', ['order_id' => 456]);
$result = Orisun::saveEvents('orders', [$event]);

// Get events
$events = Orisun::getEvents('orders', 0, 100);

// Subscribe to stream
Orisun::subscribeToStream('orders', function (Event $event) {
    Log::info('Received event: ' . $event->getEventType());
}, 'order-processor');
```

## Advanced Usage

### Event Creation

```php
use Orisun\Client\Event;
use Ramsey\Uuid\Uuid;

// Basic event
$event = new Event(
    'product.updated',
    ['product_id' => 789, 'name' => 'New Product'],
    ['user_id' => 123, 'timestamp' => time()]
);

// Event with custom ID
$event = new Event(
    'payment.processed',
    ['amount' => 99.99, 'currency' => 'USD'],
    ['gateway' => 'stripe'],
    Uuid::uuid4()->toString()
);
```

### Batch Event Saving

```php
$events = [];
for ($i = 0; $i < 100; $i++) {
    $events[] = new Event(
        'batch.event',
        ['sequence' => $i],
        ['batch_id' => 'batch-123']
    );
}

$result = $orisun->saveEvents('batch-stream', $events);
echo "Saved to position: " . $result->getLogPosition();
```

### Reading Events with Pagination

```php
$fromVersion = 0;
$pageSize = 50;

do {
    $events = $orisun->getEvents('user-stream', $fromVersion, $pageSize);
    
    foreach ($events as $event) {
        echo "Event: {$event->getEventType()}\n";
        echo "Data: " . $event->getDataAsJson() . "\n";
    }
    
    $fromVersion += count($events);
} while (count($events) === $pageSize);
```

### Event Subscriptions

```php
// Subscribe to specific stream
$orisun->subscribeToStream(
    'notifications',
    function (Event $event) {
        // Process notification event
        $data = $event->getDataAsArray();
        Mail::to($data['email'])->send(new NotificationMail($data));
    },
    'email-sender',
    -1 // Start from beginning
);

// Subscribe to all events
$orisun->subscribeToEventStore(
    function (Event $event) {
        // Log all events
        Log::info('Event received', [
            'type' => $event->getEventType(),
            'stream' => $event->getStreamId(),
            'position' => $event->getPosition()?->toString()
        ]);
    },
    'audit-logger'
);
```

## Load Balancing

The PHP client supports client-side load balancing to distribute requests across multiple server instances.

### Configuration

```php
use Orisun\Client\OrisunClient;

// Round-robin load balancing (default)
$client = new OrisunClient('localhost:50051', [
    'loadBalancingPolicy' => 'round_robin',
    'boundary' => 'production'
]);

// Pick-first load balancing
$client = new OrisunClient('localhost:50051', [
    'loadBalancingPolicy' => 'pick_first',
    'boundary' => 'production'
]);
```

### Load Balancing Policies

- **`round_robin`** (default): Distributes requests evenly across all available servers in a round-robin fashion
- **`pick_first`**: Uses the first available server and only switches to another if the current one becomes unavailable

### Keepalive Configuration

Configure connection keepalive settings for better connection management:

```php
$client = new OrisunClient('localhost:50051', [
    'loadBalancingPolicy' => 'round_robin',
    'keepaliveTimeMs' => 30000,        // Send keepalive ping every 30 seconds
    'keepaliveTimeoutMs' => 10000,     // Wait 10 seconds for keepalive response
    'keepalivePermitWithoutCalls' => true, // Allow pings when no active calls
]);
```

### Multiple Server Endpoints

The PHP client supports various target formats for different deployment scenarios:

#### 1. Single Host (Default)
```php
$client = new OrisunClient('localhost:50051');
```

#### 2. Multiple Hosts (Comma-separated)
```php
// Load balancing across multiple servers
$client = new OrisunClient('server1:50051,server2:50051,server3:50051', [
    'loadBalancingPolicy' => 'round_robin'
]);
```

#### 3. DNS-based Service Discovery
```php
// DNS resolution with automatic load balancing
$client = new OrisunClient('dns:///eventstore.example.com:50051', [
    'loadBalancingPolicy' => 'round_robin'
]);
```

#### 4. IPv4/IPv6 Targets
```php
// IPv4 target
$client = new OrisunClient('ipv4:10.0.0.10:50051');

// IPv6 target
$client = new OrisunClient('ipv6:[::1]:50051');
```

## Laravel Artisan Commands

### Test Connection

```bash
# Basic connection test
php artisan orisun:test

# Test with custom stream
php artisan orisun:test --stream=my-test-stream

# Test with more events
php artisan orisun:test --events=10

# Test and cleanup
php artisan orisun:test --cleanup
```

## Configuration Options

The `config/orisun.php` file provides extensive configuration options:

```php
return [
    // Single host configuration (default)
    'host' => env('ORISUN_HOST', 'localhost'),
    'port' => env('ORISUN_PORT', 50051),
    
    // Multiple hosts for load balancing
    'hosts' => env('ORISUN_HOSTS'), // e.g., 'host1:50051,host2:50051'
    
    // Full target string (DNS, IPv4, IPv6)
    'target' => env('ORISUN_TARGET'), // e.g., 'dns:///eventstore.example.com:50051'
    
    // TLS configuration
    'tls' => [
        'enabled' => env('ORISUN_TLS_ENABLED', false),
        'cert_file' => env('ORISUN_TLS_CERT_FILE'),
        'key_file' => env('ORISUN_TLS_KEY_FILE'),
        'ca_file' => env('ORISUN_TLS_CA_FILE'),
    ],
    
    // Client settings
    'boundary' => env('ORISUN_BOUNDARY', 'default'),
    'timeout' => env('ORISUN_TIMEOUT', 30),
    
    // Load balancing configuration
    'load_balancing' => [
        'policy' => env('ORISUN_LOAD_BALANCING_POLICY', 'round_robin'), // 'round_robin' or 'pick_first'
    ],
    
    // Keepalive configuration
    'keepalive' => [
        'time_ms' => env('ORISUN_KEEPALIVE_TIME_MS', 30000),
        'timeout_ms' => env('ORISUN_KEEPALIVE_TIMEOUT_MS', 10000),
        'permit_without_calls' => env('ORISUN_KEEPALIVE_PERMIT_WITHOUT_CALLS', true),
    ],
    
    // Default stream settings
    'defaults' => [
        'event_count' => env('ORISUN_DEFAULT_EVENT_COUNT', 100),
        'direction' => env('ORISUN_DEFAULT_DIRECTION', 'ASC'),
        'retry_attempts' => env('ORISUN_RETRY_ATTEMPTS', 3),
        'retry_delay' => env('ORISUN_RETRY_DELAY', 1000),
    ],
    
    // Logging configuration
    'logging' => [
        'enabled' => env('ORISUN_LOGGING_ENABLED', true),
        'level' => env('ORISUN_LOGGING_LEVEL', 'info'),
        'channel' => env('ORISUN_LOGGING_CHANNEL', 'default'),
    ],
];
```

## Error Handling

```php
use Orisun\Client\OrisunException;

try {
    $result = $orisun->saveEvents('stream', [$event]);
} catch (OrisunException $e) {
    if ($e->isConcurrencyConflict()) {
        // Handle version conflict
        Log::warning('Concurrency conflict detected', [
            'stream' => 'stream',
            'expected_version' => $expectedVersion
        ]);
    } elseif ($e->isStreamNotFound()) {
        // Handle missing stream
        Log::error('Stream not found: stream');
    } elseif ($e->isConnectionError()) {
        // Handle connection issues
        Log::error('Connection error: ' . $e->getMessage());
    } else {
        // Handle other errors
        Log::error('Orisun error: ' . $e->getMessage());
    }
}
```

## Performance Tips

### 1. Batch Operations

```php
// Good: Batch multiple events
$events = [$event1, $event2, $event3];
$orisun->saveEvents('stream', $events);

// Avoid: Multiple single-event calls
$orisun->saveEvents('stream', [$event1]);
$orisun->saveEvents('stream', [$event2]);
$orisun->saveEvents('stream', [$event3]);
```

### 2. Connection Reuse

```php
// The client automatically reuses connections
// Use dependency injection to share the same instance
class EventService
{
    public function __construct(
        private OrisunClient $orisun
    ) {}
}
```

### 3. Async Processing

```php
// Use Laravel queues for heavy event processing
class ProcessEventJob implements ShouldQueue
{
    public function handle(OrisunClient $orisun)
    {
        $events = $orisun->getEvents('heavy-stream', 0, 1000);
        
        foreach ($events as $event) {
            // Process event
        }
    }
}
```

## Testing

### Unit Testing

```php
use Orisun\Client\OrisunClient;
use Mockery;

class EventServiceTest extends TestCase
{
    public function test_saves_event()
    {
        $mockClient = Mockery::mock(OrisunClient::class);
        $mockClient->shouldReceive('saveEvents')
                   ->once()
                   ->andReturn(new WriteResult());
        
        $this->app->instance(OrisunClient::class, $mockClient);
        
        $service = new EventService($mockClient);
        $result = $service->saveUserEvent(['user_id' => 123]);
        
        $this->assertTrue($result);
    }
}
```

### Integration Testing

```php
class OrisunIntegrationTest extends TestCase
{
    public function test_real_connection()
    {
        if (!env('ORISUN_INTEGRATION_TESTS')) {
            $this->markTestSkipped('Integration tests disabled');
        }
        
        $orisun = app(OrisunClient::class);
        
        $event = new Event('test.event', ['test' => true]);
        $result = $orisun->saveEvents('test-stream', [$event]);
        
        $this->assertNotNull($result->getLogPosition());
    }
}
```

## Troubleshooting

### Common Issues

1. **gRPC Extension Not Installed**
   ```
   Error: Class 'Grpc\ChannelCredentials' not found
   ```
   Solution: Install the gRPC PHP extension

2. **Connection Refused**
   ```
   Error: Connection refused
   ```
   Solution: Check if Orisun server is running and accessible

3. **TLS Certificate Issues**
   ```
   Error: SSL certificate verification failed
   ```
   Solution: Check TLS configuration and certificate paths

### Debug Mode

Enable debug logging in your `.env`:

```env
ORISUN_LOGGING_LEVEL=debug
LOG_LEVEL=debug
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

MIT License. See LICENSE file for details.

## Support

For issues and questions:
- GitHub Issues: [orisun/php-client/issues](https://github.com/orisun/php-client/issues)
- Documentation: [docs.orisun.io](https://docs.orisun.io)