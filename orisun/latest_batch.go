package orisun

// ReadTag is the protobuf-free criterion tag used by embedded callers and
// storage backends.
type ReadTag struct {
	Key   string
	Value string
}

// ReadCriterion is one conjunction of equality tags. Criteria in a
// LatestByCriteriaQuery are independent and retain their input order.
type ReadCriterion struct {
	Tags []ReadTag
}

// LatestByCriteriaQuery is the protobuf-free request used below the gRPC
// boundary.
type LatestByCriteriaQuery struct {
	Boundary string
	Criteria []ReadCriterion
}

// LatestCriterionMatch is positionally aligned with the corresponding input
// criterion. Event is valid only when Found is true.
type LatestCriterionMatch struct {
	Event ReadEvent
	Found bool
}

// LatestByCriteriaBatch is the packed result used by embedded callers and
// storage backends. Matches is positionally aligned with the input criteria.
type LatestByCriteriaBatch struct {
	Matches                []LatestCriterionMatch
	ContextCommitPosition  int64
	ContextPreparePosition int64
}
