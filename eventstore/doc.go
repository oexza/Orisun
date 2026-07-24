// Package eventstore defines Orisun's transport-neutral event-store value
// model.
//
// Types in this package must remain independent of protobuf, gRPC, storage
// drivers, and runtime implementations. Transports and backends adapt these
// values at their respective boundaries.
package eventstore
