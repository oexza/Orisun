package delete_user

import (
	"context"
	"fmt"
	admin_common "github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/admin/templates"
	l "github.com/oexza/Orisun/logging"
	"net/http"
	"strings"

	"github.com/goccy/go-json"

	"github.com/oexza/Orisun/admin/events"
	eventstore "github.com/oexza/Orisun/orisun"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar-go/datastar"
)

type DeleteUserHandler struct {
	logger     l.Logger
	saveEvents admin_common.SaveEventsType
	getEvents  admin_common.GetEventsType
	boundary   string
}

func NewDeleteUserHandler(logger l.Logger, saveEvents admin_common.SaveEventsType, getEvents admin_common.GetEventsType, boundary string) *DeleteUserHandler {
	return &DeleteUserHandler{
		logger:     logger,
		saveEvents: saveEvents,
		getEvents:  getEvents,
		boundary:   boundary,
	}
}

func (dUH *DeleteUserHandler) HandleUserDelete(w http.ResponseWriter, r *http.Request) {
	userId := chi.URLParam(r, "userId")
	currentUser := admin_common.GetCurrentUser(r)
	sse := datastar.NewSSE(w, r)

	if err := dUH.deleteUser(r.Context(), userId, currentUser.Id); err != nil {
		sse.RemoveElement("#alert")
		sse.PatchElementTempl(templates.Alert(err.Error(), templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}
	sse.PatchElementTempl(templates.Alert("User Deleted", templates.AlertSuccess), datastar.WithSelector("body"),
		datastar.WithModePrepend(),
	)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.RemoveElement("#user_" + userId)
}

func (dUH *DeleteUserHandler) deleteUser(ctx context.Context, userId string, currentUserId string) error {
	return DeleteUser(
		ctx,
		userId,
		currentUserId,
		dUH.boundary,
		dUH.saveEvents,
		dUH.getEvents,
		dUH.logger,
	)
}

func DeleteUser(
	ctx context.Context,
	userId string,
	currentUserId string,
	boundary string,
	saveEvents admin_common.SaveEventsType,
	getEvents admin_common.GetEventsType,
	logger l.Logger,
) error {
	userId = strings.TrimSpace(userId)
	currentUserId = strings.TrimSpace(currentUserId)
	logger.Debug("Current Userrrr: " + currentUserId)

	if userId == currentUserId {
		return fmt.Errorf("You cannot delete your own account")
	}
	logger.Infof("Deleting user: %s", userId)
	evts, err := getEvents(
		ctx,
		&eventstore.GetEventsRequest{
			Boundary:  boundary,
			Count:     2,
			Direction: eventstore.Direction_DESC,
			Query: &eventstore.Query{
				Criteria: []*eventstore.Criterion{
					{
						Tags: []*eventstore.Tag{
							{Key: "user_id", Value: userId},
							{Key: "eventType", Value: events.EventTypeUserCreated},
						},
					},
					{
						Tags: []*eventstore.Tag{
							{Key: "user_id", Value: userId},
							{Key: "eventType", Value: events.EventTypeUserDeleted},
						},
					},
				},
			},
		},
	)
	if err != nil {
		return err
	}

	if len(evts.Events) > 0 {
		for i := 0; i < len(evts.Events); i++ {
			if evts.Events[i].EventType == events.EventTypeUserDeleted {
				return fmt.Errorf("error: user already deleted")
			}
		}

		event := events.UserDeleted{
			UserId: userId,
		}

		eventData, err := json.Marshal(event)
		if err != nil {
			return err
		}

		// Store event
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		lastExpectedVersion := evts.Events[len(evts.Events)-1].Position

		_, err = saveEvents(ctx, &eventstore.SaveEventsRequest{
			Boundary: boundary,
			Query: &eventstore.SaveQuery{
				ExpectedPosition: lastExpectedVersion,
				SubsetQuery: &eventstore.Query{
					Criteria: []*eventstore.Criterion{
						{
							Tags: []*eventstore.Tag{
								{Key: "user_id", Value: userId},
								{Key: "eventType", Value: events.EventTypeUserCreated},
							},
						},
						{
							Tags: []*eventstore.Tag{
								{Key: "user_id", Value: userId},
								{Key: "eventType", Value: events.EventTypeUserDeleted},
							},
						},
					},
				},
			},
			Events: []*eventstore.EventToSave{{
				EventId:   id.String(),
				EventType: events.EventTypeUserDeleted,
				Data:      string(eventData),
				Metadata:  "{\"schema\":\"" + boundary + "\",\"createdBy\":\"" + id.String() + "\"}",
			}},
		})

		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("error: user not found")
}
