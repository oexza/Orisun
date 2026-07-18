package orisun

import (
	"strconv"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// DefaultReadBatchSize is used by storage backends when an in-process
	// caller does not provide a page size.
	DefaultReadBatchSize uint32 = 1_000
	// MaxReadBatchSize bounds one database read and its result allocation.
	// Larger logical reads must advance by position and fetch another page.
	MaxReadBatchSize uint32 = 10_000
)

// ReadEvent is the backend-neutral, contiguous read representation. Positions
// and timestamps are scalar values so storage and internal consumers do not
// allocate protobuf Event, Position, and Timestamp objects for every row.
type ReadEvent struct {
	EventId         string
	EventType       string
	Data            string
	Metadata        string
	CommitPosition  int64
	PreparePosition int64
	DateCreated     time.Time
}

// ReadEventBatch keeps database results in one value slab. Protobuf objects
// are materialized only when a public API actually needs them.
type ReadEventBatch []ReadEvent

type protoEventRow struct {
	event     Event
	position  Position
	timestamp timestamppb.Timestamp
}

// ProtoEvent materializes one event for streaming or other public consumers.
func (e *ReadEvent) ProtoEvent() *Event {
	if e == nil {
		return nil
	}
	row := &protoEventRow{}
	fillProtoEventRow(row, e)
	return &row.event
}

func fillProtoEventRow(row *protoEventRow, read *ReadEvent) {
	if row == nil || read == nil {
		return
	}
	row.position.CommitPosition = read.CommitPosition
	row.position.PreparePosition = read.PreparePosition
	row.timestamp.Seconds = read.DateCreated.Unix()
	row.timestamp.Nanos = int32(read.DateCreated.Nanosecond())
	row.event.EventId = read.EventId
	row.event.EventType = read.EventType
	row.event.Data = read.Data
	row.event.Metadata = read.Metadata
	row.event.Position = &row.position
	row.event.DateCreated = &row.timestamp
}

// ProtoResponse materializes one contiguous row slab plus its protobuf pointer
// index rather than three heap objects per row.
func (b ReadEventBatch) ProtoResponse() *GetEventsResponse {
	rows := make([]protoEventRow, len(b))
	pointers := make([]*Event, len(b))
	for i := range b {
		read := &b[i]
		row := &rows[i]
		fillProtoEventRow(row, read)
		pointers[i] = &row.event
	}
	return &GetEventsResponse{Events: pointers}
}

// MarshalJSON preserves the NATS event envelope emitted for protobuf Events
// while keeping the publisher on the packed representation.
func (e ReadEvent) MarshalJSON() ([]byte, error) {
	buf := make([]byte, 0, 192+len(e.EventId)+len(e.EventType)+len(e.Data)+len(e.Metadata))
	buf = append(buf, '{')
	comma := false
	buf, comma = appendJSONField(buf, comma, `"event_id":`, e.EventId)
	buf, comma = appendJSONField(buf, comma, `"event_type":`, e.EventType)
	buf, comma = appendJSONField(buf, comma, `"data":`, e.Data)
	buf, comma = appendJSONField(buf, comma, `"metadata":`, e.Metadata)
	if comma {
		buf = append(buf, ',')
	}
	buf = append(buf, `"position":{`...)
	if e.CommitPosition != 0 {
		buf = append(buf, `"commit_position":`...)
		buf = strconv.AppendInt(buf, e.CommitPosition, 10)
		if e.PreparePosition != 0 {
			buf = append(buf, ',')
		}
	}
	if e.PreparePosition != 0 {
		buf = append(buf, `"prepare_position":`...)
		buf = strconv.AppendInt(buf, e.PreparePosition, 10)
	}
	buf = append(buf, `},"date_created":{`...)
	seconds := e.DateCreated.Unix()
	nanos := int32(e.DateCreated.Nanosecond())
	if seconds != 0 {
		buf = append(buf, `"seconds":`...)
		buf = strconv.AppendInt(buf, seconds, 10)
		if nanos != 0 {
			buf = append(buf, ',')
		}
	}
	if nanos != 0 {
		buf = append(buf, `"nanos":`...)
		buf = strconv.AppendInt(buf, int64(nanos), 10)
	}
	buf = append(buf, '}', '}')
	return buf, nil
}

func appendJSONField(buf []byte, comma bool, name, value string) ([]byte, bool) {
	if value == "" {
		return buf, comma
	}
	if comma {
		buf = append(buf, ',')
	}
	buf = append(buf, name...)
	buf = appendJSONString(buf, value)
	return buf, true
}

// appendJSONString mirrors the escaping used by goccy/go-json without
// allocating a temporary encoded string for every event field.
func appendJSONString(buf []byte, value string) []byte {
	const hex = "0123456789abcdef"
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(value); {
		c := value[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '\\' && c != '"' && c != '<' && c != '>' && c != '&' {
				i++
				continue
			}
			buf = append(buf, value[start:i]...)
			switch c {
			case '\\', '"':
				buf = append(buf, '\\', c)
			case '\n':
				buf = append(buf, `\n`...)
			case '\r':
				buf = append(buf, `\r`...)
			case '\t':
				buf = append(buf, `\t`...)
			default:
				buf = append(buf, `\u00`...)
				buf = append(buf, hex[c>>4], hex[c&0x0f])
			}
			i++
			start = i
			continue
		}

		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			buf = append(buf, value[start:i]...)
			buf = append(buf, `\ufffd`...)
			i++
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			buf = append(buf, value[start:i]...)
			buf = append(buf, `\u202`...)
			buf = append(buf, byte('8'+r-'\u2028'))
			i += size
			start = i
			continue
		}
		i += size
	}
	buf = append(buf, value[start:]...)
	return append(buf, '"')
}
