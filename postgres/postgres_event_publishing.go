package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/OrisunLabs/Orisun/config"
	"github.com/OrisunLabs/Orisun/logging"
	"github.com/OrisunLabs/Orisun/orisun"
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
	db       *sql.DB
	logger   logging.Logger
	registry *BoundaryRegistry
}

func (s *PostgresEventPublishing) Schema(boundary string) (string, error) {
	entry, ok := s.registry.lookup(boundary)
	if !ok {
		return "", fmt.Errorf("no schema found for Boundary %s", boundary)
	}
	return entry.mapping.Schema, nil
}
func NewPostgresEventPublishing(db *sql.DB, logger logging.Logger, boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresEventPublishing {
	return NewPostgresEventPublishingWithRegistry(db, logger, NewBoundaryRegistry(boundarySchemaMappings))
}

func NewPostgresEventPublishingWithRegistry(db *sql.DB, logger logging.Logger, registry *BoundaryRegistry) *PostgresEventPublishing {
	return &PostgresEventPublishing{
		db:       db,
		logger:   logger,
		registry: registry,
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

	entry, ok := s.registry.lookup(boundary)
	if !ok {
		return orisun.Position{}, fmt.Errorf("no schema found for Boundary %s", boundary)
	}

	var transactionID int64
	var globalID int64
	err = tx.QueryRowContext(ctx, entry.getLastPublished, boundary).Scan(&transactionID, &globalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return orisun.NotExistsPosition(), nil
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
	entry, ok := s.registry.lookup(boundaryOfInterest)
	if !ok {
		return fmt.Errorf("no schema found for Boundary %s", boundaryOfInterest)
	}

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		entry.insertLastPublished,
		boundaryOfInterest,
		transactionId,
		globalId,
		now,
		now,
	)
	return err
}
