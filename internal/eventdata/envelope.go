package eventdata

import (
	"fmt"

	"github.com/goccy/go-json"
)

// Object owns parsed application JSON. Its fields are private and never mutated,
// so backends can prepare it before acquiring writer ownership and encode it
// repeatedly after assigning positions without reparsing or losing precision.
type Object struct {
	fields map[string]json.RawMessage
}

func ParseObject(data string) (Object, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &fields); err != nil {
		return Object{}, err
	}
	if fields == nil {
		return Object{}, fmt.Errorf("event data must be an object")
	}
	return Object{fields: fields}, nil
}

// WithFields parses and encodes a document in one call.
func WithFields(data string, values map[string]any) (string, error) {
	object, err := ParseObject(data)
	if err != nil {
		return "", err
	}
	return object.WithFields(values)
}

// WithFields encodes assigned fields without changing the prepared object.
func (object Object) WithFields(values map[string]any) (string, error) {
	if object.fields == nil {
		return "", fmt.Errorf("event data must be an object")
	}
	fields := object.fields
	// Keep application values as raw JSON, but let the final encoder marshal
	// all supplied fields together instead of allocating an encoding per field.
	merged := make(map[string]any, len(fields)+len(values))
	for key, value := range fields {
		if _, replaced := values[key]; !replaced {
			merged[key] = value
		}
	}
	for key, value := range values {
		merged[key] = value
	}
	encoded, err := json.Marshal(merged)
	return string(encoded), err
}

func MetadataValue(metadata string) json.RawMessage {
	if metadata == "" {
		return json.RawMessage(`null`)
	}
	return json.RawMessage(metadata)
}
