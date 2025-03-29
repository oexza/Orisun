package admin

import (
	"fmt"
	globalCommon "orisun/common"

	"golang.org/x/crypto/bcrypt"
)

type Authenticator struct {
	getUserByUsername func(username string) (globalCommon.User, error)
}

func NewAuthenticator(getUserByUsername func(username string) (globalCommon.User, error)) *Authenticator {
	return &Authenticator{
		getUserByUsername: getUserByUsername,
	}
}

func (a *Authenticator) ValidateCredentials(username string, password string) (globalCommon.User, error) {
	user, err := a.getUserByUsername(username)

	if err != nil {
		return globalCommon.User{}, fmt.Errorf("user not found")
	}

	if err != nil {
		return globalCommon.User{}, fmt.Errorf("failed to hash password")
	}

	if err = ComparePassword(user.HashedPassword, password); err != nil {
		return globalCommon.User{}, fmt.Errorf("invalid credentials")
	}
	return user, nil
}

func (a *Authenticator) HasRole(roles []globalCommon.Role, requiredRole globalCommon.Role) bool {
	for _, role := range roles {
		if role == requiredRole || role == globalCommon.RoleAdmin {
			return true
		}
	}
	return false
}

func ComparePassword(hashedPassword string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
