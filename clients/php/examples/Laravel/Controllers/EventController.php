<?php

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use Illuminate\Http\JsonResponse;
use Illuminate\Routing\Controller;
use Orisun\Client\OrisunClient;
use Orisun\Client\Event;
use Orisun\Client\OrisunException;
use Illuminate\Support\Facades\Log;
use Illuminate\Support\Facades\Validator;

/**
 * Example Laravel controller demonstrating Orisun Event Store usage
 */
class EventController extends Controller
{
    public function __construct(
        private OrisunClient $orisun
    ) {}

    /**
     * Save a single event to a stream
     */
    public function store(Request $request): JsonResponse
    {
        $validator = Validator::make($request->all(), [
            'stream' => 'required|string|max:255',
            'event_type' => 'required|string|max:255',
            'data' => 'required|array',
            'metadata' => 'sometimes|array',
            'expected_version' => 'sometimes|integer|min:-1',
        ]);

        if ($validator->fails()) {
            return response()->json([
                'error' => 'Validation failed',
                'details' => $validator->errors()
            ], 422);
        }

        try {
            $event = new Event(
                $request->input('event_type'),
                $request->input('data'),
                $request->input('metadata', [])
            );

            $result = $this->orisun->saveEvents(
                $request->input('stream'),
                [$event],
                $request->input('expected_version')
            );

            return response()->json([
                'success' => true,
                'event_id' => $event->getEventId(),
                'log_position' => $result->getLogPosition(),
                'message' => 'Event saved successfully'
            ], 201);

        } catch (OrisunException $e) {
            Log::error('Failed to save event', [
                'stream' => $request->input('stream'),
                'event_type' => $request->input('event_type'),
                'error' => $e->getMessage(),
                'code' => $e->getCode()
            ]);

            if ($e->isConcurrencyConflict()) {
                return response()->json([
                    'error' => 'Concurrency conflict',
                    'message' => 'The stream has been modified by another process'
                ], 409);
            }

            if ($e->isConnectionError()) {
                return response()->json([
                    'error' => 'Connection error',
                    'message' => 'Unable to connect to event store'
                ], 503);
            }

            return response()->json([
                'error' => 'Event store error',
                'message' => $e->getMessage()
            ], 500);
        }
    }

    /**
     * Save multiple events to a stream in a batch
     */
    public function storeBatch(Request $request): JsonResponse
    {
        $validator = Validator::make($request->all(), [
            'stream' => 'required|string|max:255',
            'events' => 'required|array|min:1|max:100',
            'events.*.event_type' => 'required|string|max:255',
            'events.*.data' => 'required|array',
            'events.*.metadata' => 'sometimes|array',
            'expected_version' => 'sometimes|integer|min:-1',
        ]);

        if ($validator->fails()) {
            return response()->json([
                'error' => 'Validation failed',
                'details' => $validator->errors()
            ], 422);
        }

        try {
            $events = [];
            foreach ($request->input('events') as $eventData) {
                $events[] = new Event(
                    $eventData['event_type'],
                    $eventData['data'],
                    $eventData['metadata'] ?? []
                );
            }

            $result = $this->orisun->saveEvents(
                $request->input('stream'),
                $events,
                $request->input('expected_version')
            );

            return response()->json([
                'success' => true,
                'events_saved' => count($events),
                'event_ids' => array_map(fn($e) => $e->getEventId(), $events),
                'log_position' => $result->getLogPosition(),
                'message' => 'Events saved successfully'
            ], 201);

        } catch (OrisunException $e) {
            Log::error('Failed to save batch events', [
                'stream' => $request->input('stream'),
                'event_count' => count($request->input('events', [])),
                'error' => $e->getMessage()
            ]);

            return response()->json([
                'error' => 'Failed to save events',
                'message' => $e->getMessage()
            ], 500);
        }
    }

    /**
     * Get events from a stream
     */
    public function index(Request $request, string $stream): JsonResponse
    {
        $validator = Validator::make($request->all(), [
            'from_version' => 'sometimes|integer|min:0',
            'count' => 'sometimes|integer|min:1|max:1000',
            'direction' => 'sometimes|in:ASC,DESC',
        ]);

        if ($validator->fails()) {
            return response()->json([
                'error' => 'Validation failed',
                'details' => $validator->errors()
            ], 422);
        }

        try {
            $events = $this->orisun->getEvents(
                $stream,
                $request->input('from_version', 0),
                $request->input('count', 100),
                $request->input('direction', 'ASC')
            );

            return response()->json([
                'success' => true,
                'stream' => $stream,
                'event_count' => count($events),
                'events' => array_map(function (Event $event) {
                    return [
                        'event_id' => $event->getEventId(),
                        'event_type' => $event->getEventType(),
                        'data' => $event->getDataAsArray(),
                        'metadata' => $event->getMetadataAsArray(),
                        'version' => $event->getVersion(),
                        'date_created' => $event->getDateCreated(),
                        'position' => $event->getPosition()?->toString()
                    ];
                }, $events)
            ]);

        } catch (OrisunException $e) {
            Log::error('Failed to get events', [
                'stream' => $stream,
                'error' => $e->getMessage()
            ]);

            if ($e->isStreamNotFound()) {
                return response()->json([
                    'error' => 'Stream not found',
                    'message' => "Stream '{$stream}' does not exist"
                ], 404);
            }

            return response()->json([
                'error' => 'Failed to retrieve events',
                'message' => $e->getMessage()
            ], 500);
        }
    }

    /**
     * Get stream information
     */
    public function show(string $stream): JsonResponse
    {
        try {
            // Get the latest events to determine stream info
            $latestEvents = $this->orisun->getEvents($stream, 0, 1, 'DESC');
            
            if (empty($latestEvents)) {
                return response()->json([
                    'error' => 'Stream not found or empty',
                    'message' => "Stream '{$stream}' does not exist or has no events"
                ], 404);
            }

            $latestEvent = $latestEvents[0];
            
            // Get first event to calculate total events
            $firstEvents = $this->orisun->getEvents($stream, 0, 1, 'ASC');
            $firstEvent = $firstEvents[0] ?? null;

            return response()->json([
                'success' => true,
                'stream' => $stream,
                'latest_version' => $latestEvent->getVersion(),
                'first_version' => $firstEvent?->getVersion() ?? 0,
                'estimated_event_count' => $latestEvent->getVersion() + 1,
                'latest_event' => [
                    'event_id' => $latestEvent->getEventId(),
                    'event_type' => $latestEvent->getEventType(),
                    'date_created' => $latestEvent->getDateCreated(),
                    'position' => $latestEvent->getPosition()?->toString()
                ]
            ]);

        } catch (OrisunException $e) {
            Log::error('Failed to get stream info', [
                'stream' => $stream,
                'error' => $e->getMessage()
            ]);

            return response()->json([
                'error' => 'Failed to retrieve stream information',
                'message' => $e->getMessage()
            ], 500);
        }
    }
}