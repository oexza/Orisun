package users_page

import (
	"net/http"
	common "orisun/admin/slices/common"
	"orisun/admin/templates"
	globalCommon "orisun/common"
	l "orisun/logging"
)

type UsersPageHandler struct {
	logger         l.Logger
	boundary       string
	ListAdminUsers func() ([]*globalCommon.User, error)
}

func NewUsersPageHandler(logger l.Logger, boundary string,
	listAdminUsers func() ([]*globalCommon.User, error)) *UsersPageHandler {
	return &UsersPageHandler{
		logger:         logger,
		boundary:       boundary,
		ListAdminUsers: listAdminUsers,
	}
}

func (s *UsersPageHandler) HandleUsersPage(w http.ResponseWriter, r *http.Request) {
	users, err := s.ListAdminUsers()
	if err != nil {
		s.logger.Debugf("Failed to list users: %v", err)
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	// Convert internal user type to template user type
	templateUsers := make([]templates.User, len(users))
	for i, user := range users {
		// Convert []Role to []string for template compatibility
		roles := make([]string, len(user.Roles))
		for j, role := range user.Roles {
			roles[j] = string(role)
		}

		templateUsers[i] = templates.User{
			Name:     user.Name,
			Id:       user.Id,
			Username: user.Username,
			Roles:    roles,
		}
	}

	currentUser := common.GetCurrentUser(r)

	templates.Users(templateUsers, r.URL.Path, currentUser.Id).Render(r.Context(), w)
}
