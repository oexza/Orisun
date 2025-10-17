package admin_common

import (
	"context"
	"fmt"
	"net/http"
	ev "orisun/admin/events"
	t "orisun/admin/templates"
	"orisun/eventstore"
	l "orisun/logging"
	"strings"

	"github.com/goccy/go-json"

	pb "orisun/eventstore"

	admin_common "orisun/admin/slices/common"

	globalCommon "orisun/common"

	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar-go/datastar"
)

type PageStartHandler struct {
	logger                l.Logger
	boundary              string
	ListAdminUsers        func() ([]*globalCommon.User, error)
	subscribeToEventstore admin_common.SubscribeToEventStoreType
}

func NewUsersPageHandler(logger l.Logger, boundary string,
	listAdminUsers func() ([]*globalCommon.User, error),
	subscribeToEventstore admin_common.SubscribeToEventStoreType) *PageStartHandler {
	return &PageStartHandler{
		logger:                logger,
		boundary:              boundary,
		ListAdminUsers:        listAdminUsers,
		subscribeToEventstore: subscribeToEventstore,
	}
}

func (s *PageStartHandler) HandleUsersPage(w http.ResponseWriter, r *http.Request) {
	users, err := s.listUsers()
	if err != nil {
		s.logger.Debugf("Failed to list users: %v", err)
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	currentUser := admin_common.GetCurrentUser(r)

	if isDatastarRequest(r) {
		sse, tabId := admin_common.GetOrCreateSSEConnection(w, r)
		grp, gctx := errgroup.WithContext(r.Context())
		userSubscription := globalCommon.NewMessageHandler[eventstore.Event](gctx)

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
	} else {
		templates.UsersPage(users, r.URL.Path, currentUser.Id).Render(r.Context(), w)
	}
}

func isDatastarRequest(r *http.Request) bool {
	return r.Header.Get("datastar-request") == "true"
}