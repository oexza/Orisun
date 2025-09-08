<?php

namespace Orisun\Client;

use Eventstore\Event as ProtoEvent;
use Ramsey\Uuid\Uuid;

/**
 * Event class for Orisun Event Store
 * 
 * Provides a convenient wrapper around protobuf Event messages
 */
class Event
{
    private string $eventId;
    private string $eventType;
    private string $data;
    private string $metadata;
    private ?string $streamId;
    private ?int $version;
    private ?\DateTimeImmutable $dateCreated;
    private ?Position $position;
    
    /**
     * @param string $eventType The type of the event
     * @param array|string $data Event data (will be JSON encoded if array)
     * @param array|string $metadata Event metadata (will be JSON encoded if array)
     * @param string|null $eventId Unique event ID (auto-generated if null)
     */
    public function __construct(
        string $eventType,
        $data = [],
        $metadata = [],
        ?string $eventId = null
    ) {
        $this->eventId = $eventId ?? Uuid::uuid4()->toString();
        $this->eventType = $eventType;
        $this->data = is_string($data) ? $data : json_encode($data, JSON_THROW_ON_ERROR);
        $this->metadata = is_string($metadata) ? $metadata : json_encode($metadata, JSON_THROW_ON_ERROR);
        $this->streamId = null;
        $this->version = null;
        $this->dateCreated = null;
        $this->position = null;
    }
    
    /**
     * Create Event from protobuf Event
     */
    public static function fromProto(ProtoEvent $protoEvent): self
    {
        $event = new self(
            $protoEvent->getEventType(),
            $protoEvent->getData(),
            $protoEvent->getMetadata(),
            $protoEvent->getEventId()
        );
        
        $event->streamId = $protoEvent->getStreamId();
        $event->version = $protoEvent->getVersion();
        
        if ($protoEvent->hasDateCreated()) {
            $timestamp = $protoEvent->getDateCreated();
            $event->dateCreated = \DateTimeImmutable::createFromFormat(
                'U',
                (string) $timestamp->getSeconds()
            )->setTime(
                (int) date('H', $timestamp->getSeconds()),
                (int) date('i', $timestamp->getSeconds()),
                (int) date('s', $timestamp->getSeconds()),
                (int) ($timestamp->getNanos() / 1000)
            );
        }
        
        if ($protoEvent->hasPosition()) {
            $event->position = Position::fromProto($protoEvent->getPosition());
        }
        
        return $event;
    }
    
    public function getEventId(): string
    {
        return $this->eventId;
    }
    
    public function getEventType(): string
    {
        return $this->eventType;
    }
    
    public function getData(): string
    {
        return $this->data;
    }
    
    /**
     * Get event data as decoded JSON array
     */
    public function getDataAsArray(): array
    {
        return json_decode($this->data, true, 512, JSON_THROW_ON_ERROR);
    }
    
    public function getMetadata(): string
    {
        return $this->metadata;
    }
    
    /**
     * Get event metadata as decoded JSON array
     */
    public function getMetadataAsArray(): array
    {
        return json_decode($this->metadata, true, 512, JSON_THROW_ON_ERROR);
    }
    
    public function getStreamId(): ?string
    {
        return $this->streamId;
    }
    
    public function getVersion(): ?int
    {
        return $this->version;
    }
    
    public function getDateCreated(): ?\DateTimeImmutable
    {
        return $this->dateCreated;
    }
    
    public function getPosition(): ?Position
    {
        return $this->position;
    }
    
    /**
     * Convert to array representation
     */
    public function toArray(): array
    {
        return [
            'event_id' => $this->eventId,
            'event_type' => $this->eventType,
            'data' => $this->getDataAsArray(),
            'metadata' => $this->getMetadataAsArray(),
            'stream_id' => $this->streamId,
            'version' => $this->version,
            'date_created' => $this->dateCreated?->format(\DateTimeInterface::ATOM),
            'position' => $this->position?->toArray(),
        ];
    }
    
    /**
     * Convert to JSON string
     */
    public function toJson(): string
    {
        return json_encode($this->toArray(), JSON_THROW_ON_ERROR);
    }
}