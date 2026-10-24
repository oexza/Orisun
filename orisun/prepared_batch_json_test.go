package orisun

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func TestPreparedEventBatchJSONMatchesIndividualEncoding(t *testing.T) {
	for name, batch := range map[string]PreparedEventBatch{
		"nil":   nil,
		"empty": {},
		"documents": {
			{EventId: "first", EventType: "quoted\"\\\n<&>世界", DataJSON: `{"large":9007199254740993,"decimal":0.123456789012345678901,"nested":[null,true,{"x":"<&>"}]}`, MetadataJSON: `{"trace":"a\u2028b"}`},
			{EventId: "second", EventType: "Other", DataJSON: " { \"a\" : 2 } ", MetadataJSON: `{}`},
		},
		"invalid data":     {{DataJSON: `{"broken":`, MetadataJSON: `{}`}},
		"invalid metadata": {{DataJSON: `{}`, MetadataJSON: `{"broken":`}},
		"empty raw values": {{}},
	} {
		t.Run(name, func(t *testing.T) {
			// Converting to the unnamed slice exercises the existing per-event
			// encoder, independently of the batch marshaler.
			want, wantErr := json.Marshal([]PreparedEvent(batch))
			got, err := batch.MarshalJSON()
			wrapped, wrappedErr := json.Marshal(batch)
			if wantErr != nil {
				require.Error(t, err)
				require.Error(t, wrappedErr)
				return
			}
			require.NoError(t, err)
			require.NoError(t, wrappedErr)
			require.Equal(t, string(want), string(got))
			require.Equal(t, string(want), string(wrapped))
		})
	}
}

func BenchmarkPreparedEventBatchEncoding(b *testing.B) {
	for _, count := range []int{1, 8, 128} {
		for _, size := range []int{128, 4096} {
			batch := make(PreparedEventBatch, count)
			for i := range batch {
				batch[i] = PreparedEvent{EventId: fmt.Sprint(i), EventType: "Encoding", DataJSON: `{"payload":"` + strings.Repeat("x", size) + `"}`, MetadataJSON: `{}`}
			}
			for _, individual := range []bool{true, false} {
				b.Run(fmt.Sprintf("events_%d/bytes_%d/individual_%t", count, size, individual), func(b *testing.B) {
					b.ReportAllocs()
					for range b.N {
						var err error
						if individual {
							_, err = json.Marshal([]PreparedEvent(batch))
						} else {
							_, err = batch.MarshalJSON()
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
