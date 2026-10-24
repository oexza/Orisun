package eventdata

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func TestWithFieldsPreservesApplicationJSON(t *testing.T) {
	fields := map[string]any{"__position": int64(9007199254740993), "__metadata": json.RawMessage(`{"trace":"a"}`), "__type": "quoted\"\\<&>"}
	got, err := WithFields(`{"number":9007199254740993,"decimal":0.123456789012345678901,"nested":{"__position":1},"__position":-1}`, fields)
	require.NoError(t, err)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(got), &decoded))
	require.Equal(t, "9007199254740993", string(decoded["number"]))
	require.Equal(t, "0.123456789012345678901", string(decoded["decimal"]))
	require.Equal(t, "9007199254740993", string(decoded["__position"]))
	require.JSONEq(t, `{"__position":1}`, string(decoded["nested"]))
	require.JSONEq(t, `{"trace":"a"}`, string(decoded["__metadata"]))
	require.Len(t, fields, 3, "caller-owned values must not be changed")
	for _, empty := range []map[string]any{nil, {}} {
		got, err := WithFields(`{"number":9007199254740993}`, empty)
		require.NoError(t, err)
		require.Equal(t, `{"number":9007199254740993}`, got)
	}
	for _, invalid := range []string{"null", "[]", `{"broken":`, `{} {}`} {
		_, err := WithFields(invalid, fields)
		require.Error(t, err, invalid)
	}
	_, err = WithFields(`{}`, map[string]any{"__metadata": json.RawMessage(`{"broken":`)})
	require.Error(t, err)
}

// Retain the previous per-field encoder only as a benchmark reference, so both
// implementations can be measured in the same process and environment.
func marshalFieldsIndividually(data string, values map[string]any) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &fields); err != nil {
		return "", err
	}
	for key, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		fields[key] = encoded
	}
	encoded, err := json.Marshal(fields)
	return string(encoded), err
}

func BenchmarkEnvelopeFields(b *testing.B) {
	for _, size := range []int{16, 4096} {
		data := `{"payload":"` + strings.Repeat("x", size) + `","number":9007199254740993}`
		fields := map[string]any{"__eventId": "event", "__eventType": "Fact", "__metadata": json.RawMessage(`{}`), "__dateCreated": "2026-10-16T00:00:00Z", "__commitPosition": int64(10), "__preparePosition": int64(9), "__writeId": "10:10"}
		for _, individual := range []bool{true, false} {
			b.Run(fmt.Sprintf("bytes_%d/individual_%t", size, individual), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					var err error
					if individual {
						_, err = marshalFieldsIndividually(data, fields)
					} else {
						_, err = WithFields(data, fields)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestPreparedObjectCanBeEncodedIndependently(t *testing.T) {
	object, err := ParseObject(`{"number":9007199254740993,"decimal":0.123456789012345678901,"position":-1}`)
	require.NoError(t, err)
	for i := 0; i < 32; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			got, err := object.WithFields(map[string]any{"position": i, "temporary": true})
			require.NoError(t, err)
			var decoded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(got), &decoded))
			require.Equal(t, fmt.Sprint(i), string(decoded["position"]))
			require.Equal(t, "9007199254740993", string(decoded["number"]))
			require.Equal(t, "0.123456789012345678901", string(decoded["decimal"]))
			original, err := object.WithFields(nil)
			require.NoError(t, err)
			require.NotContains(t, original, "temporary")
			require.Contains(t, original, `"position":-1`)
		})
	}
}
