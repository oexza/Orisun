package common

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	eventstore "orisun/src/orisun/eventstore"
	"sync"

	datastar "github.com/starfederation/datastar/sdk/go"
	"golang.org/x/crypto/bcrypt"
	globalCommon "orisun/src/orisun/common"
)

type DB interface {
	ListAdminUsers() ([]*globalCommon.User, error)
	GetProjectorLastPosition(projectorName string) (*eventstore.Position, error)
	UpdateProjectorPosition(name string, position *eventstore.Position) error
	CreateNewUser(id string, username string, password_hash string, name string, roles []globalCommon.Role) error
	DeleteUser(id string) error
	GetUserByUsername(username string) (globalCommon.User, error)
	GetUsersCount() (uint32, error)
	SaveUsersCount(uint32) error
}

type EventPublishing interface {
	GetLastPublishedEventPosition(ctx context.Context, boundary string) (eventstore.Position, error)
	InsertLastPublishedEvent(ctx context.Context, boundaryOfInterest string, transactionId uint64, globalId uint64) error
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
	handler globalCommon.MessageHandler[eventstore.Event],
) error

type CommonSSESignals struct {
	TabId string
}

var sseConnections map[string]*datastar.ServerSentEventGenerator = map[string]*datastar.ServerSentEventGenerator{}
var sseConnectionsMutex sync.RWMutex

func CreateSSEConnection(w http.ResponseWriter, r *http.Request, tabId string) *datastar.ServerSentEventGenerator {
	sseConnectionsMutex.Lock()
	defer sseConnectionsMutex.Unlock()

	sse := datastar.NewSSE(w, r)
	sse.MarshalAndMergeSignals(CommonSSESignals{
		TabId: tabId,
	})
	sseConnections[tabId] = sse

	// Set up cleanup when connection closes
	go func() {
		<-r.Context().Done()
		sseConnectionsMutex.Lock()
		defer sseConnectionsMutex.Unlock()
		delete(sseConnections, tabId)
	}()

	return sse
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

	var user globalCommon.User
	if err := json.Unmarshal(decodedBytes, &user); err != nil {
		return "", err
	}

	if user.Id == "" {
		return "", fmt.Errorf("user ID is empty")
	}

	return user.Id, nil
}
