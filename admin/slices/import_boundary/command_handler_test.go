package import_boundary

import (
	"context"
	"testing"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"
)

func TestImportBoundaryCommandHandlerEmitsBoundaryImported(t *testing.T) {
	retriever := &fakeRetriever{batch: emptyBatch()}
	saver := &fakeSaver{position: coreeventstore.Position{CommitPosition: 8, PreparePosition: 3}}
	result, err := ImportBoundaryCommandHandler(context.Background(), ImportBoundaryCommand{
		Name:        "billing",
		Description: "Billing context",
		Placement:   boundarymodel.Placement{Backend: "postgres", Namespace: "accounting"},
	}, "orisun_admin", saver, retriever)
	require.NoError(t, err)
	require.Equal(t, boundarymodel.OriginImported, result.Boundary.Origin)
	require.Equal(t, &coreeventstore.Position{CommitPosition: 8, PreparePosition: 3}, result.Boundary.DefinitionPosition)
	require.Len(t, saver.events, 1)
	require.Equal(t, adminevents.EventTypeBoundaryImported, saver.events[0].EventType)
	require.Equal(t, subsetQuery(definitionCriteria("billing")), saver.query)
}

func TestImportBoundaryCommandHandlerReturnsAlreadyExistsInsteadOfEventlessSuccess(t *testing.T) {
	existing := readEvent(t, adminevents.EventTypeBoundaryImported, adminevents.BoundaryImported{
		Boundary:  "billing",
		Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "accounting"},
	}, 7, 2)
	batch := emptyBatch()
	batch.Matches[1] = coreeventstore.LatestCriterionMatch{Event: existing, Found: true}
	batch.ContextPosition = coreeventstore.Position{CommitPosition: 7, PreparePosition: 2}
	saver := &fakeSaver{}

	_, err := ImportBoundaryCommandHandler(context.Background(), ImportBoundaryCommand{
		Name:      "billing",
		Placement: boundarymodel.Placement{Backend: "postgres", Namespace: "accounting"},
	}, "orisun_admin", saver, &fakeRetriever{batch: batch})
	require.Equal(t, statuscode.AlreadyExists, statuscode.CodeOf(err))
	require.Empty(t, saver.events)
}

type fakeSaver struct {
	events   []coreeventstore.EventToAppend
	query    coreeventstore.Query
	position coreeventstore.Position
}

func (s *fakeSaver) Append(_ context.Context, request coreeventstore.AppendRequest) (coreeventstore.AppendResult, error) {
	s.events, s.query = request.Events, request.Subset
	return coreeventstore.AppendResult{Position: s.position}, nil
}

type fakeRetriever struct {
	batch coreeventstore.LatestByCriteriaResult
}

func (r *fakeRetriever) LatestByCriteria(context.Context, coreeventstore.LatestByCriteriaRequest) (coreeventstore.LatestByCriteriaResult, error) {
	return r.batch, nil
}

func emptyBatch() coreeventstore.LatestByCriteriaResult {
	return coreeventstore.LatestByCriteriaResult{
		Matches:         make([]coreeventstore.LatestCriterionMatch, 2),
		ContextPosition: coreeventstore.NotExistsPosition(),
	}
}

func readEvent(t *testing.T, eventType string, data any, commit, prepare int64) coreeventstore.ReadEvent {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	return coreeventstore.ReadEvent{
		EventType: eventType,
		Data:      string(raw),
		Position:  coreeventstore.Position{CommitPosition: commit, PreparePosition: prepare},
	}
}
