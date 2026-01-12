package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// SQLiteEventPublishingTracker tracks event publishing positions
type SQLiteEventPublishingTracker struct {
	conn   *sqlite.Conn
	logger logging.Logger
}

// NewSQLiteEventPublishingTracker creates a new SQLiteEventPublishingTracker instance
func NewSQLiteEventPublishingTracker(conn *sqlite.Conn, logger logging.Logger) *SQLiteEventPublishingTracker {
	return &SQLiteEventPublishingTracker{
		conn:   conn,
		logger: logger,
	}
}

// GetLastPublishedEventPosition retrieves the last published position for a boundary
func (t *SQLiteEventPublishingTracker) GetLastPublishedEventPosition(ctx context.Context, boundary string) (orisun.Position, error) {
	var commitPos, preparePos int64
	var found bool

	err := sqlitex.ExecuteTransient(t.conn, `
		SELECT COALESCE(last_commit_position, 0), COALESCE(last_prepare_position, 0)
		FROM event_publishing_tracker
		WHERE boundary = ?
	`, &sqlitex.ExecOptions{
		Args: []interface{}{boundary},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			hasRow, err := stmt.Step()
			if err != nil {
				return err
			}
			if hasRow {
				found = true
				commitPos = stmt.ColumnInt64(0)
				preparePos = stmt.ColumnInt64(1)
			}
			return nil
		},
	})

	if err != nil {
		t.logger.Errorf("Failed to get last published position for boundary %s: %v", boundary, err)
		return orisun.Position{}, fmt.Errorf("failed to get last published position: %w", err)
	}

	if !found {
		// Return default position if not found
		t.logger.Debugf("No published position found for boundary %s, using default", boundary)
		return orisun.Position{
			CommitPosition:  0,
			PreparePosition: 0,
		}, nil
	}

	return orisun.Position{
		CommitPosition:  commitPos,
		PreparePosition: preparePos,
	}, nil
}

// InsertLastPublishedEvent updates the last published position for a boundary
func (t *SQLiteEventPublishingTracker) InsertLastPublishedEvent(
	ctx context.Context,
	boundary string,
	transactionId int64,
	globalId int64,
) error {
	now := time.Now().Unix()

	err := sqlitex.ExecuteTransient(t.conn, `
		INSERT INTO event_publishing_tracker (boundary, last_commit_position, last_prepare_position, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (boundary) DO UPDATE SET
			last_commit_position = excluded.last_commit_position,
			last_prepare_position = excluded.last_prepare_position,
			updated_at = excluded.updated_at
	`, &sqlitex.ExecOptions{
		Args: []interface{}{
			boundary,
			transactionId,
			globalId,
			now,
		},
	})

	if err != nil {
		t.logger.Errorf("Failed to update last published position for boundary %s: %v", boundary, err)
		return fmt.Errorf("failed to update last published position: %w", err)
	}

	t.logger.Debugf("Successfully updated last published position for boundary %s: (%d, %d)",
		boundary, transactionId, globalId)
	return nil
}
