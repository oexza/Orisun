package events

import (
	globalCommon "orisun/src/orisun/common"
)
const (
	UserStreamPrefix = "User-Registration:::::"
	RegistrationTag  = "Registration"
	UsernameTag      = "Registration_username"
)

// Event types
const (
	EventTypeUserCreated     = "$UserCreated"
	EventTypeUserDeleted     = "$UserDeleted"
	EventTypeRolesChanged    = "$RolesChanged"
	EventTypePasswordChanged = "$PasswordChanged"
)

type UserCreated struct {
	Name         string `json:"name"`
	UserId       string `json:"user_id"`
	Username     string `json:"username"`
	Roles        []globalCommon.Role `json:"roles,omitempty"`
	PasswordHash string `json:"password_hash,omitempty"`
}

type UserDeleted struct {
	UserId string `json:"user_id"`
}

type UserRolesChanged struct {
	UserId string   `json:"username"`
	Roles  []string `json:"roles,omitempty"`
}

type UserPasswordChanged struct {
	UserId       string `json:"username"`
	PasswordHash string `json:"password_hash,omitempty"`
}
