<?php

namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;
use Illuminate\Http\Response;
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Illuminate\Support\Facades\Log;
use Carbon\Carbon;
use Symfony\Component\HttpFoundation\Response as SymfonyResponse;

/**
 * Middleware to log HTTP requests and responses as events
 */
class EventLoggingMiddleware
{
    public function __construct(
        private OrisunClient $orisun
    ) {}

    /**
     * Handle an incoming request.
     */
    public function handle(Request $request, Closure $next): SymfonyResponse
    {
        $startTime = microtime(true);
        $requestId = $this->generateRequestId();
        
        // Log request start event
        $this->logRequestStart($request, $requestId, $startTime);
        
        $response = $next($request);
        
        $endTime = microtime(true);
        $duration = ($endTime - $startTime) * 1000; // Convert to milliseconds
        
        // Log request completion event
        $this->logRequestCompletion($request, $response, $requestId, $duration);
        
        return $response;
    }

    /**
     * Log request start event
     */
    private function logRequestStart(Request $request, string $requestId, float $startTime): void
    {
        try {
            $event = new Event(
                'http.request.started',
                [
                    'request_id' => $requestId,
                    'method' => $request->method(),
                    'url' => $request->fullUrl(),
                    'path' => $request->path(),
                    'query_params' => $request->query->all(),
                    'headers' => $this->sanitizeHeaders($request->headers->all()),
                    'user_agent' => $request->userAgent(),
                    'ip_address' => $request->ip(),
                    'started_at' => Carbon::createFromTimestamp($startTime)->toISOString(),
                ],
                [
                    'source' => 'http_middleware',
                    'timestamp' => Carbon::now()->toISOString(),
                    'user_id' => auth()->id(),
                    'session_id' => session()->getId(),
                ]
            );

            $this->orisun->saveEvents('http-requests', [$event]);
            
        } catch (OrisunException $e) {
            Log::warning('Failed to log request start event', [
                'request_id' => $requestId,
                'error' => $e->getMessage()
            ]);
        }
    }

    /**
     * Log request completion event
     */
    private function logRequestCompletion(
        Request $request, 
        SymfonyResponse $response, 
        string $requestId, 
        float $duration
    ): void {
        try {
            $event = new Event(
                'http.request.completed',
                [
                    'request_id' => $requestId,
                    'method' => $request->method(),
                    'url' => $request->fullUrl(),
                    'path' => $request->path(),
                    'status_code' => $response->getStatusCode(),
                    'duration_ms' => round($duration, 2),
                    'response_size' => strlen($response->getContent()),
                    'completed_at' => Carbon::now()->toISOString(),
                ],
                [
                    'source' => 'http_middleware',
                    'timestamp' => Carbon::now()->toISOString(),
                    'user_id' => auth()->id(),
                    'session_id' => session()->getId(),
                    'is_successful' => $response->isSuccessful(),
                    'is_client_error' => $response->isClientError(),
                    'is_server_error' => $response->isServerError(),
                ]
            );

            $this->orisun->saveEvents('http-requests', [$event]);
            
        } catch (OrisunException $e) {
            Log::warning('Failed to log request completion event', [
                'request_id' => $requestId,
                'error' => $e->getMessage()
            ]);
        }
    }

    /**
     * Generate a unique request ID
     */
    private function generateRequestId(): string
    {
        return uniqid('req_', true);
    }

    /**
     * Sanitize headers to remove sensitive information
     */
    private function sanitizeHeaders(array $headers): array
    {
        $sensitiveHeaders = [
            'authorization',
            'cookie',
            'x-api-key',
            'x-auth-token',
            'x-csrf-token',
        ];

        $sanitized = [];
        foreach ($headers as $key => $value) {
            if (in_array(strtolower($key), $sensitiveHeaders)) {
                $sanitized[$key] = '[REDACTED]';
            } else {
                $sanitized[$key] = $value;
            }
        }

        return $sanitized;
    }
}