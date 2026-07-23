package events

import (
	boundaryevents "github.com/OrisunLabs/Orisun/boundary/events"
	"github.com/OrisunLabs/Orisun/orisun"
)

const (
	AdminStream = "OrisunAdmin"
)

// Event types
const (
	EventTypeUserCreated         = "$UserCreated"
	EventTypeUserDeleted         = "$UserDeleted"
	EventTypeRolesChanged        = "$RolesChanged"
	EventTypeUserPasswordChanged = "$UserPasswordChanged"
	EventTypeBoundaryCreated     = boundaryevents.EventTypeBoundaryCreated
	EventTypeBoundaryImported    = boundaryevents.EventTypeBoundaryImported
	EventTypeBoundaryActivated   = boundaryevents.EventTypeBoundaryActivated
	EventTypeBoundaryFailed      = boundaryevents.EventTypeBoundaryFailed
)

type UserCreated struct {
	Name         string        `json:"name"`
	UserId       string        `json:"user_id"`
	Username     string        `json:"username"`
	Roles        []orisun.Role `json:"roles,omitempty"`
	PasswordHash string        `json:"password_hash,omitempty"`
}

type UserDeleted struct {
	UserId string `json:"user_id"`
}

type UserPasswordChanged struct {
	UserId       string `json:"user_id"`
	PasswordHash string `json:"password_hash,omitempty"`
}

// Deprecated: use boundary/events.BoundaryCreated.
type BoundaryCreated = boundaryevents.BoundaryCreated

// Deprecated: use boundary/events.BoundaryImported.
type BoundaryImported = boundaryevents.BoundaryImported

// Deprecated: use boundary/events.BoundaryActivated.
type BoundaryActivated = boundaryevents.BoundaryActivated

// Deprecated: use boundary/events.BoundaryProvisioningFailed.
type BoundaryProvisioningFailed = boundaryevents.BoundaryProvisioningFailed
