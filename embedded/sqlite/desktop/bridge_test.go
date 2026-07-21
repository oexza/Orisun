package desktop

import (
	"testing"
	"time"

	"github.com/goccy/go-json"
)

func TestBridgeSaveReadConflictAndLifecycle(t *testing.T) {
	b := NewBridge()
	storeHandle := responseHandle(t, b.Open(t.TempDir(), `["accounts"]`))

	events := `[{"event_id":"event-1","event_type":"AccountOpened","data":{"accountId":"a-1"},"metadata":{}}]`
	query := `{"criteria":[{"tags":[{"key":"accountId","value":"a-1"}]}]}`
	notExists := `{"commit_position":-1,"prepare_position":-1}`

	saved := responseValue(t, b.SaveEvents(storeHandle, "accounts", events, notExists, query))
	var position map[string]int64
	if err := json.Unmarshal(saved, &position); err != nil {
		t.Fatalf("decode saved position: %v", err)
	}
	if position["commit_position"] < 0 || position["prepare_position"] < 0 {
		t.Fatalf("unexpected saved position: %s", saved)
	}

	read := responseValue(t, b.GetEvents(storeHandle, "accounts", "", "", 10, false))
	var readEvents []map[string]any
	if err := json.Unmarshal(read, &readEvents); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(readEvents) != 1 || readEvents[0]["event_id"] != "event-1" {
		t.Fatalf("unexpected events: %s", read)
	}

	conflict := decodeEnvelope(t, b.SaveEvents(storeHandle, "accounts", events, notExists, query))
	if conflict.OK || conflict.Error == nil || conflict.Error.Code != "already_exists" {
		t.Fatalf("expected already_exists response, got %+v", conflict)
	}

	if response := decodeEnvelope(t, b.Close(storeHandle)); !response.OK {
		t.Fatalf("close failed: %+v", response)
	}
	closed := decodeEnvelope(t, b.GetEvents(storeHandle, "accounts", "", "", 1, false))
	if closed.OK {
		t.Fatalf("closed store read unexpectedly succeeded: %+v", closed)
	}
}

func TestBridgeSubscriptionDeliversAndStops(t *testing.T) {
	b := NewBridge()
	storeHandle := responseHandle(t, b.Open(t.TempDir(), `["accounts"]`))
	subscriptionHandle := responseHandle(t, b.Subscribe(storeHandle, "accounts", "desktop-test", "", ""))

	events := `[{"event_id":"event-1","event_type":"AccountOpened","data":{"accountId":"a-1"},"metadata":{}}]`
	responseValue(t, b.SaveEvents(storeHandle, "accounts", events, "", ""))

	next := decodeEnvelope(t, b.NextSubscriptionMessage(subscriptionHandle, 2*time.Second))
	if !next.OK || next.Kind != "event" {
		t.Fatalf("expected event response, got %+v", next)
	}
	var event map[string]any
	if err := json.Unmarshal(next.Value, &event); err != nil {
		t.Fatalf("decode subscription event: %v", err)
	}
	if event["event_id"] != "event-1" {
		t.Fatalf("unexpected subscription event: %s", next.Value)
	}

	if response := decodeEnvelope(t, b.StopSubscription(subscriptionHandle)); !response.OK {
		t.Fatalf("stop failed: %+v", response)
	}
	if response := decodeEnvelope(t, b.Close(storeHandle)); !response.OK {
		t.Fatalf("close failed: %+v", response)
	}
}

func responseHandle(t *testing.T, encoded string) uint64 {
	t.Helper()
	response := decodeEnvelope(t, encoded)
	if !response.OK || response.Handle == 0 {
		t.Fatalf("expected handle response, got %+v", response)
	}
	return response.Handle
}

func responseValue(t *testing.T, encoded string) json.RawMessage {
	t.Helper()
	response := decodeEnvelope(t, encoded)
	if !response.OK {
		t.Fatalf("expected successful response, got %+v", response)
	}
	return response.Value
}

func decodeEnvelope(t *testing.T, encoded string) envelope {
	t.Helper()
	var response envelope
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatalf("decode envelope %q: %v", encoded, err)
	}
	return response
}
