package boundary_catalog

import (
	"testing"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func TestCatalogFoldsCreatedBoundaryLifecycle(t *testing.T) {
	catalog := NewCatalog()

	created := lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
		Boundary:    "orders",
		Description: "Orders context",
		Placement: boundarymodel.Placement{
			Backend:   "postgres",
			Namespace: "sales",
		},
	}, 10, 2)
	applied, err := catalog.Apply(created)
	require.NoError(t, err)
	require.True(t, applied)

	boundary, ok := catalog.Get("orders")
	require.True(t, ok)
	require.Equal(t, boundarymodel.StatusProvisioning, boundary.Status)
	require.False(t, boundary.ExistedBeforeCatalog)
	require.Equal(t, "Orders context", boundary.Description)
	require.Equal(t, "postgres", boundary.Placement.Backend)
	require.Equal(t, "sales", boundary.Placement.Namespace)
	require.Equal(t, &coreeventstore.Position{CommitPosition: 10, PreparePosition: 2}, boundary.DefinitionPosition)

	activated := lifecycleEvent(t, adminevents.EventTypeBoundaryActivated, adminevents.BoundaryActivated{
		Boundary: "orders",
	}, 11, 3)
	applied, err = catalog.Apply(activated)
	require.NoError(t, err)
	require.True(t, applied)

	boundary, ok = catalog.Get("orders")
	require.True(t, ok)
	require.Equal(t, boundarymodel.StatusActive, boundary.Status)
	require.Empty(t, boundary.LastError)
	require.Equal(t, &coreeventstore.Position{CommitPosition: 11, PreparePosition: 3}, boundary.StatusPosition)
}

func TestCatalogFoldsExistingStorageFailureAndRecovery(t *testing.T) {
	catalog := NewCatalog()

	_, err := catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
		Boundary:             "billing",
		ExistedBeforeCatalog: true,
		Placement: boundarymodel.Placement{
			Backend:   "sqlite",
			Namespace: "billing.db",
		},
	}, 1, 1))
	require.NoError(t, err)

	_, err = catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryFailed, adminevents.BoundaryProvisioningFailed{
		Boundary: "billing",
		Error:    "database is locked",
	}, 2, 2))
	require.NoError(t, err)

	boundary, ok := catalog.Get("billing")
	require.True(t, ok)
	require.True(t, boundary.ExistedBeforeCatalog)
	require.Equal(t, boundarymodel.StatusFailed, boundary.Status)
	require.Equal(t, "database is locked", boundary.LastError)

	_, err = catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryActivated, adminevents.BoundaryActivated{
		Boundary: "billing",
	}, 3, 3))
	require.NoError(t, err)

	boundary, ok = catalog.Get("billing")
	require.True(t, ok)
	require.Equal(t, boundarymodel.StatusActive, boundary.Status)
	require.Empty(t, boundary.LastError)
}

func TestCatalogDoesNotDowngradeActiveBoundary(t *testing.T) {
	catalog := NewCatalog()
	_, err := catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
		Boundary:  "orders",
		Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "public"},
	}, 1, 1))
	require.NoError(t, err)
	_, err = catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryActivated, adminevents.BoundaryActivated{
		Boundary: "orders",
	}, 2, 2))
	require.NoError(t, err)
	_, err = catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryFailed, adminevents.BoundaryProvisioningFailed{
		Boundary: "orders",
		Error:    "late node-local failure",
	}, 3, 3))
	require.NoError(t, err)

	boundary, ok := catalog.Get("orders")
	require.True(t, ok)
	require.Equal(t, boundarymodel.StatusActive, boundary.Status)
	require.Empty(t, boundary.LastError)
}

func TestCatalogIgnoresOutcomeBeforeDefinition(t *testing.T) {
	catalog := NewCatalog()

	applied, err := catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryActivated, adminevents.BoundaryActivated{
		Boundary: "orders",
	}, 1, 1))
	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, catalog.List())

	_, err = catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
		Boundary:  "orders",
		Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "public"},
	}, 2, 2))
	require.NoError(t, err)

	boundary, ok := catalog.Get("orders")
	require.True(t, ok)
	require.Equal(t, boundarymodel.StatusProvisioning, boundary.Status)
}

func TestCatalogRejectsInvalidLifecycle(t *testing.T) {
	tests := []struct {
		name  string
		event coreeventstore.ReadEvent
		want  string
	}{
		{
			name:  "outcome without boundary",
			event: lifecycleEvent(t, adminevents.EventTypeBoundaryActivated, adminevents.BoundaryActivated{}, 1, 1),
			want:  "has no boundary",
		},
		{
			name: "definition without backend",
			event: lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
				Boundary:  "orders",
				Placement: boundarymodel.Placement{Namespace: "public"},
			}, 1, 1),
			want: "no placement backend",
		},
		{
			name: "failed without error",
			event: lifecycleEvent(t, adminevents.EventTypeBoundaryFailed, adminevents.BoundaryProvisioningFailed{
				Boundary: "orders",
			}, 2, 2),
			want: "empty error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := NewCatalog()
			if tt.name == "failed without error" {
				_, err := catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
					Boundary: "orders",
					Placement: boundarymodel.Placement{
						Backend:   "postgres",
						Namespace: "public",
					},
				}, 1, 1))
				require.NoError(t, err)
			}

			applied, err := catalog.Apply(tt.event)
			require.True(t, applied)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCatalogRejectsDuplicateDefinition(t *testing.T) {
	catalog := NewCatalog()
	definition := adminevents.BoundaryCreated{
		Boundary: "orders",
		Placement: boundarymodel.Placement{
			Backend:   "postgres",
			Namespace: "public",
		},
	}

	_, err := catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, definition, 1, 1))
	require.NoError(t, err)
	_, err = catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, definition, 2, 2))
	require.ErrorContains(t, err, "already defined")
}

func TestCatalogIgnoresOtherAdminEventsAndListsByName(t *testing.T) {
	catalog := NewCatalog()
	applied, err := catalog.Apply(coreeventstore.ReadEvent{EventType: "$UserCreated", Data: `{}`})
	require.NoError(t, err)
	require.False(t, applied)

	for i, name := range []string{"zeta", "alpha"} {
		_, err := catalog.Apply(lifecycleEvent(t, adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
			Boundary: name,
			Placement: boundarymodel.Placement{
				Backend:   "postgres",
				Namespace: "public",
			},
		}, int64(i+1), int64(i+1)))
		require.NoError(t, err)
	}

	list := catalog.List()
	require.Len(t, list, 2)
	require.Equal(t, "alpha", list[0].Name)
	require.Equal(t, "zeta", list[1].Name)

	list[0].Status = boundarymodel.StatusFailed
	unchanged, ok := catalog.Get("alpha")
	require.True(t, ok)
	require.Equal(t, boundarymodel.StatusProvisioning, unchanged.Status)
}

func lifecycleEvent(t *testing.T, eventType string, data any, commit, prepare int64) coreeventstore.ReadEvent {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	return coreeventstore.ReadEvent{
		EventType: eventType,
		Data:      string(raw),
		Position: coreeventstore.Position{
			CommitPosition:  commit,
			PreparePosition: prepare,
		},
	}
}
