package orisun

import (
	"context"
	"testing"
)

type captureSaver struct {
	events []EventWithMapTags
}

func (s *captureSaver) Save(ctx context.Context, events []EventWithMapTags, boundary string, expectedPosition *Position, subSet *Query) (string, int64, error) {
	s.events = events
	return "7", 8, nil
}

func TestOrisunServerSaveEventsAddsEventTypeToData(t *testing.T) {
	saver := &captureSaver{}
	server := &OrisunServer{saveEvents: saver}

	pos, err := server.SaveEvents(context.Background(), []EventWithMapTags{
		{
			EventId:   "event-1",
			EventType: "OrderPlaced",
			Data: map[string]any{
				"order_id":  "order-1",
				"eventType": "stale",
			},
			Metadata: map[string]any{},
		},
	}, "orders", nil, nil)
	if err != nil {
		t.Fatalf("SaveEvents returned error: %v", err)
	}
	if pos.CommitPosition != 7 || pos.PreparePosition != 8 {
		t.Fatalf("unexpected position: %+v", pos)
	}
	if len(saver.events) != 1 {
		t.Fatalf("expected one saved event, got %d", len(saver.events))
	}

	data, ok := saver.events[0].Data.(map[string]any)
	if !ok {
		t.Fatalf("expected normalized data map, got %T", saver.events[0].Data)
	}
	if data["eventType"] != "OrderPlaced" {
		t.Fatalf("expected canonical eventType in data, got %v", data["eventType"])
	}
	if data["order_id"] != "order-1" {
		t.Fatalf("expected original data to be preserved, got %v", data["order_id"])
	}
}

func TestNormalizeEventsForSaveAcceptsJSONStringData(t *testing.T) {
	events, err := normalizeEventsForSave([]EventWithMapTags{
		{
			EventId:   "event-1",
			EventType: "CustomerCreated",
			Data:      `{"customer_id":"customer-1"}`,
		},
	})
	if err != nil {
		t.Fatalf("normalizeEventsForSave returned error: %v", err)
	}

	data := events[0].Data.(map[string]any)
	if data["eventType"] != "CustomerCreated" {
		t.Fatalf("expected eventType in data, got %v", data["eventType"])
	}
	if data["customer_id"] != "customer-1" {
		t.Fatalf("expected original JSON data to be preserved, got %v", data["customer_id"])
	}
}
