package login

import (
	"context"
	l "github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
)

type Authenticator interface {
	ValidateCredentials(ctx context.Context, username string, password string) (orisun.User, error)
}

type LoginHandler struct {
	logger        l.Logger
	boundary      string
	authenticator Authenticator
}

func NewLoginHandler(
	logger l.Logger,
	boundary string,
	authenticator Authenticator,
) *LoginHandler {
	return &LoginHandler{
		logger:        logger,
		boundary:      boundary,
		authenticator: authenticator,
	}
}

type LoginRequest struct {
	Username string
	Password string
}

func (s *LoginHandler) Login(ctx context.Context, username, password string) (orisun.User, error) {
	// Validate credentials
	user, err := s.authenticator.ValidateCredentials(ctx, username, password)
	if err != nil {
		return orisun.User{}, err
	}

	return user, nil
}
