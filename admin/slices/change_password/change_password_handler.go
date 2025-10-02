package changepassword

import (
	"context"
	"errors"
	"net/http"
	admin_common "orisun/admin/slices/common"
	"orisun/admin/templates"
	"orisun/eventstore"
	l "orisun/logging"
	// "sync"

	admin_events "orisun/admin/events"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/starfederation/datastar-go/datastar"
	"golang.org/x/sync/errgroup"
)

type ChangePasswordHandler struct {
	logger     l.Logger
	boundary   string
	saveEvents admin_common.SaveEventsType
	getEvents  admin_common.GetEventsType
}

func NewChangePasswordHandler(
	logger l.Logger,
	boundary string,
	saveEvents admin_common.SaveEventsType,
	getEvents admin_common.GetEventsType) *ChangePasswordHandler {
	return &ChangePasswordHandler{
		logger:     logger,
		boundary:   boundary,
		getEvents:  getEvents,
		saveEvents: saveEvents,
	}
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

func (r *ChangePasswordRequest) validate() error {
	if r.CurrentPassword == "" {
		return errors.New("current password is required")
	}
	if r.NewPassword == "" {
		return errors.New("new password is required")
	}
	if r.ConfirmPassword == "" {
		return errors.New("confirm password is required")
	}
	if r.NewPassword != r.ConfirmPassword {
		return errors.New("new password and confirm password do not match")
	}
	return nil
}

func (s *ChangePasswordHandler) HandleChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)

	sse.PatchElementTempl(ChangePasswordDialog(), datastar.WithModeReplace())
	sse.ExecuteScript("setTimeout(() => { document.querySelector('#change-password-dialog').show() }, 1)")
}

func (s *ChangePasswordHandler) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	currentUser := admin_common.GetCurrentUser(r)
	if currentUser == nil {
		sse := datastar.NewSSE(w, r)
		sse.PatchElementTempl(templates.Alert("You must be logged in to change your password", templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}

	changePasswordRequest := &ChangePasswordRequest{}

	response := struct {
		Message string `json:"message"`
		Success bool   `json:"success"`
		Failed  bool   `json:"failed"`
	}{}

	if err := datastar.ReadSignals(r, changePasswordRequest); err != nil {
		sse := datastar.NewSSE(w, r)

		s.logger.Infof("Error reading signals %v", err)
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndPatchSignals(response)
		return
	}

	if err := changePasswordRequest.validate(); err != nil {
		sse := datastar.NewSSE(w, r)
		s.logger.Infof("Handling change password request %v", &changePasswordRequest)

		sse.PatchElementTempl(templates.Alert(err.Error(), templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}

	// Call the changePassword function with concurrent event fetching
	err := changePassword(
		r.Context(),
		changePasswordRequest.CurrentPassword,
		changePasswordRequest.NewPassword,
		s.boundary,
		s.saveEvents,
		s.getEvents,
		s.logger,
		currentUser.Id,
	)

	sse := datastar.NewSSE(w, r)
	if err != nil {
		sse.PatchElementTempl(templates.Alert(err.Error(), templates.AlertDanger), datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}

	// Password changed successfully
	sse.PatchElementTempl(templates.Alert("Password changed successfully", templates.AlertSuccess), datastar.WithSelector("body"),
		datastar.WithModePrepend(),
	)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.ExecuteScript("document.querySelector('#change-password-dialog').hide()")
	response.Success = true
	response.Message = "Password changed successfully"
	sse.MarshalAndPatchSignals(response)
}

func changePassword(
	ctx context.Context,
	currentPassword, newPassword string,
	boundary string,
	saveEvents admin_common.SaveEventsType,
	getEvents admin_common.GetEventsType,
	logger l.Logger,
	currentUserId string,
) error {
	// Use goroutines to fetch events concurrently with errgroup for cancellation
	var userCreated, userDeleted, passwordChanged *eventstore.GetEventsResponse

	// Create a common request template
	baseRequest := eventstore.GetEventsRequest{
		Boundary:  boundary,
		Count:     1,
		Direction: eventstore.Direction_DESC,
		Stream: &eventstore.GetStreamQuery{
			Name:        admin_events.AdminStream,
			FromVersion: 999999999999999999,
		},
	}

	// Fetch UserCreated event
	// Create a true copy of the base request, then take its address for getEvents
	reqCopy := baseRequest
	reqCopy.Query = &eventstore.Query{
		Criteria: []*eventstore.Criterion{
			{
				Tags: []*eventstore.Tag{
					{Key: "user_id", Value: currentUserId},
					{Key: "eventType", Value: admin_events.EventTypeUserCreated},
				},
			},
		},
	}
	var err error
	userCreated, err = getEvents(ctx, &reqCopy)
	if err != nil {
		return err
	}

	// Concurrently fetch UserDeleted and PasswordChanged using errgroup
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		// UserDeleted
		req := baseRequest
		req.Query = &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "user_id", Value: currentUserId},
						{Key: "eventType", Value: admin_events.EventTypeUserDeleted},
					},
				},
			},
		}
		resp, err := getEvents(gctx, &req)
		if err != nil {
			return err
		}
		userDeleted = resp
		return nil
	})

	g.Go(func() error {
		// PasswordChanged
		req := baseRequest
		req.Query = &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "user_id", Value: currentUserId},
						{Key: "eventType", Value: admin_events.EventTypeUserPasswordChanged},
					},
				},
			},
		}
		resp, err := getEvents(gctx, &req)
		if err != nil {
			return err
		}
		passwordChanged = resp
		return nil
	})

	if err := g.Wait(); err != nil {
		return err
	}

	// Validate user existence and not deleted
	if userCreated == nil || len(userCreated.Events) == 0 {
		return errors.New("user not found")
	}
	if userDeleted != nil && len(userDeleted.Events) > 0 {
		return errors.New("user not found")
	}

	// Determine current password hash
	var currentPasswordHash string
	if passwordChanged != nil && len(passwordChanged.Events) > 0 {
		var passwordChangedEventData admin_events.UserPasswordChanged
		if err := json.Unmarshal([]byte(passwordChanged.Events[0].Data), &passwordChangedEventData); err != nil {
			return err
		}
		currentPasswordHash = passwordChangedEventData.PasswordHash
	} else {
		var userCreatedEventData admin_events.UserCreated
		if err := json.Unmarshal([]byte(userCreated.Events[0].Data), &userCreatedEventData); err != nil {
			return err
		}
		currentPasswordHash = userCreatedEventData.PasswordHash
	}

	// Verify current password
	if err := admin_common.ComparePassword(currentPasswordHash, currentPassword); err != nil {
		return errors.New("invalid current password")
	}

	// Save password change event
	hash, err := admin_common.HashPassword(newPassword)
	if err != nil {
		return err
	}
	passwordChangedEvent := admin_events.UserPasswordChanged{
		UserId:       currentUserId,
		PasswordHash: hash,
	}

	payload, err := json.Marshal(passwordChangedEvent)
	if err != nil {
		return err
	}

	tags := []*eventstore.Tag{
		{Key: "eventType", Value: admin_events.EventTypeUserPasswordChanged},
		{Key: "user_id", Value: currentUserId},
	}

	_, err = saveEvents(ctx, &eventstore.SaveEventsRequest{
		Boundary: boundary,
		Stream: &eventstore.SaveStreamQuery{
			Name:            admin_events.AdminStream,
			ExpectedVersion: 0,
			SubsetQuery:     &eventstore.Query{Criteria: []*eventstore.Criterion{{Tags: tags}}},
		},
		Events: []*eventstore.EventToSave{{
			EventId:   uuid.NewString(),
			EventType: admin_events.EventTypeUserPasswordChanged,
			Data:      string(payload),
			Metadata:  "",
		}},
	})
	if err != nil {
		return err
	}

	return nil
}
