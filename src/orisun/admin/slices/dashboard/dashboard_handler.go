package dashboard

import (
	"net/http"
	l "orisun/src/orisun/logging"

	common "orisun/src/orisun/admin/slices/common"
	"orisun/src/orisun/admin/slices/dashboard/user_count"
	globalCommon "orisun/src/orisun/common"

	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar/sdk/go"
)

type DashboardHandler struct {
	logger               l.Logger
	boundary             string
	getUserCount         user_count.GetUserCount
	subscribeToUserCount user_count.SubscribeToUserCount
}

func NewDashboardHandler(
	logger l.Logger,
	boundary string,
	getUserCount user_count.GetUserCount,
	subscribeToUserCount user_count.SubscribeToUserCount,
) *DashboardHandler {
	return &DashboardHandler{
		logger:               logger,
		boundary:             boundary,
		getUserCount:         getUserCount,
		subscribeToUserCount: subscribeToUserCount,
	}
}

func (dh *DashboardHandler) HandleDashboardPage(w http.ResponseWriter, r *http.Request) {
	userCount, err := dh.getUserCount()
	dh.logger.Debugf("User count: %d", userCount.Count)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}

	isDatastarRequest := r.Header.Get("datastar-request") == "true"
	if !isDatastarRequest {
		Dashboard(r.URL.Path, DashboardDetails{
			UserCount: userCount.Count,
		}).Render(r.Context(), w)
	} else {
		signals := common.CommonSSESignals{}
		datastar.ReadSignals(r, signals)
		newTabId, err := uuid.NewV7()
		if err != nil {
			dh.logger.Error("Error generating tab id: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		signals.TabId = "Dashboard:::::" + newTabId.String()

		sse := common.CreateSSEConnection(w, r, signals.TabId)
		sse.MergeFragmentTempl(UserCountFragement(userCount.Count), datastar.WithSelectorID(UserCountId))

		subscription := globalCommon.NewMessageHandler[user_count.UserCountReadModel](r.Context())

		go func() {
			for {
				select {
				case <-sse.Context().Done():
					dh.logger.Debug("Context done, stopping dashboard event processing")
					return // Exit the goroutine completely
				default:
					// Only try to receive if context is not done
					event, err := subscription.Recv()
					if err != nil {
						dh.logger.Error("Error receiving user count: %v", err)
						continue
					}
					sse.MergeFragmentTempl(UserCountFragement(event.Count), datastar.WithSelectorID(UserCountId))
				}
			}
		}()

		dh.subscribeToUserCount(signals.TabId, r.Context(), subscription)

		// Wait for connection to close
		<-sse.Context().Done()
	}
}
