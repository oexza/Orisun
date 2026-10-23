package sqlite

import (
	"testing"

	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func TestPositionedDocumentsKeepEventFieldsIndependent(t *testing.T) {
	fields := map[string]any{"__commitPosition": int64(12), "__dateCreated": "2026-10-17T00:00:00Z", "__writeId": "12:12"}
	first, err := positionedDocument(prepareEventDocument(orisun.PreparedEvent{EventId: "first", EventType: "A", DataJSON: `{"amount":9007199254740993,"__eventId":"spoof","__preparePosition":999}`, MetadataJSON: `{"source":"first"}`}), 11, fields)
	require.NoError(t, err)
	second, err := positionedDocument(prepareEventDocument(orisun.PreparedEvent{EventId: "second", EventType: "B", DataJSON: `{"amount":0.123456789012345678901}`, MetadataJSON: "null"}), 12, fields)
	require.NoError(t, err)
	for i, document := range []string{first, second} {
		var got map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(document), &got))
		require.Equal(t, "12", string(got["__commitPosition"]))
		require.Equal(t, `"12:12"`, string(got["__writeId"]))
		require.Equal(t, `"2026-10-17T00:00:00Z"`, string(got["__dateCreated"]))
		require.Equal(t, []string{`"first"`, `"second"`}[i], string(got["__eventId"]))
		require.Equal(t, []string{`"A"`, `"B"`}[i], string(got["__eventType"]))
		require.Equal(t, []string{"11", "12"}[i], string(got["__preparePosition"]))
		require.Equal(t, []string{`{"source":"first"}`, "null"}[i], string(got["__metadata"]))
		require.Equal(t, []string{"9007199254740993", "0.123456789012345678901"}[i], string(got["amount"]))
	}
}
