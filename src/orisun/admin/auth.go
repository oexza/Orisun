package admin

import (
	"fmt"
)

type Role string

const (
	RoleAdmin      Role = "ADMIN"
	RoleOperations Role = "OPERATIONS"
	RoleRead       Role = "READ"
	RoleWrite      Role = "WRITE"
)

var Roles = []Role{RoleAdmin, RoleOperations, RoleRead, RoleWrite}

type User struct {
	Id             string `json:"id"`
	Name           string `json:"name"`
	Username       string `json:"username"`
	HashedPassword string `json:"hashed_password"`
	Roles          []Role `json:"roles"`
}

type Authenticator struct {
	db DB
}

func NewAuthenticator(db DB) *Authenticator {
	return &Authenticator{
		db: db,
	}
}

func (a *Authenticator) ValidateCredentials(username string, password string) (User, error) {
	user, err := a.db.GetUserByUsername(username)

	if err != nil {
		return User{}, fmt.Errorf("user not found")
	}

	if err != nil {
		return User{}, fmt.Errorf("failed to hash password")
	}

	if err = ComparePassword(user.HashedPassword, password); err != nil {
		return User{}, fmt.Errorf("invalid credentials")
	}
	return user, nil
}

func (a *Authenticator) HasRole(user User, requiredRole Role) bool {
	for _, role := range user.Roles {
		if role == requiredRole || role == RoleAdmin {
			return true
		}
	}
	return false
}
