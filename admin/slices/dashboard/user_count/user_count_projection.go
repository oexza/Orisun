package user_count

import (
	"context"
	ev "orisun/admin/events"
	common "orisun/admin/slices/common"
	globalCommon "orisun/common"
	"orisun/eventstore"
	l "orisun/logging"
	"time"

	"github.com/goccy/go-json"
)

const (
	projectorName            = "User_Count_Projection"
	UserCountPubSubscription = "users-count"
)

type UserCountReadModel struct {
	Count uint32
}

type GetUserCount = func() (UserCountReadModel, error)
type SaveUserCount = func(uint32) error

type SubscribeToUserCount = func(consumerName string, ctx context.Context, stream *globalCommon.MessageHandler[UserCountReadModel]) error

type UserCountEventHandler struct {
	boundary                 string
	getProjectorLastPosition common.GetProjectorLastPositionType
	publishUserCountToPubSub common.PublishToPubSubType
	getUsersCount            GetUserCount
	saveUserCount            SaveUserCount
	subscribeToEventStore    common.SubscribeToEventStoreType
	updateProjectorPosition  common.UpdateProjectorPositionType
	logger                   l.Logger
}

func NewUserCountProjection(
	boundary string,
	getProjectorLastPosition common.GetProjectorLastPositionType,
	publishUsersCountToPubSub common.PublishToPubSubType,
	getUsersCount GetUserCount,
	saveUserCount SaveUserCount,
	subscribeToEventStore common.SubscribeToEventStoreType,
	updateProjectorPosition common.UpdateProjectorPositionType,
	logger l.Logger,
) *UserCountEventHandler {
	return &UserCountEventHandler{
		boundary:                 boundary,
		publishUserCountToPubSub: publishUsersCountToPubSub,
		getUsersCount:            getUsersCount,
		subscribeToEventStore:    subscribeToEventStore,
		getProjectorLastPosition: getProjectorLastPosition,
		updateProjectorPosition:  updateProjectorPosition,
		logger:                   logger,
		saveUserCount:            saveUserCount,
	}
}

func (p *UserCountEventHandler) Start(ctx context.Context) error {
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

			p.logger.Debugf("Receiving events for: %s", "users_count_projection")
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
		*stream,
	)
	if err != nil {
		return err
	}

	return nil
}

func (p *UserCountEventHandler) Project(ctx context.Context, event *eventstore.Event) error {
	switch event.EventType {
	case ev.EventTypeUserCreated:
		{
			// Get current user count
			currentCount, err := p.getUsersCount()
			if err != nil {
				return err
			}

			newCount := currentCount.Count + 1
			// Increment the count
			updatedCount := &UserCountReadModel{
				Count: newCount,
			}
			marshaled, err := json.Marshal(updatedCount)
			if err != nil {
				return err
			}
			p.saveUserCount(newCount)
			p.publishUserCountToPubSub(ctx, &eventstore.PublishRequest{
				Id:      "users-count",
				Subject: "users-count",
				Data:    marshaled,
			})
		}
	case ev.EventTypeUserDeleted:
		{
			// Get current user count
			currentCount, err := p.getUsersCount()
			if err != nil {
				return err
			}

			// Increment the count
			newCount := currentCount.Count - 1

			updatedCount := UserCountReadModel{
				Count: newCount,
			}

			marshaled, err := json.Marshal(updatedCount)
			if err != nil {
				return err
			}
			
			p.saveUserCount(newCount)
			p.publishUserCountToPubSub(ctx, &eventstore.PublishRequest{
				Id:      "users-count",
				Subject: UserCountPubSubscription,
				Data:    marshaled,
			})
		}
	}
	return nil
}
