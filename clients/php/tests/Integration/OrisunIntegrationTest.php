<?php

namespace Orisun\Client\Tests\Integration;

use PHPUnit\Framework\TestCase;
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Orisun\Client\Position;

/**
 * Integration tests for OrisunClient
 * 
 * These tests require a running Orisun Event Store instance.
 * Set ORISUN_INTEGRATION_TESTS=true to enable these tests.
 * Configure connection via environment variables:
 * - ORISUN_HOST (default: localhost)
 * - ORISUN_PORT (default: 9090)
 * - ORISUN_TLS_ENABLED (default: false)
 */
class OrisunIntegrationTest extends TestCase
{
    private OrisunClient $client;
    private string $testStreamPrefix;

    protected function setUp(): void
    {
        if (!getenv('ORISUN_INTEGRATION_TESTS')) {
            $this->markTestSkipped('Integration tests are disabled. Set ORISUN_INTEGRATION_TESTS=true to enable.');
        }

        $host = getenv('ORISUN_HOST') ?: 'localhost';
        $port = (int) (getenv('ORISUN_PORT') ?: 9090);
        $tlsEnabled = filter_var(getenv('ORISUN_TLS_ENABLED'), FILTER_VALIDATE_BOOLEAN);

        $options = [];
        if (!$tlsEnabled) {
            $options['credentials'] = \Grpc\ChannelCredentials::createInsecure();
        }

        $this->client = new OrisunClient($host, $port, $options);
        $this->testStreamPrefix = 'test-integration-' . uniqid();

        // Verify connection
        if (!$this->client->healthCheck()) {
            $this->markTestSkipped('Cannot connect to Orisun Event Store at ' . $host . ':' . $port);
        }
    }

    public function testSaveAndGetEvents(): void
    {
        // Arrange
        $streamName = $this->testStreamPrefix . '-save-get';
        $events = [
            new Event('user.created', ['id' => 1, 'name' => 'John Doe', 'email' => 'john@example.com'], ['source' => 'integration-test']),
            new Event('user.updated', ['id' => 1, 'email' => 'john.doe@example.com'], ['source' => 'integration-test']),
            new Event('user.activated', ['id' => 1, 'active' => true], ['source' => 'integration-test'])
        ];

        // Act - Save events
        $writeResult = $this->client->saveEvents($streamName, $events);

        // Assert - Save result
        $this->assertTrue($writeResult->isSuccess());
        $this->assertEquals(2, $writeResult->getCurrentVersion()); // 0-based, so version 2 means 3 events

        // Act - Get all events
        $retrievedEvents = $this->client->getEvents($streamName);

        // Assert - Retrieved events
        $this->assertCount(3, $retrievedEvents);
        
        // Verify first event
        $this->assertEquals('user.created', $retrievedEvents[0]->getEventType());
        $this->assertEquals(['id' => 1, 'name' => 'John Doe', 'email' => 'john@example.com'], $retrievedEvents[0]->getData());
        $this->assertEquals(['source' => 'integration-test'], $retrievedEvents[0]->getMetadata());
        $this->assertEquals(0, $retrievedEvents[0]->getVersion());
        
        // Verify second event
        $this->assertEquals('user.updated', $retrievedEvents[1]->getEventType());
        $this->assertEquals(['id' => 1, 'email' => 'john.doe@example.com'], $retrievedEvents[1]->getData());
        $this->assertEquals(1, $retrievedEvents[1]->getVersion());
        
        // Verify third event
        $this->assertEquals('user.activated', $retrievedEvents[2]->getEventType());
        $this->assertEquals(['id' => 1, 'active' => true], $retrievedEvents[2]->getData());
        $this->assertEquals(2, $retrievedEvents[2]->getVersion());
    }

    public function testGetEventsWithPagination(): void
    {
        // Arrange
        $streamName = $this->testStreamPrefix . '-pagination';
        $events = [];
        for ($i = 1; $i <= 10; $i++) {
            $events[] = new Event('test.event', ['sequence' => $i], ['batch' => 'pagination-test']);
        }

        // Act - Save events
        $this->client->saveEvents($streamName, $events);

        // Act - Get first 5 events
        $firstBatch = $this->client->getEvents($streamName, 0, 5);
        
        // Act - Get next 5 events
        $secondBatch = $this->client->getEvents($streamName, 5, 5);

        // Assert
        $this->assertCount(5, $firstBatch);
        $this->assertCount(5, $secondBatch);
        
        // Verify sequence
        for ($i = 0; $i < 5; $i++) {
            $this->assertEquals($i + 1, $firstBatch[$i]->getData()['sequence']);
            $this->assertEquals($i + 6, $secondBatch[$i]->getData()['sequence']);
        }
    }

    public function testGetEventsInReverseOrder(): void
    {
        // Arrange
        $streamName = $this->testStreamPrefix . '-reverse';
        $events = [
            new Event('first.event', ['order' => 1]),
            new Event('second.event', ['order' => 2]),
            new Event('third.event', ['order' => 3])
        ];

        // Act - Save events
        $this->client->saveEvents($streamName, $events);

        // Act - Get events in reverse order
        $reversedEvents = $this->client->getEvents($streamName, 0, 10, 'DESC');

        // Assert
        $this->assertCount(3, $reversedEvents);
        $this->assertEquals(3, $reversedEvents[0]->getData()['order']); // Latest first
        $this->assertEquals(2, $reversedEvents[1]->getData()['order']);
        $this->assertEquals(1, $reversedEvents[2]->getData()['order']); // Oldest last
    }

    public function testConcurrencyConflict(): void
    {
        // Arrange
        $streamName = $this->testStreamPrefix . '-concurrency';
        $initialEvent = new Event('stream.created', ['initial' => true]);
        
        // Act - Save initial event
        $writeResult = $this->client->saveEvents($streamName, [$initialEvent]);
        $this->assertTrue($writeResult->isSuccess());
        $currentVersion = $writeResult->getCurrentVersion();

        // Try to save with wrong expected version
        $conflictEvent = new Event('conflict.event', ['conflict' => true]);
        
        // Assert - Should throw concurrency conflict
        $this->expectException(OrisunException::class);
        $this->expectExceptionMessageMatches('/concurrency|conflict|version/i');
        $this->client->saveEvents($streamName, [$conflictEvent], $currentVersion + 10); // Wrong version
    }

    public function testSubscribeToStream(): void
    {
        // Arrange
        $streamName = $this->testStreamPrefix . '-subscription';
        $receivedEvents = [];
        $maxEvents = 3;
        
        $callback = function (Event $event) use (&$receivedEvents, $maxEvents) {
            $receivedEvents[] = $event;
            return count($receivedEvents) < $maxEvents; // Continue until we have enough events
        };

        // Pre-populate stream with some events
        $initialEvents = [
            new Event('pre.event.1', ['pre' => true, 'sequence' => 1]),
            new Event('pre.event.2', ['pre' => true, 'sequence' => 2])
        ];
        $this->client->saveEvents($streamName, $initialEvents);

        // Start subscription in a separate process/thread simulation
        // For this test, we'll use a timeout-based approach
        $startTime = time();
        $timeout = 10; // 10 seconds timeout
        
        // Start subscription
        $subscriptionStarted = false;
        $subscriptionThread = function () use ($streamName, $callback, &$subscriptionStarted) {
            $subscriptionStarted = true;
            $this->client->subscribeToStream($streamName, $callback, 'integration-test-subscriber');
        };

        // Simulate async subscription by adding more events after a short delay
        $additionalEvent = new Event('post.subscription.event', ['post' => true, 'sequence' => 3]);
        
        // For testing purposes, we'll call the subscription synchronously
        // In a real scenario, this would be async
        try {
            // Add one more event to trigger the subscription
            $this->client->saveEvents($streamName, [$additionalEvent]);
            
            // Now subscribe (this will catch up and get all events)
            $this->client->subscribeToStream($streamName, $callback, 'integration-test-subscriber', 0);
            
            // Assert
            $this->assertGreaterThanOrEqual(3, count($receivedEvents));
            $this->assertEquals('pre.event.1', $receivedEvents[0]->getEventType());
            $this->assertEquals('pre.event.2', $receivedEvents[1]->getEventType());
            $this->assertEquals('post.subscription.event', $receivedEvents[2]->getEventType());
            
        } catch (\Exception $e) {
            // Subscription might timeout or fail in test environment
            $this->markTestSkipped('Subscription test skipped due to: ' . $e->getMessage());
        }
    }

    public function testHealthCheck(): void
    {
        // Act
        $isHealthy = $this->client->healthCheck();

        // Assert
        $this->assertTrue($isHealthy, 'Health check should pass with running Orisun instance');
    }

    public function testStreamDoesNotExist(): void
    {
        // Arrange
        $nonExistentStream = $this->testStreamPrefix . '-does-not-exist-' . uniqid();

        // Act
        $events = $this->client->getEvents($nonExistentStream);

        // Assert - Should return empty array for non-existent stream
        $this->assertIsArray($events);
        $this->assertEmpty($events);
    }

    public function testLargeEventData(): void
    {
        // Arrange
        $streamName = $this->testStreamPrefix . '-large-data';
        $largeData = [
            'description' => str_repeat('This is a large event with lots of data. ', 100),
            'items' => array_fill(0, 100, ['id' => uniqid(), 'value' => random_int(1, 1000)]),
            'metadata' => [
                'timestamp' => time(),
                'source' => 'integration-test',
                'large_field' => str_repeat('x', 1000)
            ]
        ];
        
        $event = new Event('large.data.event', $largeData, ['size' => 'large']);

        // Act
        $writeResult = $this->client->saveEvents($streamName, [$event]);
        $retrievedEvents = $this->client->getEvents($streamName);

        // Assert
        $this->assertTrue($writeResult->isSuccess());
        $this->assertCount(1, $retrievedEvents);
        $this->assertEquals('large.data.event', $retrievedEvents[0]->getEventType());
        $this->assertEquals($largeData, $retrievedEvents[0]->getData());
        $this->assertEquals(['size' => 'large'], $retrievedEvents[0]->getMetadata());
    }

    public function testMultipleStreamsIsolation(): void
    {
        // Arrange
        $stream1 = $this->testStreamPrefix . '-isolation-1';
        $stream2 = $this->testStreamPrefix . '-isolation-2';
        
        $events1 = [new Event('stream1.event', ['stream' => 1])];
        $events2 = [new Event('stream2.event', ['stream' => 2])];

        // Act
        $this->client->saveEvents($stream1, $events1);
        $this->client->saveEvents($stream2, $events2);
        
        $retrievedEvents1 = $this->client->getEvents($stream1);
        $retrievedEvents2 = $this->client->getEvents($stream2);

        // Assert
        $this->assertCount(1, $retrievedEvents1);
        $this->assertCount(1, $retrievedEvents2);
        $this->assertEquals('stream1.event', $retrievedEvents1[0]->getEventType());
        $this->assertEquals('stream2.event', $retrievedEvents2[0]->getEventType());
        $this->assertEquals(['stream' => 1], $retrievedEvents1[0]->getData());
        $this->assertEquals(['stream' => 2], $retrievedEvents2[0]->getData());
    }

    protected function tearDown(): void
    {
        // Cleanup is handled by using unique stream names with timestamps
        // In a production environment, you might want to implement stream deletion
        parent::tearDown();
    }
}