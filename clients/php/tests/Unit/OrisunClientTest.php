<?php

namespace Orisun\Client\Tests\Unit;

use PHPUnit\Framework\TestCase;
use PHPUnit\Framework\MockObject\MockObject;
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Orisun\Client\Position;
use Eventstore\EventStoreClient;
use Eventstore\SaveEventsRequest;
use Eventstore\WriteResult;
use Eventstore\GetEventsRequest;
use Eventstore\GetEventsResponse;
use Eventstore\CatchUpSubscribeToStreamRequest;
use Eventstore\Event as GrpcEvent;
use Grpc\UnaryCall;
use Grpc\ServerStreamingCall;
use Psr\Log\LoggerInterface;

class OrisunClientTest extends TestCase
{
    private MockObject $mockGrpcClient;
    private MockObject $mockLogger;
    private OrisunClient $client;

    protected function setUp(): void
    {
        $this->mockGrpcClient = $this->createMock(EventStoreClient::class);
        $this->mockLogger = $this->createMock(LoggerInterface::class);
        
        $this->client = new OrisunClient(
            'localhost:50051',
            ['timeout' => 5000],
            $this->mockLogger
        );
        
        // Use reflection to inject the mock gRPC client
        $reflection = new \ReflectionClass($this->client);
        $property = $reflection->getProperty('grpcClient');
        $property->setAccessible(true);
        $property->setValue($this->client, $this->mockGrpcClient);
    }

    public function testSaveEventsSuccess(): void
    {
        // Arrange
        $events = [new Event('test.event', '{}', '{}', 'test-id')];
        $mockCall = $this->createMock(UnaryCall::class);
        $mockWriteResult = $this->createMock(WriteResult::class);
        $mockPosition = $this->createMock(\Eventstore\Position::class);
        $mockWriteResult->method('getLogPosition')->willReturn($mockPosition);
        $mockWriteResult->method('getNewStreamVersion')->willReturn(1);
        $mockStatus = (object)['code' => \Grpc\STATUS_OK];
        
        $mockCall->expects($this->once())
            ->method('wait')
            ->willReturn([$mockWriteResult, $mockStatus]);

        $this->mockGrpcClient->expects($this->once())
            ->method('SaveEvents')
            ->willReturn($mockCall);

        // Act
        $result = $this->client->saveEvents('test-stream', $events);

        // Assert
        $this->assertInstanceOf(\Orisun\Client\WriteResult::class, $result);
    }

    public function testSaveEventsWithConcurrencyConflict(): void
    {
        // Arrange
        $events = [new Event('test.event', '{}', '{}', 'test-id')];
        $mockCall = $this->createMock(UnaryCall::class);
        $mockWriteResult = $this->createMock(WriteResult::class);
        $mockStatus = (object)['code' => 9, 'details' => 'Version conflict']; // FAILED_PRECONDITION
        
        $mockCall->expects($this->once())
            ->method('wait')
            ->willReturn([$mockWriteResult, $mockStatus]);

        $this->mockGrpcClient->expects($this->once())
            ->method('SaveEvents')
            ->willReturn($mockCall);

        // Act & Assert
        $this->expectException(OrisunException::class);
        $this->expectExceptionMessage('Failed to save_events: Version conflict');
        
        $this->client->saveEvents('test-stream', $events);
    }

    public function testLoadBalancingConfiguration(): void
    {
        // Test round_robin policy
        $client = new OrisunClient(
            'localhost:50051',
            [
                'loadBalancingPolicy' => 'round_robin',
                'keepaliveTimeMs' => 30000,
                'keepaliveTimeoutMs' => 5000,
                'keepalivePermitWithoutCalls' => true
            ],
            $this->mockLogger
        );
        
        $this->assertInstanceOf(OrisunClient::class, $client);
        
        // Test pick_first policy
        $client2 = new OrisunClient(
            'localhost:50051',
            ['loadBalancingPolicy' => 'pick_first'],
            $this->mockLogger
        );
        
        $this->assertInstanceOf(OrisunClient::class, $client2);
    }
    
    public function testMultipleEndpoints(): void
    {
        $client = new OrisunClient(
            'server1:50051,server2:50051,server3:50051',
            ['loadBalancingPolicy' => 'round_robin'],
            $this->mockLogger
        );
        
        $this->assertInstanceOf(OrisunClient::class, $client);
    }
    
    public function testDnsTarget(): void
    {
        $client = new OrisunClient(
            'dns:///eventstore.example.com:50051',
            ['loadBalancingPolicy' => 'round_robin'],
            $this->mockLogger
        );
        
        $this->assertInstanceOf(OrisunClient::class, $client);
    }
    
    public function testIpv4Target(): void
    {
        $client = new OrisunClient(
            'ipv4:10.0.0.10:50051',
            [],
            $this->mockLogger
        );
        
        $this->assertInstanceOf(OrisunClient::class, $client);
    }
    
    public function testIpv6Target(): void
    {
        $client = new OrisunClient(
            'ipv6:[::1]:50051',
            [],
            $this->mockLogger
        );
        
        $this->assertInstanceOf(OrisunClient::class, $client);
    }
    
    public function testInvalidTargetFormat(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage('Invalid host:port format: invalid-target-format');
        
        new OrisunClient(
            'invalid-target-format',
            [],
            $this->mockLogger
        );
    }
    
    public function testEmptyTarget(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage('Invalid host:port format: ');
        
        new OrisunClient(
            '',
            [],
            $this->mockLogger
        );
    }
    
    public function testInvalidLoadBalancingPolicy(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage('Invalid load balancing policy: invalid_policy');
        
        new OrisunClient(
            'localhost:50051',
            ['loadBalancingPolicy' => 'invalid_policy'],
            $this->mockLogger
        );
    }

    public function testSaveEventsWithGrpcError(): void
    {
        // Arrange
        $streamName = 'test-stream';
        $events = [new Event('test.event', ['data' => 'test'])];

        $mockUnaryCall = $this->createMock(UnaryCall::class);
        $grpcStatus = (object) ['code' => 14, 'details' => 'Connection failed'];

        $mockUnaryCall->expects($this->once())
            ->method('wait')
            ->willReturn([null, $grpcStatus]);

        $this->mockGrpcClient->expects($this->once())
            ->method('SaveEvents')
            ->willReturn($mockUnaryCall);

        // Act & Assert
        $this->expectException(OrisunException::class);
        $this->expectExceptionMessage('Failed to save_events: Connection failed');
        $this->client->saveEvents($streamName, $events);
    }

    public function testGetEventsSuccess(): void
    {
        // Arrange
        $streamName = 'test-stream';
        $fromVersion = 0;
        $maxCount = 10;

        $mockUnaryCall = $this->createMock(UnaryCall::class);
        $response = new GetEventsResponse();
        $mockStatus = (object)['code' => \Grpc\STATUS_OK];
        
        $grpcEvent1 = new GrpcEvent();
        $grpcEvent1->setEventId('event-1');
        $grpcEvent1->setEventType('user.created');
        $grpcEvent1->setData(json_encode(['id' => 1, 'name' => 'John']));
        $grpcEvent1->setMetadata(json_encode(['source' => 'test']));
        $grpcEvent1->setVersion(1);
        
        $mockTimestamp = $this->createMock(\Google\Protobuf\Timestamp::class);
        $mockTimestamp->method('getSeconds')->willReturn(time());
        $mockTimestamp->method('getNanos')->willReturn(0);
        $grpcEvent1->setDateCreated($mockTimestamp);
        
        $response->setEvents([$grpcEvent1]);

        $mockUnaryCall->expects($this->once())
            ->method('wait')
            ->willReturn([$response, $mockStatus]);

        $this->mockGrpcClient->expects($this->once())
            ->method('GetEvents')
            ->with(
                $this->callback(function (GetEventsRequest $request) use ($streamName, $fromVersion, $maxCount) {
                    return $request->getStream()->getName() === $streamName &&
                           $request->getCount() === $maxCount;
                }),
                $this->anything()
            )
            ->willReturn($mockUnaryCall);

        // Act
        $events = $this->client->getEvents($streamName, $fromVersion, $maxCount);

        // Assert
        $this->assertCount(1, $events);
        $this->assertInstanceOf(Event::class, $events[0]);
        $this->assertEquals('event-1', $events[0]->getEventId());
        $this->assertEquals('user.created', $events[0]->getEventType());
        $this->assertEquals(['id' => 1, 'name' => 'John'], $events[0]->getDataAsArray());
        $this->assertEquals(['source' => 'test'], $events[0]->getMetadataAsArray());
        $this->assertEquals(1, $events[0]->getVersion());
    }

    public function testGetEventsWithInvalidStreamName(): void
    {
        // Arrange
        $this->mockGrpcClient->expects($this->once())
            ->method('GetEvents')
            ->willThrowException(new \Exception('Stream not found'));

        // Act & Assert
        $this->expectException(OrisunException::class);
        $this->expectExceptionMessage('Stream not found');
        
        $this->client->getEvents('non-existent-stream');
    }

    public function testSubscribeToStreamSuccess(): void
    {
        // Arrange
        $streamName = 'test-stream';
        $subscriberId = 'test-subscriber';
        $receivedEvents = [];
        
        $callback = function (Event $event) use (&$receivedEvents) {
            $receivedEvents[] = $event;
            return count($receivedEvents) < 2; // Stop after 2 events
        };

        $mockServerStreamingCall = $this->createMock(ServerStreamingCall::class);
        
        // Mock the iterator behavior
        $grpcEvent1 = new GrpcEvent();
        $grpcEvent1->setEventId('event-1');
        $grpcEvent1->setEventType('test.event');
        $grpcEvent1->setData(json_encode(['test' => 'data1']));
        $grpcEvent1->setMetadata(json_encode(['source' => 'test']));
        $grpcEvent1->setVersion(1);
        $mockTimestamp1 = $this->createMock(\Google\Protobuf\Timestamp::class);
        $mockTimestamp1->method('getSeconds')->willReturn(time());
        $mockTimestamp1->method('getNanos')->willReturn(0);
        $grpcEvent1->setDateCreated($mockTimestamp1);
        
        $grpcEvent2 = new GrpcEvent();
        $grpcEvent2->setEventId('event-2');
        $grpcEvent2->setEventType('test.event');
        $grpcEvent2->setData(json_encode(['test' => 'data2']));
        $grpcEvent2->setMetadata(json_encode(['source' => 'test']));
        $grpcEvent2->setVersion(2);
        $mockTimestamp2 = $this->createMock(\Google\Protobuf\Timestamp::class);
        $mockTimestamp2->method('getSeconds')->willReturn(time());
        $mockTimestamp2->method('getNanos')->willReturn(0);
        $grpcEvent2->setDateCreated($mockTimestamp2);

        $mockServerStreamingCall->expects($this->once())
            ->method('responses')
            ->willReturn(new \ArrayIterator([$grpcEvent1, $grpcEvent2]));

        $this->mockGrpcClient->expects($this->once())
            ->method('CatchUpSubscribeToStream')
            ->with(
                $this->callback(function (CatchUpSubscribeToStreamRequest $request) use ($streamName, $subscriberId) {
                    return $request->getStream() === $streamName &&
                           $request->getSubscriberName() === $subscriberId;
                }),
                $this->anything()
            )
            ->willReturn($mockServerStreamingCall);

        // Act
        $this->client->subscribeToStream($streamName, $callback, $subscriberId);

        // Assert
        $this->assertCount(2, $receivedEvents);
        $this->assertEquals('event-1', $receivedEvents[0]->getEventId());
        $this->assertEquals('event-2', $receivedEvents[1]->getEventId());
    }

    public function testHealthCheckSuccess(): void
    {
        // Arrange - Health check is implemented by trying to connect
        $mockCall = $this->createMock(UnaryCall::class);
        $mockResponse = $this->createMock(GetEventsResponse::class);
        $mockResponse->method('getEvents')->willReturn([]);
        $mockStatus = (object)['code' => \Grpc\STATUS_OK];
        
        $mockCall->expects($this->once())
            ->method('wait')
            ->willReturn([$mockResponse, $mockStatus]);

        $this->mockGrpcClient->expects($this->once())
            ->method('GetEvents')
            ->willReturn($mockCall);

        // Act
        $result = $this->client->healthCheck();

        // Assert
        $this->assertIsArray($result);
        $this->assertEquals('healthy', $result['status']);
        $this->assertArrayHasKey('timestamp', $result);
        $this->assertArrayHasKey('response_time_ms', $result);
        $this->assertArrayHasKey('metrics', $result);
        $this->assertNull($result['error']);
    }

    public function testHealthCheckFailure(): void
    {
        // Arrange
        $this->mockGrpcClient->expects($this->once())
            ->method('GetEvents')
            ->willThrowException(new \Exception('Connection failed'));

        // Act
        $result = $this->client->healthCheck();

        // Assert
        $this->assertIsArray($result);
        $this->assertEquals('unhealthy', $result['status']);
        $this->assertArrayHasKey('timestamp', $result);
        $this->assertArrayHasKey('response_time_ms', $result);
        $this->assertArrayHasKey('metrics', $result);
        $this->assertEquals('Unexpected error getting events: Connection failed', $result['error']);
    }

    public function testSetCustomLogger(): void
    {
        // Arrange
        $customLogger = $this->createMock(LoggerInterface::class);

        // Act
        $this->client->setLogger($customLogger);
        $retrievedLogger = $this->client->getLogger();

        // Assert
        $this->assertSame($customLogger, $retrievedLogger);
    }

    public function testEventCreation(): void
    {
        // Arrange
        $eventId = 'test-event-id';
        $eventType = 'test.event';
        $data = '{"key": "value"}';
        $metadata = '{}';

        // Act
        $event = new Event($eventType, $data, $metadata, $eventId);

        // Assert
        $this->assertEquals($eventId, $event->getEventId());
        $this->assertEquals($eventType, $event->getEventType());
        $this->assertEquals($data, $event->getData());
        $this->assertEquals($metadata, $event->getMetadata());
        $this->assertNull($event->getVersion());
        $this->assertNull($event->getDateCreated());
    }

    public function testPositionCreation(): void
    {
        // Arrange
        $commitPosition = 1000;
        $preparePosition = 999;

        // Act
        $position = new Position($commitPosition, $preparePosition);

        // Assert
        $this->assertEquals($commitPosition, $position->getCommitPosition());
        $this->assertEquals($preparePosition, $position->getPreparePosition());
        $this->assertEquals('1000:999', $position->__toString());
    }

    public function testPositionFromString(): void
    {
        // Arrange
        $positionString = '1000:999';

        // Act
        $position = Position::fromString($positionString);

        // Assert
        $this->assertEquals(1000, $position->getCommitPosition());
        $this->assertEquals(999, $position->getPreparePosition());
    }

    public function testPositionFromInvalidString(): void
    {
        // Act & Assert
        $this->expectException(OrisunException::class);
        $this->expectExceptionMessage('Invalid position string format. Expected "commit:prepare"');
        
        Position::fromString('invalid-format');
    }
}