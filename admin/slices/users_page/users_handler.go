package users_page

import (
	admin_events "github.com/oexza/Orisun/admin/events"
	"github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/admin/templates"
	l "github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"net/http"
	"time"

	"github.com/starfederation/datastar-go/datastar"
	"golang.org/x/sync/errgroup"
)

type UsersPageHandler struct {
	logger                l.Logger
	boundary              string
	ListAdminUsers        func() ([]*orisun.User, error)
	subscribeToEventstore admin_common.SubscribeToEventStoreType
}

func NewUsersPageHandler(logger l.Logger, boundary string,
	listAdminUsers func() ([]*orisun.User, error),
	subscribeToEventstore admin_common.SubscribeToEventStoreType) *UsersPageHandler {
	return &UsersPageHandler{
		logger:                logger,
		boundary:              boundary,
		ListAdminUsers:        listAdminUsers,
		subscribeToEventstore: subscribeToEventstore,
	}
}

func (s *UsersPageHandler) HandleUsersPage(w http.ResponseWriter, r *http.Request) {
	currentUser := admin_common.GetCurrentUser(r)
	templates.UsersPage([]templates.User{}, r.URL.Path, currentUser.Id).Render(r.Context(), w)
}

func (s *UsersPageHandler) HandleUsersPageSSE(w http.ResponseWriter, r *http.Request) {
	currentUser := admin_common.GetCurrentUser(r)

	if isDatastarRequest(r) {
		sse, tabId := admin_common.GetOrCreateSSEConnection(w, r)
		grp, gctx := errgroup.WithContext(r.Context())
		userSubscription := orisun.NewMessageHandler[orisun.Event](gctx)

		grp.Go(func() error {
			for {
				select {
				case <-gctx.Done():
					s.logger.Debugf("Context done, stopping dashboard event processing")
					return nil
				default:
					event, err := userSubscription.Recv()
					if err != nil {
						if gctx.Err() != nil {
							return nil
						}
						s.logger.Errorf("Error receiving events: %v", err)
						time.Sleep(100 * time.Millisecond)
						continue
					}
					if event.EventType == admin_events.EventTypeUserCreated || event.EventType == admin_events.EventTypeUserDeleted {
						s.logger.Info("Received user event, updating users dashboard")
						time.Sleep(1 * time.Second)
						users, err := s.listUsers()
						if err != nil {
							s.logger.Errorf("Error listing users: %v", err)
							continue
						}
						sse.PatchElementTempl(templates.PageContainer(users, r.URL.Path, currentUser.Id), datastar.WithSelectorID(templates.UserPageId))
					}
				}
			}
		})
		s.subscribeToEventstore(gctx, s.boundary, "users-dashboard-"+tabId, nil, nil, userSubscription)
		_ = grp.Wait()
		// Wait for connection to close
		<-sse.Context().Done()
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
