package delete_user

import (
	"context"
	"fmt"
	"net/http"
	admin_common "orisun/admin/slices/common"
	"orisun/admin/templates"
	l "orisun/logging"
	"strings"

	"github.com/goccy/go-json"

	"orisun/admin/events"
	eventstore "orisun/eventstore"

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
	currentUser, err := admin_common.GetCurrentUser(r)
	sse := datastar.NewSSE(w, r)

	if err != nil {
		sse.RemoveElement("#alert")
		sse.PatchElementTempl(templates.Alert(err.Error(), templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}

	if err := dUH.deleteUser(r.Context(), userId, currentUser); err != nil {
		sse.RemoveElement("#alert")
		sse.PatchElementTempl(templates.Alert(err.Error(), templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		// time.Sleep(1000 * time.Millisecond)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}
	sse.PatchElementTempl(templates.Alert("User Deleted", templates.AlertSuccess), datastar.WithSelector("body"),
		datastar.WithModePrepend(),
	)
	// time.Sleep(1000 * time.Millisecond)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.RemoveElement("#user_" + userId)
}

func (dUH *DeleteUserHandler) deleteUser(ctx context.Context, userId string, currentUserId string) error {
	userId = strings.TrimSpace(userId)
	currentUserId = strings.TrimSpace(currentUserId)
	dUH.logger.Debug("Current Userrrr: " + currentUserId)

	if userId == currentUserId {
		return fmt.Errorf("You cannot delete your own account")
	}
	dUH.logger.Infof("Deleting user: %s", userId)
	evts, err := dUH.getEvents(
		ctx,
		&eventstore.GetEventsRequest{
			Boundary:  dUH.boundary,
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
			Stream: &eventstore.GetStreamQuery{
				Name:        events.AdminStream,
				FromVersion: 999999999,
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
		lastExpectedVersion := -1

		lastExpectedVersion = int(evts.Events[len(evts.Events)-1].Version)

		_, err = dUH.saveEvents(ctx, &eventstore.SaveEventsRequest{
			Boundary: dUH.boundary,
			// ConsistencyCondition: nil,
			Stream: &eventstore.SaveStreamQuery{
				Name:            events.AdminStream,
				ExpectedVersion: int64(lastExpectedVersion),
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
				// Tags: []*eventstore.Tag{
				// 	{Key: events.RegistrationTag, Value: userId},
				// },
				Metadata: "{\"schema\":\"" + dUH.boundary + "\",\"createdBy\":\"" + id.String() + "\"}",
			}},
		})

		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("error: user not found")
}
