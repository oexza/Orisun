package changepassword

import (
	"context"
	"errors"
	"net/http"
	admin_common "orisun/admin/slices/common"
	"orisun/admin/templates"
	"orisun/eventstore"
	l "orisun/logging"
	"sync"

	admin_events "orisun/admin/events"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/starfederation/datastar-go/datastar"
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
	// Use goroutines to fetch events concurrently
	var wg sync.WaitGroup
	var userCreated, userDeleted, passwordChanged *eventstore.GetEventsResponse
	var userCreatedErr, userDeletedErr, passwordChangedErr error

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
	wg.Add(1)
	go func() {
		defer wg.Done()
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
		userCreated, userCreatedErr = getEvents(ctx, &reqCopy)
	}()

	// Fetch UserDeleted event
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Create a true copy of the base request, then take its address for getEvents
		reqCopy := baseRequest 
		reqCopy.Query = &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "user_id", Value: currentUserId},
						{Key: "eventType", Value: admin_events.EventTypeUserDeleted},
					},
				},
			},
		}
		userDeleted, userDeletedErr = getEvents(ctx, &reqCopy)
	}()

	// Fetch PasswordChanged event
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Create a true copy of the base request, then take its address for getEvents
		reqCopy := baseRequest 
		reqCopy.Query = &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{
					Tags: []*eventstore.Tag{
						{Key: "user_id", Value: currentUserId},
						{Key: "eventType", Value: admin_events.EventTypeUserPasswordChanged},
					},
				},
			},
		}
		passwordChanged, passwordChangedErr = getEvents(ctx, &reqCopy)
		logger.Infof("PasswordChanged event: %v", passwordChanged)
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	// Check for errors
	if userCreatedErr != nil {
		return userCreatedErr
	}
	if userDeletedErr != nil {
		return userDeletedErr
	}
	if passwordChangedErr != nil {
		return passwordChangedErr
	}

	// Check if user exists
	if len(userCreated.Events) == 0 {
		return errors.New("user not found")
	}

	// Check if user is deleted
	if len(userDeleted.Events) > 0 {
		return errors.New("cannot change password for a deleted user")
	}

	// Determine which password hash to use for verification
	var currentPasswordHash string

	// If there's a password changed event, use the most recent password hash
	if len(passwordChanged.Events) > 0 {
		var passwordChangedEventData admin_events.UserPasswordChanged
		err := json.Unmarshal([]byte(passwordChanged.Events[0].Data), &passwordChangedEventData)
		if err != nil {
			return err
		}
		currentPasswordHash = passwordChangedEventData.PasswordHash
	} else {
		// Otherwise use the original password from user creation
		var userCreatedEventData admin_events.UserCreated
		err := json.Unmarshal([]byte(userCreated.Events[0].Data), &userCreatedEventData)
		if err != nil {
			return err
		}
		currentPasswordHash = userCreatedEventData.PasswordHash
	}

	// Verify current password
	if err := admin_common.ComparePassword(currentPasswordHash, currentPassword); err != nil {
		return errors.New("current password is incorrect")
	}

	// Hash the new password
	newPasswordHash, err := admin_common.HashPassword(newPassword)
	if err != nil {
		return err
	}

	// Create password changed event
	passwordChangedEvent := admin_events.UserPasswordChanged{
		UserId:       currentUserId,
		PasswordHash: newPasswordHash,
	}

	passwordChangedEventData, err := json.Marshal(passwordChangedEvent)
	if err != nil {
		return err
	}

	// We know userCreated events exist since we checked earlier
	// Initialize with the version from userCreated event
	lastExpectedVersion := int64(userCreated.Events[len(userCreated.Events)-1].Version)

	// Check if any password changed events have a higher version
	for _, event := range passwordChanged.Events {
		if int64(event.Version) > lastExpectedVersion {
			lastExpectedVersion = int64(event.Version)
		}
	}

	_, err = saveEvents(
		ctx,
		&eventstore.SaveEventsRequest{
			Boundary: boundary,
			Stream: &eventstore.SaveStreamQuery{
				Name: admin_events.AdminStream,
				// Set ExpectedVersion to the highest version found
				ExpectedVersion: lastExpectedVersion,
				SubsetQuery: &eventstore.Query{
					Criteria: []*eventstore.Criterion{
						{
							Tags: []*eventstore.Tag{
								{Key: "user_id", Value: currentUserId},
								{Key: "eventType", Value: admin_events.EventTypeUserCreated},
							},
						},
						{
							Tags: []*eventstore.Tag{
								{Key: "user_id", Value: currentUserId},
								{Key: "eventType", Value: admin_events.EventTypeUserPasswordChanged},
							},
						},
						{
							Tags: []*eventstore.Tag{
								{Key: "user_id", Value: currentUserId},
								{Key: "eventType", Value: admin_events.EventTypeUserDeleted},
							},
						},
					},
				},
			},
			Events: []*eventstore.EventToSave{
				{
					EventId:   uuid.New().String(),
					EventType: admin_events.EventTypeUserPasswordChanged,
					Data:      string(passwordChangedEventData),
					Metadata:  "{\"current_user_id\":\"" + currentUserId + "\"}",
				},
			},
		},
	)
	if err != nil {
		return err
	}

	return nil
}
