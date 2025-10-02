package event_count

import (
	"context"
	admin_common "orisun/admin/slices/common"
	globalCommon "orisun/common"
	eventstore "orisun/eventstore"
	l "orisun/logging"
	"time"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
)

const (
	projectorName             = "Event_Count_Projection"
	EventCountPubSubscription = "events-count"
)

type EventCountReadModel struct {
	Count int
	Boundary string
}

type GetEventCount = func(string) (EventCountReadModel, error)
type SaveEventCount = func(int, string) error

type SubscribeToEventCount = func(consumerName string, boundary string, ctx context.Context, stream *globalCommon.MessageHandler[EventCountReadModel]) error

type EventCountEventHandler struct {
	boundary                  string
	getProjectorLastPosition  admin_common.GetProjectorLastPositionType
	publishEventCountToPubSub admin_common.PublishToPubSubType
	getEventsCount            GetEventCount
	saveEventCount            SaveEventCount
	subscribeToEventStore     admin_common.SubscribeToEventStoreType
	updateProjectorPosition   admin_common.UpdateProjectorPositionType
	logger                    l.Logger
}

func NewEventCountProjection(
	boundary string,
	getProjectorLastPosition admin_common.GetProjectorLastPositionType,
	publishEventCountToPubSub admin_common.PublishToPubSubType,
	getEventsCount GetEventCount,
	saveEventCount SaveEventCount,
	subscribeToEventStore admin_common.SubscribeToEventStoreType,
	updateProjectorPosition admin_common.UpdateProjectorPositionType,
	logger l.Logger,
) *EventCountEventHandler {
	return &EventCountEventHandler{
		boundary:                  boundary,
		publishEventCountToPubSub: publishEventCountToPubSub,
		getEventsCount:            getEventsCount,
		subscribeToEventStore:     subscribeToEventStore,
		getProjectorLastPosition:  getProjectorLastPosition,
		updateProjectorPosition:   updateProjectorPosition,
		logger:                    logger,
		saveEventCount:            saveEventCount,
	}
}

func (p *EventCountEventHandler) Start(ctx context.Context) error {
	stream := globalCommon.NewMessageHandler[eventstore.Event](ctx)

	// Get last checkpoint
	pos, err := p.getProjectorLastPosition(projectorName)
	if err != nil {
		return err
	}

	go func() {
		for {
			if ctx.Err() != nil {
				return
			}

			p.logger.Debugf("Receiving events for: %s", "events_count_projection")
			event, err := stream.Recv()
			if err != nil {
				p.logger.Error("Error receiving event: %v", err)
				continue
			}

			for {
				if err := p.Project(ctx, event); err != nil {
					p.logger.Error("Error handling event: %v", err)

					time.Sleep(5 * time.Second)
					continue
				}

				var pos = eventstore.Position{
					CommitPosition:  event.Position.CommitPosition,
					PreparePosition: event.Position.PreparePosition,
				}

				// Update checkpoint
				err := p.updateProjectorPosition(
					projectorName,
					&pos,
				)

				if err != nil {
					p.logger.Error("Error updating checkpoint: %v", err)
					time.Sleep(5 * time.Second)
					continue
				}
				break
			}
		}
	}()

	err = p.subscribeToEventStore(
		ctx,
		p.boundary,
		projectorName,
		pos,
		nil,
		stream,
	)
	if err != nil {
		return err
	}

	return nil
}

func (p *EventCountEventHandler) Project(ctx context.Context, event *eventstore.Event) error {
	// For any event type, we increment the event count
	// p.logger.Infof("Projecting event: %v", event)
	// Get current event count
	currentCount, err := p.getEventsCount(p.boundary)
	if err != nil {
		return err
	}

	newCount := currentCount.Count + 1
	// Increment the count
	updatedCount := &EventCountReadModel{
		Count:    newCount,
		Boundary: p.boundary,
	}
	marshaled, err := json.Marshal(updatedCount)
	if err != nil {
		return err
	}
	p.saveEventCount(newCount, p.boundary)
	eventId, err := uuid.NewV7()
	if err != nil {
		return err
	}
	p.publishEventCountToPubSub(ctx, &admin_common.PublishRequest{
		Id:      eventId.String(),
		Subject: EventCountPubSubscription,
		Data:    marshaled,
	})

	return nil
}
