package create_user

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	ev "orisun/src/orisun/admin/events"
	t "orisun/src/orisun/admin/templates"
	l "orisun/src/orisun/logging"
	"strings"

	pb "orisun/src/orisun/eventstore"

	"orisun/src/orisun/admin/slices/common"

	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar/sdk/go"
)

type CreateUserHandler struct {
	logger     l.Logger
	boundary   string
	saveEvents common.SaveEventsType
	getEvents  common.GetEventsType
}

func NewCreateUserHandler(
	logger l.Logger,
	boundary string,
	saveEvents common.SaveEventsType,
	getEvents common.GetEventsType) *CreateUserHandler {
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
	roleStrings := make([]string, len(ev.Roles))
	for i, role := range ev.Roles {
		roleStrings[i] = role.String()
	}

	sse.MergeFragmentTempl(AddUser(r.URL.Path, roleStrings), datastar.WithMergeMode(datastar.FragmentMergeModeOuter))
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
	for _, role := range ev.Roles {
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
	store := &AddNewUserRequest{}
	response := struct {
		Message string `json:"message"`
		Success bool   `json:"success"`
		Failed  bool   `json:"failed"`
	}{}

	if err := datastar.ReadSignals(r, store); err != nil {
		sse := datastar.NewSSE(w, r)
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}

	err := store.validate()

	if err != nil {
		sse := datastar.NewSSE(w, r)
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}

	s.logger.Debugf("Creating user %v", store)

	sse := datastar.NewSSE(w, r)

	evt, err := CreateUser(
		store.Name,
		store.Username,
		store.Password,
		[]ev.Role{ev.Role(strings.ToUpper(store.Role))},
		s.boundary,
		s.saveEvents,
		s.getEvents,
	)
	if err != nil {
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}

	response.Success = true
	response.Message = "User created successfully"
	sse.MarshalAndMergeSignals(response)

	currentUser, err := common.GetCurrentUser(r)
	if err != nil {
		response.Failed = true
		response.Message = err.Error()
		sse.MarshalAndMergeSignals(response)
		return
	}
	sse.MergeFragmentTempl(t.UserRow(&t.User{
		Name:     evt.Name,
		Id:       evt.UserId,
		Username: evt.Username,
		Roles:    []string{store.Role},
	}, currentUser),
		datastar.WithMergeAppend(),
		datastar.WithSelectorID("users-table-body"),
	)
	sse.MergeFragmentTempl(t.Alert("User created!", t.AlertSuccess), datastar.WithSelector("body"),
		datastar.WithMergeMode(datastar.FragmentMergeModePrepend),
	)
	sse.ExecuteScript("document.querySelector('#alert').toast()")
	sse.ExecuteScript("document.querySelector('#add-user-dialog').hide()")
}

func CreateUser(name, username, password string,
	roles []ev.Role, boundary string,
	saveEvents common.SaveEventsType,
	getEvents common.GetEventsType) (*ev.UserCreated, error) {
	username = strings.TrimSpace(username)
	events := []*ev.Event{}

	userCreatedEvent, err := getEvents(
		context.Background(),
		&pb.GetEventsRequest{
			Boundary:  boundary,
			Direction: pb.Direction_DESC,
			Count:     1,
			Query: &pb.Query{
				Criteria: []*pb.Criterion{
					{
						Tags: []*pb.Tag{
							{Key: ev.UsernameTag, Value: username},
							{Key: "eventType", Value: ev.EventTypeUserCreated},
						},
					},
				},
			},
		},
	)

	if err != nil {
		return nil, err
	}

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
			context.Background(),
			&pb.GetEventsRequest{
				Boundary:  boundary,
				Direction: pb.Direction_DESC,
				Count:     1,
				Stream:    &pb.GetStreamQuery{Name: ev.UserStreamPrefix + userCreatedEvent.UserId},
				Query: &pb.Query{
					Criteria: []*pb.Criterion{
						{
							Tags: []*pb.Tag{
								{Key: "eventType", Value: ev.EventTypeUserDeleted},
							},
						},
					},
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
		eventsToSave = append(eventsToSave, &pb.EventToSave{
			EventId:   eventId.String(),
			EventType: event.EventType,
			Data:      string(eventData),
			Tags: []*pb.Tag{
				{Key: ev.UsernameTag, Value: username},
				{Key: ev.RegistrationTag, Value: username},
			},
			Metadata: "{\"schema\":\"" + boundary + "\",\"createdBy\":\"" + username + "\"}",
		})
	}

	_, err = saveEvents(context.Background(), &pb.SaveEventsRequest{
		Boundary: boundary,
		ConsistencyCondition: &pb.IndexLockCondition{
			ConsistencyMarker: &pb.Position{
				PreparePosition: 0,
				CommitPosition:  0,
			},
			Query: &pb.Query{
				Criteria: []*pb.Criterion{
					{
						Tags: []*pb.Tag{
							{Key: ev.UsernameTag, Value: username},
						},
					},
				},
			},
		},
		Events: eventsToSave,
		Stream: &pb.SaveStreamQuery{
			Name:            ev.UserStreamPrefix + userId.String(),
			ExpectedVersion: 0,
		},
	})

	userCreated, ok := newEvents[0].Data.(ev.UserCreated)
	if !ok {
		return nil, fmt.Errorf("unexpected event data type")
	}
	return &userCreated, nil
}

func createUserCommandHandler(userId string, username, password string, name string, roles []ev.Role, events []*ev.Event) ([]*ev.Event, error) {
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

	hash, err := common.HashPassword(password)
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
