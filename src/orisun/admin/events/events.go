package events

type Role string

const (
	RoleAdmin      Role = "ADMIN"
	RoleOperations Role = "OPERATIONS"
	RoleRead       Role = "READ"
	RoleWrite      Role = "WRITE"
)

const (
	UserStreamPrefix = "User-Registration:::::"
	RegistrationTag  = "Registration"
	UsernameTag      = "Registration_username"
)

var Roles = []Role{RoleAdmin, RoleOperations, RoleRead, RoleWrite}
func (r Role) String() string {
	return string(r)
}
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
	Roles        []Role `json:"roles,omitempty"`
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
