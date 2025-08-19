package login

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"orisun/eventstore"
	l "orisun/logging"
	"sync"

	"github.com/goccy/go-json"

	globalCommon "orisun/common"

	admin_events "orisun/admin/events"
	admin_common "orisun/admin/slices/common"

	datastar "github.com/starfederation/datastar-go/datastar"
)

type LoginHandler struct {
	logger    l.Logger
	boundary  string
	getEvents admin_common.GetEventsType
}

func NewLoginHandler(
	logger l.Logger,
	boundary string,
	getEvents admin_common.GetEventsType,
) *LoginHandler {
	return &LoginHandler{
		logger:    logger,
		boundary:  boundary,
		getEvents: getEvents,
	}
}

type LoginRequest struct {
	Username string
	Password string
}

func (s *LoginHandler) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	err := Login().Render(r.Context(), w)

	if err != nil {
		s.logger.Errorf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (s *LoginHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	store := &LoginRequest{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate credentials
	user, err := s.login(
		r.Context(),
		store.Username,
		store.Password,
		s.boundary,
		s.getEvents,
	)
	if err != nil {
		sse, _ := admin_common.GetOrCreateSSEConnection(w, r)
		sse.RemoveElement("message")
		sse.PatchElements(`<div id="message">` + `Login Failed` + `</div>`)
		return
	}

	userAsString, err := json.Marshal(user)
	if err != nil {
		sse, _ := admin_common.GetOrCreateSSEConnection(w, r)
		sse.PatchElements(`<div id="message">` + `Login Failed` + `</div>`)
		return
	}

	// Base64 encode the JSON string
	encodedValue := base64.StdEncoding.EncodeToString(userAsString)

	// Set the token as an HTTP-only cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth",
		Value:    encodedValue,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	sse, _ := admin_common.GetOrCreateSSEConnection(w, r)

	sse.PatchElements(`<div id="message">` + `Login Succeded` + `</div>`)

	// Redirect to users page after successful login
	sse.Redirect("/dashboard")
}

func (s *LoginHandler) login(ctx context.Context, username, password string, boundary string,
	getEvents admin_common.GetEventsType) (globalCommon.User, error) {
	// Use goroutines to fetch events concurrently
	var userCreated, userDeleted, passwordChanged *eventstore.GetEventsResponse
	var userDeletedErr, passwordChangedErr error

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
					{Key: "username", Value: username},
					{Key: "eventType", Value: admin_events.EventTypeUserCreated},
				},
			},
		},
	}
	userCreated, err := getEvents(ctx, &reqCopy)
	if err != nil {
		return globalCommon.User{}, err
	}

	if userCreated == nil || len(userCreated.Events) == 0 {
		return globalCommon.User{}, fmt.Errorf("user Not found, Please contact the administrator")
	}

	var userCreatedEventData admin_events.UserCreated
	err = json.Unmarshal([]byte(userCreated.Events[0].Data), &userCreatedEventData)
	if err != nil {
		return globalCommon.User{}, err
	}

	var wg sync.WaitGroup

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
						{Key: "user_id", Value: userCreatedEventData.UserId},
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
						{Key: "user_id", Value: userCreatedEventData.UserId},
						{Key: "eventType", Value: admin_events.EventTypeUserPasswordChanged},
					},
				},
			},
		}
		passwordChanged, passwordChangedErr = getEvents(ctx, &reqCopy)
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	// Check for errors in goroutines
	if userDeletedErr != nil || passwordChangedErr != nil {
		return globalCommon.User{}, fmt.Errorf("error fetching events: %v, %v", userDeletedErr, passwordChangedErr)
	}

	// Check if user is deleted
	if userDeleted != nil && len(userDeleted.Events) > 0 {
		return globalCommon.User{}, fmt.Errorf("user Not found, Please contact the administrator")
	}

	// Determine which password hash to use for verification
	var currentPasswordHash string

	// If there's a password changed event, use the most recent password hash
	if len(passwordChanged.Events) > 0 {
		var passwordChangedEventData admin_events.UserPasswordChanged
		err := json.Unmarshal([]byte(passwordChanged.Events[0].Data), &passwordChangedEventData)
		if err != nil {
			return globalCommon.User{}, err
		}
		currentPasswordHash = passwordChangedEventData.PasswordHash
	} else {
		// Otherwise use the original password from user creation
		currentPasswordHash = userCreatedEventData.PasswordHash
	}

	// Compare the provided password with the stored hash
	if err := admin_common.ComparePassword(currentPasswordHash, password); err != nil {
		return globalCommon.User{}, fmt.Errorf("invalid credentials")
	}

	user := globalCommon.User{
		Id:             userCreatedEventData.UserId,
		Username:       username,
		Name:           userCreatedEventData.Name,
		HashedPassword: currentPasswordHash,
		Roles:          userCreatedEventData.Roles,
	}

	return user, nil
}
