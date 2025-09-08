<?php

namespace Orisun\Client\Laravel\Facades;

use Illuminate\Support\Facades\Facade;
use Orisun\Client\OrisunClient;

/**
 * Laravel Facade for Orisun Event Store
 * 
 * @method static \Orisun\Client\WriteResult saveEvents(string $streamName, array $events, ?int $expectedVersion = null, ?\Eventstore\Query $subsetQuery = null)
 * @method static array getEvents(string $streamName, int $fromVersion = 0, int $maxCount = 100, ?\Eventstore\Query $query = null)
 * @method static void subscribeToStream(string $streamName, callable $eventHandler, string $subscriberName, int $afterVersion = -1, ?\Eventstore\Query $query = null)
 * @method static void subscribeToEventStore(callable $eventHandler, string $subscriberName, ?\Orisun\Client\Position $afterPosition = null, ?\Eventstore\Query $query = null)
 * 
 * @see \Orisun\Client\OrisunClient
 */
class Orisun extends Facade
{
    /**
     * Get the registered name of the component.
     */
    protected static function getFacadeAccessor(): string
    {
        return OrisunClient::class;
    }
}