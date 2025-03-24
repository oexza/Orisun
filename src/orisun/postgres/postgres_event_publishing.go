package postgres

import (
	"context"
	"database/sql"
	"fmt"
	config "orisun/src/orisun/config"
	"orisun/src/orisun/eventstore"
	logging "orisun/src/orisun/logging"
	"time"
)

type PostgresEventPublishing struct {
	db                     *sql.DB
	logger                 logging.Logger
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping
}

func (s *PostgresEventPublishing) Schema(boundary string) (string, error) {
	schema := s.boundarySchemaMappings[boundary]
	if (schema == config.BoundaryToPostgresSchemaMapping{}) {
		return "", fmt.Errorf("No schema found for Boundary " + boundary)
	}
	return schema.Schema, nil
}
func NewPostgresEventPublishing(db *sql.DB, logger logging.Logger, boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresEventPublishing {
	return &PostgresEventPublishing{
		db:                     db,
		logger:                 logger,
		boundarySchemaMappings: boundarySchemaMappings,
	}
}

func (s *PostgresEventPublishing) GetLastPublishedEventPosition(ctx context.Context, boundary string) (eventstore.Position, error) {
	conn, err := s.db.Conn(ctx)

	if err != nil {
		return eventstore.Position{}, err
	}

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{})
	defer tx.Rollback()

	if err != nil {
		return eventstore.Position{}, err
	}

	schema, err := s.Schema(boundary)
	if err != nil {
		return eventstore.Position{}, err
	}

	_, err = tx.ExecContext(ctx, fmt.Sprintf(setSearchPath, schema))
	if err != nil {
		return eventstore.Position{}, fmt.Errorf("failed to set search path: %v", err)
	}
	var transactionID uint64
	var globalID uint64
	err = tx.QueryRowContext(ctx, fmt.Sprintf(getLastPublishedEventQuery, schema), boundary).Scan(&transactionID, &globalID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Return default position (0,0) if no rows found
			return eventstore.Position{
				CommitPosition:  0,
				PreparePosition: 0,
			}, nil
		}
		return eventstore.Position{}, err
	}

	return eventstore.Position{
		CommitPosition:  transactionID,
		PreparePosition: globalID,
	}, nil
}

func (s *PostgresEventPublishing) InsertLastPublishedEvent(ctx context.Context,
	boundaryOfInterest string, transactionId uint64, globalId uint64) error {
	conn, err := s.db.Conn(ctx)

	if err != nil {
		return err
	}

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{})
	defer tx.Rollback()

	if err != nil {
		return err
	}

	schema, err := s.Schema(boundaryOfInterest)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, fmt.Sprintf(setSearchPath, schema))
	if err != nil {
		fmt.Errorf("failed to set search path: %v", err)
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx,
		fmt.Sprintf(insertLastPublishedPosition, schema),
		boundaryOfInterest,
		transactionId,
		globalId,
		now,
		now,
	)
	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}
