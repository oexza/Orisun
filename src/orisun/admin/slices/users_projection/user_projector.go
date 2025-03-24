package users_projection

import (
	"context"
	"encoding/json"
	ev "orisun/src/orisun/admin/events"
	common "orisun/src/orisun/admin/slices/common"
	"orisun/src/orisun/eventstore"
	l "orisun/src/orisun/logging"
	"time"
	globalCommon "orisun/src/orisun/common"
)

type CreateNewUserType = func(id string, username string, password_hash string, name string, roles []ev.Role) error
type DeleteUserType = func(id string) error

type CountUsersType = func() error

type UserProjector struct {
	getProjectorLastPosition common.GetProjectorLastPositionType
	updateProjectorPosition  common.UpdateProjectorPositionType
	createNewUser            CreateNewUserType
	deleteUser               DeleteUserType
	logger                   l.Logger
	boundary                 string
	saveEvents               common.SaveEventsType
	getEvents                common.GetEventsType
	subscribeToEvents        common.SubscribeToEventStoreType
	countUsers               CountUsersType
	publishToPubSub          common.PublishToPubSubType
}

func NewUserProjector(
	getProjectorLastPosition common.GetProjectorLastPositionType,
	updateProjectorPosition common.UpdateProjectorPositionType,
	createNewUser CreateNewUserType,
	deleteUser DeleteUserType,
	saveEvents common.SaveEventsType,
	getEvents common.GetEventsType,
	logger l.Logger,
	boundary string,
	subscribeToEvents common.SubscribeToEventStoreType,
) *UserProjector {

	return &UserProjector{
		getProjectorLastPosition: getProjectorLastPosition,
		updateProjectorPosition:  updateProjectorPosition,
		createNewUser:            createNewUser,
		deleteUser:               deleteUser,
		logger:                   logger,
		boundary:                 boundary,
		saveEvents:               saveEvents,
		getEvents:                getEvents,
		subscribeToEvents:        subscribeToEvents,
	}
}

func (p *UserProjector) Start(ctx context.Context) error {
	p.logger.Info("Starting user projector")
	var projectorName = "user-projector"
	// Get last checkpoint
	pos, err := p.getProjectorLastPosition(projectorName)
	if err != nil {
		return err
	}

	stream := globalCommon.NewMessageHandler[eventstore.Event](ctx)

	go func() {
		for {
			p.logger.Debugf("Receiving events for: %s", projectorName)
			event, err := stream.Recv()
			if err != nil {
				p.logger.Error("Error receiving event: %v", err)
				continue
			}

			for {
				if err := p.handleEvent(event); err != nil {
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
	// Subscribe from last checkpoint
	err = p.subscribeToEvents(
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

func (p *UserProjector) handleEvent(event *eventstore.Event) error {
	p.logger.Debug("Handling event %v", event)

	switch event.EventType {
	case ev.EventTypeUserCreated:
		var userEvent ev.UserCreated
		if err := json.Unmarshal([]byte(event.Data), &userEvent); err != nil {
			return err
		}

		err := p.createNewUser(
			userEvent.UserId,
			userEvent.Username,
			userEvent.PasswordHash,
			userEvent.Name,
			userEvent.Roles,
		)

		if err != nil {
			return err
		}

	case ev.EventTypeUserDeleted:
		var userEvent ev.UserDeleted
		if err := json.Unmarshal([]byte(event.Data), &userEvent); err != nil {
			return err
		}

		err := p.deleteUser(userEvent.UserId)
		if err != nil {
			return err
		}

		// case EventTypeRolesChanged:
		// 	_, err = tx.Exec(
		// 		fmt.Sprintf("UPDATE %s.users SET roles = $1 WHERE username = $2",
		// 			p.schema),
		// 		userEvent.Roles, userEvent.Username,
		// 	)

		// case EventTypePasswordChanged:
		// 	_, err = tx.Exec(
		// 		fmt.Sprintf("UPDATE %s.users SET password_hash = $1 WHERE username = $2",
		// 			p.schema),
		// 		userEvent.PasswordHash, userEvent.Username,
		// 	)
	}
	return nil
}
