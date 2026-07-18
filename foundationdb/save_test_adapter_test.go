//go:build foundationdb

package foundationdb

import (
	"context"

	eventstore "github.com/oexza/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Save keeps backend tests concise while production accepts prepared batches
// exclusively through SavePrepared.
func (b *Backend) Save(
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
	return b.SavePrepared(ctx, prepared, boundary, expectedPosition, query)
}
