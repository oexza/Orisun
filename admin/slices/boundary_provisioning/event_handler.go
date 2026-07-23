package boundary_provisioning

import (
	"context"
	"time"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"github.com/goccy/go-json"
)

type BoundaryProvisioningEventHandler struct {
	adminBoundary string
	appender      EventAppender
	retriever     LatestByCriteriaRetriever
	provision     ProvisionBoundary
	activate      ActivateBoundary
}

// ActivateBoundary exposes a successfully activated catalog boundary to
// public event-store requests in this process.
type ActivateBoundary func(ctx context.Context, boundary string) error

func NewBoundaryProvisioningEventHandler(
	adminBoundary string,
	appender EventAppender,
	retriever LatestByCriteriaRetriever,
	provision ProvisionBoundary,
	activate ActivateBoundary,
) *BoundaryProvisioningEventHandler {
	return &BoundaryProvisioningEventHandler{
		adminBoundary: adminBoundary,
		appender:      appender,
		retriever:     retriever,
		provision:     provision,
		activate:      activate,
	}
}

func (h *BoundaryProvisioningEventHandler) Handle(ctx context.Context, event coreeventstore.ReadEvent) error {
	if h == nil || h.activate == nil {
		return statuscode.New(statuscode.Internal, "boundary provisioning event handler is not configured")
	}
	definition, err := definitionFromEvent(event)
	if err != nil {
		return err
	}
	_, err = ProvisionBoundaryCommandHandler(ctx, ProvisionBoundaryCommand{
		Definition: definition,
		Metadata: map[string]any{
			"source":     "event_handler",
			"handler":    "boundary_provisioning",
			"capturedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"reactedTo": map[string]any{
				"eventId":   event.EventID,
				"eventType": event.EventType,
				"position": map[string]int64{
					"commit":  event.Position.CommitPosition,
					"prepare": event.Position.PreparePosition,
				},
			},
		},
	}, h.adminBoundary, h.appender, h.retriever, h.provision)
	if err != nil && statuscode.CodeOf(err) != statuscode.AlreadyExists {
		return err
	}
	// The public request gate advances only after BoundaryActivated is durable,
	// or after replay proves that another node already made it durable. This
	// keeps a partially provisioned or FAILED boundary inaccessible even when
	// its physical backend has already been registered locally.
	return h.activate(ctx, definition.Name)
}

func definitionFromEvent(event coreeventstore.ReadEvent) (boundarymodel.Definition, error) {
	switch event.EventType {
	case adminevents.EventTypeBoundaryCreated:
		var data adminevents.BoundaryCreated
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return boundarymodel.Definition{}, statuscode.Errorf(statuscode.Internal, "decode %s: %v", event.EventType, err)
		}
		return boundarymodel.Definition{Name: data.Boundary, Description: data.Description, Placement: data.Placement}, nil
	case adminevents.EventTypeBoundaryImported:
		var data adminevents.BoundaryImported
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return boundarymodel.Definition{}, statuscode.Errorf(statuscode.Internal, "decode %s: %v", event.EventType, err)
		}
		return boundarymodel.Definition{Name: data.Boundary, Description: data.Description, Placement: data.Placement}, nil
	default:
		return boundarymodel.Definition{}, statuscode.Errorf(statuscode.InvalidArgument, "unsupported boundary provisioning event %q", event.EventType)
	}
}
