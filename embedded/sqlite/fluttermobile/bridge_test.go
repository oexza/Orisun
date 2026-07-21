package fluttermobile

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestBridgeUsesDesktopProtocol(t *testing.T) {
	bridge := NewBridge()
	if bridge.ABIVersion() != 1 {
		t.Fatalf("ABI version = %d", bridge.ABIVersion())
	}
	opened := decodeEnvelope(t, bridge.Open(filepath.Join(t.TempDir(), "data"), `["orders"]`))
	handle := int64(opened["handle"].(float64))

	saved := decodeEnvelope(t, bridge.SaveEvents(handle, "orders", `[{"event_id":"1","event_type":"created","data":{"order_id":"1"}}]`, "", ""))
	if saved["ok"] != true {
		t.Fatalf("save response = %#v", saved)
	}
	read := decodeEnvelope(t, bridge.GetEvents(handle, "orders", "", "", 10, false))
	if read["ok"] != true {
		t.Fatalf("read response = %#v", read)
	}
	closed := decodeEnvelope(t, bridge.Close(handle))
	if closed["ok"] != true {
		t.Fatalf("close response = %#v", closed)
	}
}

func decodeEnvelope(t *testing.T, encoded string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(encoded), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
