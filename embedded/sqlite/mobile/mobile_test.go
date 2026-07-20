package mobile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type recordingListener struct {
	events chan string
	errors chan string
}

func newRecordingListener() *recordingListener {
	return &recordingListener{events: make(chan string, 1), errors: make(chan string, 1)}
}

func (l *recordingListener) OnEvent(eventJSON string) {
	l.events <- eventJSON
}

func (l *recordingListener) OnError(message string) {
	l.errors <- message
}

func openMobileStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir(), `["mobile"]`)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

const mobileEvent = `[{"event_id":"event-1","event_type":"ValueSet","data":{"key":"counter","value":1},"metadata":{"source":"ios"}}]`
const secondMobileEvent = `[{"event_id":"event-2","event_type":"ValueSet","data":{"key":"counter","value":2},"metadata":{"source":"ios"}}]`
const counterQuery = `{"criteria":[{"tags":[{"key":"key","value":"counter"}]}]}`
const notExistsPosition = `{"commit_position":-1,"prepare_position":-1}`

func TestMobileStoreJSONRoundTripAndCCC(t *testing.T) {
	store := openMobileStore(t)

	position, err := store.SaveEvents("mobile", mobileEvent, notExistsPosition, counterQuery)
	if err != nil {
		t.Fatalf("SaveEvents() returned error: %v", err)
	}
	if position != `{"commit_position":1,"prepare_position":1}` {
		t.Fatalf("position = %s", position)
	}

	if _, err := store.SaveEvents("mobile", mobileEvent, notExistsPosition, counterQuery); err == nil {
		t.Fatal("stale SaveEvents() succeeded")
	}

	eventsJSON, err := store.GetEvents("mobile", "", "", 10, false)
	if err != nil {
		t.Fatalf("GetEvents() returned error: %v", err)
	}
	var events []map[string]any
	if err := json.Unmarshal([]byte(eventsJSON), &events); err != nil {
		t.Fatalf("decode GetEvents() response: %v", err)
	}
	if len(events) != 1 || events[0]["event_id"] != "event-1" {
		t.Fatalf("unexpected events response: %s", eventsJSON)
	}
	data := events[0]["data"].(map[string]any)
	if data["eventType"] != "ValueSet" || data["key"] != "counter" {
		t.Fatalf("unexpected event data: %#v", data)
	}

	latestJSON, err := store.GetLatestByCriteria("mobile", counterQuery)
	if err != nil {
		t.Fatalf("GetLatestByCriteria() returned error: %v", err)
	}
	if !strings.Contains(latestJSON, `"found":true`) || !strings.Contains(latestJSON, `"commit_position":1`) {
		t.Fatalf("unexpected latest response: %s", latestJSON)
	}
}

func TestMobileStoreSubscriptionCallback(t *testing.T) {
	store := openMobileStore(t)
	if _, err := store.SaveEvents("mobile", mobileEvent, "", ""); err != nil {
		t.Fatalf("save historical event: %v", err)
	}
	listener := newRecordingListener()
	subscription, err := store.Subscribe("mobile", "screen", "", "", listener)
	if err != nil {
		t.Fatalf("Subscribe() returned error: %v", err)
	}
	defer subscription.Stop()

	if _, err := store.SaveEvents("mobile", secondMobileEvent, "", ""); err != nil {
		t.Fatalf("SaveEvents() returned error: %v", err)
	}
	select {
	case event := <-listener.events:
		if !strings.Contains(event, `"event_id":"event-2"`) {
			t.Fatalf("unexpected callback event: %s", event)
		}
	case message := <-listener.errors:
		t.Fatalf("subscription error callback: %s", message)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback event")
	}

	subscription.Stop()
	deadline := time.Now().Add(time.Second)
	for !subscription.IsDone() {
		if time.Now().After(deadline) {
			t.Fatal("subscription did not stop")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMobileStoreCreatesAndDropsBoundaryIndex(t *testing.T) {
	store := openMobileStore(t)
	if err := store.CreateBoundaryIndex(
		"mobile",
		"counter_key",
		`[{"json_key":"key","value_type":"text"}]`,
		"",
		"AND",
	); err != nil {
		t.Fatalf("CreateBoundaryIndex() returned error: %v", err)
	}
	if err := store.DropBoundaryIndex("mobile", "counter_key"); err != nil {
		t.Fatalf("DropBoundaryIndex() returned error: %v", err)
	}
}

func TestMobileStoreRejectsMalformedBoundaryJSON(t *testing.T) {
	if _, err := Open(t.TempDir(), `{"mobile":true}`); err == nil {
		t.Fatal("Open() accepted non-array boundaries")
	}
}
