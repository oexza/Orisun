package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"time"
)

const insertLastPublishedPosition = `
insert into %s.%s_orisun_last_published_event_position (boundary, transaction_id, global_id, date_created, date_updated)
values ($1, $2, $3, $4, $5)
ON CONFLICT (boundary)
    do update set transaction_id = $2,
                  global_id      = $3,
                  date_updated=$5
`

const getLastPublishedEventQuery = `
select transaction_id, global_id from %s.%s_orisun_last_published_event_position where boundary = $1
`

type PostgresEventPublishing struct {
	db                     *sql.DB
	logger                 logging.Logger
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping
	getQueries             map[string]string // boundary -> pre-formatted SELECT
	insertQueries          map[string]string // boundary -> pre-formatted INSERT
}

func (s *PostgresEventPublishing) Schema(boundary string) (string, error) {
	schema := s.boundarySchemaMappings[boundary]
	if (schema == config.BoundaryToPostgresSchemaMapping{}) {
		return "", fmt.Errorf("no schema found for Boundary %s", boundary)
	}
	return schema.Schema, nil
}
func NewPostgresEventPublishing(db *sql.DB, logger logging.Logger, boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresEventPublishing {
	getQ := make(map[string]string, len(boundarySchemaMappings))
	insQ := make(map[string]string, len(boundarySchemaMappings))
	for boundary, m := range boundarySchemaMappings {
		getQ[boundary] = fmt.Sprintf(getLastPublishedEventQuery, m.Schema, boundary)
		insQ[boundary] = fmt.Sprintf(insertLastPublishedPosition, m.Schema, boundary)
	}
	return &PostgresEventPublishing{
		db:                     db,
		logger:                 logger,
		boundarySchemaMappings: boundarySchemaMappings,
		getQueries:             getQ,
		insertQueries:          insQ,
	}
}

func (s *PostgresEventPublishing) GetLastPublishedEventPosition(ctx context.Context, boundary string) (orisun.Position, error) {
	conn, err := s.db.Conn(ctx)

	if err != nil {
		return orisun.Position{}, err
	}
	defer conn.Close() // Ensure connection is always closed

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
	defer tx.Rollback()

	if err != nil {
		return orisun.Position{}, err
	}

	query, ok := s.getQueries[boundary]
	if !ok {
		return orisun.Position{}, fmt.Errorf("no schema found for Boundary %s", boundary)
	}

	var transactionID int64
	var globalID int64
	err = tx.QueryRowContext(ctx, query, boundary).Scan(&transactionID, &globalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Return default position (0,0) if no rows found
			return orisun.Position{
				CommitPosition:  0,
				PreparePosition: 0,
			}, nil
		}
		return orisun.Position{}, err
	}

	return orisun.Position{
		CommitPosition:  transactionID,
		PreparePosition: globalID,
	}, nil
}

func (s *PostgresEventPublishing) InsertLastPublishedEvent(ctx context.Context,
	boundaryOfInterest string, transactionId int64, globalId int64) error {
	conn, err := s.db.Conn(ctx)

	if err != nil {
		return err
	}
	defer conn.Close() // Ensure connection is always closed

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{})
	defer tx.Rollback()

	if err != nil {
		return err
	}

	query, ok := s.insertQueries[boundaryOfInterest]
	if !ok {
		return fmt.Errorf("no schema found for Boundary %s", boundaryOfInterest)
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx,
		query,
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
