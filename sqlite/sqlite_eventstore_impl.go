package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/knaka/go-sqlite3-fts5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SQLiteSaveEvents handles saving events to SQLite
type SQLiteSaveEvents struct {
	db     *sql.DB
	logger logging.Logger
}

// NewSQLiteSaveEvents creates a new SQLiteSaveEvents instance
func NewSQLiteSaveEvents(db *sql.DB, logger logging.Logger) *SQLiteSaveEvents {
	return &SQLiteSaveEvents{
		db:     db,
		logger: logger,
	}
}

// Save saves events to SQLite with optimistic concurrency control
func (s *SQLiteSaveEvents) Save(
	ctx context.Context,
	events []orisun.EventWithMapTags,
	boundary string,
	expectedPosition *orisun.Position,
	streamConsistencyCondition *orisun.Query,
) (transactionID string, globalID int64, err error) {
	s.logger.Debugf("SQLite: Saving %d events for boundary: %s", len(events), boundary)

	if len(events) == 0 {
		return "", 0, status.Errorf(codes.InvalidArgument, "events cannot be empty")
	}

	// Start transaction with immediate lock (SQLite's replacement for advisory locks)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
		ReadOnly:  false,
	})
	if err != nil {
		s.logger.Errorf("Failed to begin transaction: %v", err)
		return "", 0, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Generate transaction ID (using nanoseconds since epoch)
	currentTxID := time.Now().UnixNano()

	// Check optimistic concurrency if criteria provided
	if streamConsistencyCondition != nil && len(streamConsistencyCondition.Criteria) > 0 {
		latestTxID, latestGID, err := s.getLatestPositionForCriteria(tx, streamConsistencyCondition.Criteria)
		if err != nil {
			s.logger.Errorf("Failed to check latest position: %v", err)
			return "", 0, status.Errorf(codes.Internal, "failed to check latest position: %v", err)
		}

		// Check if expected position matches
		if expectedPosition != nil {
			expectedTxID := expectedPosition.CommitPosition
			expectedGID := expectedPosition.PreparePosition

			if latestTxID != -1 && (latestTxID != expectedTxID || latestGID != expectedGID) {
				s.logger.Errorf("OptimisticConcurrencyException: Expected (%d, %d), Actual (%d, %d)",
					expectedTxID, expectedGID, latestTxID, latestGID)
				return "", 0, status.Error(codes.AlreadyExists,
					fmt.Sprintf("OptimisticConcurrencyException: Expected (%d, %d), Actual (%d, %d)",
						expectedTxID, expectedGID, latestTxID, latestGID))
			}
		}
	}

	// Prepare statement for batch insert - build placeholders for all events
	valuePlaceholders := make([]string, len(events))
	insertArgs := make([]interface{}, 0, len(events)*5)

	for i, event := range events {
		// Marshal data and metadata to JSON
		dataJSON, err := json.Marshal(event.Data)
		if err != nil {
			s.logger.Errorf("Failed to marshal event data: %v", err)
			return "", 0, status.Errorf(codes.Internal, "failed to marshal event data: %v", err)
		}

		metadataJSON, err := json.Marshal(event.Metadata)
		if err != nil {
			s.logger.Errorf("Failed to marshal event metadata: %v", err)
			return "", 0, status.Errorf(codes.Internal, "failed to marshal event metadata: %v", err)
		}

		valuePlaceholders[i] = "(?, ?, ?, ?, ?)"
		insertArgs = append(insertArgs,
			currentTxID,
			event.EventId,
			event.EventType,
			string(dataJSON),
			string(metadataJSON),
		)
	}

	// Build the batch insert query
	query := fmt.Sprintf(`
		INSERT INTO orisun_es_event (transaction_id, event_id, event_type, data, metadata)
		VALUES %s
	`, strings.Join(valuePlaceholders, ", "))

	// Execute batch insert in a single statement
	result, err := tx.ExecContext(ctx, query, insertArgs...)
	if err != nil {
		s.logger.Errorf("Failed to insert events: %v", err)
		return "", 0, status.Errorf(codes.Internal, "failed to insert events: %v", err)
	}

	// Get the last insert ID (global_id of the last inserted row)
	lastGlobalID, err := result.LastInsertId()
	if err != nil {
		s.logger.Errorf("Failed to get last insert ID: %v", err)
		return "", 0, status.Errorf(codes.Internal, "failed to get last insert ID: %v", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		s.logger.Errorf("Failed to commit transaction: %v", err)
		return "", 0, status.Errorf(codes.Internal, "failed to commit transaction: %v", err)
	}

	s.logger.Debugf("Successfully saved events: TransactionID=%d, GlobalID=%d", currentTxID, lastGlobalID)
	return fmt.Sprintf("%d", currentTxID), lastGlobalID, nil
}

// getLatestPositionForCriteria finds the latest event matching the criteria using FTS5
func (s *SQLiteSaveEvents) getLatestPositionForCriteria(tx *sql.Tx, criteria []*orisun.Criterion) (txID int64, globalID int64, err error) {
	// Build FTS query from criteria
	ftsQuery := buildFTSQuery(criteria)
	if ftsQuery == "" {
		return -1, -1, nil
	}

	// Query using FTS5 index to find latest matching event
	query := `
		SELECT e.transaction_id, e.global_id
		FROM orisun_es_event e
		INNER JOIN events_search_idx idx ON e.global_id = idx.rowid
		WHERE events_search_idx MATCH ?
		ORDER BY e.global_id DESC
		LIMIT 1
	`

	row := tx.QueryRow(query, ftsQuery)
	err = row.Scan(&txID, &globalID)
	if err == sql.ErrNoRows {
		return -1, -1, nil
	}
	if err != nil {
		return -1, -1, fmt.Errorf("failed to scan latest position: %w", err)
	}

	return txID, globalID, nil
}

// SQLiteGetEvents handles retrieving events from SQLite
type SQLiteGetEvents struct {
	db     *sql.DB
	logger logging.Logger
}

// NewSQLiteGetEvents creates a new SQLiteGetEvents instance
func NewSQLiteGetEvents(db *sql.DB, logger logging.Logger) *SQLiteGetEvents {
	return &SQLiteGetEvents{
		db:     db,
		logger: logger,
	}
}

// Get retrieves events based on the request parameters
func (s *SQLiteGetEvents) Get(ctx context.Context, req *orisun.GetEventsRequest) (*orisun.GetEventsResponse, error) {
	s.logger.Debugf("SQLite: Getting events for boundary: %s", req.Boundary)

	// Start read-only transaction
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
		ReadOnly:  true,
	})
	if err != nil {
		s.logger.Errorf("Failed to begin read transaction: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Build query
	var query strings.Builder
	var args []interface{}

	// Base query
	query.WriteString(`
		SELECT event_id, event_type, data, metadata, transaction_id, global_id, date_created
		FROM orisun_es_event
		WHERE 1=1
	`)

	// Add criteria filter if present
	if req.Query != nil && len(req.Query.Criteria) > 0 {
		ftsQuery := buildFTSQuery(req.Query.Criteria)
		if ftsQuery != "" {
			query.WriteString(`
				AND global_id IN (
					SELECT rowid FROM events_search_idx
					WHERE events_search_idx MATCH ?
				)
			`)
			args = append(args, ftsQuery)
		}
	}

	// Add position filter
	if req.FromPosition != nil {
		op := ">"
		if req.Direction == orisun.Direction_DESC {
			op = "<"
		}
		query.WriteString(fmt.Sprintf(" AND (transaction_id %s ? OR (transaction_id = ? AND global_id %s ?))", op, op))
		args = append(args, req.FromPosition.CommitPosition, req.FromPosition.CommitPosition, req.FromPosition.PreparePosition)
	}

	// Add ordering
	orderDir := "ASC"
	if req.Direction == orisun.Direction_DESC {
		orderDir = "DESC"
	}
	query.WriteString(fmt.Sprintf(" ORDER BY transaction_id %s, global_id %s", orderDir, orderDir))

	// Add limit
	query.WriteString(" LIMIT ?")
	args = append(args, req.Count)

	// Execute query
	rows, err := tx.QueryContext(ctx, query.String(), args...)
	if err != nil {
		s.logger.Errorf("Failed to execute query: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to execute query: %v", err)
	}
	defer rows.Close()

	// Parse results
	events := make([]*orisun.Event, 0, req.Count)
	for rows.Next() {
		var event orisun.Event
		var txID, globalID int64
		var dateCreated int64

		err := rows.Scan(
			&event.EventId,
			&event.EventType,
			&event.Data,
			&event.Metadata,
			&txID,
			&globalID,
			&dateCreated,
		)
		if err != nil {
			s.logger.Errorf("Failed to scan row: %v", err)
			return nil, status.Errorf(codes.Internal, "failed to scan row: %v", err)
		}

		// Set position
		event.Position = &orisun.Position{
			CommitPosition:  txID,
			PreparePosition: globalID,
		}

		// Set date created
		event.DateCreated = timestamppb.New(time.Unix(dateCreated, 0))

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		s.logger.Errorf("Error iterating rows: %v", err)
		return nil, status.Errorf(codes.Internal, "error iterating rows: %v", err)
	}

	s.logger.Debugf("Successfully retrieved %d events", len(events))
	return &orisun.GetEventsResponse{Events: events}, nil
}

// buildFTSQuery builds an FTS5 query from criteria groups
// This replicates PostgreSQL's @> containment operator behavior
func buildFTSQuery(criteria []*orisun.Criterion) string {
	if len(criteria) == 0 {
		return ""
	}

	var groupParts []string

	for _, criterion := range criteria {
		if len(criterion.Tags) == 0 {
			continue
		}

		var tagParts []string
		for _, tag := range criterion.Tags {
			// Format: "key:value" - wrapped in quotes for FTS5 phrase matching
			tagParts = append(tagParts, fmt.Sprintf("\"%s:%s\"", tag.Key, escapeFTSValue(tag.Value)))
		}

		// AND logic within a group
		if len(tagParts) > 0 {
			groupParts = append(groupParts, strings.Join(tagParts, " "))
		}
	}

	// OR logic between groups
	if len(groupParts) > 0 {
		return strings.Join(groupParts, " OR ")
	}

	return ""
}

// escapeFTSValue escapes special FTS5 characters in values
func escapeFTSValue(value string) string {
	// FTS5 special characters: " - ( )
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"-", "\\-",
		"(", "\\(",
		")", "\\)",
	)
	return replacer.Replace(value)
}

// SQLiteLockProvider implements distributed locking using SQLite
type SQLiteLockProvider struct {
	db     *sql.DB
	logger logging.Logger
}

// NewSQLiteLockProvider creates a new SQLiteLockProvider instance
func NewSQLiteLockProvider(db *sql.DB, logger logging.Logger) *SQLiteLockProvider {
	return &SQLiteLockProvider{
		db:     db,
		logger: logger,
	}
}

// Lock attempts to acquire a lock with the given name
// Implements application-level locking to replace PostgreSQL's advisory locks
func (p *SQLiteLockProvider) Lock(ctx context.Context, lockName string) error {
	p.logger.Debugf("Attempting to acquire lock: %s", lockName)

	// First check if lock exists and is held
	var existingLockedBy string
	var existingLockedAt int64
	err := p.db.QueryRowContext(ctx, "SELECT locked_by, locked_at FROM locks WHERE lock_name = ?", lockName).Scan(&existingLockedBy, &existingLockedAt)

	// If lock exists and is not expired, fail
	if err == nil && (time.Now().Unix()-existingLockedAt) < 30000 {
		p.logger.Debugf("Lock %s is already held by %s", lockName, existingLockedBy)
		return status.Errorf(codes.AlreadyExists, "lock is already held")
	}

	// Lock doesn't exist or is expired, acquire it
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
		ReadOnly:  false,
	})
	if err != nil {
		p.logger.Errorf("Failed to begin lock transaction: %v", err)
		return status.Errorf(codes.Internal, "failed to begin lock transaction: %v", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Insert or update the lock
	_, err = tx.ExecContext(ctx, `
		INSERT INTO locks (lock_name, locked_at, locked_by)
		VALUES (?, ?, ?)
		ON CONFLICT (lock_name) DO UPDATE SET
			locked_at = excluded.locked_at,
			locked_by = excluded.locked_by
	`,
		lockName,
		time.Now().Unix(),
		lockName,
	)

	if err != nil {
		p.logger.Errorf("Failed to acquire lock: %v", err)
		return status.Errorf(codes.Internal, "failed to acquire lock: %v", err)
	}

	// Commit the transaction to persist the lock
	if err := tx.Commit(); err != nil {
		p.logger.Errorf("Failed to commit lock transaction: %v", err)
		return status.Errorf(codes.Internal, "failed to commit lock transaction: %v", err)
	}

	p.logger.Debugf("Successfully acquired lock: %s", lockName)
	return nil
}

// Unlock releases a lock with the given name
func (p *SQLiteLockProvider) Unlock(ctx context.Context, lockName string) error {
	p.logger.Debugf("Releasing lock: %s", lockName)

	_, err := p.db.ExecContext(ctx, "DELETE FROM locks WHERE lock_name = ? AND locked_by = ?", lockName, lockName)
	if err != nil {
		p.logger.Errorf("Failed to release lock: %v", err)
		return status.Errorf(codes.Internal, "failed to release lock: %v", err)
	}

	p.logger.Debugf("Successfully released lock: %s", lockName)
	return nil
}

// InitializeLocksTable creates the locks table if it doesn't exist
func InitializeLocksTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS locks (
			lock_name TEXT PRIMARY KEY,
			locked_at INTEGER NOT NULL,
			locked_by TEXT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_locks_locked_at ON locks(locked_at);
	`)
	return err
}
