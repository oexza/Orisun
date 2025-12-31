package admin_common

import (
	"context"
	"github.com/oexza/Orisun/orisun"
	"golang.org/x/crypto/bcrypt"
	"net/http"
)

type DB interface {
	ListAdminUsers() ([]*orisun.User, error)
	GetProjectorLastPosition(projectorName string) (*orisun.Position, error)
	UpdateProjectorPosition(name string, position *orisun.Position) error
	UpsertUser(user orisun.User) error
	DeleteUser(id string) error
	GetUserByUsername(username string) (orisun.User, error)
	GetUserById(username string) (orisun.User, error)
	GetUsersCount() (uint32, error)
	SaveUsersCount(uint32) error
	GetEventsCount(boundary string) (int, error)
	SaveEventCount(int, string) error
}

type SaveEventsType = func(ctx context.Context, in *orisun.SaveEventsRequest) (resp *orisun.WriteResult, err error)
type GetEventsType = func(ctx context.Context, in *orisun.GetEventsRequest) (*orisun.GetEventsResponse, error)
type GetProjectorLastPositionType = func(projectorName string) (*orisun.Position, error)
type UpdateProjectorPositionType = func(projectorName string, position *orisun.Position) error
type SubscribeToEventStoreType = func(
	ctx context.Context,
	boundary string,
	subscriberName string,
	pos *orisun.Position,
	query *orisun.Query,
	handler *orisun.MessageHandler[orisun.Event],
) error

type PublishRequest struct {
	Id      string `json:"id"`
	Subject string `json:"subject"`
	Data    []byte `json:"data"`
}

type PublishToPubSubType = func(ctx context.Context, req *PublishRequest) error

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func GetCurrentUser(r *http.Request) *orisun.User {
	currentUser := r.Context().Value(orisun.UserContextKey).(orisun.User)
	if currentUser.Id != "" {
		return &currentUser
	}
	return nil
}

func ComparePassword(hashedPassword string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
