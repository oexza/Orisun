package sqlite

import (
	"github.com/OrisunLabs/Orisun/internal/eventdata"
	eventstore "github.com/OrisunLabs/Orisun/orisun"
)

// The insertion boundary owns the request's reserved fields. The map is reused
// only within one accepted request; WithFields encodes without retaining it.
func positionedDocument(event eventstore.PreparedEvent, gid int64, fields map[string]any) (string, error) {
	fields["__eventId"] = event.EventId
	fields["__eventType"] = event.EventType
	fields["__metadata"] = eventdata.MetadataValue(event.MetadataJSON)
	fields["__preparePosition"] = gid
	return eventdata.WithFields(event.DataJSON, fields)
}
