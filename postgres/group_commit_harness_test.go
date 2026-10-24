package postgres

import (
	"context"
	eventstore "github.com/OrisunLabs/Orisun/orisun"
)

// Mutable fixtures deliberately exercise invalid inputs and sequential replay.
// Production queues contain only preparedPostgresSave values.
type postgresTestRequest struct {
	ctx         context.Context
	events      eventstore.PreparedEventBatch
	consistency []eventstore.ConsistencyCheck
}
type postgresTestOutcome struct {
	req           *postgresTestRequest
	transactionID string
	globalID      int64
	err           error
}

func (s *PostgresSaveEvents) executeTestBatch(ctx context.Context, boundary string, inputs []*postgresTestRequest) ([]postgresTestOutcome, error) {
	outcomes := make([]postgresTestOutcome, len(inputs))
	var prepared []preparedPostgresSave
	var indexes []int
	for i, input := range inputs {
		outcomes[i].req = input
		request, err := s.prepareSave(input.events, input.consistency)
		if err != nil {
			outcomes[i].err = err
			continue
		}
		prepared = append(prepared, request)
		indexes = append(indexes, i)
	}
	results, err := s.executeBatch(ctx, boundary, prepared)
	if err != nil {
		return nil, err
	}
	for i, result := range results {
		outcome := &outcomes[indexes[i]]
		outcome.transactionID, outcome.globalID, outcome.err = result.transactionID, result.globalID, result.err
	}
	return outcomes, nil
}
