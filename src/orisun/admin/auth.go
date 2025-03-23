package admin

import (
	"fmt"
	events "orisun/src/orisun/admin/events"
	common "orisun/src/orisun/admin/slices/common"

	"golang.org/x/crypto/bcrypt"
)

type Authenticator struct {
	getUserByUsername func(username string) (common.User, error)
}

func NewAuthenticator(getUserByUsername func(username string) (common.User, error)) *Authenticator {
	return &Authenticator{
		getUserByUsername: getUserByUsername,
	}
}

func (a *Authenticator) ValidateCredentials(username string, password string) (common.User, error) {
	user, err := a.getUserByUsername(username)

	if err != nil {
		return common.User{}, fmt.Errorf("user not found")
	}

	if err != nil {
		return common.User{}, fmt.Errorf("failed to hash password")
	}

	if err = ComparePassword(user.HashedPassword, password); err != nil {
		return common.User{}, fmt.Errorf("invalid credentials")
	}
	return user, nil
}

func (a *Authenticator) HasRole(roles []events.Role, requiredRole events.Role) bool {
	for _, role := range roles {
		if role == requiredRole || role == events.RoleAdmin {
			return true
		}
	}
	return false
}

func ComparePassword(hashedPassword string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
