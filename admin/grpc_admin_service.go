package admin

import (
	"context"
	"errors"
	"fmt"

	changepassword "github.com/oexza/Orisun/admin/slices/change_password"
	createuser "github.com/oexza/Orisun/admin/slices/create_user"
	deleteuser "github.com/oexza/Orisun/admin/slices/delete_user"
	l "github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
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
	orisun.UnimplementedAdminServer
	logger        l.Logger
	boundary      string
	getEvents     GetEventsFunc
	saveEvents    SaveEventsFunc
	listAdminUser ListAdminUsersFunc
	authenticator *Authenticator
}

// GetEventsFunc is the function signature for getting events
type GetEventsFunc func(ctx context.Context, req *orisun.GetEventsRequest) (*orisun.GetEventsResponse, error)

// SaveEventsFunc is the function signature for saving events
type SaveEventsFunc func(ctx context.Context, req *orisun.SaveEventsRequest) (*orisun.WriteResult, error)

// ListAdminUsersFunc is the function signature for listing admin users
type ListAdminUsersFunc func() ([]*orisun.User, error)

// NewGRPCAdminServer creates a new AdminServiceServer
func NewGRPCAdminServer(
	logger l.Logger,
	boundary string,
	getEvents GetEventsFunc,
	saveEvents SaveEventsFunc,
	listAdminUsers ListAdminUsersFunc,
	authenticator *Authenticator,
) *AdminServiceServer {
	return &AdminServiceServer{
		logger:        logger,
		boundary:      boundary,
		getEvents:     getEvents,
		saveEvents:    saveEvents,
		listAdminUser: listAdminUsers,
		authenticator: authenticator,
	}
}

// CreateUser creates a new user with the given details
func (s *AdminServiceServer) CreateUser(ctx context.Context, req *orisun.CreateUserRequest) (*orisun.CreateUserResponse, error) {
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
	return &orisun.CreateUserResponse{User: convertToProtoUser(user)}, nil
}

// DeleteUser deletes a user by ID
func (s *AdminServiceServer) DeleteUser(ctx context.Context, req *orisun.DeleteUserRequest) (*orisun.DeleteUserResponse, error) {
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
	return &orisun.DeleteUserResponse{Success: true}, nil
}

// ChangePassword changes a user's password
func (s *AdminServiceServer) ChangePassword(ctx context.Context, req *orisun.ChangePasswordRequest) (*orisun.ChangePasswordResponse, error) {
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
	return &orisun.ChangePasswordResponse{Success: true}, nil
}

// ListUsers returns all users
func (s *AdminServiceServer) ListUsers(ctx context.Context, req *orisun.ListUsersRequest) (*orisun.ListUsersResponse, error) {
	users, err := s.listAdminUser()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list users: %v", err)
	}

	// Convert to proto AdminUser format
	protoUsers := make([]*orisun.AdminUser, 0, len(users))
	for _, user := range users {
		// Skip deleted users
		if isUserDeleted(ctx, s.getEvents, s.boundary, user.Id) {
			continue
		}
		protoUsers = append(protoUsers, convertToProtoUser(user))
	}

	return &orisun.ListUsersResponse{Users: protoUsers}, nil
}

// ValidateCredentials validates username and password
func (s *AdminServiceServer) ValidateCredentials(ctx context.Context, req *orisun.ValidateCredentialsRequest) (*orisun.ValidateCredentialsResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, status.Errorf(codes.InvalidArgument, "username and password are required")
	}

	// Use the Authenticator to validate credentials
	user, _, err := s.authenticator.ValidateCredentials(ctx, req.Username, req.Password)
	if err != nil {
		// Return success: false instead of an error for invalid credentials
		return &orisun.ValidateCredentialsResponse{Success: false}, nil
	}

	// Convert to proto user
	protoUser := convertToProtoUser(&user)

	return &orisun.ValidateCredentialsResponse{
		Success: true,
		User:    protoUser,
	}, nil
}

// GetUserCount returns the total number of users
func (s *AdminServiceServer) GetUserCount(ctx context.Context, req *orisun.GetUserCountRequest) (*orisun.GetUserCountResponse, error) {
	users, err := s.listAdminUser()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to count users: %v", err)
	}

	return &orisun.GetUserCountResponse{Count: int64(len(users))}, nil
}

// GetEventCount returns the number of events in a boundary
func (s *AdminServiceServer) GetEventCount(ctx context.Context, req *orisun.GetEventCountRequest) (*orisun.GetEventCountResponse, error) {
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

	return &orisun.GetEventCountResponse{Count: count}, nil
}

// Helper methods

func (s *AdminServiceServer) validateCreateUserRequest(req *orisun.CreateUserRequest) error {
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

func (s *AdminServiceServer) changeUserPassword(ctx context.Context, req *orisun.ChangePasswordRequest) error {
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

func convertToProtoUser(user *orisun.User) *orisun.AdminUser {
	if user == nil {
		return nil
	}

	// Convert []Role to []string
	roles := make([]string, len(user.Roles))
	for i, role := range user.Roles {
		roles[i] = string(role)
	}

	return &orisun.AdminUser{
		UserId:    user.Id,
		Name:      user.Name,
		Username:  user.Username,
		Roles:     roles,
		CreatedAt: timestamppb.Now(),
	}
}
