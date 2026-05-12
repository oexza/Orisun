package sqlite

import (
	"context"
	"fmt"
	"os"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/oexza/Orisun/logging"
	eventstore "github.com/oexza/Orisun/orisun"
)

type SqliteEventPublishing struct {
	pools  map[string]*BoundaryPools
	logger logging.Logger
}

func NewSqliteEventPublishing(pools map[string]*BoundaryPools, logger logging.Logger) *SqliteEventPublishing {
	return &SqliteEventPublishing{pools: pools, logger: logger}
}

func (p *SqliteEventPublishing) GetLastPublishedEventPosition(ctx context.Context, boundary string) (eventstore.Position, error) {
	pool, ok := p.pools[boundary]
	if !ok {
		return eventstore.Position{}, fmt.Errorf("unknown boundary: %s", boundary)
	}
	conn, err := pool.Read.Take(ctx)
	if err != nil {
		return eventstore.Position{}, err
	}
	defer pool.Read.Put(conn)

	var commit, prepare int64
	err = sqlitex.Execute(conn,
		"SELECT transaction_id, global_id FROM orisun_last_published_event_position WHERE boundary = ?",
		&sqlitex.ExecOptions{
			Args: []any{boundary},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				commit = stmt.ColumnInt64(0)
				prepare = stmt.ColumnInt64(1)
				return nil
			},
		})
	if err != nil {
		return eventstore.Position{}, err
	}
	return eventstore.Position{CommitPosition: commit, PreparePosition: prepare}, nil
}

func (p *SqliteEventPublishing) InsertLastPublishedEvent(ctx context.Context, boundary string, transactionID, globalID int64) error {
	pool, ok := p.pools[boundary]
	if !ok {
		return fmt.Errorf("unknown boundary: %s", boundary)
	}
	conn, err := pool.Write.Take(ctx)
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	return sqlitex.Execute(conn,
		`INSERT INTO orisun_last_published_event_position (boundary, transaction_id, global_id, date_created, date_updated)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(boundary) DO UPDATE SET
		   transaction_id = excluded.transaction_id,
		   global_id = excluded.global_id,
		   date_updated = excluded.date_updated`,
		&sqlitex.ExecOptions{
			Args: []any{boundary, transactionID, globalID, now, now},
		})
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
