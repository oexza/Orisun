<?php

namespace App\Jobs;

use Illuminate\Bus\Queueable;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Bus\Dispatchable;
use Illuminate\Queue\InteractsWithQueue;
use Illuminate\Queue\SerializesModels;
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Illuminate\Support\Facades\Log;
use Illuminate\Support\Facades\Mail;
use App\Mail\EventNotificationMail;
use Carbon\Carbon;

/**
 * Job to process events from a specific stream asynchronously
 */
class ProcessEventStreamJob implements ShouldQueue
{
    use Dispatchable, InteractsWithQueue, Queueable, SerializesModels;

    public int $tries = 3;
    public int $maxExceptions = 3;
    public int $timeout = 300; // 5 minutes

    public function __construct(
        private string $streamName,
        private int $fromVersion = 0,
        private int $batchSize = 100,
        private ?string $processorName = null
    ) {
        $this->processorName = $processorName ?? 'default-processor';
    }

    /**
     * Execute the job.
     */
    public function handle(OrisunClient $orisun): void
    {
        Log::info('Starting event stream processing', [
            'stream' => $this->streamName,
            'from_version' => $this->fromVersion,
            'batch_size' => $this->batchSize,
            'processor' => $this->processorName
        ]);

        try {
            $processedCount = 0;
            $currentVersion = $this->fromVersion;
            
            do {
                $events = $orisun->getEvents(
                    $this->streamName,
                    $currentVersion,
                    $this->batchSize
                );

                if (empty($events)) {
                    Log::info('No more events to process', [
                        'stream' => $this->streamName,
                        'last_version' => $currentVersion
                    ]);
                    break;
                }

                foreach ($events as $event) {
                    $this->processEvent($event);
                    $processedCount++;
                    $currentVersion = $event->getVersion() + 1;
                }

                // Log progress
                Log::info('Processed event batch', [
                    'stream' => $this->streamName,
                    'batch_count' => count($events),
                    'total_processed' => $processedCount,
                    'current_version' => $currentVersion
                ]);

                // Prevent memory issues by clearing processed events
                unset($events);
                
            } while (true);

            Log::info('Event stream processing completed', [
                'stream' => $this->streamName,
                'total_processed' => $processedCount,
                'final_version' => $currentVersion
            ]);

        } catch (OrisunException $e) {
            Log::error('Event stream processing failed', [
                'stream' => $this->streamName,
                'error' => $e->getMessage(),
                'processor' => $this->processorName
            ]);
            
            $this->fail($e);
        }
    }

    /**
     * Process a single event
     */
    private function processEvent(Event $event): void
    {
        $eventType = $event->getEventType();
        $data = $event->getDataAsArray();
        $metadata = $event->getMetadataAsArray();

        Log::debug('Processing event', [
            'event_id' => $event->getEventId(),
            'event_type' => $eventType,
            'stream' => $this->streamName,
            'version' => $event->getVersion()
        ]);

        try {
            // Route event to appropriate handler based on type
            match (true) {
                str_starts_with($eventType, 'user.') => $this->handleUserEvent($event),
                str_starts_with($eventType, 'order.') => $this->handleOrderEvent($event),
                str_starts_with($eventType, 'payment.') => $this->handlePaymentEvent($event),
                str_starts_with($eventType, 'notification.') => $this->handleNotificationEvent($event),
                default => $this->handleGenericEvent($event)
            };

        } catch (\Exception $e) {
            Log::error('Failed to process event', [
                'event_id' => $event->getEventId(),
                'event_type' => $eventType,
                'error' => $e->getMessage(),
                'trace' => $e->getTraceAsString()
            ]);
            
            // Re-throw to trigger job retry
            throw $e;
        }
    }

    /**
     * Handle user-related events
     */
    private function handleUserEvent(Event $event): void
    {
        $eventType = $event->getEventType();
        $data = $event->getDataAsArray();

        switch ($eventType) {
            case 'user.registered':
                $this->sendWelcomeEmail($data);
                $this->updateUserAnalytics($data, 'registration');
                break;
                
            case 'user.login.successful':
                $this->updateLastLoginTime($data);
                $this->checkSuspiciousActivity($data);
                break;
                
            case 'user.password.changed':
                $this->sendPasswordChangeNotification($data);
                break;
                
            case 'user.deleted':
                $this->cleanupUserData($data);
                break;
        }
    }

    /**
     * Handle order-related events
     */
    private function handleOrderEvent(Event $event): void
    {
        $eventType = $event->getEventType();
        $data = $event->getDataAsArray();

        switch ($eventType) {
            case 'order.created':
                $this->processNewOrder($data);
                $this->updateInventory($data);
                break;
                
            case 'order.cancelled':
                $this->restoreInventory($data);
                $this->processRefund($data);
                break;
                
            case 'order.shipped':
                $this->sendShippingNotification($data);
                break;
        }
    }

    /**
     * Handle payment-related events
     */
    private function handlePaymentEvent(Event $event): void
    {
        $eventType = $event->getEventType();
        $data = $event->getDataAsArray();

        switch ($eventType) {
            case 'payment.processed':
                $this->confirmPayment($data);
                $this->updateAccountingRecords($data);
                break;
                
            case 'payment.failed':
                $this->handlePaymentFailure($data);
                break;
                
            case 'payment.refunded':
                $this->processRefundAccounting($data);
                break;
        }
    }

    /**
     * Handle notification events
     */
    private function handleNotificationEvent(Event $event): void
    {
        $data = $event->getDataAsArray();
        
        if (isset($data['email']) && isset($data['message'])) {
            Mail::to($data['email'])->send(
                new EventNotificationMail($data['message'], $data)
            );
        }
    }

    /**
     * Handle generic events
     */
    private function handleGenericEvent(Event $event): void
    {
        Log::info('Processing generic event', [
            'event_id' => $event->getEventId(),
            'event_type' => $event->getEventType(),
            'data' => $event->getDataAsArray()
        ]);
        
        // Add custom logic for unhandled event types
    }

    // Example handler methods (implement based on your business logic)
    
    private function sendWelcomeEmail(array $data): void
    {
        // Implementation for welcome email
        Log::info('Sending welcome email', ['user_id' => $data['user_id'] ?? null]);
    }

    private function updateUserAnalytics(array $data, string $action): void
    {
        // Implementation for analytics update
        Log::info('Updating user analytics', [
            'user_id' => $data['user_id'] ?? null,
            'action' => $action
        ]);
    }

    private function updateLastLoginTime(array $data): void
    {
        // Implementation for login time update
        Log::info('Updating last login time', ['user_id' => $data['user_id'] ?? null]);
    }

    private function checkSuspiciousActivity(array $data): void
    {
        // Implementation for security checks
        Log::info('Checking for suspicious activity', ['user_id' => $data['user_id'] ?? null]);
    }

    private function sendPasswordChangeNotification(array $data): void
    {
        // Implementation for password change notification
        Log::info('Sending password change notification', ['user_id' => $data['user_id'] ?? null]);
    }

    private function cleanupUserData(array $data): void
    {
        // Implementation for user data cleanup
        Log::info('Cleaning up user data', ['user_id' => $data['user_id'] ?? null]);
    }

    private function processNewOrder(array $data): void
    {
        // Implementation for new order processing
        Log::info('Processing new order', ['order_id' => $data['order_id'] ?? null]);
    }

    private function updateInventory(array $data): void
    {
        // Implementation for inventory update
        Log::info('Updating inventory', ['order_id' => $data['order_id'] ?? null]);
    }

    private function restoreInventory(array $data): void
    {
        // Implementation for inventory restoration
        Log::info('Restoring inventory', ['order_id' => $data['order_id'] ?? null]);
    }

    private function processRefund(array $data): void
    {
        // Implementation for refund processing
        Log::info('Processing refund', ['order_id' => $data['order_id'] ?? null]);
    }

    private function sendShippingNotification(array $data): void
    {
        // Implementation for shipping notification
        Log::info('Sending shipping notification', ['order_id' => $data['order_id'] ?? null]);
    }

    private function confirmPayment(array $data): void
    {
        // Implementation for payment confirmation
        Log::info('Confirming payment', ['payment_id' => $data['payment_id'] ?? null]);
    }

    private function updateAccountingRecords(array $data): void
    {
        // Implementation for accounting update
        Log::info('Updating accounting records', ['payment_id' => $data['payment_id'] ?? null]);
    }

    private function handlePaymentFailure(array $data): void
    {
        // Implementation for payment failure handling
        Log::info('Handling payment failure', ['payment_id' => $data['payment_id'] ?? null]);
    }

    private function processRefundAccounting(array $data): void
    {
        // Implementation for refund accounting
        Log::info('Processing refund accounting', ['payment_id' => $data['payment_id'] ?? null]);
    }

    /**
     * Handle job failure
     */
    public function failed(\Throwable $exception): void
    {
        Log::error('Event stream processing job failed permanently', [
            'stream' => $this->streamName,
            'from_version' => $this->fromVersion,
            'processor' => $this->processorName,
            'error' => $exception->getMessage(),
            'trace' => $exception->getTraceAsString()
        ]);

        // Optionally send alert to administrators
        // Mail::to(config('app.admin_email'))->send(
        //     new JobFailedMail($this, $exception)
        // );
    }
}