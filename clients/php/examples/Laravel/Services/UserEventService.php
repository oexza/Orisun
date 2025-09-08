<?php

namespace App\Services;

use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Illuminate\Support\Facades\Log;
use App\Models\User;
use Carbon\Carbon;

/**
 * Example service demonstrating user-related event patterns
 */
class UserEventService
{
    private const STREAM_PREFIX = 'user-';
    
    public function __construct(
        private OrisunClient $orisun
    ) {}

    /**
     * Handle user registration event
     */
    public function handleUserRegistration(User $user, array $metadata = []): void
    {
        $event = new Event(
            'user.registered',
            [
                'user_id' => $user->id,
                'email' => $user->email,
                'name' => $user->name,
                'email_verified_at' => $user->email_verified_at?->toISOString(),
                'created_at' => $user->created_at->toISOString(),
            ],
            array_merge([
                'source' => 'user_registration',
                'timestamp' => Carbon::now()->toISOString(),
                'user_agent' => request()->userAgent(),
                'ip_address' => request()->ip(),
            ], $metadata)
        );

        try {
            $this->orisun->saveEvents(
                $this->getUserStreamName($user->id),
                [$event]
            );

            Log::info('User registration event saved', [
                'user_id' => $user->id,
                'event_id' => $event->getEventId()
            ]);
        } catch (OrisunException $e) {
            Log::error('Failed to save user registration event', [
                'user_id' => $user->id,
                'error' => $e->getMessage()
            ]);
            throw $e;
        }
    }

    /**
     * Handle user profile update event
     */
    public function handleUserProfileUpdate(User $user, array $changes, array $metadata = []): void
    {
        $event = new Event(
            'user.profile.updated',
            [
                'user_id' => $user->id,
                'changes' => $changes,
                'updated_at' => $user->updated_at->toISOString(),
            ],
            array_merge([
                'source' => 'profile_update',
                'timestamp' => Carbon::now()->toISOString(),
                'changed_fields' => array_keys($changes),
            ], $metadata)
        );

        try {
            $this->orisun->saveEvents(
                $this->getUserStreamName($user->id),
                [$event]
            );

            Log::info('User profile update event saved', [
                'user_id' => $user->id,
                'changed_fields' => array_keys($changes)
            ]);
        } catch (OrisunException $e) {
            Log::error('Failed to save user profile update event', [
                'user_id' => $user->id,
                'error' => $e->getMessage()
            ]);
            throw $e;
        }
    }

    /**
     * Handle user login event
     */
    public function handleUserLogin(User $user, bool $successful = true, array $metadata = []): void
    {
        $eventType = $successful ? 'user.login.successful' : 'user.login.failed';
        
        $event = new Event(
            $eventType,
            [
                'user_id' => $user->id,
                'email' => $user->email,
                'successful' => $successful,
                'login_at' => Carbon::now()->toISOString(),
            ],
            array_merge([
                'source' => 'authentication',
                'timestamp' => Carbon::now()->toISOString(),
                'user_agent' => request()->userAgent(),
                'ip_address' => request()->ip(),
            ], $metadata)
        );

        try {
            $this->orisun->saveEvents(
                $this->getUserStreamName($user->id),
                [$event]
            );

            Log::info('User login event saved', [
                'user_id' => $user->id,
                'successful' => $successful
            ]);
        } catch (OrisunException $e) {
            Log::error('Failed to save user login event', [
                'user_id' => $user->id,
                'error' => $e->getMessage()
            ]);
            // Don't throw for login events to avoid disrupting authentication
        }
    }

    /**
     * Handle user password change event
     */
    public function handlePasswordChange(User $user, array $metadata = []): void
    {
        $event = new Event(
            'user.password.changed',
            [
                'user_id' => $user->id,
                'changed_at' => Carbon::now()->toISOString(),
            ],
            array_merge([
                'source' => 'password_change',
                'timestamp' => Carbon::now()->toISOString(),
                'ip_address' => request()->ip(),
            ], $metadata)
        );

        try {
            $this->orisun->saveEvents(
                $this->getUserStreamName($user->id),
                [$event]
            );

            Log::info('User password change event saved', [
                'user_id' => $user->id
            ]);
        } catch (OrisunException $e) {
            Log::error('Failed to save password change event', [
                'user_id' => $user->id,
                'error' => $e->getMessage()
            ]);
            throw $e;
        }
    }

    /**
     * Handle user deletion event
     */
    public function handleUserDeletion(int $userId, array $userData, array $metadata = []): void
    {
        $event = new Event(
            'user.deleted',
            [
                'user_id' => $userId,
                'deleted_user_data' => $userData,
                'deleted_at' => Carbon::now()->toISOString(),
            ],
            array_merge([
                'source' => 'user_deletion',
                'timestamp' => Carbon::now()->toISOString(),
                'admin_user_id' => auth()->id(),
            ], $metadata)
        );

        try {
            $this->orisun->saveEvents(
                $this->getUserStreamName($userId),
                [$event]
            );

            Log::info('User deletion event saved', [
                'user_id' => $userId
            ]);
        } catch (OrisunException $e) {
            Log::error('Failed to save user deletion event', [
                'user_id' => $userId,
                'error' => $e->getMessage()
            ]);
            throw $e;
        }
    }

    /**
     * Get user event history
     */
    public function getUserEventHistory(int $userId, int $fromVersion = 0, int $count = 100): array
    {
        try {
            $events = $this->orisun->getEvents(
                $this->getUserStreamName($userId),
                $fromVersion,
                $count
            );

            return array_map(function (Event $event) {
                return [
                    'event_id' => $event->getEventId(),
                    'event_type' => $event->getEventType(),
                    'data' => $event->getDataAsArray(),
                    'metadata' => $event->getMetadataAsArray(),
                    'version' => $event->getVersion(),
                    'date_created' => $event->getDateCreated(),
                    'position' => $event->getPosition()?->toString()
                ];
            }, $events);

        } catch (OrisunException $e) {
            Log::error('Failed to get user event history', [
                'user_id' => $userId,
                'error' => $e->getMessage()
            ]);
            throw $e;
        }
    }

    /**
     * Subscribe to user events for real-time processing
     */
    public function subscribeToUserEvents(callable $eventHandler, string $subscriberName = 'user-event-processor'): void
    {
        try {
            $this->orisun->subscribeToEventStore(
                function (Event $event) use ($eventHandler) {
                    // Only process user-related events
                    if (str_starts_with($event->getStreamId(), self::STREAM_PREFIX)) {
                        $eventHandler($event);
                    }
                },
                $subscriberName
            );
        } catch (OrisunException $e) {
            Log::error('Failed to subscribe to user events', [
                'subscriber' => $subscriberName,
                'error' => $e->getMessage()
            ]);
            throw $e;
        }
    }

    /**
     * Batch save multiple user events
     */
    public function saveUserEventBatch(int $userId, array $events): void
    {
        if (empty($events)) {
            return;
        }

        try {
            $this->orisun->saveEvents(
                $this->getUserStreamName($userId),
                $events
            );

            Log::info('User event batch saved', [
                'user_id' => $userId,
                'event_count' => count($events)
            ]);
        } catch (OrisunException $e) {
            Log::error('Failed to save user event batch', [
                'user_id' => $userId,
                'event_count' => count($events),
                'error' => $e->getMessage()
            ]);
            throw $e;
        }
    }

    /**
     * Get user stream name
     */
    private function getUserStreamName(int $userId): string
    {
        return self::STREAM_PREFIX . $userId;
    }

    /**
     * Create a user event with common metadata
     */
    private function createUserEvent(string $eventType, array $data, array $metadata = []): Event
    {
        return new Event(
            $eventType,
            $data,
            array_merge([
                'timestamp' => Carbon::now()->toISOString(),
                'source' => 'user_service',
            ], $metadata)
        );
    }
}