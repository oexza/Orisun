package create_user

import (
	"context"
	"fmt"
	"net/http"
	ev "orisun/admin/events"
	t "orisun/admin/templates"
	"orisun/eventstore"
	l "orisun/logging"
	"strings"

	"github.com/goccy/go-json"

	pb "orisun/eventstore"

	admin_common "orisun/admin/slices/common"

	globalCommon "orisun/common"

	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar-go/datastar"
)

type CreateUserHandler struct {
	logger     l.Logger
	boundary   string
	saveEvents admin_common.SaveEventsType
	getEvents  admin_common.GetEventsType
}

func NewCreateUserHandler(
	logger l.Logger,
	boundary string,
	saveEvents admin_common.SaveEventsType,
	getEvents admin_common.GetEventsType) *CreateUserHandler {
	return &CreateUserHandler{
		logger:     logger,
		boundary:   boundary,
		getEvents:  getEvents,
		saveEvents: saveEvents,
	}
}

func (s *CreateUserHandler) HandleCreateUserPage(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)

	// Convert []Role to []string for template compatibility
	roleStrings := make([]string, len(globalCommon.Roles))
	for i, role := range globalCommon.Roles {
		roleStrings[i] = role.String()
	}

	sse.PatchElementTempl(AddUser(r.URL.Path, roleStrings), datastar.WithModeReplace())
	sse.ExecuteScript("setTimeout(() => { document.querySelector('#add-user-dialog').show() }, 1)")
}

type AddNewUserRequest struct {
	Name     string
	Username string
	Password string
	Role     string
}

func (r *AddNewUserRequest) validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}

	if r.Username == "" {
		return fmt.Errorf("username is required")
	}

	if len(r.Username) < 3 {
		return fmt.Errorf("username must be at least 3 characters")
	}

	if r.Password == "" {
		return fmt.Errorf("password is required")
	}

	if len(r.Password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	if r.Role == "" {
		return fmt.Errorf("role is required")
	}

	// Check if role is valid
	validRole := false
	for _, role := range globalCommon.Roles {
		if strings.EqualFold(string(role), r.Role) {
			validRole = true
			break
		}
	}

	if !validRole {
		return fmt.Errorf("invalid role: %s", r.Role)
	}

	return nil
}

func (s *CreateUserHandler) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	addUserRequest := &AddNewUserRequest{}
	response := struct {
		Message string `json:"message"`
		Success bool   `json:"success"`
		Failed  bool   `json:"failed"`
	}{}

	if err := datastar.ReadSignals(r, addUserRequest); err != nil {
		sse := datastar.NewSSE(w, r)
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndPatchSignals(response)
		return
	}

	err := addUserRequest.validate()

	if err != nil {
		sse := datastar.NewSSE(w, r)
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndPatchSignals(response)
		sse.PatchElementTempl(
			t.Alert(err.Error(), t.AlertDanger),
			datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		sse.ExecuteScript("document.querySelector('#alert').toast()")
		return
	}

	s.logger.Debugf("Creating user %v", addUserRequest)

	sse := datastar.NewSSE(w, r)

	currentUser := admin_common.GetCurrentUser(r)

	_, err = CreateUser(
		r.Context(),
		addUserRequest.Name,
		addUserRequest.Username,
		addUserRequest.Password,
		[]globalCommon.Role{globalCommon.Role(strings.ToUpper(addUserRequest.Role))},
		s.boundary,
		s.saveEvents,
		s.getEvents,
		s.logger,
		&currentUser.Id,
	)
	if err != nil {
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndPatchSignals(response)
		sse.PatchElementTempl(
			t.Alert(err.Error(), t.AlertDanger),
			datastar.WithSelector("body"),
			datastar.WithModePrepend(),
		)
		// delay the toast
		sse.ExecuteScript("setTimeout(() => { document.querySelector('#alert').toast() }, 50)")
		return
	}

	response.Success = true
	response.Message = "User created successfully"
	sse.MarshalAndPatchSignals(response)

	sse.PatchElementTempl(
		t.Alert("User created!", t.AlertSuccess),
		datastar.WithSelector("body"),
		datastar.WithModePrepend(),
	)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.ExecuteScript("document.querySelector('#add-user-dialog').hide()")
}

func CreateUser(
	ctx context.Context,
	name, username, password string,
	roles []globalCommon.Role,
	boundary string,
	saveEvents admin_common.SaveEventsType,
	getEvents admin_common.GetEventsType,
	logger l.Logger,
	currentUserId *string,
) (*ev.UserCreated, error) {
	username = strings.TrimSpace(username)

	userCreatedEvent, err := getEvents(
		ctx,
		&pb.GetEventsRequest{
			Boundary:  boundary,
			Count:     1,
			Direction: pb.Direction_DESC,
			Query: &pb.Query{
				Criteria: []*pb.Criterion{
					{
						Tags: []*pb.Tag{
							{Key: "username", Value: username},
							{Key: "eventType", Value: ev.EventTypeUserCreated},
						},
					},
				},
			},
			Stream: &pb.GetStreamQuery{
				Name:        ev.AdminStream,
				FromVersion: 999999999999999999,
			},
		},
	)

	if err != nil {
		return nil, err
	}

	logger.Infof("userCreatedEvent: %v", userCreatedEvent)

	events := []*ev.Event{}
	for _, event := range userCreatedEvent.Events {
		var userCreatedEvent = ev.UserCreated{}
		err := json.Unmarshal([]byte(event.Data), &userCreatedEvent)
		if err != nil {
			return nil, err
		}
		events = append(events, &ev.Event{
			EventType: event.EventType,
			Data:      userCreatedEvent,
		})
	}

	if len(userCreatedEvent.Events) > 0 {
		userCreatedEvent := events[0].Data.(ev.UserCreated)
		UserDeletedEvent, err := getEvents(
			ctx,
			&pb.GetEventsRequest{
				Boundary:  boundary,
				Direction: pb.Direction_DESC,
				Count:     1,
				Query: &pb.Query{
					Criteria: []*pb.Criterion{
						{
							Tags: []*pb.Tag{
								{Key: "eventType", Value: ev.EventTypeUserDeleted},
								{Key: "userId", Value: userCreatedEvent.UserId},
							},
						},
					},
				},
				Stream: &pb.GetStreamQuery{
					Name:        ev.AdminStream,
					FromVersion: 999999999999999999,
				},
			},
		)

		if err != nil {
			return nil, err
		}

		for _, event := range UserDeletedEvent.Events {
			var userDeletedEvent = ev.UserDeleted{}
			err := json.Unmarshal([]byte(event.Data), &userDeletedEvent)
			if err != nil {
				return nil, err
			}
			events = append(events, &ev.Event{
				EventType: event.EventType,
				Data:      userDeletedEvent,
			})
		}
	}

	userId, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	newEvents, err := createUserCommandHandler(userId.String(), username, password, name, roles, events)

	if err != nil {
		return nil, err
	}

	eventsToSave := []*pb.EventToSave{}

	for _, event := range newEvents {
		eventData, err := json.Marshal(event.Data)
		if err != nil {
			return nil, err
		}
		eventId, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		currentUserIdVerified := "SYSTEM"
		if currentUserId != nil {
			currentUserIdVerified = *currentUserId
		}
		eventsToSave = append(eventsToSave, &pb.EventToSave{
			EventId:   eventId.String(),
			EventType: event.EventType,
			Data:      string(eventData),
			Metadata:  "{\"schema\":\"" + boundary + "\",\"createdBy\":\"" + currentUserIdVerified + "\"}",
		})
	}

	_, err = saveEvents(ctx, &pb.SaveEventsRequest{
		Boundary: boundary,
		Events:   eventsToSave,
		Stream: &pb.SaveStreamQuery{
			Name:            ev.AdminStream,
			ExpectedVersion: eventstore.StreamDoesNotExist,
			SubsetQuery: &pb.Query{
				Criteria: []*pb.Criterion{
					{
						Tags: []*pb.Tag{
							{Key: "username", Value: username},
							{Key: "eventType", Value: ev.EventTypeUserCreated},
						},
					},
					{
						Tags: []*pb.Tag{
							{Key: "eventType", Value: ev.EventTypeUserDeleted},
							{Key: "userId", Value: userId.String()},
						},
					},
				},
			},
		},
	})

	userCreated, ok := newEvents[0].Data.(ev.UserCreated)
	if !ok {
		return nil, fmt.Errorf("unexpected event data type")
	}
	return &userCreated, nil
}

func createUserCommandHandler(userId string, username, password string, name string, roles []globalCommon.Role, events []*ev.Event) ([]*ev.Event, error) {
	//check if events contains any user created event and not user deleted event
	if len(events) > 0 {
		hasUserCreated := false
		hasUserDeleted := false
		for _, event := range events {
			switch event.EventType {
			case ev.EventTypeUserCreated:
				hasUserCreated = true
			case ev.EventTypeUserDeleted:
				hasUserDeleted = true
			}
		}
		if hasUserCreated && !hasUserDeleted {
			return nil, UserExistsError{
				username: username,
			}
		}
	}

	hash, err := admin_common.HashPassword(password)
	if err != nil {
		return nil, err
	}

	// Create user created event
	event := ev.Event{
		EventType: ev.EventTypeUserCreated,
		Data: ev.UserCreated{
			Name:         name,
			Username:     username,
			Roles:        roles,
			PasswordHash: string(hash),
			UserId:       userId,
		},
	}
	return []*ev.Event{&event}, nil
}

type UserExistsError struct {
	username string
}

func (e UserExistsError) Error() string {
	return fmt.Sprintf("username %s already exists", e.username)
}
