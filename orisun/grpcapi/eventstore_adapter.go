//go:build !orisun_embedded

package grpcapi

import (
	"context"

	"github.com/OrisunLabs/Orisun/internal/grpcstatus"
	"github.com/OrisunLabs/Orisun/orisun"
)

// EventStoreAdapter connects the generated gRPC transport contract to the
// transport-independent EventStore implementation.
type EventStoreAdapter struct {
	UnimplementedEventStoreServer
	eventStore *orisun.EventStore
}

func AdaptEventStore(eventStore *orisun.EventStore) *EventStoreAdapter {
	return &EventStoreAdapter{eventStore: eventStore}
}

func (a *EventStoreAdapter) SaveEvents(ctx context.Context, req *orisun.SaveEventsRequest) (*orisun.WriteResult, error) {
	response, err := a.eventStore.SaveEvents(ctx, req)
	return response, grpcstatus.FromError(err)
}

func (a *EventStoreAdapter) GetEvents(ctx context.Context, req *orisun.GetEventsRequest) (*orisun.GetEventsResponse, error) {
	response, err := a.eventStore.GetEvents(ctx, req)
	return response, grpcstatus.FromError(err)
}

func (a *EventStoreAdapter) GetLatestByCriteria(ctx context.Context, req *orisun.GetLatestByCriteriaRequest) (*orisun.GetLatestByCriteriaResponse, error) {
	response, err := a.eventStore.GetLatestByCriteria(ctx, req)
	return response, grpcstatus.FromError(err)
}

func (a *EventStoreAdapter) CatchUpSubscribeToEvents(req *orisun.CatchUpSubscribeToEventStoreRequest, stream EventStore_CatchUpSubscribeToEventsServer) error {
	return grpcstatus.FromError(a.eventStore.CatchUpSubscribeToEvents(req, stream))
}

func (a *EventStoreAdapter) Ping(ctx context.Context, req *orisun.PingRequest) (*orisun.PingResponse, error) {
	response, err := a.eventStore.Ping(ctx, req)
	return response, grpcstatus.FromError(err)
}

func (a *EventStoreAdapter) CreateIndex(ctx context.Context, req *orisun.CreateIndexRequest) (*orisun.CreateIndexResponse, error) {
	response, err := a.eventStore.CreateIndex(ctx, req)
	return response, grpcstatus.FromError(err)
}

func (a *EventStoreAdapter) DropIndex(ctx context.Context, req *orisun.DropIndexRequest) (*orisun.DropIndexResponse, error) {
	response, err := a.eventStore.DropIndex(ctx, req)
	return response, grpcstatus.FromError(err)
}
