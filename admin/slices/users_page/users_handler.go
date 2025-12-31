package users_page

import (
	"github.com/oexza/Orisun/admin/slices/common"
	l "github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
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
