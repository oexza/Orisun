package boundary_provisioning

import (
	"context"
	"errors"
	"testing"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/stretchr/testify/require"
)

func TestRuntimeEventHandlerInstallsThenActivatesLocally(t *testing.T) {
	definitionEvent := createdBoundaryEvent(t, "orders", 10, 2)
	activationEvent := lifecycleEventRead(t, adminevents.EventTypeBoundaryActivated, adminevents.BoundaryActivated{
		Boundary: "orders",
	}, 11, 3)
	installer := &captureInstaller{}
	activator := &captureActivator{}
	handler := NewBoundaryRuntimeEventHandler(
		"orisun_admin",
		runtimeCatalogRetriever{events: coreeventstore.ReadEventBatch{definitionEvent, activationEvent}},
		installer.InstallBoundary,
		activator.ActivateBoundary,
	)

	require.NoError(t, handler.Handle(t.Context(), activationEvent))
	require.Equal(t, []boundarymodel.Definition{{
		Name:        "orders",
		Description: "Orders context",
		Placement:   boundarymodel.Placement{Backend: "postgres", Namespace: "sales"},
	}}, installer.definitions)
	require.Equal(t, []string{"orders"}, activator.boundaries)
}

func TestRuntimeEventHandlerKeepsGateClosedWhenLocalInstallFails(t *testing.T) {
	definitionEvent := createdBoundaryEvent(t, "orders", 10, 2)
	activationEvent := lifecycleEventRead(t, adminevents.EventTypeBoundaryActivated, adminevents.BoundaryActivated{
		Boundary: "orders",
	}, 11, 3)
	installErr := errors.New("register LISTEN: unavailable")
	installer := &captureInstaller{err: installErr}
	activator := &captureActivator{}
	handler := NewBoundaryRuntimeEventHandler(
		"orisun_admin",
		runtimeCatalogRetriever{events: coreeventstore.ReadEventBatch{definitionEvent, activationEvent}},
		installer.InstallBoundary,
		activator.ActivateBoundary,
	)

	require.ErrorIs(t, handler.Handle(t.Context(), activationEvent), installErr)
	require.Empty(t, activator.boundaries)
}

type runtimeCatalogRetriever struct {
	events coreeventstore.ReadEventBatch
}

func (r runtimeCatalogRetriever) Read(context.Context, coreeventstore.ReadRequest) (coreeventstore.ReadEventBatch, error) {
	return r.events, nil
}

type captureInstaller struct {
	definitions []boundarymodel.Definition
	err         error
}

func (i *captureInstaller) InstallBoundary(_ context.Context, definition boundarymodel.Definition) error {
	i.definitions = append(i.definitions, definition)
	return i.err
}
