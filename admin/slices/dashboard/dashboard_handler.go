package dashboard

import (
	adminCommon "github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/admin/slices/dashboard/event_count"
	"github.com/oexza/Orisun/admin/slices/dashboard/user_count"
	l "github.com/oexza/Orisun/logging"
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
