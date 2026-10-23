package eventdata

import (
	"fmt"

	"github.com/goccy/go-json"
)

// WithFields builds a stored document without converting application numbers
// through floating point. Only the backend may supply reserved fields.
func WithFields(data string, values map[string]any) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &fields); err != nil {
		return "", err
	}
	if fields == nil {
		return "", fmt.Errorf("event data must be an object")
	}
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
