package dashboard

import (
	"net/http"
	pb "orisun/src/orisun/eventstore"
	l "orisun/src/orisun/logging"

	common "orisun/src/orisun/admin/slices/common"
)

type DashboardHandler struct {
	logger     l.Logger
	eventStore *pb.EventStore
	boundary   string
}

func NewDashboardHandler(
	logger l.Logger,
	eventStore *pb.EventStore,
	boundary string,
) *DashboardHandler {
	return &DashboardHandler{
		logger:     logger,
		eventStore: eventStore,
		boundary:   boundary,
	}
}

func (s *DashboardHandler) HandleDashboardPage(w http.ResponseWriter, r *http.Request) {
	isDatastarRequest := r.Header.Get("datastar-request") == "true"

	if !isDatastarRequest {
		Dashboard(r.URL.Path).Render(r.Context(), w)
	} else {
		common.CreateSSEConnection(w, r)
		// Wait for connection to close
		<-r.Context().Done()
	}
}
