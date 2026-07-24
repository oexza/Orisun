package create_boundary

import (
	"context"
	"testing"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/goccy/go-json"
)

func TestReconcileLegacyBoundariesCreatesOnlyMissingDefinitions(t *testing.T) {
	retriever := &legacyRetriever{existing: map[string]boundarymodel.Definition{
		"orders": {Name: "orders", Description: "Catalog description", Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "public"}},
	}}
	saver := &legacySaver{retriever: retriever}
	result, err := ReconcileLegacyBoundaries(
		t.Context(),
		[]boundarymodel.Definition{
			{Name: "sales", Description: "Sales", Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "tenant_data"}},
			{Name: "orders", Description: "Orders", Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "public"}},
		},
		"orisun_admin",
		saver,
		retriever,
	)
	if err != nil {
		t.Fatalf("ReconcileLegacyBoundaries() error = %v", err)
	}
	if len(result.Created) != 1 || result.Created[0] != "sales" {
		t.Fatalf("created = %#v", result.Created)
	}
	if len(result.Existing) != 1 || result.Existing[0] != "orders" {
		t.Fatalf("existing = %#v", result.Existing)
	}
	if len(saver.events) != 1 || saver.events[0].EventType != adminevents.EventTypeBoundaryCreated {
		t.Fatalf("events = %#v", saver.events)
	}
	var data adminevents.BoundaryCreated
	if err := json.Unmarshal([]byte(saver.events[0].Data), &data); err != nil {
		t.Fatal(err)
	}
	if data.Boundary != "sales" || data.Placement.Namespace != "tenant_data" || !data.ExistedBeforeCatalog {
		t.Fatalf("event data = %#v", data)
	}
}

func TestReconcileLegacyBoundariesRejectsPlacementConflict(t *testing.T) {
	retriever := &legacyRetriever{existing: map[string]boundarymodel.Definition{
		"orders": {Name: "orders", Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "catalog_schema"}},
	}}
	_, err := ReconcileLegacyBoundaries(
		t.Context(),
		[]boundarymodel.Definition{{Name: "orders", Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "configured_schema"}}},
		"orisun_admin",
		&legacySaver{retriever: retriever},
		retriever,
	)
	if err == nil {
		t.Fatal("ReconcileLegacyBoundaries() error = nil")
	}
}

func TestReconcileLegacyBoundariesRejectsDuplicates(t *testing.T) {
	definition := boundarymodel.Definition{Name: "sales", Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "public"}}
	_, err := ReconcileLegacyBoundaries(
		t.Context(),
		[]boundarymodel.Definition{definition, definition},
		"orisun_admin",
		&legacySaver{},
		&legacyRetriever{},
	)
	if err == nil {
		t.Fatal("ReconcileLegacyBoundaries() error = nil")
	}
}

type legacyRetriever struct {
	existing map[string]boundarymodel.Definition
}

func (r *legacyRetriever) LatestByCriteria(_ context.Context, query coreeventstore.LatestByCriteriaRequest) (coreeventstore.LatestByCriteriaResult, error) {
	matches := make([]coreeventstore.LatestCriterionMatch, len(query.Criteria))
	name := query.Criteria[0].Tags[0].Value
	if definition, ok := r.existing[name]; ok {
		data, err := json.Marshal(adminevents.BoundaryCreated{
			Boundary:             definition.Name,
			Description:          definition.Description,
			Placement:            definition.Placement,
			ExistedBeforeCatalog: true,
		})
		if err != nil {
			return coreeventstore.LatestByCriteriaResult{}, err
		}
		matches[0] = coreeventstore.LatestCriterionMatch{
			Found: true,
			Event: coreeventstore.ReadEvent{EventType: adminevents.EventTypeBoundaryCreated, Data: string(data)},
		}
	}
	return coreeventstore.LatestByCriteriaResult{
		Matches:         matches,
		ContextPosition: coreeventstore.NotExistsPosition(),
	}, nil
}

type legacySaver struct {
	retriever *legacyRetriever
	events    []coreeventstore.EventToAppend
}

func (s *legacySaver) Append(_ context.Context, request coreeventstore.AppendRequest) (coreeventstore.AppendResult, error) {
	s.events = append(s.events, request.Events...)
	if s.retriever != nil {
		if s.retriever.existing == nil {
			s.retriever.existing = make(map[string]boundarymodel.Definition)
		}
		var data adminevents.BoundaryCreated
		if err := json.Unmarshal([]byte(request.Events[0].Data), &data); err == nil {
			s.retriever.existing[data.Boundary] = boundarymodel.Definition{
				Name:                 data.Boundary,
				Description:          data.Description,
				Placement:            data.Placement,
				ExistedBeforeCatalog: data.ExistedBeforeCatalog,
			}
		}
	}
	return coreeventstore.AppendResult{Position: coreeventstore.Position{
		CommitPosition:  1,
		PreparePosition: int64(len(s.events)),
	}}, nil
}
