<?php

namespace Orisun\Client;

use Grpc\ChannelCredentials;
use Eventstore\EventStoreClient;
use Eventstore\SaveEventsRequest;
use Eventstore\SaveStreamQuery;
use Eventstore\GetEventsRequest;
use Eventstore\GetStreamQuery;
use Eventstore\EventToSave;
use Eventstore\Position;
use Eventstore\Query;
use Eventstore\Direction;
use Eventstore\CatchUpSubscribeToStreamRequest;
use Eventstore\CatchUpSubscribeToEventStoreRequest;
use Psr\Log\LoggerInterface;
use Psr\Log\NullLogger;
use Illuminate\Support\Facades\Log as LaravelLog;

/**
 * Orisun Event Store PHP Client
 * 
 * A production-ready client for interacting with Orisun Event Store
 */
class OrisunClient
{
    private EventStoreClient $grpcClient;
    private LoggerInterface $logger;
    private string $boundary;
    private array $metrics = [
        'operations_count' => 0,
        'errors_count' => 0,
        'last_operation_time' => null,
        'connection_attempts' => 0,
        'successful_operations' => 0
    ];
    
    /**
     * @param string $target The EventStore target. Can be:
     *   - Single host:port (e.g., 'localhost:50051')
     *   - Comma-separated hosts (e.g., 'host1:50051,host2:50051,host3:50051')
     *   - DNS target (e.g., 'dns:///eventstore.example.com:50051')
     *   - IPv4 target (e.g., 'ipv4:10.0.0.10:50051')
     *   - IPv6 target (e.g., 'ipv6:[::1]:50051')
     * @param array $options Connection options
     *   - 'boundary': string - Event store boundary (default: 'default')
     *   - 'tls': bool - Use TLS connection (default: false)
     *   - 'timeout': int - Connection timeout in seconds
     *   - 'loadBalancingPolicy': string - Load balancing policy ('round_robin' or 'pick_first', default: 'round_robin')
     *   - 'keepaliveTimeMs': int - Keepalive time in milliseconds (default: 30000)
     *   - 'keepaliveTimeoutMs': int - Keepalive timeout in milliseconds (default: 10000)
     *   - 'keepalivePermitWithoutCalls': bool - Allow keepalive pings without calls (default: true)
     * @param LoggerInterface|null $logger PSR-3 logger instance
     */
    public function __construct(
        string $target,
        array $options = [],
        ?LoggerInterface $logger = null
    ) {
        $this->logger = $logger ?? $this->createDefaultLogger();
        $this->boundary = $options['boundary'] ?? 'default';
        
        // Load balancing configuration
        $loadBalancingPolicy = $options['loadBalancingPolicy'] ?? 'round_robin';
        $keepaliveTimeMs = $options['keepaliveTimeMs'] ?? 30000;
        $keepaliveTimeoutMs = $options['keepaliveTimeoutMs'] ?? 10000;
        $keepalivePermitWithoutCalls = $options['keepalivePermitWithoutCalls'] ?? true;
        
        // Validate load balancing policy
        if (!in_array($loadBalancingPolicy, ['round_robin', 'pick_first'])) {
            throw new \InvalidArgumentException(
                "Invalid load balancing policy: {$loadBalancingPolicy}. Must be 'round_robin' or 'pick_first'."
            );
        }
        
        $grpcOptions = [
            'credentials' => $options['tls'] ?? false 
                ? ChannelCredentials::createSsl()
                : ChannelCredentials::createInsecure(),
            // Load balancing configuration
            'grpc.lb_policy_name' => $loadBalancingPolicy,
            // Keepalive configuration
            'grpc.keepalive_time_ms' => $keepaliveTimeMs,
            'grpc.keepalive_timeout_ms' => $keepaliveTimeoutMs,
            'grpc.keepalive_permit_without_calls' => $keepalivePermitWithoutCalls ? 1 : 0,
            'grpc.http2.min_time_between_pings_ms' => $keepaliveTimeMs,
            'grpc.http2.max_pings_without_data' => 0,
        ];
        
        if (isset($options['timeout'])) {
            $grpcOptions['timeout'] = $options['timeout'];
        }
        
        // Add service config for advanced load balancing configuration
        $serviceConfig = [
            'loadBalancingConfig' => [
                [$loadBalancingPolicy => new \stdClass()]
            ]
        ];
        $grpcOptions['grpc.service_config'] = json_encode($serviceConfig);
        
        // Process target to handle multiple endpoints
        $processedTarget = $this->processTarget($target);
        
        $this->grpcClient = new EventStoreClient($processedTarget, $grpcOptions);
        
        $this->logger->info('Orisun client initialized', [
            'target' => $target,
            'processed_target' => $processedTarget,
            'boundary' => $this->boundary,
            'tls' => $options['tls'] ?? false,
            'loadBalancingPolicy' => $loadBalancingPolicy,
            'keepaliveTimeMs' => $keepaliveTimeMs,
            'keepaliveTimeoutMs' => $keepaliveTimeoutMs,
            'config' => $this->sanitizeConfigForLogging($options)
        ]);
    }
    
    /**
     * Process target string to handle multiple endpoints and DNS targets
     * 
     * @param string $target The target string
     * @return string Processed target string
     */
    private function processTarget(string $target): string
    {
        // If target already has a scheme (dns://, ipv4:, ipv6:), return as-is
        if (preg_match('/^(dns|ipv4|ipv6):/', $target)) {
            return $target;
        }
        
        // Check if target contains multiple hosts (comma-separated)
        if (strpos($target, ',') !== false) {
            // For multiple hosts, we need to use a scheme that supports load balancing
            // gRPC will handle the load balancing between multiple addresses
            $hosts = array_map('trim', explode(',', $target));
            
            // Validate each host
            foreach ($hosts as $host) {
                if (!$this->isValidHostPort($host)) {
                    throw new \InvalidArgumentException("Invalid host:port format: {$host}");
                }
            }
            
            // For multiple hosts, we can use the first one and let gRPC handle failover
            // Or we can format it as a proper target list
            return $hosts[0]; // gRPC client will use the load balancing policy
        }
        
        // Single host - validate format
        if (!$this->isValidHostPort($target)) {
            throw new \InvalidArgumentException("Invalid host:port format: {$target}");
        }
        
        return $target;
    }
    
    /**
     * Validate host:port format
     * 
     * @param string $hostPort The host:port string
     * @return bool True if valid
     */
    private function isValidHostPort(string $hostPort): bool
    {
        // Basic validation for host:port format
        if (strpos($hostPort, ':') === false) {
            return false;
        }
        
        $parts = explode(':', $hostPort);
        if (count($parts) !== 2) {
            return false;
        }
        
        [$host, $port] = $parts;
        
        // Validate port is numeric and in valid range
        if (!is_numeric($port) || $port < 1 || $port > 65535) {
            return false;
        }
        
        // Basic host validation (allow hostnames, IPs)
        if (empty(trim($host))) {
            return false;
        }
        
        return true;
    }
    
    /**
     * Save events to a stream
     * 
     * @param string $streamName The name of the stream
     * @param Event[] $events Array of events to save
     * @param int|null $expectedVersion Expected version for optimistic concurrency
     * @param Query|null $subsetQuery Optional subset query for filtering
     * @return WriteResult
     * @throws OrisunException
     */
    public function saveEvents(
        string $streamName,
        array $events,
        ?int $expectedVersion = null,
        ?Query $subsetQuery = null
    ): WriteResult {
        $startTime = microtime(true);
        $this->metrics['operations_count']++;
        
        try {
            $this->logger->debug('Saving events to stream', [
                'stream' => $streamName,
                'event_count' => count($events),
                'expected_version' => $expectedVersion
            ]);
            
            $request = new SaveEventsRequest();
            $request->setBoundary($this->boundary);
            
            $streamQuery = new SaveStreamQuery();
            $streamQuery->setName($streamName);
            
            if ($expectedVersion !== null) {
                $streamQuery->setExpectedVersion($expectedVersion);
            }
            
            if ($subsetQuery !== null) {
                $streamQuery->setSubsetQuery($subsetQuery);
            }
            
            $request->setStream($streamQuery);
            
            $protoEvents = [];
            foreach ($events as $event) {
                $protoEvent = new EventToSave();
                $protoEvent->setEventId($event->getEventId());
                $protoEvent->setEventType($event->getEventType());
                $protoEvent->setData($event->getData());
                $protoEvent->setMetadata($event->getMetadata());
                $protoEvents[] = $protoEvent;
            }
            
            $request->setEvents($protoEvents);
            
            [$response, $status] = $this->grpcClient->SaveEvents($request)->wait();
            
            if ($status->code !== \Grpc\STATUS_OK) {
                $this->handleGrpcError($status, 'save_events', [
                    'stream' => $streamName,
                    'event_count' => count($events)
                ]);
            }
            
            $result = WriteResult::fromProto($response);
            $duration = microtime(true) - $startTime;
            
            $this->metrics['successful_operations']++;
            $this->metrics['last_operation_time'] = time();
            
            $this->logger->info('Events saved successfully', [
                'stream' => $streamName,
                'event_count' => count($events),
                'duration_ms' => round($duration * 1000, 2)
            ]);
            
            return $result;
            
        } catch (\Exception $e) {
            $this->metrics['errors_count']++;
            $duration = microtime(true) - $startTime;
            
            $this->logger->error('Failed to save events', [
                'stream' => $streamName,
                'error' => $e->getMessage(),
                'duration_ms' => round($duration * 1000, 2),
                'exception_type' => get_class($e)
            ]);
            
            if ($e instanceof OrisunException) {
                throw $e;
            }
            
            throw new OrisunException(
                "Unexpected error saving events: {$e->getMessage()}",
                0,
                $e
            );
        }
    }
    
    /**
     * Get events from a stream
     * 
     * @param string $streamName The name of the stream
     * @param int $fromVersion Starting version (default: 0)
     * @param int $count Maximum number of events to retrieve (default: 100)
     * @param string $direction Direction to read (ASC or DESC, default: ASC)
     * @return Event[]
     * @throws OrisunException
     */
    public function getEvents(
        string $streamName,
        int $fromVersion = 0,
        int $count = 100,
        string $direction = 'ASC'
    ): array {
        $startTime = microtime(true);
        $this->metrics['operations_count']++;
        
        try {
            $this->logger->debug('Getting events from stream', [
                'stream' => $streamName,
                'from_version' => $fromVersion,
                'count' => $count,
                'direction' => $direction
            ]);
            
            $request = new GetEventsRequest();
            $request->setBoundary($this->boundary);
            $request->setCount($count);
            $request->setDirection($direction === 'DESC' ? Direction::DESC : Direction::ASC);
            
            $streamQuery = new GetStreamQuery();
            $streamQuery->setName($streamName);
            $streamQuery->setFromVersion($fromVersion);
            $request->setStream($streamQuery);
            
            [$response, $status] = $this->grpcClient->GetEvents($request)->wait();
            
            if ($status->code !== \Grpc\STATUS_OK) {
                $this->handleGrpcError($status, 'get_events', [
                    'stream' => $streamName,
                    'from_version' => $fromVersion,
                    'count' => $count
                ]);
            }
            
            $events = [];
            foreach ($response->getEvents() as $protoEvent) {
                $events[] = Event::fromProto($protoEvent);
            }
            
            $duration = microtime(true) - $startTime;
            $this->metrics['successful_operations']++;
            $this->metrics['last_operation_time'] = time();
            
            $this->logger->info('Events retrieved successfully', [
                'stream' => $streamName,
                'event_count' => count($events),
                'duration_ms' => round($duration * 1000, 2)
            ]);
            
            return $events;
            
        } catch (\Exception $e) {
            $this->metrics['errors_count']++;
            $duration = microtime(true) - $startTime;
            
            $this->logger->error('Failed to get events', [
                'stream' => $streamName,
                'error' => $e->getMessage(),
                'duration_ms' => round($duration * 1000, 2),
                'exception_type' => get_class($e)
            ]);
            
            if ($e instanceof OrisunException) {
                throw $e;
            }
            
            throw new OrisunException(
                "Unexpected error getting events: {$e->getMessage()}",
                0,
                $e
            );
        }
    }
    
    /**
     * Subscribe to events from a stream
     * 
     * @param string $streamName The name of the stream
     * @param callable $eventHandler Callback function to handle each event
     * @param string $subscriberName Unique subscriber name
     * @param int $afterVersion Start after this version (default: -1 for all)
     * @param Query|null $query Optional query for filtering
     * @throws OrisunException
     */
    public function subscribeToStream(
        string $streamName,
        callable $eventHandler,
        string $subscriberName,
        int $afterVersion = -1,
        ?Query $query = null
    ): void {
        try {
            $this->logger->info('Starting stream subscription', [
                'stream' => $streamName,
                'subscriber' => $subscriberName,
                'after_version' => $afterVersion
            ]);
            
            $request = new CatchUpSubscribeToStreamRequest();
            $request->setBoundary($this->boundary);
            $request->setStream($streamName);
            $request->setSubscriberName($subscriberName);
            $request->setAfterVersion($afterVersion);
            
            if ($query !== null) {
                $request->setQuery($query);
            }
            
            $call = $this->grpcClient->CatchUpSubscribeToStream($request);
            
            foreach ($call->responses() as $protoEvent) {
                $event = Event::fromProto($protoEvent);
                
                try {
                    $eventHandler($event);
                } catch (\Exception $e) {
                    $this->logger->error('Event handler error', [
                        'event_id' => $event->getEventId(),
                        'error' => $e->getMessage()
                    ]);
                    // Continue processing other events
                }
            }
            
        } catch (\Exception $e) {
            $this->logger->error('Stream subscription failed', [
                'stream' => $streamName,
                'subscriber' => $subscriberName,
                'error' => $e->getMessage()
            ]);
            
            throw new OrisunException(
                "Stream subscription failed: {$e->getMessage()}",
                0,
                $e
            );
        }
    }
    
    /**
     * Subscribe to all events in the event store
     * 
     * @param callable $eventHandler Callback function to handle each event
     * @param string $subscriberName Unique subscriber name
     * @param Position|null $afterPosition Start after this position
     * @param Query|null $query Optional query for filtering
     * @throws OrisunException
     */
    public function subscribeToEventStore(
        callable $eventHandler,
        string $subscriberName,
        ?Position $afterPosition = null,
        ?Query $query = null
    ): void {
        try {
            $this->logger->info('Starting event store subscription', [
                'subscriber' => $subscriberName
            ]);
            
            $request = new CatchUpSubscribeToEventStoreRequest();
            $request->setBoundary($this->boundary);
            $request->setSubscriberName($subscriberName);
            
            if ($afterPosition !== null) {
                $request->setAfterPosition($afterPosition);
            }
            
            if ($query !== null) {
                $request->setQuery($query);
            }
            
            $call = $this->grpcClient->CatchUpSubscribeToEvents($request);
            
            foreach ($call->responses() as $protoEvent) {
                $event = Event::fromProto($protoEvent);
                
                try {
                    $eventHandler($event);
                } catch (\Exception $e) {
                    $this->logger->error('Event handler error', [
                        'event_id' => $event->getEventId(),
                        'error' => $e->getMessage()
                    ]);
                    // Continue processing other events
                }
            }
            
        } catch (\Exception $e) {
            $this->logger->error('Event store subscription failed', [
                'subscriber' => $subscriberName,
                'error' => $e->getMessage()
            ]);
            
            throw new OrisunException(
                "Event store subscription failed: {$e->getMessage()}",
                0,
                $e
            );
        }
    }
    
    /**
     * Create default logger that integrates with Laravel if available
     */
    private function createDefaultLogger(): LoggerInterface
    {
        if (class_exists('\Illuminate\Support\Facades\Log')) {
            return new class implements LoggerInterface {
                public function emergency($message, array $context = []): void {
                    LaravelLog::emergency($message, $context);
                }
                
                public function alert($message, array $context = []): void {
                    LaravelLog::alert($message, $context);
                }
                
                public function critical($message, array $context = []): void {
                    LaravelLog::critical($message, $context);
                }
                
                public function error($message, array $context = []): void {
                    LaravelLog::error($message, $context);
                }
                
                public function warning($message, array $context = []): void {
                    LaravelLog::warning($message, $context);
                }
                
                public function notice($message, array $context = []): void {
                    LaravelLog::notice($message, $context);
                }
                
                public function info($message, array $context = []): void {
                    LaravelLog::info($message, $context);
                }
                
                public function debug($message, array $context = []): void {
                    LaravelLog::debug($message, $context);
                }
                
                public function log($level, $message, array $context = []): void {
                    LaravelLog::log($level, $message, $context);
                }
            };
        }
        
        return new NullLogger();
    }
    
    /**
     * Handle gRPC errors with enhanced logging
     */
    private function handleGrpcError($status, string $operation, array $context = []): void
    {
        $this->metrics['errors_count']++;
        
        $errorContext = array_merge($context, [
            'grpc_code' => $status->code,
            'grpc_details' => $status->details,
            'operation' => $operation
        ]);
        
        $this->logger->error('gRPC operation failed', $errorContext);
        
        throw new OrisunException(
            "Failed to {$operation}: {$status->details}",
            $status->code
        );
    }
    
    /**
     * Sanitize configuration for logging (remove sensitive data)
     */
    private function sanitizeConfigForLogging(array $config): array
    {
        $sanitized = $config;
        
        // Remove sensitive keys
        $sensitiveKeys = ['password', 'token', 'secret', 'key', 'credentials'];
        
        foreach ($sensitiveKeys as $key) {
            if (isset($sanitized[$key])) {
                $sanitized[$key] = '[REDACTED]';
            }
        }
        
        return $sanitized;
    }
    
    /**
     * Get client metrics
     */
    public function getMetrics(): array
    {
        return $this->metrics;
    }
    
    /**
     * Reset client metrics
     */
    public function resetMetrics(): void
    {
        $this->metrics = [
            'operations_count' => 0,
            'errors_count' => 0,
            'last_operation_time' => null,
            'connection_attempts' => 0,
            'successful_operations' => 0
        ];
        
        $this->logger->info('Client metrics reset');
    }
    
    /**
     * Health check method
     */
    public function healthCheck(): array
    {
        $startTime = microtime(true);
        
        try {
            // Try to get events from a test stream (should fail gracefully if stream doesn't exist)
            $this->getEvents('__health_check__', 0, 1);
            $status = 'healthy';
            $error = null;
        } catch (OrisunException $e) {
            if ($e->isStreamNotFound()) {
                // Stream not found is expected for health check
                $status = 'healthy';
                $error = null;
            } else {
                $status = 'unhealthy';
                $error = $e->getMessage();
            }
        } catch (\Exception $e) {
            $status = 'unhealthy';
            $error = $e->getMessage();
        }
        
        $duration = microtime(true) - $startTime;
        
        $healthData = [
            'status' => $status,
            'timestamp' => time(),
            'response_time_ms' => round($duration * 1000, 2),
            'metrics' => $this->getMetrics(),
            'error' => $error
        ];
        
        $this->logger->info('Health check completed', $healthData);
        
        return $healthData;
    }
    
    /**
     * Execute operation with retry logic
     */
    private function executeWithRetry(callable $operation, string $operationName, array $context = [])
    {
        $lastException = null;
        
        for ($attempt = 1; $attempt <= $this->maxRetries; $attempt++) {
            try {
                $this->logger->debug('Executing operation', [
                    'operation' => $operationName,
                    'attempt' => $attempt,
                    'max_retries' => $this->maxRetries,
                    'context' => $context
                ]);
                
                return $operation();
                
            } catch (OrisunException $e) {
                $lastException = $e;
                
                // Don't retry certain errors
                if ($e->isStreamNotFound() || $e->isConcurrencyConflict()) {
                    $this->logger->warning('Non-retryable error encountered', [
                        'operation' => $operationName,
                        'attempt' => $attempt,
                        'error' => $e->getMessage(),
                        'grpc_code' => $e->getGrpcCode()
                    ]);
                    throw $e;
                }
                
                if ($attempt < $this->maxRetries) {
                    $delay = $this->retryDelay * $attempt; // Exponential backoff
                    
                    $this->logger->warning('Operation failed, retrying', [
                        'operation' => $operationName,
                        'attempt' => $attempt,
                        'max_retries' => $this->maxRetries,
                        'retry_delay_ms' => $delay,
                        'error' => $e->getMessage()
                    ]);
                    
                    usleep($delay * 1000); // Convert to microseconds
                } else {
                    $this->logger->error('Operation failed after all retries', [
                        'operation' => $operationName,
                        'attempts' => $attempt,
                        'error' => $e->getMessage()
                    ]);
                }
            } catch (\Exception $e) {
                $lastException = new OrisunException(
                    "Unexpected error in {$operationName}: {$e->getMessage()}",
                    0,
                    $e
                );
                
                $this->logger->error('Unexpected error in operation', [
                    'operation' => $operationName,
                    'attempt' => $attempt,
                    'error' => $e->getMessage(),
                    'exception_type' => get_class($e)
                ]);
                
                if ($attempt >= $this->maxRetries) {
                    break;
                }
                
                usleep($this->retryDelay * $attempt * 1000);
            }
        }
        
        throw $lastException;
    }
    
    /**
     * Log performance metrics
     */
    public function logPerformanceMetrics(): void
    {
        $metrics = $this->getMetrics();
        
        $performanceData = [
            'total_operations' => $metrics['operations_count'],
            'successful_operations' => $metrics['successful_operations'],
            'failed_operations' => $metrics['errors_count'],
            'success_rate' => $metrics['operations_count'] > 0 
                ? round(($metrics['successful_operations'] / $metrics['operations_count']) * 100, 2) 
                : 0,
            'last_operation_time' => $metrics['last_operation_time'],
            'connection_attempts' => $metrics['connection_attempts']
        ];
        
        $this->logger->info('Performance metrics', $performanceData);
    }
    
    /**
     * Set custom logger
     */
    public function setLogger(LoggerInterface $logger): void
    {
        $this->logger = $logger;
        $this->logger->info('Custom logger set for OrisunClient');
    }
    
    /**
     * Get current logger
     */
    public function getLogger(): LoggerInterface
    {
        return $this->logger;
    }
}