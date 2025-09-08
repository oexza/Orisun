# Laravel Integration Examples

This directory contains comprehensive examples demonstrating how to integrate Orisun Event Store with Laravel applications. These examples showcase various patterns and use cases for event-driven architecture.

## Directory Structure

```
Laravel/
├── Controllers/
│   └── EventController.php          # Basic event operations
├── Services/
│   └── UserEventService.php         # Domain-specific event service
├── Middleware/
│   └── EventLoggingMiddleware.php    # HTTP request/response logging
├── Jobs/
│   └── ProcessEventStreamJob.php     # Asynchronous event processing
├── Listeners/
│   └── EventStoreListener.php        # Laravel event to Orisun bridge
├── EventSourcing/
│   └── UserAggregate.php             # Event sourcing aggregate example
└── USAGE_EXAMPLES.md                 # This file
```

## Quick Start

### 1. Install and Configure

```bash
# Install the package
composer require orisun/php-client

# Publish configuration
php artisan vendor:publish --provider="Orisun\Client\Laravel\OrisunServiceProvider"

# Configure environment variables
echo "ORISUN_HOST=localhost" >> .env
echo "ORISUN_PORT=50051" >> .env
echo "ORISUN_TLS_ENABLED=false" >> .env
```

### 2. Test Connection

```bash
# Test the connection
php artisan orisun:test

# Test with cleanup
php artisan orisun:test --cleanup
```

## Usage Patterns

### Pattern 1: Basic Event Operations (EventController)

Use this pattern for simple event store operations in your controllers.

```php
// Save a single event
$controller = new EventController();
$response = $controller->saveEvent($request);

// Get events from a stream
$response = $controller->getEvents('user-123');

// Get stream information
$response = $controller->getStreamInfo('user-123');
```

**When to use:**
- Simple CRUD operations
- API endpoints for event management
- Administrative interfaces

### Pattern 2: Domain Service (UserEventService)

Use this pattern to encapsulate domain-specific event logic.

```php
$userService = app(UserEventService::class);

// Handle user registration
$userService->handleUserRegistration([
    'user_id' => '123',
    'email' => 'user@example.com',
    'name' => 'John Doe'
]);

// Get user event history
$events = $userService->getUserEventHistory('123');

// Subscribe to user events
$userService->subscribeToUserEvents(function($event) {
    // Handle event
});
```

**When to use:**
- Domain-driven design
- Complex business logic
- Event-driven microservices

### Pattern 3: HTTP Request Logging (EventLoggingMiddleware)

Use this pattern to automatically log HTTP requests and responses.

```php
// In app/Http/Kernel.php
protected $middleware = [
    // ...
    \App\Http\Middleware\EventLoggingMiddleware::class,
];

// Or apply to specific routes
Route::middleware(['event-logging'])->group(function () {
    Route::get('/api/users', [UserController::class, 'index']);
});
```

**When to use:**
- Audit logging
- Request/response tracking
- Performance monitoring
- Debugging and troubleshooting

### Pattern 4: Asynchronous Processing (ProcessEventStreamJob)

Use this pattern for background event processing.

```php
// Dispatch job to process events
ProcessEventStreamJob::dispatch(
    'user-events',     // stream name
    0,                 // from version
    100,               // batch size
    'user-processor'   // processor name
);

// Or dispatch with delay
ProcessEventStreamJob::dispatch(
    'order-events',
    0,
    50
)->delay(now()->addMinutes(5));
```

**When to use:**
- High-volume event processing
- Long-running operations
- Event replay scenarios
- Data migration and synchronization

### Pattern 5: Laravel Event Bridge (EventStoreListener)

Use this pattern to automatically save Laravel events to Orisun.

```php
// In EventServiceProvider.php
protected $listen = [
    'App\Events\UserRegistered' => [
        'App\Listeners\EventStoreListener',
    ],
    'App\Events\OrderCreated' => [
        'App\Listeners\EventStoreListener',
    ],
];

// Custom event with stream name
class UserRegistered
{
    public function __construct(
        public User $user
    ) {}
    
    public function getStreamName(): string
    {
        return "user-{$this->user->id}";
    }
    
    public function getEventData(): array
    {
        return $this->user->toArray();
    }
}
```

**When to use:**
- Existing Laravel applications
- Gradual migration to event sourcing
- Cross-cutting concerns
- Event-driven architecture adoption

### Pattern 6: Event Sourcing (UserAggregate)

Use this pattern for full event sourcing implementation.

```php
// Create new user aggregate
$user = new UserAggregate(app(OrisunClient::class));
$user->register('user@example.com', 'John Doe', 'password');
$user->activate();
$user->assignRole('admin');
$user->save();

// Load existing user aggregate
$user = UserAggregate::load(app(OrisunClient::class), '123');
$user->updateProfile('Jane Doe', 'jane@example.com');
$user->save();

// Check user state
if ($user->isActive() && $user->hasRole('admin')) {
    // User can perform admin actions
}
```

**When to use:**
- Complex domain models
- Audit requirements
- Temporal queries
- CQRS architecture

## Advanced Scenarios

### Scenario 1: E-commerce Order Processing

```php
// 1. Create order aggregate
$order = new OrderAggregate(app(OrisunClient::class));
$order->create($customerId, $items);
$order->addPayment($paymentDetails);
$order->confirm();
$order->save();

// 2. Process order events asynchronously
ProcessEventStreamJob::dispatch("order-{$order->getId()}");

// 3. Subscribe to order events for notifications
$orderService = app(OrderEventService::class);
$orderService->subscribeToOrderEvents(function($event) {
    match($event->getEventType()) {
        'order.confirmed' => $this->sendConfirmationEmail($event),
        'order.shipped' => $this->sendShippingNotification($event),
        'order.delivered' => $this->requestReview($event),
    };
});
```

### Scenario 2: User Activity Tracking

```php
// 1. Log all user activities via middleware
// (EventLoggingMiddleware automatically captures HTTP requests)

// 2. Process user events for analytics
ProcessEventStreamJob::dispatch('user-activity', 0, 1000, 'analytics-processor');

// 3. Generate user behavior insights
$userService = app(UserEventService::class);
$insights = $userService->generateUserInsights('123', Carbon::now()->subDays(30));
```

### Scenario 3: Microservices Event Communication

```php
// Service A: Publish events
$userService = app(UserEventService::class);
$userService->handleUserRegistration($userData);

// Service B: Subscribe to events
$orisun = app(OrisunClient::class);
$orisun->subscribeToEventStore(function($event) {
    if (str_starts_with($event->getEventType(), 'user.')) {
        // Handle user events in this service
        $this->handleUserEvent($event);
    }
});
```

## Configuration Examples

### Environment Configuration

```env
# Basic connection
ORISUN_HOST=localhost
ORISUN_PORT=50051
ORISUN_TLS_ENABLED=false

# Production with TLS
ORISUN_HOST=orisun.production.com
ORISUN_PORT=443
ORISUN_TLS_ENABLED=true
ORISUN_TLS_CERT_PATH=/path/to/cert.pem

# Client configuration
ORISUN_CLIENT_TIMEOUT=30
ORISUN_CLIENT_RETRY_ATTEMPTS=3
ORISUN_CLIENT_RETRY_DELAY=1000

# Default stream
ORISUN_DEFAULT_STREAM=application-events

# Logging
ORISUN_LOG_LEVEL=info
ORISUN_LOG_CHANNEL=orisun
```

### Queue Configuration

```php
// config/queue.php
'connections' => [
    'orisun-events' => [
        'driver' => 'database',
        'table' => 'orisun_event_jobs',
        'queue' => 'orisun-events',
        'retry_after' => 300,
        'after_commit' => false,
    ],
],
```

### Logging Configuration

```php
// config/logging.php
'channels' => [
    'orisun' => [
        'driver' => 'daily',
        'path' => storage_path('logs/orisun.log'),
        'level' => env('ORISUN_LOG_LEVEL', 'info'),
        'days' => 14,
    ],
],
```

## Testing Examples

### Unit Testing

```php
use Tests\TestCase;
use Orisun\Client\OrisunClient;
use App\EventSourcing\UserAggregate;

class UserAggregateTest extends TestCase
{
    public function test_user_registration()
    {
        $orisun = $this->mock(OrisunClient::class);
        $user = new UserAggregate($orisun);
        
        $user->register('test@example.com', 'Test User', 'password');
        
        $this->assertEquals('test@example.com', $user->getEmail());
        $this->assertEquals('Test User', $user->getName());
        $this->assertFalse($user->isActive());
        $this->assertTrue($user->hasUncommittedEvents());
    }
}
```

### Integration Testing

```php
use Tests\TestCase;
use App\Services\UserEventService;

class UserEventServiceTest extends TestCase
{
    public function test_user_registration_flow()
    {
        $service = app(UserEventService::class);
        
        $result = $service->handleUserRegistration([
            'user_id' => '123',
            'email' => 'test@example.com',
            'name' => 'Test User'
        ]);
        
        $this->assertTrue($result);
        
        $events = $service->getUserEventHistory('123');
        $this->assertCount(1, $events);
        $this->assertEquals('user.registered', $events[0]->getEventType());
    }
}
```

## Performance Tips

1. **Batch Operations**: Use batch saving for multiple events
2. **Async Processing**: Use jobs for heavy event processing
3. **Stream Partitioning**: Use entity-specific streams for better performance
4. **Connection Pooling**: Configure gRPC connection pooling
5. **Caching**: Cache frequently accessed aggregates
6. **Monitoring**: Monitor event store performance and health

## Troubleshooting

### Common Issues

1. **Connection Errors**: Check host, port, and TLS configuration
2. **Timeout Issues**: Increase client timeout settings
3. **Memory Issues**: Use streaming for large event sets
4. **Concurrency Conflicts**: Implement proper retry logic
5. **Serialization Errors**: Ensure data is JSON serializable

### Debug Commands

```bash
# Test connection
php artisan orisun:test

# Check configuration
php artisan config:show orisun

# Monitor logs
tail -f storage/logs/orisun.log

# Process failed jobs
php artisan queue:retry all
```

## Next Steps

1. **Choose Your Pattern**: Start with the pattern that best fits your use case
2. **Implement Gradually**: Begin with simple event operations and evolve
3. **Monitor Performance**: Set up monitoring and alerting
4. **Scale Horizontally**: Use multiple workers for event processing
5. **Optimize Queries**: Design efficient event queries and projections

For more detailed information, refer to the main [README.md](../../README.md) file.