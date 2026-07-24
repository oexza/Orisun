package dashboard

import (
	"github.com/OrisunLabs/Orisun/admin/slices/dashboard/event_count"
	"github.com/OrisunLabs/Orisun/admin/slices/dashboard/user_count"
	l "github.com/OrisunLabs/Orisun/logging"
)

type GetCatchupSubscriptionCount = func() uint32

type DashboardHandler struct {
	logger                l.Logger
	boundaries            []string
	getUserCount          user_count.GetUserCount
	subscribeToUserCount  user_count.SubscribeToUserCount
	getEventCount         event_count.GetEventCount
	subscribeToEventCount event_count.SubscribeToEventCount
}

func NewDashboardHandler(
	logger l.Logger,
	boundaries []string,
	getUserCount user_count.GetUserCount,
	subscribeToUserCount user_count.SubscribeToUserCount,
	getEventCount event_count.GetEventCount,
	subscribeToEventCount event_count.SubscribeToEventCount,
) *DashboardHandler {
	return &DashboardHandler{
		logger:                logger,
		boundaries:            boundaries,
		getUserCount:          getUserCount,
		subscribeToUserCount:  subscribeToUserCount,
		getEventCount:         getEventCount,
		subscribeToEventCount: subscribeToEventCount,
	}
}
