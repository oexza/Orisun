package admin

import (
	"fmt"
	admin_common "orisun/admin/slices/common"
	globalCommon "orisun/common"
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

	if err = admin_common.ComparePassword(user.HashedPassword, password); err != nil {
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
