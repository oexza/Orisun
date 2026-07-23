package admin

import (
	l "github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/OrisunLabs/Orisun/orisun/grpcapi"
)

// Compatibility aliases keep the existing server-composition API while the
// generated contract and its adapter are owned by grpcapi.
type AdminServiceServer = grpcapi.AdminServiceServer
type GetEventsFunc = grpcapi.GetEventsFunc
type SaveEventsFunc = grpcapi.SaveEventsFunc
type ListAdminUsersFunc = grpcapi.ListAdminUsersFunc

var (
	ErrUserNotFound       = grpcapi.ErrUserNotFound
	ErrUserAlreadyExists  = grpcapi.ErrUserAlreadyExists
	ErrInvalidCredentials = grpcapi.ErrInvalidCredentials
	ErrPasswordMismatch   = grpcapi.ErrPasswordMismatch
	ErrCannotDeleteSelf   = grpcapi.ErrCannotDeleteSelf
)

func NewGRPCAdminServer(
	logger l.Logger,
	boundary string,
	getEvents GetEventsFunc,
	saveEvents SaveEventsFunc,
	listAdminUsers ListAdminUsersFunc,
	authenticator *Authenticator,
) *AdminServiceServer {
	return grpcapi.NewGRPCAdminServer(
		logger,
		boundary,
		getEvents,
		saveEvents,
		listAdminUsers,
		authenticator,
	)
}

func NewGRPCAdminServerWithBoundaryCommands(
	logger l.Logger,
	boundary string,
	getEvents GetEventsFunc,
	saveEvents SaveEventsFunc,
	listAdminUsers ListAdminUsersFunc,
	authenticator *Authenticator,
	boundarySaver orisun.EventsSaver,
	boundaryReader orisun.EventsRetriever,
) *AdminServiceServer {
	return grpcapi.NewGRPCAdminServerWithBoundaryCommands(
		logger,
		boundary,
		getEvents,
		saveEvents,
		listAdminUsers,
		authenticator,
		boundarySaver,
		boundaryReader,
	)
}
