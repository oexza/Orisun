<?php

namespace App\Listeners;

use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Queue\InteractsWithQueue;
use Illuminate\Support\Facades\Log;
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Carbon\Carbon;

/**
 * Event listener that automatically saves Laravel events to Orisun Event Store
 */
class EventStoreListener implements ShouldQueue
{
    use InteractsWithQueue;

    public int $tries = 3;
    public int $timeout = 60;

    public function __construct(
        private OrisunClient $orisun
    ) {}

    /**
     * Handle the event.
     */
    public function handle(object $event): void
    {
        try {
            // Convert Laravel event to Orisun event
            $orisunEvent = $this->convertToOrisunEvent($event);
            
            if ($orisunEvent) {
                // Determine stream name based on event type
                $streamName = $this->getStreamName($event);
                
                // Save to event store
                $result = $this->orisun->saveEvents($streamName, [$orisunEvent]);
                
                Log::info('Event saved to Orisun Event Store', [
                    'event_type' => get_class($event),
                    'stream_name' => $streamName,
                    'event_id' => $orisunEvent->getEventId(),
                    'log_position' => $result->getLogPosition()
                ]);
            }
            
        } catch (OrisunException $e) {
            Log::error('Failed to save event to Orisun Event Store', [
                'event_type' => get_class($event),
                'error' => $e->getMessage(),
                'grpc_code' => $e->getGrpcCode()
            ]);
            
            // Re-throw to trigger retry mechanism
            throw $e;
        } catch (\Exception $e) {
            Log::error('Unexpected error saving event to Orisun Event Store', [
                'event_type' => get_class($event),
                'error' => $e->getMessage()
            ]);
            
            throw $e;
        }
    }

    /**
     * Convert Laravel event to Orisun Event
     */
    private function convertToOrisunEvent(object $event): ?Event
    {
        // Skip certain Laravel framework events
        if ($this->shouldSkipEvent($event)) {
            return null;
        }

        $eventType = $this->getEventType($event);
        $data = $this->extractEventData($event);
        $metadata = $this->extractEventMetadata($event);

        return Event::create(
            $eventType,
            $data,
            $metadata
        );
    }

    /**
     * Determine if event should be skipped
     */
    private function shouldSkipEvent(object $event): bool
    {
        $skipPatterns = [
            'Illuminate\\',
            'Laravel\\',
            'Spatie\\',
            'Barryvdh\\',
        ];

        $eventClass = get_class($event);
        
        foreach ($skipPatterns as $pattern) {
            if (str_starts_with($eventClass, $pattern)) {
                return true;
            }
        }

        return false;
    }

    /**
     * Get event type from Laravel event
     */
    private function getEventType(object $event): string
    {
        $className = get_class($event);
        
        // Convert class name to event type
        // Example: App\Events\UserRegistered -> user.registered
        $eventType = str_replace(['App\\Events\\', '\\'], ['', '.'], $className);
        $eventType = strtolower(preg_replace('/(?<!^)[A-Z]/', '.$0', $eventType));
        
        return $eventType;
    }

    /**
     * Extract event data
     */
    private function extractEventData(object $event): array
    {
        $data = [];
        
        // Use reflection to get public properties
        $reflection = new \ReflectionClass($event);
        $properties = $reflection->getProperties(\ReflectionProperty::IS_PUBLIC);
        
        foreach ($properties as $property) {
            $name = $property->getName();
            $value = $property->getValue($event);
            
            // Convert objects to arrays for serialization
            if (is_object($value)) {
                if (method_exists($value, 'toArray')) {
                    $data[$name] = $value->toArray();
                } elseif (method_exists($value, '__toString')) {
                    $data[$name] = (string) $value;
                } else {
                    $data[$name] = get_class($value);
                }
            } else {
                $data[$name] = $value;
            }
        }
        
        // Also check for custom data methods
        if (method_exists($event, 'getEventData')) {
            $customData = $event->getEventData();
            if (is_array($customData)) {
                $data = array_merge($data, $customData);
            }
        }
        
        return $data;
    }

    /**
     * Extract event metadata
     */
    private function extractEventMetadata(object $event): array
    {
        $metadata = [
            'laravel_event_class' => get_class($event),
            'timestamp' => Carbon::now()->toISOString(),
            'application' => config('app.name', 'Laravel'),
            'environment' => config('app.env', 'production'),
        ];
        
        // Add request context if available
        if (app()->bound('request')) {
            $request = request();
            $metadata['request'] = [
                'ip' => $request->ip(),
                'user_agent' => $request->userAgent(),
                'url' => $request->fullUrl(),
                'method' => $request->method(),
            ];
            
            // Add authenticated user info if available
            if ($request->user()) {
                $metadata['user'] = [
                    'id' => $request->user()->id,
                    'email' => $request->user()->email ?? null,
                ];
            }
        }
        
        // Add custom metadata if event supports it
        if (method_exists($event, 'getEventMetadata')) {
            $customMetadata = $event->getEventMetadata();
            if (is_array($customMetadata)) {
                $metadata = array_merge($metadata, $customMetadata);
            }
        }
        
        return $metadata;
    }

    /**
     * Determine stream name for the event
     */
    private function getStreamName(object $event): string
    {
        // Check if event has custom stream name method
        if (method_exists($event, 'getStreamName')) {
            return $event->getStreamName();
        }
        
        // Extract entity from event data for entity-specific streams
        $data = $this->extractEventData($event);
        
        // Look for common entity identifiers
        if (isset($data['user_id']) || isset($data['user'])) {
            $userId = $data['user_id'] ?? $data['user']['id'] ?? $data['user'];
            return "user-{$userId}";
        }
        
        if (isset($data['order_id']) || isset($data['order'])) {
            $orderId = $data['order_id'] ?? $data['order']['id'] ?? $data['order'];
            return "order-{$orderId}";
        }
        
        if (isset($data['payment_id']) || isset($data['payment'])) {
            $paymentId = $data['payment_id'] ?? $data['payment']['id'] ?? $data['payment'];
            return "payment-{$paymentId}";
        }
        
        // Default to event type based stream
        $eventType = $this->getEventType($event);
        $category = explode('.', $eventType)[0];
        
        return "{$category}-events";
    }

    /**
     * Handle failed job
     */
    public function failed(object $event, \Throwable $exception): void
    {
        Log::error('EventStoreListener failed permanently', [
            'event_type' => get_class($event),
            'error' => $exception->getMessage(),
            'trace' => $exception->getTraceAsString()
        ]);
    }
}