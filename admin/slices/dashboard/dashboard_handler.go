package dashboard

import (
	"net/http"
	l "orisun/logging"

	adminCommon "orisun/admin/slices/common"
	"orisun/admin/slices/dashboard/event_count"
	"orisun/admin/slices/dashboard/user_count"
	globalCommon "orisun/common"

	datastar "github.com/starfederation/datastar-go/datastar"
)

type GetCatchupSubscriptionCount = func() uint32

type DashboardHandler struct {
	logger                      l.Logger
	boundaries                  []string
	getUserCount                user_count.GetUserCount
	subscribeToUserCount        user_count.SubscribeToUserCount
	getCatchupSubscriptionCount GetCatchupSubscriptionCount
	getEventCount               event_count.GetEventCount
	subscribeToEventCount       event_count.SubscribeToEventCount
	db                          adminCommon.DB
}

func NewDashboardHandler(
	logger l.Logger,
	boundaries []string,
	getUserCount user_count.GetUserCount,
	subscribeToUserCount user_count.SubscribeToUserCount,
	getEventCount event_count.GetEventCount,
	subscribeToEventCount event_count.SubscribeToEventCount,
	// getGetCatchupSubscriptionCount GetCatchupSubscriptionCount,
) *DashboardHandler {
	return &DashboardHandler{
		logger:                logger,
		boundaries:            boundaries,
		getUserCount:          getUserCount,
		subscribeToUserCount:  subscribeToUserCount,
		getEventCount:         getEventCount,
		subscribeToEventCount: subscribeToEventCount,
		// getCatchupSubscriptionCount: getGetCatchupSubscriptionCount,
	}
}

func (dh *DashboardHandler) HandleDashboardPage(w http.ResponseWriter, r *http.Request) {
	userCount, err := dh.getUserCount()
	dh.logger.Debugf("User count: %d", userCount.Count)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	eventCounts := map[string]int{}
	for _, boundary := range dh.boundaries {
		eventCountModel, err := dh.getEventCount(boundary)
		if err != nil {
			dh.logger.Errorf("Error getting events count: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		dh.logger.Debugf("Event count: %d", eventCountModel.Count)
		eventCounts[boundary] = eventCountModel.Count
	}

	isDatastarRequest := r.Header.Get("datastar-request") == "true"
	if !isDatastarRequest {
		Dashboard(r.URL.Path, DashboardDetails{
			UserCount:    userCount.Count,
			CatchupCount: 0,
			EventCounts:  eventCounts,
		}).Render(r.Context(), w)
	} else {
		sse, tabId := adminCommon.GetOrCreateSSEConnection(w, r)
		dh.handleUserCount(sse, r, tabId, dh.boundaries)

		// Wait for connection to close
		<-sse.Context().Done()
	}
}

func (dh *DashboardHandler) handleUserCount(
	sse *datastar.ServerSentEventGenerator,
	r *http.Request,
	tabId string,
	boudaries []string,
) {
	userSubscription := globalCommon.NewMessageHandler[user_count.UserCountReadModel](r.Context())

	go func() {
		for {
			select {
			case <-r.Context().Done():
				dh.logger.Debugf("Context done, stopping dashboard event processing")
				return // Exit the goroutine completely
			default:
				// Only try to receive if context is not done
				event, err := userSubscription.Recv()
				if err != nil {
					dh.logger.Errorf("Error receiving user count: %v", err)
					return
				}
				sse.PatchElementTempl(UserCountFragement(event.Count), datastar.WithSelectorID(UserCountId))
			}
		}
	}()
	dh.subscribeToUserCount("tab::::"+tabId, r.Context(), userSubscription)

	for _, boundary := range boudaries {
		eventSubscription := globalCommon.NewMessageHandler[event_count.EventCountReadModel](r.Context())
		go func() {
			for {
				select {
				case <-r.Context().Done():
					dh.logger.Debugf("Context done, stopping event count processing")
					return // Exit the goroutine completely
				default:
					// Only try to receive if context is not done
					event, err := eventSubscription.Recv()
					if err != nil {
						dh.logger.Errorf("Error receiving event count: %v", err)
						return
					}
					sse.PatchElementTempl(EventCountFragment(event.Count, boundary), datastar.WithSelectorID(eventCountId+boundary))
				}
			}
		}()
		dh.subscribeToEventCount("tab::::"+tabId+boundary, boundary, r.Context(), eventSubscription)
	}
}
