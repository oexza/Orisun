<?php

namespace Orisun\Client\Laravel\Commands;

use Illuminate\Console\Command;
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;

/**
 * Artisan command to test Orisun Event Store connection
 */
class OrisunTestCommand extends Command
{
    /**
     * The name and signature of the console command.
     */
    protected $signature = 'orisun:test 
                            {--stream=test-stream : The stream name to use for testing}
                            {--events=5 : Number of test events to create}
                            {--cleanup : Clean up test events after testing}';

    /**
     * The console command description.
     */
    protected $description = 'Test connection to Orisun Event Store';

    /**
     * Execute the console command.
     */
    public function handle(OrisunClient $orisun): int
    {
        $streamName = $this->option('stream');
        $eventCount = (int) $this->option('events');
        $cleanup = $this->option('cleanup');

        $this->info('Testing Orisun Event Store connection...');
        $this->newLine();

        try {
            // Test 1: Save events
            $this->info("📝 Test 1: Saving {$eventCount} events to stream '{$streamName}'...");
            
            $events = [];
            for ($i = 1; $i <= $eventCount; $i++) {
                $events[] = new Event(
                    'test.event.created',
                    ['message' => "Test event #{$i}", 'timestamp' => time()],
                    ['test' => true, 'command' => 'orisun:test']
                );
            }

            $writeResult = $orisun->saveEvents($streamName, $events);
            $this->info("✅ Successfully saved {$eventCount} events");
            
            if ($writeResult->getLogPosition()) {
                $this->info("   Log position: {$writeResult->getLogPosition()}");
            }

            // Test 2: Read events
            $this->info("\n📖 Test 2: Reading events from stream '{$streamName}'...");
            
            $retrievedEvents = $orisun->getEvents($streamName, 0, $eventCount + 10);
            $this->info("✅ Successfully retrieved " . count($retrievedEvents) . " events");

            // Display some event details
            if (!empty($retrievedEvents)) {
                $this->info("\n📋 Event details:");
                foreach (array_slice($retrievedEvents, -min(3, count($retrievedEvents))) as $event) {
                    $this->line("   - {$event->getEventType()} (ID: {$event->getEventId()})");
                    $data = $event->getDataAsArray();
                    if (isset($data['message'])) {
                        $this->line("     Message: {$data['message']}");
                    }
                }
            }

            // Test 3: Stream subscription (brief test)
            $this->info("\n🔄 Test 3: Testing stream subscription...");
            
            $subscriptionEvents = [];
            $maxEvents = 3;
            
            try {
                $orisun->subscribeToStream(
                    $streamName,
                    function (Event $event) use (&$subscriptionEvents, $maxEvents) {
                        $subscriptionEvents[] = $event;
                        if (count($subscriptionEvents) >= $maxEvents) {
                            throw new \Exception('Max events reached for test');
                        }
                    },
                    'test-subscriber',
                    -1
                );
            } catch (\Exception $e) {
                if (!str_contains($e->getMessage(), 'Max events reached')) {
                    throw $e;
                }
            }
            
            $this->info("✅ Subscription test completed (received " . count($subscriptionEvents) . " events)");

            // Cleanup if requested
            if ($cleanup) {
                $this->info("\n🧹 Cleaning up test data...");
                // Note: Orisun doesn't typically support event deletion
                // This is just a placeholder for cleanup logic
                $this->warn("   Note: Event deletion not implemented (events are immutable)");
            }

            $this->newLine();
            $this->info('🎉 All tests passed! Orisun Event Store is working correctly.');
            
            return self::SUCCESS;

        } catch (OrisunException $e) {
            $this->error("❌ Orisun Error: {$e->getMessage()}");
            
            if ($e->isConcurrencyConflict()) {
                $this->warn('   This appears to be a concurrency conflict.');
            } elseif ($e->isConnectionError()) {
                $this->warn('   This appears to be a connection error. Check your configuration.');
            }
            
            return self::FAILURE;
            
        } catch (\Exception $e) {
            $this->error("❌ Unexpected Error: {$e->getMessage()}");
            
            if ($this->output->isVerbose()) {
                $this->error($e->getTraceAsString());
            }
            
            return self::FAILURE;
        }
    }
}