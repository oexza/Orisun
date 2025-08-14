package dashboard

import (
	"net/http"
	l "orisun/logging"

	common "orisun/admin/slices/common"
	"orisun/admin/slices/dashboard/user_count"
	globalCommon "orisun/common"

	datastar "github.com/starfederation/datastar-go/datastar"
)

type GetCatchupSubscriptionCount = func() uint32

type DashboardHandler struct {
	logger                      l.Logger
	boundary                    string
	getUserCount                user_count.GetUserCount
	subscribeToUserCount        user_count.SubscribeToUserCount
	getCatchupSubscriptionCount GetCatchupSubscriptionCount
}

func NewDashboardHandler(
	logger l.Logger,
	boundary string,
	getUserCount user_count.GetUserCount,
	subscribeToUserCount user_count.SubscribeToUserCount,
	// getGetCatchupSubscriptionCount GetCatchupSubscriptionCount,
) *DashboardHandler {
	return &DashboardHandler{
		logger:               logger,
		boundary:             boundary,
		getUserCount:         getUserCount,
		subscribeToUserCount: subscribeToUserCount,
		// getCatchupSubscriptionCount: getGetCatchupSubscriptionCount,
	}
}

func (dh *DashboardHandler) HandleDashboardPage(w http.ResponseWriter, r *http.Request) {
	userCount, err := dh.getUserCount()
	dh.logger.Debugf("User count: %d", userCount.Count)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}

	// catchUpSubscriptionCount := dh.getCatchupSubscriptionCount()

	isDatastarRequest := r.Header.Get("datastar-request") == "true"
	if !isDatastarRequest {
		Dashboard(r.URL.Path, DashboardDetails{
			UserCount:    userCount.Count,
			CatchupCount: 0,
		}).Render(r.Context(), w)
	} else {
		sse, tabId := common.GetOrCreateSSEConnection(w, r)
		dh.handleUserCount(sse, r, tabId, userCount)

		// Wait for connection to close
		<-sse.Context().Done()
	}
}

func (dh *DashboardHandler) handleUserCount(sse *datastar.ServerSentEventGenerator, r *http.Request,
	tabId string, userCount user_count.UserCountReadModel) {
	sse.PatchElementTempl(UserCountFragement(userCount.Count), datastar.WithSelectorID(UserCountId))

	subscription := globalCommon.NewMessageHandler[user_count.UserCountReadModel](r.Context())

	go func() {
		for {
			select {
			case <-sse.Context().Done():
				dh.logger.Debugf("Context done, stopping dashboard event processing")
				return // Exit the goroutine completely
			default:
				// Only try to receive if context is not done
				event, err := subscription.Recv()
				if err != nil {
					dh.logger.Errorf("Error receiving user count: %v", err)
					continue
				}
				sse.PatchElementTempl(UserCountFragement(event.Count), datastar.WithSelectorID(UserCountId))
			}
		}
	}()

	dh.subscribeToUserCount("tab::::"+tabId, r.Context(), subscription)
}
