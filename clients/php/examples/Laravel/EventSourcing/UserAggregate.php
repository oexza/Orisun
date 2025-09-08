<?php

namespace App\EventSourcing;

use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Illuminate\Support\Facades\Log;
use Carbon\Carbon;
use Ramsey\Uuid\Uuid;

/**
 * Example User Aggregate implementing Event Sourcing patterns
 * This demonstrates how to build aggregates that store state through events
 */
class UserAggregate
{
    private string $id;
    private string $email;
    private string $name;
    private bool $isActive;
    private ?Carbon $lastLoginAt;
    private array $roles;
    private int $version;
    private array $uncommittedEvents;

    public function __construct(
        private OrisunClient $orisun,
        ?string $id = null
    ) {
        $this->id = $id ?? Uuid::uuid4()->toString();
        $this->email = '';
        $this->name = '';
        $this->isActive = false;
        $this->lastLoginAt = null;
        $this->roles = [];
        $this->version = 0;
        $this->uncommittedEvents = [];
    }

    /**
     * Create a new user
     */
    public function register(string $email, string $name, string $password): void
    {
        if ($this->version > 0) {
            throw new \InvalidArgumentException('User already exists');
        }

        $this->applyEvent(Event::create(
            'user.registered',
            [
                'user_id' => $this->id,
                'email' => $email,
                'name' => $name,
                'password_hash' => password_hash($password, PASSWORD_DEFAULT),
                'registered_at' => Carbon::now()->toISOString()
            ],
            [
                'aggregate_type' => 'user',
                'aggregate_id' => $this->id,
                'command' => 'register_user'
            ]
        ));
    }

    /**
     * Activate user account
     */
    public function activate(): void
    {
        if ($this->isActive) {
            throw new \InvalidArgumentException('User is already active');
        }

        $this->applyEvent(Event::create(
            'user.activated',
            [
                'user_id' => $this->id,
                'activated_at' => Carbon::now()->toISOString()
            ],
            [
                'aggregate_type' => 'user',
                'aggregate_id' => $this->id,
                'command' => 'activate_user'
            ]
        ));
    }

    /**
     * Deactivate user account
     */
    public function deactivate(string $reason): void
    {
        if (!$this->isActive) {
            throw new \InvalidArgumentException('User is already inactive');
        }

        $this->applyEvent(Event::create(
            'user.deactivated',
            [
                'user_id' => $this->id,
                'reason' => $reason,
                'deactivated_at' => Carbon::now()->toISOString()
            ],
            [
                'aggregate_type' => 'user',
                'aggregate_id' => $this->id,
                'command' => 'deactivate_user'
            ]
        ));
    }

    /**
     * Update user profile
     */
    public function updateProfile(string $name, ?string $email = null): void
    {
        if (!$this->isActive) {
            throw new \InvalidArgumentException('Cannot update inactive user');
        }

        $changes = [];
        if ($name !== $this->name) {
            $changes['name'] = ['old' => $this->name, 'new' => $name];
        }
        if ($email && $email !== $this->email) {
            $changes['email'] = ['old' => $this->email, 'new' => $email];
        }

        if (empty($changes)) {
            return; // No changes to apply
        }

        $this->applyEvent(Event::create(
            'user.profile.updated',
            [
                'user_id' => $this->id,
                'changes' => $changes,
                'updated_at' => Carbon::now()->toISOString()
            ],
            [
                'aggregate_type' => 'user',
                'aggregate_id' => $this->id,
                'command' => 'update_profile'
            ]
        ));
    }

    /**
     * Record user login
     */
    public function recordLogin(string $ipAddress, string $userAgent): void
    {
        if (!$this->isActive) {
            throw new \InvalidArgumentException('Inactive user cannot login');
        }

        $this->applyEvent(Event::create(
            'user.login.successful',
            [
                'user_id' => $this->id,
                'ip_address' => $ipAddress,
                'user_agent' => $userAgent,
                'login_at' => Carbon::now()->toISOString()
            ],
            [
                'aggregate_type' => 'user',
                'aggregate_id' => $this->id,
                'command' => 'record_login'
            ]
        ));
    }

    /**
     * Assign role to user
     */
    public function assignRole(string $role): void
    {
        if (!$this->isActive) {
            throw new \InvalidArgumentException('Cannot assign role to inactive user');
        }

        if (in_array($role, $this->roles)) {
            throw new \InvalidArgumentException('User already has this role');
        }

        $this->applyEvent(Event::create(
            'user.role.assigned',
            [
                'user_id' => $this->id,
                'role' => $role,
                'assigned_at' => Carbon::now()->toISOString()
            ],
            [
                'aggregate_type' => 'user',
                'aggregate_id' => $this->id,
                'command' => 'assign_role'
            ]
        ));
    }

    /**
     * Remove role from user
     */
    public function removeRole(string $role): void
    {
        if (!in_array($role, $this->roles)) {
            throw new \InvalidArgumentException('User does not have this role');
        }

        $this->applyEvent(Event::create(
            'user.role.removed',
            [
                'user_id' => $this->id,
                'role' => $role,
                'removed_at' => Carbon::now()->toISOString()
            ],
            [
                'aggregate_type' => 'user',
                'aggregate_id' => $this->id,
                'command' => 'remove_role'
            ]
        ));
    }

    /**
     * Apply an event to the aggregate
     */
    private function applyEvent(Event $event): void
    {
        // Apply the event to update internal state
        $this->when($event);
        
        // Add to uncommitted events
        $this->uncommittedEvents[] = $event;
        
        // Increment version
        $this->version++;
    }

    /**
     * Apply event to internal state (event handler)
     */
    private function when(Event $event): void
    {
        $eventType = $event->getEventType();
        $data = $event->getDataAsArray();

        match ($eventType) {
            'user.registered' => $this->whenUserRegistered($data),
            'user.activated' => $this->whenUserActivated($data),
            'user.deactivated' => $this->whenUserDeactivated($data),
            'user.profile.updated' => $this->whenProfileUpdated($data),
            'user.login.successful' => $this->whenLoginSuccessful($data),
            'user.role.assigned' => $this->whenRoleAssigned($data),
            'user.role.removed' => $this->whenRoleRemoved($data),
            default => Log::warning('Unknown event type in UserAggregate', [
                'event_type' => $eventType,
                'aggregate_id' => $this->id
            ])
        };
    }

    private function whenUserRegistered(array $data): void
    {
        $this->email = $data['email'];
        $this->name = $data['name'];
        $this->isActive = false; // Users start inactive until activated
    }

    private function whenUserActivated(array $data): void
    {
        $this->isActive = true;
    }

    private function whenUserDeactivated(array $data): void
    {
        $this->isActive = false;
    }

    private function whenProfileUpdated(array $data): void
    {
        $changes = $data['changes'];
        
        if (isset($changes['name'])) {
            $this->name = $changes['name']['new'];
        }
        
        if (isset($changes['email'])) {
            $this->email = $changes['email']['new'];
        }
    }

    private function whenLoginSuccessful(array $data): void
    {
        $this->lastLoginAt = Carbon::parse($data['login_at']);
    }

    private function whenRoleAssigned(array $data): void
    {
        $this->roles[] = $data['role'];
    }

    private function whenRoleRemoved(array $data): void
    {
        $this->roles = array_values(array_filter(
            $this->roles,
            fn($role) => $role !== $data['role']
        ));
    }

    /**
     * Save uncommitted events to the event store
     */
    public function save(): void
    {
        if (empty($this->uncommittedEvents)) {
            return;
        }

        try {
            $streamName = "user-{$this->id}";
            $expectedVersion = $this->version - count($this->uncommittedEvents);
            
            $result = $this->orisun->saveEvents(
                $streamName,
                $this->uncommittedEvents,
                $expectedVersion
            );

            Log::info('User aggregate events saved', [
                'aggregate_id' => $this->id,
                'stream_name' => $streamName,
                'events_count' => count($this->uncommittedEvents),
                'new_version' => $this->version,
                'log_position' => $result->getLogPosition()
            ]);

            // Clear uncommitted events
            $this->uncommittedEvents = [];

        } catch (OrisunException $e) {
            Log::error('Failed to save user aggregate events', [
                'aggregate_id' => $this->id,
                'error' => $e->getMessage(),
                'grpc_code' => $e->getGrpcCode()
            ]);
            
            throw $e;
        }
    }

    /**
     * Load aggregate from event store
     */
    public static function load(OrisunClient $orisun, string $id): self
    {
        $aggregate = new self($orisun, $id);
        $streamName = "user-{$id}";

        try {
            $events = $orisun->getEvents($streamName);
            
            foreach ($events as $event) {
                $aggregate->when($event);
                $aggregate->version++;
            }

            Log::info('User aggregate loaded from event store', [
                'aggregate_id' => $id,
                'stream_name' => $streamName,
                'events_count' => count($events),
                'version' => $aggregate->version
            ]);

        } catch (OrisunException $e) {
            if ($e->isStreamNotFound()) {
                // Stream doesn't exist, return new aggregate
                Log::info('User aggregate stream not found, returning new aggregate', [
                    'aggregate_id' => $id
                ]);
            } else {
                Log::error('Failed to load user aggregate', [
                    'aggregate_id' => $id,
                    'error' => $e->getMessage()
                ]);
                throw $e;
            }
        }

        return $aggregate;
    }

    // Getters
    public function getId(): string { return $this->id; }
    public function getEmail(): string { return $this->email; }
    public function getName(): string { return $this->name; }
    public function isActive(): bool { return $this->isActive; }
    public function getLastLoginAt(): ?Carbon { return $this->lastLoginAt; }
    public function getRoles(): array { return $this->roles; }
    public function getVersion(): int { return $this->version; }
    public function hasRole(string $role): bool { return in_array($role, $this->roles); }
    public function getUncommittedEvents(): array { return $this->uncommittedEvents; }
    public function hasUncommittedEvents(): bool { return !empty($this->uncommittedEvents); }
}