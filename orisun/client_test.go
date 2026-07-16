package orisun

import (
	"context"
	"encoding/json"
	"testing"
)

type capturePreparedSaver struct {
	prepared PreparedEventBatch
}

func (s *capturePreparedSaver) SavePrepared(_ context.Context, events PreparedEventBatch, _ string, _ *Position, _ *Query) (string, int64, error) {
	s.prepared = events
	return "7", 8, nil
}

func TestOrisunServerSaveEventsAddsEventTypeToData(t *testing.T) {
	saver := &capturePreparedSaver{}
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
	if len(saver.prepared) != 1 {
		t.Fatalf("expected one saved event, got %d", len(saver.prepared))
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(saver.prepared[0].DataJSON), &data); err != nil {
		t.Fatalf("prepared data is invalid JSON: %v", err)
	}
	if data["eventType"] != "OrderPlaced" {
		t.Fatalf("expected canonical eventType in data, got %v", data["eventType"])
	}
	if data["order_id"] != "order-1" {
		t.Fatalf("expected original data to be preserved, got %v", data["order_id"])
	}
}

func TestOrisunServerSaveEventsUsesPreparedBatch(t *testing.T) {
	saver := &capturePreparedSaver{}
	server := &OrisunServer{saveEvents: saver}

	_, err := server.SaveEvents(context.Background(), []EventWithMapTags{{
		EventId:   "event-1",
		EventType: "OrderPlaced",
		Data:      `{"order_id":"order-1","eventType":"stale"}`,
		Metadata:  `{"trace_id":"trace-1"}`,
	}}, "orders", nil, nil)
	if err != nil {
		t.Fatalf("SaveEvents returned error: %v", err)
	}
	if len(saver.prepared) != 1 {
		t.Fatalf("expected one prepared event, got %d", len(saver.prepared))
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(saver.prepared[0].DataJSON), &data); err != nil {
		t.Fatalf("prepared data is invalid JSON: %v", err)
	}
	if data["eventType"] != "OrderPlaced" || data["order_id"] != "order-1" {
		t.Fatalf("unexpected prepared data: %v", data)
	}
	if saver.prepared[0].MetadataJSON != `{"trace_id":"trace-1"}` {
		t.Fatalf("metadata was unnecessarily rewritten: %s", saver.prepared[0].MetadataJSON)
	}
}

func TestPrepareEventsForSaveOwnsByteInput(t *testing.T) {
	data := []byte(`{"value":1}`)
	metadata := []byte(`{"source":"test"}`)
	prepared, err := PrepareEventsForSave([]EventWithMapTags{{
		EventId: "event-1", EventType: "Created", Data: data, Metadata: metadata,
	}})
	if err != nil {
		t.Fatalf("PrepareEventsForSave returned error: %v", err)
	}
	data[2] = 'X'
	metadata[2] = 'X'
	var got map[string]any
	if err := json.Unmarshal([]byte(prepared[0].DataJSON), &got); err != nil {
		t.Fatalf("prepared data is invalid JSON: %v", err)
	}
	if got["value"] != float64(1) {
		t.Fatalf("prepared data affected by caller mutation: %s", prepared[0].DataJSON)
	}
	if prepared[0].MetadataJSON != `{"source":"test"}` {
		t.Fatalf("prepared metadata affected by caller mutation: %s", prepared[0].MetadataJSON)
	}
}
