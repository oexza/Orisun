package changepassword

import (
	"context"
	"errors"
	admin_common "github.com/oexza/Orisun/admin/slices/common"
	l "github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	// "sync"

	admin_events "github.com/oexza/Orisun/admin/events"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
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

func ChangePassword(
	ctx context.Context,
	currentPassword, newPassword string,
	boundary string,
	saveEvents admin_common.SaveEventsType,
	getEvents admin_common.GetEventsType,
	logger l.Logger,
	currentUserId string,
) error {
	// Use goroutines to fetch events concurrently with errgroup for cancellation
	var userCreated, userDeleted, passwordChanged *orisun.GetEventsResponse

	// Create a common request template
	baseRequest := orisun.GetEventsRequest{
		Boundary:  boundary,
		Count:     1,
		Direction: orisun.Direction_DESC,
	}

	// Fetch UserCreated event
	// Create a true copy of the base request, then take its address for getEvents
	reqCopy := baseRequest
	reqCopy.Query = &orisun.Query{
		Criteria: []*orisun.Criterion{
			{
				Tags: []*orisun.Tag{
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
		req.Query = &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
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
		req.Query = &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
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

	tags := []*orisun.Tag{
		{Key: "eventType", Value: admin_events.EventTypeUserPasswordChanged},
		{Key: "user_id", Value: currentUserId},
	}

	var expectedPosition *orisun.Position = nil
	if passwordChanged != nil && len(passwordChanged.Events) > 0 {
		expectedPosition = passwordChanged.Events[0].Position
	}

	if expectedPosition == nil {
		expectedPosition = userCreated.Events[0].Position
	}
	_, err = saveEvents(ctx, &orisun.SaveEventsRequest{
		Boundary: boundary,
		Query: &orisun.SaveQuery{
			ExpectedPosition: expectedPosition,
			SubsetQuery:      &orisun.Query{Criteria: []*orisun.Criterion{{Tags: tags}}},
		},
		Events: []*orisun.EventToSave{{
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
