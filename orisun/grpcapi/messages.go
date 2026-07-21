// Package grpcapi contains the generated gRPC transport contract. Protobuf
// messages remain owned by the transport-neutral parent package; these aliases
// let protoc-gen-go-grpc target this package without duplicating message types.
package grpcapi

import "github.com/OrisunLabs/Orisun/orisun"

type (
	AdminUser                   = orisun.AdminUser
	CreateUserRequest           = orisun.CreateUserRequest
	CreateUserResponse          = orisun.CreateUserResponse
	DeleteUserRequest           = orisun.DeleteUserRequest
	DeleteUserResponse          = orisun.DeleteUserResponse
	ChangePasswordRequest       = orisun.ChangePasswordRequest
	ChangePasswordResponse      = orisun.ChangePasswordResponse
	ListUsersRequest            = orisun.ListUsersRequest
	ListUsersResponse           = orisun.ListUsersResponse
	ValidateCredentialsRequest  = orisun.ValidateCredentialsRequest
	ValidateCredentialsResponse = orisun.ValidateCredentialsResponse
	GetUserCountRequest         = orisun.GetUserCountRequest
	GetUserCountResponse        = orisun.GetUserCountResponse
	GetEventCountRequest        = orisun.GetEventCountRequest
	GetEventCountResponse       = orisun.GetEventCountResponse

	Event                               = orisun.Event
	SaveEventsRequest                   = orisun.SaveEventsRequest
	WriteResult                         = orisun.WriteResult
	GetEventsRequest                    = orisun.GetEventsRequest
	GetEventsResponse                   = orisun.GetEventsResponse
	GetLatestByCriteriaRequest          = orisun.GetLatestByCriteriaRequest
	GetLatestByCriteriaResponse         = orisun.GetLatestByCriteriaResponse
	CatchUpSubscribeToEventStoreRequest = orisun.CatchUpSubscribeToEventStoreRequest
	PingRequest                         = orisun.PingRequest
	PingResponse                        = orisun.PingResponse
	CreateIndexRequest                  = orisun.CreateIndexRequest
	CreateIndexResponse                 = orisun.CreateIndexResponse
	DropIndexRequest                    = orisun.DropIndexRequest
	DropIndexResponse                   = orisun.DropIndexResponse
)
