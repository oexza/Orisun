//go:build !orisun_embedded

package grpcapi

import (
	"context"
	"testing"

	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type codedErrorSaver struct{}

func (codedErrorSaver) SavePrepared(
	context.Context,
	orisun.PreparedEventBatch,
	string,
	*orisun.Position,
	*orisun.Query,
) (string, int64, error) {
	return "", 0, statuscode.New(statuscode.AlreadyExists, "position conflict")
}

func TestEventStoreAdapterTranslatesStatusAtGRPCBoundary(t *testing.T) {
	logger, err := logging.ZapLogger("error")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	boundaries := []string{}
	eventStore := orisun.NewEventStoreServer(
		context.Background(), nil, codedErrorSaver{}, nil, nil, nil,
		&boundaries, orisun.EventStreamConfig{}, logger,
	)

	_, err = AdaptEventStore(eventStore).SaveEvents(context.Background(), &orisun.SaveEventsRequest{
		Boundary: "test",
		Events: []*orisun.EventToSave{{
			EventId:   "event-1",
			EventType: "Created",
			Data:      `{}`,
			Metadata:  `{}`,
		}},
	})
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("status.Code() = %v, want %v (err: %v)", got, codes.AlreadyExists, err)
	}
}
