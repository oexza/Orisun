package orisun

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	eventstore "github.com/orisunlabs/orisun-go-client/eventstore"
)

// RequestValidator provides validation methods for various request types
type RequestValidator struct{}

// NewRequestValidator creates a new RequestValidator
func NewRequestValidator() *RequestValidator {
	return &RequestValidator{}
}

// ValidateSaveEventsRequest validates a SaveEventsRequest
func (v *RequestValidator) ValidateSaveEventsRequest(request *eventstore.SaveEventsRequest) error {
	if request == nil {
		return NewOrisunException("SaveEventsRequest cannot be nil").
			AddContext("operation", "saveEvents")
	}

	// Validate boundary
	if strings.TrimSpace(request.Boundary) == "" {
		return NewOrisunException("Boundary is required").
			AddContext("operation", "saveEvents").
			AddContext("request", "SaveEventsRequest")
	}

	// Validate events
	if len(request.Events) == 0 {
		return NewOrisunException("At least one event is required").
			AddContext("operation", "saveEvents").
			AddContext("boundary", request.Boundary)
	}

	// Validate each event
	for i, event := range request.Events {
		if err := v.validateEventToSave(event, i, request.Boundary); err != nil {
			return err
		}
	}

	return nil
}

// validateEventToSave validates an EventToSave
func (v *RequestValidator) validateEventToSave(event *eventstore.EventToSave, index int, boundary string) error {
	if event == nil {
		return NewOrisunException(fmt.Sprintf("Event at index %d is nil", index)).
			AddContext("operation", "saveEvents").
			AddContext("eventIndex", index).
			AddContext("boundary", boundary)
	}

	// Validate eventId
	if strings.TrimSpace(event.EventId) == "" {
		return NewOrisunException(fmt.Sprintf("Event at index %d is missing eventId", index)).
			AddContext("operation", "saveEvents").
			AddContext("eventIndex", index).
			AddContext("boundary", boundary)
	}

	// Validate UUID format
	if _, err := uuid.Parse(event.EventId); err != nil {
		return NewOrisunException(fmt.Sprintf("Event at index %d has invalid eventId format", index)).
			AddContext("operation", "saveEvents").
			AddContext("eventIndex", index).
			AddContext("eventId", event.EventId).
			AddContext("boundary", boundary)
	}

	// Validate eventType
	if strings.TrimSpace(event.EventType) == "" {
		return NewOrisunException(fmt.Sprintf("Event at index %d is missing eventType", index)).
			AddContext("operation", "saveEvents").
			AddContext("eventIndex", index).
			AddContext("boundary", boundary)
	}

	// Validate data
	if strings.TrimSpace(event.Data) == "" {
		return NewOrisunException(fmt.Sprintf("Event at index %d is missing data", index)).
			AddContext("operation", "saveEvents").
			AddContext("eventIndex", index).
			AddContext("boundary", boundary)
	}

	return nil
}

// ValidateGetEventsRequest validates a GetEventsRequest
func (v *RequestValidator) ValidateGetEventsRequest(request *eventstore.GetEventsRequest) error {
	if request == nil {
		return NewOrisunException("GetEventsRequest cannot be nil").
			AddContext("operation", "getEvents")
	}

	// Validate boundary
	if strings.TrimSpace(request.Boundary) == "" {
		return NewOrisunException("Boundary is required").
			AddContext("operation", "getEvents").
			AddContext("request", "GetEventsRequest")
	}

	// Validate count if provided
	if request.Count <= 0 {
		return NewOrisunException("Count must be greater than 0").
			AddContext("operation", "getEvents").
			AddContext("count", request.Count).
			AddContext("boundary", request.Boundary)
	}

	return nil
}

// ValidateSubscribeRequest validates a CatchUpSubscribeToEventStoreRequest
func (v *RequestValidator) ValidateSubscribeRequest(request *eventstore.CatchUpSubscribeToEventStoreRequest) error {
	if request == nil {
		return NewOrisunException("SubscribeRequest cannot be nil").
			AddContext("operation", "subscribeToEvents")
	}

	// Validate boundary
	if strings.TrimSpace(request.Boundary) == "" {
		return NewOrisunException("Boundary is required").
			AddContext("operation", "subscribeToEvents").
			AddContext("request", "CatchUpSubscribeToEventStoreRequest")
	}

	// Validate subscriber name
	if strings.TrimSpace(request.SubscriberName) == "" {
		return NewOrisunException("Subscriber name is required").
			AddContext("operation", "subscribeToEvents").
			AddContext("boundary", request.Boundary)
	}

	return nil
}

// ExtractVersionNumbers extracts expected and actual version numbers from an error message
func ExtractVersionNumbers(errorMsg string) (expected, actual int64, err error) {
	// Define the regex pattern to match "Expected X, Actual Y"
	pattern := regexp.MustCompile(`Expected\s+(\d+),\s+Actual\s+(\d+)`)

	matches := pattern.FindStringSubmatch(errorMsg)
	if len(matches) != 3 {
		return 0, 0, fmt.Errorf("could not extract version numbers from error message: %s", errorMsg)
	}

	_, err = fmt.Sscanf(matches[1], "%d", &expected)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse expected version: %w", err)
	}

	_, err = fmt.Sscanf(matches[2], "%d", &actual)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse actual version: %w", err)
	}

	return expected, actual, nil
}
