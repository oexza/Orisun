package sqlite

import (
	"testing"

	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/stretchr/testify/require"
)

func TestSQLitePreparedSaveOwnsInput(t *testing.T) {
	events := orisun.PreparedEventBatch{{EventId: "original", EventType: "Fact", DataJSON: `{"amount":9007199254740993}`, MetadataJSON: `{"source":"original"}`}}
	checks := gcConsistency(orisun.NotExistsPosition(), gcReadCriterion(orisun.ReadTag{Key: "amount", Operator: "gte", Value: "9007199254740993"}))
	prepared, err := prepareSQLiteSave(t.Context(), events, checks)
	require.NoError(t, err)
	originalEvent := prepared.inserts[0]
	originalQueries := append([]string(nil), prepared.preparedChecks[0].queries...)
	originalContext := prepared.consistencyJSON
	events[0] = orisun.PreparedEvent{EventId: "changed", DataJSON: `{}`}
	checks[0].Position = orisun.Position{CommitPosition: 42, PreparePosition: 42}
	checks[0].Criteria[0].Tags[0].Value = "0"
	require.Equal(t, originalEvent, prepared.inserts[0])
	require.Equal(t, originalQueries, prepared.preparedChecks[0].queries)
	require.Equal(t, orisun.NotExistsPosition(), prepared.preparedChecks[0].position)
	require.Equal(t, originalContext, prepared.consistencyJSON)
}
