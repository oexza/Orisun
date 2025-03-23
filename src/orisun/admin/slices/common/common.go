package common

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	events "orisun/src/orisun/admin/events"
	"orisun/src/orisun/eventstore"

	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar/sdk/go"
	"golang.org/x/crypto/bcrypt"
)

type DB interface {
	ListAdminUsers() ([]*User, error)
	GetProjectorLastPosition(projectorName string) (*eventstore.Position, error)
	UpdateProjectorPosition(name string, position *eventstore.Position) error
	CreateNewUser(id string, username string, password_hash string, name string, roles []events.Role) error
	DeleteUser(id string) error
	GetUserByUsername(username string) (User, error)
	GetUsersCount() (uint32, error)
	SaveUsersCount(uint32) error
}

type User struct {
	Id             string        `json:"id"`
	Name           string        `json:"name"`
	Username       string        `json:"username"`
	HashedPassword string        `json:"hashed_password"`
	Roles          []events.Role `json:"roles"`
}

type SaveEventsType = func(ctx context.Context, in *eventstore.SaveEventsRequest) (resp *eventstore.WriteResult, err error)
type GetEventsType = func(ctx context.Context, in *eventstore.GetEventsRequest) (*eventstore.GetEventsResponse, error)
type GetProjectorLastPositionType = func(projectorName string) (*eventstore.Position, error)
type UpdateProjectorPositionType = func(projectorName string, position *eventstore.Position) error
type PublishToPubSubType = func(ctx context.Context, req *eventstore.PublishRequest) error
type SubscribeToEventStoreType = func(
	ctx context.Context,
	boundary string,
	subscriberName string,
	pos *eventstore.Position,
	query *eventstore.Query,
	handler eventstore.EventSubscriptionHandler,
) error

type CommonSSESignals struct {
	TabId string
}

var sseConnections map[string]*datastar.ServerSentEventGenerator = map[string]*datastar.ServerSentEventGenerator{}

func CreateSSEConnection(w http.ResponseWriter, r *http.Request) (*datastar.ServerSentEventGenerator, string) {
	tabId, err := uuid.NewV7()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, ""
	}
	tabIdStr := tabId.String()
	sse := datastar.NewSSE(w, r)
	sse.MarshalAndMergeSignals(CommonSSESignals{
		TabId: tabIdStr,
	})
	sseConnections[tabIdStr] = sse

	// Set up cleanup when connection closes
	go func() {
		<-r.Context().Done()
		delete(sseConnections, tabIdStr)
	}()

	return sse, tabIdStr
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func GetCurrentUser(r *http.Request) (string, error) {
	cookie, err := r.Cookie("auth")
	if err != nil {
		return "", err
	}

	// Base64 decode the cookie value
	decodedBytes, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return "", err
	}

	var user User
	if err := json.Unmarshal(decodedBytes, &user); err != nil {
		return "", err
	}

	if user.Id == "" {
		return "", fmt.Errorf("user ID is empty")
	}

	return user.Id, nil
}
