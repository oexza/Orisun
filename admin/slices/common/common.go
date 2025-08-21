package admin_common

import (
	"context"
	"net/http"
	eventstore "orisun/eventstore"
	"sync"
	globalCommon "orisun/common"

	datastar "github.com/starfederation/datastar-go/datastar"
	"golang.org/x/crypto/bcrypt"
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
	GetEventsCount(boundary string) (int, error)
	SaveEventCount(int, string) error
}

type EventPublishing interface {
	GetLastPublishedEventPosition(ctx context.Context, boundary string) (eventstore.Position, error)
	InsertLastPublishedEvent(ctx context.Context, boundaryOfInterest string, transactionId int64, globalId int64) error
}

type SaveEventsType = func(ctx context.Context, in *eventstore.SaveEventsRequest) (resp *eventstore.WriteResult, err error)
type GetEventsType = func(ctx context.Context, in *eventstore.GetEventsRequest) (*eventstore.GetEventsResponse, error)
type GetProjectorLastPositionType = func(projectorName string) (*eventstore.Position, error)
type UpdateProjectorPositionType = func(projectorName string, position *eventstore.Position) error
type SubscribeToEventStoreType = func(
	ctx context.Context,
	boundary string,
	subscriberName string,
	pos *eventstore.Position,
	query *eventstore.Query,
	handler globalCommon.MessageHandler[eventstore.Event],
) error

type PublishRequest struct {
	Id      string `json:"id"`
	Subject string `json:"subject"`
	Data    []byte `json:"data"`
}

type PublishToPubSubType = func(ctx context.Context, req *PublishRequest) error

var sseConnections map[string]*datastar.ServerSentEventGenerator = map[string]*datastar.ServerSentEventGenerator{}
var sseConnectionsMutex sync.RWMutex

func GetOrCreateSSEConnection(w http.ResponseWriter, r *http.Request) (*datastar.ServerSentEventGenerator, string) {
	tabId := r.Context().Value(globalCommon.DatastarTabCookieKey).(string)
	sseConnectionsMutex.Lock()
	defer sseConnectionsMutex.Unlock()

	sse := sseConnections[tabId]
	if sse != nil {
		return sse, tabId
	}
	sse = datastar.NewSSE(w, r, datastar.WithCompression())
	sseConnections[tabId] = sse

	// Set up cleanup when context closes
	go func() {
		<-r.Context().Done()
		sseConnectionsMutex.Lock()
		defer sseConnectionsMutex.Unlock()
		delete(sseConnections, tabId)
	}()

	return sse, tabId
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func GetCurrentUser(r *http.Request) (*globalCommon.User) {
	currentUser := r.Context().Value(globalCommon.UserContextKey).(globalCommon.User)
	if currentUser.Id != "" {
		return &currentUser
	}
	return nil
}

func ComparePassword(hashedPassword string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
