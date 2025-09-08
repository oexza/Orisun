<?php

namespace Orisun\Client;

use Eventstore\WriteResult as ProtoWriteResult;

/**
 * WriteResult class for Orisun Event Store
 * 
 * Represents the result of a write operation
 */
class WriteResult
{
    private ?Position $logPosition;
    
    public function __construct(?Position $logPosition = null)
    {
        $this->logPosition = $logPosition;
    }
    
    /**
     * Create WriteResult from protobuf WriteResult
     */
    public static function fromProto(ProtoWriteResult $protoWriteResult): self
    {
        $logPosition = null;
        if ($protoWriteResult->hasLogPosition()) {
            $logPosition = Position::fromProto($protoWriteResult->getLogPosition());
        }
        
        return new self($logPosition);
    }
    
    public function getLogPosition(): ?Position
    {
        return $this->logPosition;
    }
    
    /**
     * Convert to array representation
     */
    public function toArray(): array
    {
        return [
            'log_position' => $this->logPosition?->toArray(),
        ];
    }
    
    /**
     * Convert to string representation
     */
    public function __toString(): string
    {
        return $this->logPosition 
            ? "WriteResult(position: {$this->logPosition})"
            : "WriteResult(no position)";
    }
}