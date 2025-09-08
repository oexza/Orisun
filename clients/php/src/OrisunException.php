<?php

namespace Orisun\Client;

/**
 * OrisunException class for Orisun Event Store
 * 
 * Custom exception for Orisun client errors
 */
class OrisunException extends \Exception
{
    private ?int $grpcCode;
    
    public function __construct(
        string $message = "",
        int $code = 0,
        ?\Throwable $previous = null,
        ?int $grpcCode = null
    ) {
        parent::__construct($message, $code, $previous);
        $this->grpcCode = $grpcCode;
    }
    
    /**
     * Get the gRPC status code if available
     */
    public function getGrpcCode(): ?int
    {
        return $this->grpcCode;
    }
    
    /**
     * Check if this is a concurrency conflict error
     */
    public function isConcurrencyConflict(): bool
    {
        return $this->grpcCode === \Grpc\STATUS_FAILED_PRECONDITION
            || str_contains(strtolower($this->getMessage()), 'optimistic');
    }
    
    /**
     * Check if this is a stream not found error
     */
    public function isStreamNotFound(): bool
    {
        return $this->grpcCode === \Grpc\STATUS_NOT_FOUND
            || str_contains(strtolower($this->getMessage()), 'not found');
    }
    
    /**
     * Check if this is a connection error
     */
    public function isConnectionError(): bool
    {
        return $this->grpcCode === \Grpc\STATUS_UNAVAILABLE
            || $this->grpcCode === \Grpc\STATUS_DEADLINE_EXCEEDED
            || str_contains(strtolower($this->getMessage()), 'connection');
    }
}