package sqlite

import (
	"context"

	eventstore "github.com/OrisunLabs/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Save keeps backend tests concise while production accepts prepared batches
// exclusively through SavePrepared.
func (s *SqliteSaveEvents) Save(
	ctx context.Context,
	events []eventstore.EventWithMapTags,
	boundary string,
	expectedPosition *eventstore.Position,
	query *eventstore.Query,
) (string, int64, error) {
	prepared, err := eventstore.PrepareEventsForSave(events)
	if err != nil {
		return "", 0, status.Errorf(codes.InvalidArgument, "invalid event data: %v", err)
	}
	return s.SavePrepared(ctx, prepared, boundary, expectedPosition, query)
}
