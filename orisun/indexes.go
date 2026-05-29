package orisun

import "context"

type BoundaryIndexField struct {
	JsonKey   string
	ValueType string
}

type BoundaryIndexCondition struct {
	Key      string
	Operator string
	Value    string
}

const (
	IndexCombinatorAND = "AND"
	IndexCombinatorOR  = "OR"
)

type BoundaryIndexManager interface {
	CreateBoundaryIndex(ctx context.Context, boundary, name string, fields []BoundaryIndexField, conditions []BoundaryIndexCondition, combinator string) error
	DropBoundaryIndex(ctx context.Context, boundary, name string) error
}
