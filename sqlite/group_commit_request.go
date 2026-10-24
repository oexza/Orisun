package sqlite

import (
	"context"

	eventstore "github.com/OrisunLabs/Orisun/orisun"
)

// preparedSQLiteSave owns parsed event documents and compiled checks. The caller's
// context is retained solely to preserve cancellation before each request's
// turn; it never controls the shared transaction's lifetime.
type preparedSQLiteSave struct {
	ctx             context.Context
	inserts         []preparedEventDocument
	consistencyJSON string
	preparedChecks  []preparedConsistencyCheck
}

type sqliteSaveResult struct {
	transactionID string
	globalID      int64
	err           error
}

func prepareSQLiteSave(ctx context.Context, inserts eventstore.PreparedEventBatch, checks []eventstore.ConsistencyCheck) (preparedSQLiteSave, error) {
	data, err := eventstore.MarshalConsistency(checks)
	if err != nil {
		return preparedSQLiteSave{}, err
	}
	return preparedSQLiteSave{ctx: ctx, inserts: prepareEventDocuments(inserts), consistencyJSON: string(data), preparedChecks: prepareConsistencyChecks(checks)}, nil
}
