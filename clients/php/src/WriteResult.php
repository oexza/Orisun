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
    private int $newStreamVersion;
    
    public function __construct(?Position $logPosition = null, int $newStreamVersion = 0)
    {
        $this->logPosition = $logPosition;
        $this->newStreamVersion = $newStreamVersion;
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
        
        $newStreamVersion = $protoWriteResult->getNewStreamVersion();
        
        return new self($logPosition, $newStreamVersion);
    }
    
    public function getLogPosition(): ?Position
    {
        return $this->logPosition;
    }
    
    public function getNewStreamVersion(): int
    {
        return $this->newStreamVersion;
    }
    
    /**
     * Convert to array representation
     */
    public function toArray(): array
    {
        return [
            'log_position' => $this->logPosition?->toArray(),
            'new_stream_version' => $this->newStreamVersion,
        ];
    }
    
    /**
     * Convert to string representation
     */
    public function __toString(): string
    {
        return $this->logPosition 
            ? "WriteResult(position: {$this->logPosition}, newStreamVersion: {$this->newStreamVersion})"
            : "WriteResult(no position, newStreamVersion: {$this->newStreamVersion})";
    }
}