package postgres

import (
	"testing"

	"github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
	"github.com/stretchr/testify/require"
)

func TestPostgresPreparedSaveOwnsInput(t *testing.T) {
	logger, err := logging.ZapLogger("error")
	require.NoError(t, err)
	saver := &PostgresSaveEvents{logger: logger}
	events := orisun.PreparedEventBatch{{EventId: "11111111-1111-4111-8111-111111111111", EventType: "Original", DataJSON: `{"amount":9007199254740993}`, MetadataJSON: `{"source":"original"}`}}
	checks := []orisun.ConsistencyCheck{{Position: orisun.NotExistsPosition(), Criteria: []orisun.ReadCriterion{{Tags: []orisun.ReadTag{{Key: "amount", Operator: "gte", Value: "9007199254740993"}}}}}}
	prepared, err := saver.prepareSave(events, checks)
	require.NoError(t, err)
	beforeEvents, beforeChecks := string(prepared.payload.Events), string(prepared.payload.Consistency)
	events[0].EventType = "Changed"
	events[0].DataJSON = `{}`
	events[0].MetadataJSON = `null`
	checks[0].Position = orisun.Position{CommitPosition: 42, PreparePosition: 42}
	checks[0].Criteria[0].Tags[0].Value = "0"
	require.Equal(t, beforeEvents, string(prepared.payload.Events))
	require.Equal(t, beforeChecks, string(prepared.payload.Consistency))
	require.Contains(t, string(prepared.payload.Events), "9007199254740993")
	require.Equal(t, 1, prepared.eventCount)
}

func TestPostgresPreparationFailureDoesNotStartWorker(t *testing.T) {
	logger, err := logging.ZapLogger("error")
	require.NoError(t, err)
	cfg, err := normalizePostgresGroupCommitConfig(config.PostgresGroupCommitConfig{})
	require.NoError(t, err)
	saver := &PostgresSaveEvents{logger: logger}
	saver.initGroupCommit(cfg)
	defer saver.close()
	for _, events := range []orisun.PreparedEventBatch{
		nil,
		{{EventId: "invalid", EventType: "Fact", DataJSON: `{}`, MetadataJSON: `{}`}},
		{{EventId: "11111111-1111-4111-8111-111111111111", EventType: "Fact", DataJSON: `{}`, MetadataJSON: `{`}},
	} {
		_, _, err := saver.enqueue(t.Context(), "unused", events, nil)
		require.Error(t, err)
		require.Zero(t, saver.gc.Pending("unused"))
	}
}
