package grpcapi

import (
	"context"
	"errors"
	"fmt"

	boundarycatalog "github.com/OrisunLabs/Orisun/admin/slices/boundary_catalog"
	changepassword "github.com/OrisunLabs/Orisun/admin/slices/change_password"
	createboundary "github.com/OrisunLabs/Orisun/admin/slices/create_boundary"
	createuser "github.com/OrisunLabs/Orisun/admin/slices/create_user"
	deleteuser "github.com/OrisunLabs/Orisun/admin/slices/delete_user"
	importboundary "github.com/OrisunLabs/Orisun/admin/slices/import_boundary"
	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/OrisunLabs/Orisun/internal/eventstoreadapter"
	"github.com/OrisunLabs/Orisun/internal/grpcstatus"
	l "github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUserAlreadyExists  = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrPasswordMismatch   = errors.New("passwords do not match")
	ErrCannotDeleteSelf   = errors.New("cannot delete your own account")
)

// AdminServiceServer implements the Admin gRPC service
type AdminServiceServer struct {
	UnimplementedAdminServer
	logger         l.Logger
	boundary       string
	getEvents      GetEventsFunc
	saveEvents     SaveEventsFunc
	listAdminUser  ListAdminUsersFunc
	authenticator  CredentialsValidator
	boundaryEvents *eventstoreadapter.Adapter
}

// GetEventsFunc is the function signature for getting events
type GetEventsFunc func(ctx context.Context, req *orisun.GetEventsRequest) (*orisun.GetEventsResponse, error)

// SaveEventsFunc is the function signature for saving events
type SaveEventsFunc func(ctx context.Context, req *orisun.SaveEventsRequest) (*orisun.WriteResult, error)

// ListAdminUsersFunc is the function signature for listing admin users
type ListAdminUsersFunc func() ([]*orisun.User, error)

type CredentialsValidator interface {
	ValidateCredentials(context.Context, string, string) (orisun.User, string, error)
}

// NewGRPCAdminServer creates a new AdminServiceServer
func NewGRPCAdminServer(
	logger l.Logger,
	boundary string,
	getEvents GetEventsFunc,
	saveEvents SaveEventsFunc,
	listAdminUsers ListAdminUsersFunc,
	authenticator CredentialsValidator,
) *AdminServiceServer {
	return NewGRPCAdminServerWithBoundaryCommands(
		logger,
		boundary,
		getEvents,
		saveEvents,
		listAdminUsers,
		authenticator,
		nil,
		nil,
	)
}

func NewGRPCAdminServerWithBoundaryCommands(
	logger l.Logger,
	boundary string,
	getEvents GetEventsFunc,
	saveEvents SaveEventsFunc,
	listAdminUsers ListAdminUsersFunc,
	authenticator CredentialsValidator,
	boundarySaver orisun.EventsSaver,
	boundaryReader orisun.EventsRetriever,
) *AdminServiceServer {
	return &AdminServiceServer{
		logger:         logger,
		boundary:       boundary,
		getEvents:      getEvents,
		saveEvents:     saveEvents,
		listAdminUser:  listAdminUsers,
		authenticator:  authenticator,
		boundaryEvents: eventstoreadapter.New(boundarySaver, boundaryReader, nil),
	}
}

// CreateBoundary emits the definition event. Physical provisioning is handled
// asynchronously by the boundary_provisioning slice subscriber.
func (s *AdminServiceServer) CreateBoundary(ctx context.Context, req *CreateBoundaryRequest) (*CreateBoundaryResponse, error) {
	if req == nil || req.Placement == nil {
		return nil, status.Error(codes.InvalidArgument, "boundary placement is required")
	}
	result, err := createboundary.CreateBoundaryCommandHandler(
		ctx,
		createboundary.CreateBoundaryCommand{
			Name:        req.Name,
			Description: req.Description,
			Placement: boundarymodel.Placement{
				Backend:   req.Placement.Backend,
				Namespace: req.Placement.Namespace,
			},
			Metadata: createboundary.CommandMetadata{"source": "grpc", "operation": "create_boundary"},
		},
		s.boundary,
		s.boundaryEvents,
		s.boundaryEvents,
	)
	if err != nil {
		return nil, grpcstatus.FromError(err)
	}
	return &CreateBoundaryResponse{Boundary: boundaryInfo(result.Boundary)}, nil
}

// ImportBoundary emits an import definition event for an existing physical
// boundary. The provisioning adapter applies migrations idempotently.
func (s *AdminServiceServer) ImportBoundary(ctx context.Context, req *ImportBoundaryRequest) (*ImportBoundaryResponse, error) {
	if req == nil || req.Placement == nil {
		return nil, status.Error(codes.InvalidArgument, "boundary placement is required")
	}
	result, err := importboundary.ImportBoundaryCommandHandler(
		ctx,
		importboundary.ImportBoundaryCommand{
			Name:        req.Name,
			Description: req.Description,
			Placement: boundarymodel.Placement{
				Backend:   req.Placement.Backend,
				Namespace: req.Placement.Namespace,
			},
			Metadata: importboundary.CommandMetadata{"source": "grpc", "operation": "import_boundary"},
		},
		s.boundary,
		s.boundaryEvents,
		s.boundaryEvents,
	)
	if err != nil {
		return nil, grpcstatus.FromError(err)
	}
	return &ImportBoundaryResponse{Boundary: boundaryInfo(result.Boundary)}, nil
}

func (s *AdminServiceServer) ListBoundaries(ctx context.Context, _ *ListBoundariesRequest) (*ListBoundariesResponse, error) {
	boundaries, err := boundarycatalog.ListBoundariesQueryHandler(
		ctx,
		boundarycatalog.ListBoundariesQuery{},
		s.boundary,
		s.boundaryEvents,
	)
	if err != nil {
		return nil, grpcstatus.FromError(err)
	}
	response := &ListBoundariesResponse{Boundaries: make([]*BoundaryInfo, len(boundaries))}
	for i, boundary := range boundaries {
		response.Boundaries[i] = boundaryInfo(boundary)
	}
	return response, nil
}

func (s *AdminServiceServer) GetBoundary(ctx context.Context, req *GetBoundaryRequest) (*GetBoundaryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "boundary name is required")
	}
	boundary, err := boundarycatalog.GetBoundaryQueryHandler(
		ctx,
		boundarycatalog.GetBoundaryQuery{Name: req.Name},
		s.boundary,
		s.boundaryEvents,
	)
	if err != nil {
		return nil, grpcstatus.FromError(err)
	}
	return &GetBoundaryResponse{Boundary: boundaryInfo(boundary)}, nil
}

// CreateUser creates a new user with the given details
func (s *AdminServiceServer) CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
	// Validate request
	if err := s.validateCreateUserRequest(req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid request: %v", err)
	}

	// Convert string roles to Role types
	roles := make([]orisun.Role, len(req.Roles))
	for i, roleStr := range req.Roles {
		roles[i] = orisun.Role(roleStr)
	}

	// Create the user (this will use the existing create_user logic)
	user, err := s.createUser(ctx, req.Name, req.Username, req.Password, roles)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create user: %v", err)
	}

	s.logger.Infof("Created user: %s (ID: %s)", user.Username, user.Id)
	return &CreateUserResponse{User: convertToProtoUser(user)}, nil
}

// DeleteUser deletes a user by ID
func (s *AdminServiceServer) DeleteUser(ctx context.Context, req *DeleteUserRequest) (*DeleteUserResponse, error) {
	if req.UserId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "user_id is required")
	}

	// Get current user from context (set by auth interceptor)
	currentUserId := getCurrentUserIdFromContext(ctx)
	if currentUserId == req.UserId {
		return nil, status.Errorf(codes.FailedPrecondition, "%s", ErrCannotDeleteSelf)
	}

	// Check if user exists
	user, err := s.getUserById(ctx, req.UserId)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, status.Errorf(codes.NotFound, "user not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get user: %v", err)
	}

	// Check if user is already deleted
	if isUserDeleted(ctx, s.getEvents, s.boundary, req.UserId) {
		return nil, status.Errorf(codes.FailedPrecondition, "user already deleted")
	}

	// Delete the user
	if err := s.deleteUser(ctx, user, req.UserId); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete user: %v", err)
	}

	s.logger.Infof("Deleted user: %s (ID: %s)", user.Username, req.UserId)
	return &DeleteUserResponse{Success: true}, nil
}

// ChangePassword changes a user's password
func (s *AdminServiceServer) ChangePassword(ctx context.Context, req *ChangePasswordRequest) (*ChangePasswordResponse, error) {
	if req.UserId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "user_id is required")
	}
	if req.CurrentPassword == "" {
		return nil, status.Errorf(codes.InvalidArgument, "current_password is required")
	}
	if req.NewPassword == "" {
		return nil, status.Errorf(codes.InvalidArgument, "new_password is required")
	}

	// Get current user from context
	currentUserId := getCurrentUserIdFromContext(ctx)
	if currentUserId != req.UserId {
		return nil, status.Errorf(codes.PermissionDenied, "can only change your own password")
	}

	// Change password using existing logic
	if err := s.changeUserPassword(ctx, req); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return nil, status.Errorf(codes.Unauthenticated, "%s", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to change password: %v", err)
	}

	s.logger.Infof("Changed password for user: %s", req.UserId)
	return &ChangePasswordResponse{Success: true}, nil
}

// ListUsers returns all users
func (s *AdminServiceServer) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	users, err := s.listAdminUser()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list users: %v", err)
	}

	// Convert to proto AdminUser format
	protoUsers := make([]*AdminUser, 0, len(users))
	for _, user := range users {
		// Skip deleted users
		if isUserDeleted(ctx, s.getEvents, s.boundary, user.Id) {
			continue
		}
		protoUsers = append(protoUsers, convertToProtoUser(user))
	}

	return &ListUsersResponse{Users: protoUsers}, nil
}

// ValidateCredentials validates username and password
func (s *AdminServiceServer) ValidateCredentials(ctx context.Context, req *ValidateCredentialsRequest) (*ValidateCredentialsResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, status.Errorf(codes.InvalidArgument, "username and password are required")
	}

	// Use the Authenticator to validate credentials
	user, _, err := s.authenticator.ValidateCredentials(ctx, req.Username, req.Password)
	if err != nil {
		// Return success: false instead of an error for invalid credentials
		return &ValidateCredentialsResponse{Success: false}, nil
	}

	// Convert to proto user
	protoUser := convertToProtoUser(&user)

	return &ValidateCredentialsResponse{
		Success: true,
		User:    protoUser,
	}, nil
}

// GetUserCount returns the total number of users
func (s *AdminServiceServer) GetUserCount(ctx context.Context, req *GetUserCountRequest) (*GetUserCountResponse, error) {
	users, err := s.listAdminUser()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to count users: %v", err)
	}

	return &GetUserCountResponse{Count: int64(len(users))}, nil
}

// GetEventCount returns the number of events in a boundary
func (s *AdminServiceServer) GetEventCount(ctx context.Context, req *GetEventCountRequest) (*GetEventCountResponse, error) {
	if req.Boundary == "" {
		return nil, status.Errorf(codes.InvalidArgument, "boundary is required")
	}

	// orisun.Query events to get count
	resp, err := s.getEvents(ctx, &orisun.GetEventsRequest{
		Boundary:  req.Boundary,
		Count:     1,
		Direction: orisun.Direction_DESC,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get events: %v", err)
	}

	// The count would be available in the latest event's position
	// For now, we'll return a placeholder
	// TODO: Implement proper counting logic
	count := int64(0)
	if len(resp.Events) > 0 && resp.Events[0].Position != nil {
		count = resp.Events[0].Position.CommitPosition
	}

	return &GetEventCountResponse{Count: count}, nil
}

// Helper methods

func (s *AdminServiceServer) validateCreateUserRequest(req *CreateUserRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.Username == "" {
		return fmt.Errorf("username is required")
	}
	if len(req.Username) < 3 {
		return fmt.Errorf("username must be at least 3 characters")
	}
	if req.Password == "" {
		return fmt.Errorf("password is required")
	}
	if len(req.Password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}
	if len(req.Roles) == 0 {
		return fmt.Errorf("at least one role is required")
	}
	return nil
}

func (s *AdminServiceServer) getUserByUsername(ctx context.Context, username string) (*orisun.User, error) {
	// orisun.Query projection for user by username
	users, err := s.listAdminUser()
	if err != nil {
		return nil, err
	}

	for _, user := range users {
		if user.Username == username {
			return user, nil
		}
	}

	return nil, ErrUserNotFound
}

func (s *AdminServiceServer) getUserById(ctx context.Context, userId string) (*orisun.User, error) {
	// orisun.Query projection for user by ID
	users, err := s.listAdminUser()
	if err != nil {
		return nil, err
	}

	for _, user := range users {
		if user.Id == userId {
			return user, nil
		}
	}

	return nil, ErrUserNotFound
}

func (s *AdminServiceServer) createUser(ctx context.Context, name, username, password string, roles []orisun.Role) (*orisun.User, error) {
	// Call the exported CreateUser function from createuser package
	userCreated, err := createuser.CreateUser(
		ctx,
		name,
		username,
		password,
		roles,
		s.boundary,
		s.saveEvents,
		s.getEvents,
		s.logger,
		nil, // currentUserId is nil for system-created users
	)
	if err != nil {
		return nil, err
	}

	return &orisun.User{
		Id:       userCreated.UserId,
		Name:     userCreated.Name,
		Username: userCreated.Username,
		Roles:    userCreated.Roles,
	}, nil
}

func (s *AdminServiceServer) deleteUser(ctx context.Context, user *orisun.User, userId string) error {
	// Get current user from context (set by auth interceptor)
	currentUserId := getCurrentUserIdFromContext(ctx)

	return deleteuser.DeleteUser(
		ctx,
		userId,
		currentUserId,
		s.boundary,
		s.saveEvents,
		s.getEvents,
		s.logger,
	)
}

func (s *AdminServiceServer) changeUserPassword(ctx context.Context, req *ChangePasswordRequest) error {
	return changepassword.ChangePassword(
		ctx,
		req.CurrentPassword,
		req.NewPassword,
		s.boundary,
		s.saveEvents,
		s.getEvents,
		s.logger,
		req.UserId,
	)
}

func isUserDeleted(ctx context.Context, getEvents GetEventsFunc, boundary, userId string) bool {
	resp, err := getEvents(ctx, &orisun.GetEventsRequest{
		Boundary:  boundary,
		Count:     1,
		Direction: orisun.Direction_DESC,
		Query: &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "user_id", Value: userId},
						{Key: "eventType", Value: "UserDeleted"},
					},
				},
			},
		},
	})

	return err == nil && len(resp.Events) > 0
}

func getCurrentUserIdFromContext(ctx context.Context) string {
	// TODO: Extract user ID from context (set by auth interceptor)
	// For now, return empty string
	return ""
}

func convertToProtoUser(user *orisun.User) *AdminUser {
	if user == nil {
		return nil
	}

	// Convert []Role to []string
	roles := make([]string, len(user.Roles))
	for i, role := range user.Roles {
		roles[i] = string(role)
	}

	return &AdminUser{
		UserId:    user.Id,
		Name:      user.Name,
		Username:  user.Username,
		Roles:     roles,
		CreatedAt: timestamppb.Now(),
	}
}

func boundaryInfo(boundary boundarymodel.Boundary) *BoundaryInfo {
	status := BoundaryLifecycleStatus_BOUNDARY_LIFECYCLE_STATUS_UNSPECIFIED
	switch boundary.Status {
	case boundarymodel.StatusProvisioning:
		status = BoundaryLifecycleStatus_BOUNDARY_LIFECYCLE_STATUS_PROVISIONING
	case boundarymodel.StatusActive:
		status = BoundaryLifecycleStatus_BOUNDARY_LIFECYCLE_STATUS_ACTIVE
	case boundarymodel.StatusFailed:
		status = BoundaryLifecycleStatus_BOUNDARY_LIFECYCLE_STATUS_FAILED
	}
	origin := BoundaryRegistrationOrigin_BOUNDARY_REGISTRATION_ORIGIN_UNSPECIFIED
	switch boundary.Origin {
	case boundarymodel.OriginCreated:
		origin = BoundaryRegistrationOrigin_BOUNDARY_REGISTRATION_ORIGIN_CREATED
	case boundarymodel.OriginImported:
		origin = BoundaryRegistrationOrigin_BOUNDARY_REGISTRATION_ORIGIN_IMPORTED
	}
	return &BoundaryInfo{
		Name:        boundary.Name,
		Description: boundary.Description,
		Placement: &BoundaryPlacementInput{
			Backend:   boundary.Placement.Backend,
			Namespace: boundary.Placement.Namespace,
		},
		Status:             status,
		Origin:             origin,
		LastError:          boundary.LastError,
		DefinitionPosition: grpcPosition(boundary.DefinitionPosition),
		StatusPosition:     grpcPosition(boundary.StatusPosition),
	}
}

func grpcPosition(position *coreeventstore.Position) *Position {
	if position == nil {
		return nil
	}
	return &Position{
		CommitPosition:  position.CommitPosition,
		PreparePosition: position.PreparePosition,
	}
}
