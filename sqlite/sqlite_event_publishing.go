package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
)

// SQLiteEventPublishingTracker tracks event publishing positions
type SQLiteEventPublishingTracker struct {
	db     *sql.DB
	logger logging.Logger
}

// NewSQLiteEventPublishingTracker creates a new SQLiteEventPublishingTracker instance
func NewSQLiteEventPublishingTracker(db *sql.DB, logger logging.Logger) *SQLiteEventPublishingTracker {
	return &SQLiteEventPublishingTracker{
		db:     db,
		logger: logger,
	}
}

// GetLastPublishedEventPosition retrieves the last published position for a boundary
func (t *SQLiteEventPublishingTracker) GetLastPublishedEventPosition(ctx context.Context, boundary string) (orisun.Position, error) {
	var commitPos, preparePos int64

	err := t.db.QueryRowContext(ctx, `
		SELECT COALESCE(last_commit_position, 0), COALESCE(last_prepare_position, 0)
		FROM event_publishing_tracker
		WHERE boundary = ?
	`, boundary).Scan(&commitPos, &preparePos)

	if err == sql.ErrNoRows {
		// Return default position if not found
		t.logger.Debugf("No published position found for boundary %s, using default", boundary)
		return orisun.Position{
			CommitPosition:  0,
			PreparePosition: 0,
		}, nil
	}
	if err != nil {
		t.logger.Errorf("Failed to get last published position for boundary %s: %v", boundary, err)
		return orisun.Position{}, fmt.Errorf("failed to get last published position: %w", err)
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

	_, err := t.db.ExecContext(ctx, `
		INSERT INTO event_publishing_tracker (boundary, last_commit_position, last_prepare_position, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (boundary) DO UPDATE SET
			last_commit_position = excluded.last_commit_position,
			last_prepare_position = excluded.last_prepare_position,
			updated_at = excluded.updated_at
	`,
		boundary,
		transactionId,
		globalId,
		now,
	)

	if err != nil {
		t.logger.Errorf("Failed to update last published position for boundary %s: %v", boundary, err)
		return fmt.Errorf("failed to update last published position: %w", err)
	}

	t.logger.Debugf("Successfully updated last published position for boundary %s: (%d, %d)",
		boundary, transactionId, globalId)
	return nil
}
