//go:build foundationdb

package foundationdb

import (
	"slices"
	"testing"

	eventstore "github.com/OrisunLabs/Orisun/orisun"
)

func TestFoundationDBSystemIndexesCoverBoundaryDefinitionReplay(t *testing.T) {
	want := []eventstore.BoundaryIndexField{{JsonKey: "eventType", ValueType: "text"}}
	for _, index := range systemAdminIndexes {
		if index.name == "sys_admin_event_type" {
			if !slices.Equal(index.fields, want) {
				t.Fatalf("sys_admin_event_type fields = %#v", index.fields)
			}
			return
		}
	}
	t.Fatal("missing eventType-only system index required by catalog replay")
}

func TestFoundationDBBoundaryProvisioningPersistsDiscoveryMarker(t *testing.T) {
	backend := newTestBackend(t)
	definition := eventstore.BoundaryDefinition{
		Name:      "sales",
		Placement: eventstore.BoundaryPlacement{Backend: "foundationdb", Namespace: backend.root},
	}
	if err := backend.ProvisionBoundary(t.Context(), definition); err != nil {
		t.Fatalf("ProvisionBoundary() error = %v", err)
	}
	if err := backend.ProvisionBoundary(t.Context(), definition); err != nil {
		t.Fatalf("idempotent ProvisionBoundary() error = %v", err)
	}
	if err := backend.checkBoundary("sales"); err != nil {
		t.Fatalf("checkBoundary() error = %v", err)
	}

	discovered, err := discoverBoundaryNames(backend.db, backend.root)
	if err != nil {
		t.Fatalf("discoverBoundaryNames() error = %v", err)
	}
	if !slices.Contains(discovered, "sales") {
		t.Fatalf("discovered boundaries = %#v", discovered)
	}
}

func TestFoundationDBBoundaryProvisioningRejectsWrongRoot(t *testing.T) {
	backend := newTestBackend(t)
	err := backend.ProvisionBoundary(t.Context(), eventstore.BoundaryDefinition{
		Name:      "sales",
		Placement: eventstore.BoundaryPlacement{Backend: "foundationdb", Namespace: "other"},
	})
	if err == nil {
		t.Fatal("ProvisionBoundary() error = nil")
	}
}
