package delete_user

import (
	"context"
	"fmt"
	"net/http"
	"orisun/admin/slices/common"
	"orisun/admin/templates"
	l "orisun/logging"
	"strings"

	"github.com/goccy/go-json"

	"orisun/admin/events"
	eventstore "orisun/eventstore"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar/sdk/go"
)

type DeleteUserHandler struct {
	logger     l.Logger
	saveEvents common.SaveEventsType
	getEvents  common.GetEventsType
	boundary   string
}

func NewDeleteUserHandler(logger l.Logger, saveEvents common.SaveEventsType, getEvents common.GetEventsType, boundary string) *DeleteUserHandler {
	return &DeleteUserHandler{
		logger:     logger,
		saveEvents: saveEvents,
		getEvents:  getEvents,
		boundary:   boundary,
	}
}

func (s *DeleteUserHandler) HandleUserDelete(w http.ResponseWriter, r *http.Request) {
	userId := chi.URLParam(r, "userId")
	currentUser, err := common.GetCurrentUser(r)
	sse := datastar.NewSSE(w, r)

	if err != nil {
		sse.RemoveFragments("#alert")
		sse.MergeFragmentTempl(templates.Alert(err.Error(), templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}

	if err := s.deleteUser(r.Context(), userId, currentUser); err != nil {
		sse.RemoveFragments("#alert")
		sse.MergeFragmentTempl(templates.Alert(err.Error(), templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
		)
		// time.Sleep(1000 * time.Millisecond)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}
	sse.MergeFragmentTempl(templates.Alert("User Deleted", templates.AlertSuccess), datastar.WithSelector("body"),
		datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
	)
	// time.Sleep(1000 * time.Millisecond)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.RemoveFragments("#user_" + userId)
}

func (s *DeleteUserHandler) deleteUser(ctx context.Context, userId string, currentUserId string) error {
	userId = strings.TrimSpace(userId)
	currentUserId = strings.TrimSpace(currentUserId)
	s.logger.Debug("Current Userrrr: " + currentUserId)

	if userId == currentUserId {
		return fmt.Errorf("You cannot delete your own account")
	}
	evts, err := s.getEvents(
		ctx,
		&eventstore.GetEventsRequest{
			Boundary:  s.boundary,
			Direction: eventstore.Direction_DESC,
			Count:     2,
			Stream: &eventstore.GetStreamQuery{
				Name:        events.UserStreamPrefix + userId,
				FromVersion: 999999999,
				SubsetQuery: &eventstore.Query{
					Criteria: []*eventstore.Criterion{
						{
							Tags: []*eventstore.Tag{
								{Key: "eventType", Value: events.EventTypeUserCreated},
							},
						},
						{
							Tags: []*eventstore.Tag{
								{Key: "eventType", Value: events.EventTypeUserDeleted},
							},
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
		lastExpectedVersion := 0

		lastExpectedVersion = int(evts.Events[len(evts.Events)-1].Version)

		_, err = s.saveEvents(ctx, &eventstore.SaveEventsRequest{
			Boundary:             s.boundary,
			ConsistencyCondition: nil,
			Stream: &eventstore.SaveStreamQuery{
				Name:            events.UserStreamPrefix + userId,
				ExpectedVersion: int32(lastExpectedVersion),
			},
			Events: []*eventstore.EventToSave{{
				EventId:   id.String(),
				EventType: events.EventTypeUserDeleted,
				Data:      string(eventData),
				Tags: []*eventstore.Tag{
					{Key: events.RegistrationTag, Value: userId},
				},
				Metadata: "{\"schema\":\"" + s.boundary + "\",\"createdBy\":\"" + id.String() + "\"}",
			}},
		})

		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("error: user not found")
}
