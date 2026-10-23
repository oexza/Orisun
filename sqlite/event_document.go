package sqlite

import (
	"github.com/OrisunLabs/Orisun/internal/eventdata"
	eventstore "github.com/OrisunLabs/Orisun/orisun"
)

type preparedEventDocument struct {
	eventID   string
	eventType string
	metadata  string
	object    eventdata.Object
	err       error
}

func prepareEventDocument(event eventstore.PreparedEvent) preparedEventDocument {
	object, err := eventdata.ParseObject(event.DataJSON)
	return preparedEventDocument{eventID: event.EventId, eventType: event.EventType, metadata: event.MetadataJSON, object: object, err: err}
}

func prepareEventDocuments(events eventstore.PreparedEventBatch) []preparedEventDocument {
	prepared := make([]preparedEventDocument, len(events))
	for i, event := range events {
		prepared[i] = prepareEventDocument(event)
	}
	return prepared
}

// The insertion boundary owns the request's reserved fields. The map is reused
// only within one accepted request; WithFields encodes without retaining it.
func positionedDocument(event preparedEventDocument, gid int64, fields map[string]any) (string, error) {
	// Keep validation errors at the insertion boundary: CCC rejection must
	// retain precedence, and failed requests must not affect their neighbors.
	if event.err != nil {
		return "", event.err
	}
	fields["__eventId"] = event.eventID
	fields["__eventType"] = event.eventType
	fields["__metadata"] = eventdata.MetadataValue(event.metadata)
	fields["__preparePosition"] = gid
	return event.object.WithFields(fields)
}
