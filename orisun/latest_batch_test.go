package orisun

import (
	"testing"
	"time"
)

func TestLatestByCriteriaResponsePreservesDomainValues(t *testing.T) {
	criteria := []*Criterion{
		{Tags: []*Tag{{Key: "account_id", Value: "a-1"}, {Key: "currency", Value: "USD"}}},
		{Tags: []*Tag{{Key: "account_id", Value: "missing"}}},
	}
	query := latestQueryFromRequest(&GetLatestByCriteriaRequest{Boundary: "ledger", Criteria: criteria})
	if query.Boundary != "ledger" || len(query.Criteria) != 2 || len(query.Criteria[0].Tags) != 2 {
		t.Fatalf("unexpected packed query: %+v", query)
	}
	if query.Criteria[0].Tags[1] != (ReadTag{Key: "currency", Value: "USD"}) {
		t.Fatalf("unexpected packed tag: %+v", query.Criteria[0].Tags[1])
	}

	created := time.Unix(1_700_000_000, 123).UTC()
	resp := latestBatchResponse(LatestByCriteriaBatch{
		Matches: []LatestCriterionMatch{
			{Found: true, Event: ReadEvent{
				EventId: "event-1", EventType: "Credited", Data: `{}`, Metadata: `{}`,
				CommitPosition: 7, PreparePosition: 8, DateCreated: created,
			}},
			{},
		},
		ContextCommitPosition:  7,
		ContextPreparePosition: 8,
	}, criteria)

	if len(resp.Results) != 2 || resp.Results[0].Criterion != criteria[0] || resp.Results[1].Event != nil {
		t.Fatalf("unexpected latest response: %+v", resp)
	}
	if resp.Results[0].Event.EventId != "event-1" || resp.Results[0].Event.Position.PreparePosition != 8 {
		t.Fatalf("unexpected event: %+v", resp.Results[0].Event)
	}
	if got := resp.Results[0].Event.DateCreated; !got.Equal(created) {
		t.Fatalf("unexpected timestamp: %v", got)
	}
	if resp.ContextPosition.CommitPosition != 7 || resp.ContextPosition.PreparePosition != 8 {
		t.Fatalf("unexpected context: %+v", resp.ContextPosition)
	}
}
