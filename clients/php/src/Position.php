<?php

namespace Orisun\Client;

use Eventstore\Position as ProtoPosition;

/**
 * Position class for Orisun Event Store
 * 
 * Represents a position in the event store log
 */
class Position
{
    private int $commitPosition;
    private int $preparePosition;
    
    public function __construct(int $commitPosition, int $preparePosition)
    {
        $this->commitPosition = $commitPosition;
        $this->preparePosition = $preparePosition;
    }
    
    /**
     * Create Position from protobuf Position
     */
    public static function fromProto(ProtoPosition $protoPosition): self
    {
        return new self(
            $protoPosition->getCommitPosition(),
            $protoPosition->getPreparePosition()
        );
    }
    
    /**
     * Convert to protobuf Position
     */
    public function toProto(): ProtoPosition
    {
        $protoPosition = new ProtoPosition();
        $protoPosition->setCommitPosition($this->commitPosition);
        $protoPosition->setPreparePosition($this->preparePosition);
        return $protoPosition;
    }
    
    public function getCommitPosition(): int
    {
        return $this->commitPosition;
    }
    
    public function getPreparePosition(): int
    {
        return $this->preparePosition;
    }
    
    /**
     * Convert to array representation
     */
    public function toArray(): array
    {
        return [
            'commit_position' => $this->commitPosition,
            'prepare_position' => $this->preparePosition,
        ];
    }
    
    /**
     * Convert to string representation
     */
    public function __toString(): string
    {
        return $this->commitPosition . ':' . $this->preparePosition;
    }

    /**
     * Create Position from string representation
     *
     * @param string $positionString String in format "commit:prepare"
     * @return Position
     * @throws OrisunException If string format is invalid
     */
    public static function fromString(string $positionString): Position
    {
        $parts = explode(':', $positionString);
        if (count($parts) !== 2) {
            throw new OrisunException('Invalid position string format. Expected "commit:prepare"');
        }
        
        $commit = (int) $parts[0];
        $prepare = (int) $parts[1];
        
        if ($commit < 0 || $prepare < 0) {
            throw new OrisunException('Position values must be non-negative');
        }
        
        return new Position($commit, $prepare);
    }
}