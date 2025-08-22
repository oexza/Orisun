package users_page

import (
	"net/http"
	admin_events "orisun/admin/events"
	"orisun/admin/slices/common"
	"orisun/admin/templates"
	globalCommon "orisun/common"
	"orisun/eventstore"
	l "orisun/logging"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

type UsersPageHandler struct {
	logger                l.Logger
	boundary              string
	ListAdminUsers        func() ([]*globalCommon.User, error)
	subscribeToEventstore admin_common.SubscribeToEventStoreType
}

func NewUsersPageHandler(logger l.Logger, boundary string,
	listAdminUsers func() ([]*globalCommon.User, error),
	subscribeToEventstore admin_common.SubscribeToEventStoreType) *UsersPageHandler {
	return &UsersPageHandler{
		logger:                logger,
		boundary:              boundary,
		ListAdminUsers:        listAdminUsers,
		subscribeToEventstore: subscribeToEventstore,
	}
}

func (s *UsersPageHandler) HandleUsersPage(w http.ResponseWriter, r *http.Request) {
	users, err := s.listUsers()
	if err != nil {
		s.logger.Debugf("Failed to list users: %v", err)
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	currentUser := admin_common.GetCurrentUser(r)

	if isDatastarRequest(r) {
		sse, tabId := admin_common.GetOrCreateSSEConnection(w, r)
		userSubscription := globalCommon.NewMessageHandler[eventstore.Event](r.Context())

		go func() {
			for {
				select {
				case <-r.Context().Done():
					s.logger.Debugf("Context done, stopping dashboard event processing")
					return // Exit the goroutine completely
				default:
					// Only try to receive if context is not done
					event, err := userSubscription.Recv()
					if err != nil {
						s.logger.Errorf("Error receiving events: %v", err)
						if r.Context().Err() != nil {
							return
						}
						continue
					}
					if event.EventType == admin_events.EventTypeUserCreated || event.EventType == admin_events.EventTypeUserDeleted {
						//delay for 1 second
						time.Sleep(1 * time.Second)
						users, err := s.listUsers()
						if err != nil {
							s.logger.Errorf("Error listing users: %v", err)
							continue
						}
						sse.PatchElementTempl(templates.UserList(users, currentUser.Id), datastar.WithSelectorID(templates.UsersTableId))
					}
				}
			}
		}()
		s.subscribeToEventstore(r.Context(), s.boundary, "users-dashboard-"+tabId, nil, nil, *userSubscription)
		// Wait for connection to close
		<-sse.Context().Done()
	} else {
		templates.Users(users, r.URL.Path, currentUser.Id).Render(r.Context(), w)
	}
}

func isDatastarRequest(r *http.Request) bool {
	return r.Header.Get("datastar-request") == "true"
}

func (s *UsersPageHandler) listUsers() ([]templates.User, error) {
	users, err := s.ListAdminUsers()
	if err != nil {
		return nil, err
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
	return templateUsers, nil
}
