package sqlite

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	common "github.com/oexza/Orisun/admin/slices/common"
	config "github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	eventstore "github.com/oexza/Orisun/orisun"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// validateIdentifier mirrors postgres/migration.go:validateBoundaryName.
// Used for boundary names and admin index names.
func validateIdentifier(name string) error {
	if len(name) == 0 || len(name) > 63 {
		return fmt.Errorf("identifier must be 1-63 characters, got %d", len(name))
	}
	if !isAlpha(name[0]) && name[0] != '_' {
		return fmt.Errorf("identifier must start with a letter or underscore, got %q", name[0])
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !isAlpha(c) && !isDigit(c) && c != '_' {
			return fmt.Errorf("identifier may only contain letters, digits, underscores, invalid char %q at %d", c, i)
		}
	}
	return nil
}

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// jsonPathLiteral escapes a JSON object key for use inside a SQLite json_extract
// path argument like `'$."key"'`. Both " (JSON path) and ' (SQL literal) are escaped.
func jsonPathLiteral(key string) string {
	k := strings.ReplaceAll(key, `"`, `""`)
	k = strings.ReplaceAll(k, `'`, `''`)
	return `'$."` + k + `"'`
}

// buildCriteriaSQL ports the PL/pgSQL criteria builder (db_scripts_1.sql:165-180) to SQLite.
// Each inner map ANDs key=value pairs; criteria OR across the outer slice.
// Returns "1" (always-true) for empty input so callers can append to a WHERE.
//
// Values are inlined as escaped SQL literals (PG parity: format('%L') in PL/pgSQL),
// not bound parameters: SQLite's theorem prover only matches partial-index predicates
// against constant terms, so `expr = ?` would make every conditioned index unusable.
// Callers must execute the resulting SQL transiently — literal inlining gives the
// statements unbounded cardinality, which would bloat the per-conn prepared-stmt cache.
func buildCriteriaSQL(criteria []map[string]any) (string, error) {
	return buildCriteriaSQLWithTypes(criteria, nil)
}

func buildCriteriaSQLForBoundary(criteria []map[string]any, registry *sqliteIndexRegistry, boundary string) (string, error) {
	return buildCriteriaSQLWithTypes(criteria, func(key string) (string, bool, bool) {
		return registry.fieldTypeInfo(boundary, key)
	})
}

func buildCriteriaSQLWithTypes(criteria []map[string]any, fieldType func(key string) (valueType string, declaredField, known bool)) (string, error) {
	if len(criteria) == 0 {
		return "1", nil
	}
	orParts := make([]string, 0, len(criteria))
	for _, c := range criteria {
		keys := make([]string, 0, len(c))
		for k := range c {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		andParts := make([]string, 0, len(keys))
		for _, k := range keys {
			valueType := "text"
			declaredField := false
			if fieldType != nil {
				var rawValueType string
				rawValueType, declaredField, _ = fieldType(k)
				valueType = normalizeIndexValueType(rawValueType)
			}
			predicate, err := renderCriterionPredicate(k, c[k], valueType, declaredField)
			if err != nil {
				return "", err
			}
			andParts = append(andParts, predicate)
		}
		if len(andParts) > 0 {
			orParts = append(orParts, "("+strings.Join(andParts, " AND ")+")")
		}
	}
	if len(orParts) == 0 {
		return "1", nil
	}
	return strings.Join(orParts, " OR "), nil
}

// renderCriterionPredicate renders one `key = value` criterion with the value inlined
// as a literal. The expression shapes must stay byte-identical to the ones emitted by
// CreateBoundaryIndex (fields) and buildIndexConditionPredicate (conditions) — the
// prover matches expression trees, so a shape drift silently disables index use.
//
// Shape tiers for text-typed keys:
//   - declaredField: raw json_extract — matches the index field expression.
//   - condition-only or unknown key: CASE scalar-text — matches condition predicates
//     when present and preserves string-rendered criteria semantics otherwise.
func renderCriterionPredicate(key string, value any, valueType string, declaredField bool) (string, error) {
	base := fmt.Sprintf("json_extract(data, %s)", jsonPathLiteral(key))
	switch valueType {
	case "numeric":
		if f, ok := criterionFiniteFloat(value); ok {
			return "CAST(" + base + " AS REAL) = " + strconv.FormatFloat(f, 'g', -1, 64), nil
		}
		// Non-numeric value against a numeric field: keep the old CAST semantics
		// (CAST('abc' AS REAL) = 0.0) rather than erroring on a no-match query.
		lit, err := sqlValueLiteral(value)
		if err != nil {
			return "", fmt.Errorf("criteria key %q: %w", key, err)
		}
		return "CAST(" + base + " AS REAL) = CAST(" + lit + " AS REAL)", nil
	case "boolean":
		if b, ok := sqliteBooleanArg(value).(int64); ok {
			return "CAST(" + base + " AS INTEGER) = " + strconv.FormatInt(b, 10), nil
		}
		lit, err := sqlValueLiteral(value)
		if err != nil {
			return "", fmt.Errorf("criteria key %q: %w", key, err)
		}
		return "CAST(" + base + " AS INTEGER) = " + lit, nil
	default: // "text", "timestamptz"
		lit, err := sqlValueLiteral(value)
		if err != nil {
			return "", fmt.Errorf("criteria key %q: %w", key, err)
		}
		if declaredField {
			return base + " = " + lit, nil
		}
		return sqliteJSONScalarTextExpr(key) + " = " + lit, nil
	}
}

// sqliteJSONScalarTextExpr mirrors PG's `data->>'key'` text rendering: booleans
// become 'true'/'false', everything else is CAST to TEXT. JSON null falls into
// the ELSE branch where json_extract yields SQL NULL — so, like PG's ->>, a
// stored JSON null never matches any criteria value.
func sqliteJSONScalarTextExpr(key string) string {
	path := jsonPathLiteral(key)
	base := fmt.Sprintf("json_extract(data, %s)", path)
	return fmt.Sprintf(
		"CASE json_type(data, %s) WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE CAST(%s AS TEXT) END",
		path, base,
	)
}

// criterionFiniteFloat parses a criterion value as a finite float. Inf/NaN are
// rejected: ParseFloat accepts "inf"/"nan" but FormatFloat would render them as
// SQL-invalid tokens like "+Inf".
func criterionFiniteFloat(v any) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		f = parsed
	case float64:
		f = n
	case float32:
		f = float64(n)
	case int:
		f = float64(n)
	case int32:
		f = float64(n)
	case int64:
		f = float64(n)
	default:
		return 0, false
	}
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// sqlValueLiteral renders a criterion value as an escaped SQL string literal.
// NUL bytes are rejected: SQLite's parser treats NUL as end-of-input, so an
// embedded NUL would truncate the statement instead of matching data.
func sqlValueLiteral(v any) (string, error) {
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case []byte:
		s = string(t)
	default:
		s = fmt.Sprint(t)
	}
	if strings.ContainsRune(s, 0) {
		return "", errors.New("criteria value contains NUL byte")
	}
	return sqlEscapeLiteral(s), nil
}

func sqliteBooleanArg(v any) any {
	switch value := v.(type) {
	case bool:
		if value {
			return int64(1)
		}
		return int64(0)
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1":
			return int64(1)
		case "false", "0":
			return int64(0)
		}
	}
	return v
}

func criteriaAsList(query *eventstore.Query) []map[string]any {
	out := make([]map[string]any, 0, len(query.Criteria))
	for _, c := range query.Criteria {
		anded := make(map[string]any, len(c.Tags))
		for _, t := range c.Tags {
			anded[t.Key] = t.Value
		}
		out = append(out, anded)
	}
	return out
}

// ---------------------------------------------------------------------------
// SqliteSaveEvents
// ---------------------------------------------------------------------------

type SqliteSaveEvents struct {
	pools    map[string]*BoundaryPools
	logger   logging.Logger
	notifier *SqliteEventNotifier

	// Group-commit tuning; package-local, initialized from the
	// sqliteGroupCommit* constants. Fields (not constants) so package tests
	// and benchmarks can vary them.
	gcMaxBatchRequests int
	gcMaxBatchEvents   int
	gcMaxDelay         time.Duration
	gcFlushTimeout     time.Duration

	queues    map[string]chan *sqliteSaveRequest
	closed    chan struct{}
	closeOnce sync.Once
	enqueueMu sync.RWMutex
	workerWG  sync.WaitGroup

	// Flush accounting, exposed for tests and debug logging.
	gcMultiFlushes  atomic.Int64
	gcSingleFlushes atomic.Int64
	// gcTestFlushHook runs inside a flush's recover scope after the write
	// connection is taken, with the live batch size. Test-only; nil in production.
	gcTestFlushHook func(batchSize int)
}

const (
	sqliteInsertParamsPerEvent = 5
	sqliteMaxInsertParams      = 999
	sqliteMaxEventsPerInsert   = sqliteMaxInsertParams / sqliteInsertParamsPerEvent
)

// NewSqliteSaveEvents constructs the saver with the package-default
// group-commit tuning. Use NewSqliteSaveEventsWithConfig to override.
func NewSqliteSaveEvents(pools map[string]*BoundaryPools, logger logging.Logger) *SqliteSaveEvents {
	// Zero config never fails normalization.
	s, _ := NewSqliteSaveEventsWithConfig(pools, logger, config.SqliteGroupCommitConfig{})
	return s
}

func NewSqliteSaveEventsWithConfig(
	pools map[string]*BoundaryPools,
	logger logging.Logger,
	gcCfg config.SqliteGroupCommitConfig,
) (*SqliteSaveEvents, error) {
	gcCfg, err := normalizeGroupCommitConfig(gcCfg)
	if err != nil {
		return nil, err
	}
	s := &SqliteSaveEvents{
		pools:  pools,
		logger: logger,

		gcMaxBatchRequests: gcCfg.MaxBatchRequests,
		gcMaxBatchEvents:   gcCfg.MaxBatchEvents,
		gcMaxDelay:         gcCfg.MaxDelay,
		gcFlushTimeout:     gcCfg.FlushTimeout,

		queues: make(map[string]chan *sqliteSaveRequest, len(pools)),
		closed: make(chan struct{}),
	}
	for boundary, pool := range pools {
		queue := make(chan *sqliteSaveRequest, gcCfg.MaxPending)
		s.queues[boundary] = queue
		s.workerWG.Add(1)
		go s.runWorker(boundary, pool, queue)
	}
	return s, nil
}

// close stops accepting new saves and fails queued-but-unflushed requests.
// An in-progress flush finishes. Called from InitializeSqliteDatabase's
// shutdown goroutine before the pools close; also used by package tests.
func (s *SqliteSaveEvents) close() {
	s.closeOnce.Do(func() {
		close(s.closed)

		// Wait for enqueue calls that passed the closed check but have not yet
		// completed their channel send. After this barrier, no goroutine can
		// send to a queue, so it is safe to close the queues and let workers exit.
		s.enqueueMu.Lock()
		s.enqueueMu.Unlock()

		for _, queue := range s.queues {
			close(queue)
		}
		s.workerWG.Wait()
	})
}

func (s *SqliteSaveEvents) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *SqliteSaveEvents) SavePrepared(
	ctx context.Context,
	events eventstore.PreparedEventBatch,
	boundary string,
	expectedPosition *eventstore.Position,
	streamConsistencyCondition *eventstore.Query,
) (transactionID string, globalID int64, err error) {
	if len(events) == 0 {
		return "", 0, status.Errorf(codes.InvalidArgument, "events cannot be empty")
	}
	if _, ok := s.pools[boundary]; !ok {
		return "", 0, status.Errorf(codes.InvalidArgument, "unknown boundary: %s", boundary)
	}
	return s.enqueue(ctx, boundary, events, expectedPosition, streamConsistencyCondition)
}

// saveEventsOnConn runs the CCC check, ID allocation, and insert on an open
// transaction (or savepoint). The caller owns transaction begin/commit/rollback.
func (s *SqliteSaveEvents) saveEventsOnConn(
	conn *sqlite.Conn,
	pool *BoundaryPools,
	boundary string,
	eventsToInsert eventstore.PreparedEventBatch,
	expectedPosition *eventstore.Position,
	streamConsistencyCondition *eventstore.Query,
) (transactionID string, globalID int64, err error) {
	// CCC check
	if streamConsistencyCondition != nil && len(streamConsistencyCondition.Criteria) > 0 {
		criteria := criteriaAsList(streamConsistencyCondition)
		if len(criteria) > 0 {
			where, buildErr := buildCriteriaSQLForBoundary(criteria, pool.indexes, boundary)
			if buildErr != nil {
				return "", 0, status.Errorf(codes.InvalidArgument, "invalid consistency criteria: %v", buildErr)
			}
			checkSQL := "SELECT transaction_id, global_id FROM orisun_es_event WHERE " + where +
				" ORDER BY transaction_id DESC, global_id DESC LIMIT 1"

			// Transient: criteria literals are inlined, so the SQL text has unbounded
			// cardinality and must not enter the per-conn prepared-statement cache.
			latestTx, latestGid := int64(-1), int64(-1)
			if err = sqlitex.ExecuteTransient(conn, checkSQL, &sqlitex.ExecOptions{
				ResultFunc: func(stmt *sqlite.Stmt) error {
					latestTx = stmt.ColumnInt64(0)
					latestGid = stmt.ColumnInt64(1)
					return nil
				},
			}); err != nil {
				return "", 0, status.Errorf(codes.Internal, "ccc check: %v", err)
			}

			expectedTx, expectedGid := int64(-1), int64(-1)
			if expectedPosition != nil {
				expectedTx = expectedPosition.CommitPosition
				expectedGid = expectedPosition.PreparePosition
			}

			if latestTx != expectedTx || latestGid != expectedGid {
				return "", 0, status.Errorf(codes.AlreadyExists,
					"OptimisticConcurrencyException:StreamVersionConflict: Expected (%d, %d), Actual (%d, %d)",
					expectedTx, expectedGid, latestTx, latestGid)
			}
		}
	}

	// Allocate N global_ids atomically.
	n := int64(len(eventsToInsert))
	var firstID, lastID int64
	if err = sqlitex.Execute(conn,
		"UPDATE orisun_es_seq SET next_id = next_id + ? WHERE id = 1 RETURNING next_id - ?, next_id - 1",
		&sqlitex.ExecOptions{
			Args: []any{n, n},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				firstID = stmt.ColumnInt64(0)
				lastID = stmt.ColumnInt64(1)
				return nil
			},
		}); err != nil {
		return "", 0, status.Errorf(codes.Internal, "allocate ids: %v", err)
	}

	if err = insertEventBatch(conn, eventsToInsert, firstID, lastID); err != nil {
		return "", 0, status.Errorf(codes.Internal, "insert events: %v", err)
	}

	return strconv.FormatInt(lastID, 10), lastID, nil
}

// insertEventBatch inserts events in chunks of sqliteMaxEventsPerInsert to stay under
// SQLite's bound-parameter limit. Callers hold an open transaction, so the full batch
// commits atomically regardless of chunking.
func insertEventBatch(conn *sqlite.Conn, events eventstore.PreparedEventBatch, firstID, transactionID int64) error {
	for start := 0; start < len(events); start += sqliteMaxEventsPerInsert {
		end := min(start+sqliteMaxEventsPerInsert, len(events))
		chunk := events[start:end]

		var sb strings.Builder
		sb.Grow(64 + len(chunk)*48)
		sb.WriteString("INSERT INTO orisun_es_event (transaction_id, global_id, event_id, data, metadata) VALUES ")
		insertArgs := make([]any, 0, len(chunk)*sqliteInsertParamsPerEvent)
		for i, e := range chunk {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("(?, ?, ?, ?, ?)")
			gid := firstID + int64(start+i)
			insertArgs = append(insertArgs,
				transactionID, gid, e.EventId, e.DataJSON, e.MetadataJSON,
			)
		}

		if err := sqlitex.Execute(conn, sb.String(), &sqlitex.ExecOptions{Args: insertArgs}); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// SqliteGetEvents
// ---------------------------------------------------------------------------

type SqliteGetEvents struct {
	pools  map[string]*BoundaryPools
	logger logging.Logger
}

func NewSqliteGetEvents(pools map[string]*BoundaryPools, logger logging.Logger) *SqliteGetEvents {
	return &SqliteGetEvents{pools: pools, logger: logger}
}

func (s *SqliteGetEvents) GetBatch(ctx context.Context, req *eventstore.GetEventsRequest) (eventstore.ReadEventBatch, error) {
	pool, ok := s.pools[req.Boundary]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown boundary: %s", req.Boundary)
	}

	conn, err := pool.Read.Take(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "take read conn: %v", err)
	}
	defer pool.Read.Put(conn)

	var (
		whereParts []string
		args       []any
	)

	if req.Query != nil && len(req.Query.Criteria) > 0 {
		critList := criteriaAsList(req.Query)
		if len(critList) > 0 {
			where, buildErr := buildCriteriaSQLForBoundary(critList, pool.indexes, req.Boundary)
			if buildErr != nil {
				return nil, status.Errorf(codes.InvalidArgument, "invalid criteria: %v", buildErr)
			}
			whereParts = append(whereParts, "("+where+")")
		}
	}

	op := ">"
	dirSQL := "ASC"
	if req.Direction == eventstore.Direction_DESC {
		op = "<"
		dirSQL = "DESC"
	}

	if req.FromPosition != nil {
		whereParts = append(whereParts,
			fmt.Sprintf("(transaction_id, global_id) %s= (?, ?)", op))
		args = append(args, req.FromPosition.CommitPosition, req.FromPosition.PreparePosition)
	}

	whereSQL := "1"
	if len(whereParts) > 0 {
		whereSQL = strings.Join(whereParts, " AND ")
	}

	count := req.Count
	if count == 0 {
		count = eventstore.DefaultReadBatchSize
	}
	if count > eventstore.MaxReadBatchSize {
		count = eventstore.MaxReadBatchSize
	}

	q := fmt.Sprintf(
		"SELECT transaction_id, global_id, event_id, json_extract(data, '$.\"eventType\"') AS event_type, data, metadata, date_created "+
			"FROM orisun_es_event WHERE %s ORDER BY transaction_id %s, global_id %s LIMIT %d",
		whereSQL, dirSQL, dirSQL, count,
	)

	// Transient: criteria literals and LIMIT are inlined, so the SQL text has
	// unbounded cardinality and must not enter the per-conn prepared-stmt cache.
	batch := make(eventstore.ReadEventBatch, 0, count)
	if err := sqlitex.ExecuteTransient(conn, q, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			batch = append(batch, eventstore.ReadEvent{
				CommitPosition:  stmt.ColumnInt64(0),
				PreparePosition: stmt.ColumnInt64(1),
				EventId:         stmt.ColumnText(2),
				EventType:       stmt.ColumnText(3),
				Data:            stmt.ColumnText(4),
				Metadata:        stmt.ColumnText(5),
				DateCreated:     parseSQLiteEventTime(stmt.ColumnText(6)),
			})
			return nil
		},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "query events: %v", err)
	}

	return batch, nil
}

// GetLatestByCriteria returns the latest event per criterion plus the max
// observed position, all from ONE read snapshot: the per-criterion lookups run
// inside an explicit deferred read transaction, so under WAL every statement
// sees the same database state. Independent client reads cannot substitute —
// an event committing between them can hide below the observed max position.
func (s *SqliteGetEvents) GetLatestByCriteria(ctx context.Context, query eventstore.LatestByCriteriaQuery) (eventstore.LatestByCriteriaBatch, error) {
	pool, ok := s.pools[query.Boundary]
	if !ok {
		return eventstore.LatestByCriteriaBatch{}, status.Errorf(codes.InvalidArgument, "unknown boundary: %s", query.Boundary)
	}
	if len(query.Criteria) == 0 {
		return eventstore.LatestByCriteriaBatch{}, status.Errorf(codes.InvalidArgument, "at least one criterion is required")
	}

	conn, err := pool.Read.Take(ctx)
	if err != nil {
		return eventstore.LatestByCriteriaBatch{}, status.Errorf(codes.Internal, "take read conn: %v", err)
	}
	defer pool.Read.Put(conn)

	if err := sqlitex.ExecuteTransient(conn, "BEGIN DEFERRED", nil); err != nil {
		return eventstore.LatestByCriteriaBatch{}, status.Errorf(codes.Internal, "begin read tx: %v", err)
	}
	defer func() {
		_ = sqlitex.ExecuteTransient(conn, "COMMIT", nil)
	}()

	batch := eventstore.LatestByCriteriaBatch{
		Matches:                make([]eventstore.LatestCriterionMatch, len(query.Criteria)),
		ContextCommitPosition:  -1,
		ContextPreparePosition: -1,
	}
	found := false
	for i, criterion := range query.Criteria {
		anded := make(map[string]any, len(criterion.Tags))
		for _, tag := range criterion.Tags {
			anded[tag.Key] = tag.Value
		}
		if len(anded) == 0 {
			return eventstore.LatestByCriteriaBatch{}, status.Errorf(codes.InvalidArgument, "criterion has no tags")
		}
		where, buildErr := buildCriteriaSQLForBoundary([]map[string]any{anded}, pool.indexes, query.Boundary)
		if buildErr != nil {
			return eventstore.LatestByCriteriaBatch{}, status.Errorf(codes.InvalidArgument, "invalid criteria: %v", buildErr)
		}
		q := "SELECT transaction_id, global_id, event_id, json_extract(data, '$.\"eventType\"') AS event_type, data, metadata, date_created " +
			"FROM orisun_es_event WHERE " + where +
			" ORDER BY transaction_id DESC, global_id DESC LIMIT 1"

		// Transient: criteria literals are inlined (see buildCriteriaSQL).
		if err := sqlitex.ExecuteTransient(conn, q, &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				event := scanReadEventRow(stmt)
				batch.Matches[i] = eventstore.LatestCriterionMatch{Event: event, Found: true}
				if !found || (event.CommitPosition > batch.ContextCommitPosition ||
					(event.CommitPosition == batch.ContextCommitPosition &&
						event.PreparePosition > batch.ContextPreparePosition)) {
					batch.ContextCommitPosition = event.CommitPosition
					batch.ContextPreparePosition = event.PreparePosition
					found = true
				}
				return nil
			},
		}); err != nil {
			return eventstore.LatestByCriteriaBatch{}, status.Errorf(codes.Internal, "query latest by criteria: %v", err)
		}
	}
	return batch, nil
}

// scanReadEventRow decodes one orisun_es_event row in the canonical query order
// (transaction_id, global_id, event_id, event_type alias, data, metadata, date_created).
func scanReadEventRow(stmt *sqlite.Stmt) eventstore.ReadEvent {
	return eventstore.ReadEvent{
		EventId:         stmt.ColumnText(2),
		EventType:       stmt.ColumnText(3),
		Data:            stmt.ColumnText(4),
		Metadata:        stmt.ColumnText(5),
		CommitPosition:  stmt.ColumnInt64(0),
		PreparePosition: stmt.ColumnInt64(1),
		DateCreated:     parseSQLiteEventTime(stmt.ColumnText(6)),
	}
}

func parseSQLiteEventTime(created string) time.Time {
	t, parseErr := time.Parse(time.RFC3339Nano, created)
	if parseErr != nil {
		if t2, e2 := time.Parse("2006-01-02T15:04:05.000Z", created); e2 == nil {
			t = t2
		} else {
			t = time.Now().UTC()
		}
	}
	return t
}

// ---------------------------------------------------------------------------
// SqliteAdminDB
// ---------------------------------------------------------------------------

type SqliteAdminDB struct {
	pools         map[string]*BoundaryPools
	metadataPools map[string]*BoundaryPools
	adminBoundary string
	logger        logging.Logger
	userCacheMu   sync.RWMutex
	userCache     map[string]*eventstore.User
}

func NewSqliteAdminDB(pools map[string]*BoundaryPools, adminBoundary string, logger logging.Logger) *SqliteAdminDB {
	return &SqliteAdminDB{
		pools:         pools,
		adminBoundary: adminBoundary,
		logger:        logger,
		userCache:     make(map[string]*eventstore.User),
	}
}

func NewSqliteAdminDBWithMetadata(pools map[string]*BoundaryPools, metadataPools map[string]*BoundaryPools, adminBoundary string, logger logging.Logger) *SqliteAdminDB {
	return &SqliteAdminDB{
		pools:         pools,
		metadataPools: metadataPools,
		adminBoundary: adminBoundary,
		logger:        logger,
		userCache:     make(map[string]*eventstore.User),
	}
}

func (a *SqliteAdminDB) adminPool() *BoundaryPools {
	if a.metadataPools != nil {
		if pool := a.metadataPools[a.adminBoundary]; pool != nil {
			return pool
		}
	}
	return a.pools[a.adminBoundary]
}

func (a *SqliteAdminDB) metadataPoolForBoundary(boundary string) *BoundaryPools {
	if a.metadataPools != nil {
		if pool := a.metadataPools[boundary]; pool != nil {
			return pool
		}
	}
	return a.pools[boundary]
}

func (a *SqliteAdminDB) poolForProjectorName(projectorName string) *BoundaryPools {
	const eventCountPrefix = "Event_Count_Projection__"
	if boundary, ok := strings.CutPrefix(projectorName, eventCountPrefix); ok {
		if pool := a.metadataPoolForBoundary(boundary); pool != nil {
			return pool
		}
	}
	return a.adminPool()
}

func rolesToCSV(roles []eventstore.Role) string {
	parts := make([]string, len(roles))
	for i, r := range roles {
		parts[i] = string(r)
	}
	return strings.Join(parts, ",")
}

func csvToRoles(s string) []eventstore.Role {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]eventstore.Role, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, eventstore.Role(p))
		}
	}
	return out
}

func (a *SqliteAdminDB) ListAdminUsers() ([]*eventstore.User, error) {
	pool := a.adminPool()
	conn, err := pool.Read.Take(context.Background())
	if err != nil {
		return nil, err
	}
	defer pool.Read.Put(conn)

	var users []*eventstore.User
	err = sqlitex.Execute(conn,
		"SELECT id, name, username, password_hash, roles FROM users ORDER BY id",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				u := &eventstore.User{
					Id:             stmt.ColumnText(0),
					Name:           stmt.ColumnText(1),
					Username:       stmt.ColumnText(2),
					HashedPassword: stmt.ColumnText(3),
					Roles:          csvToRoles(stmt.ColumnText(4)),
				}
				users = append(users, u)
				return nil
			},
		})
	return users, err
}

func (a *SqliteAdminDB) GetProjectorLastPosition(projectorName string) (*eventstore.Position, error) {
	pool := a.poolForProjectorName(projectorName)
	conn, err := pool.Read.Take(context.Background())
	if err != nil {
		return nil, err
	}
	defer pool.Read.Put(conn)

	var commit, prepare int64
	found := false
	err = sqlitex.Execute(conn,
		"SELECT commit_position, prepare_position FROM projector_checkpoint WHERE name = ?",
		&sqlitex.ExecOptions{
			Args: []any{projectorName},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				found = true
				commit = stmt.ColumnInt64(0)
				prepare = stmt.ColumnInt64(1)
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	if !found {
		pos := eventstore.NotExistsPosition()
		return &pos, nil
	}
	return &eventstore.Position{CommitPosition: commit, PreparePosition: prepare}, nil
}

func (a *SqliteAdminDB) UpdateProjectorPosition(name string, position *eventstore.Position) error {
	pool := a.poolForProjectorName(name)
	conn, err := pool.Write.Take(context.Background())
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return sqlitex.Execute(conn,
		`INSERT INTO projector_checkpoint (id, name, commit_position, prepare_position)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET commit_position = excluded.commit_position, prepare_position = excluded.prepare_position`,
		&sqlitex.ExecOptions{
			Args: []any{id.String(), name, position.CommitPosition, position.PreparePosition},
		})
}

func (a *SqliteAdminDB) UpsertUser(user eventstore.User) error {
	pool := a.adminPool()
	conn, err := pool.Write.Take(context.Background())
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = sqlitex.Execute(conn,
		`INSERT INTO users (id, name, username, password_hash, roles, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   username = excluded.username,
		   password_hash = excluded.password_hash,
		   roles = excluded.roles,
		   updated_at = excluded.updated_at`,
		&sqlitex.ExecOptions{
			Args: []any{user.Id, user.Name, user.Username, user.HashedPassword, rolesToCSV(user.Roles), now, now},
		})
	if err != nil {
		return err
	}
	a.userCacheMu.Lock()
	defer a.userCacheMu.Unlock()
	// Reset the whole cache: a username change would otherwise leave the old
	// username's entry serving stale credentials until restart.
	a.userCache = make(map[string]*eventstore.User)
	a.userCache[user.Username] = &user
	return nil
}

func (a *SqliteAdminDB) DeleteUser(id string) error {
	pool := a.adminPool()
	conn, err := pool.Write.Take(context.Background())
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)
	if err := sqlitex.Execute(conn, "DELETE FROM users WHERE id = ?", &sqlitex.ExecOptions{Args: []any{id}}); err != nil {
		return err
	}
	a.userCacheMu.Lock()
	defer a.userCacheMu.Unlock()
	a.userCache = make(map[string]*eventstore.User)
	return nil
}

func (a *SqliteAdminDB) GetUserByUsername(username string) (eventstore.User, error) {
	a.userCacheMu.RLock()
	if u, ok := a.userCache[username]; ok && u != nil {
		a.userCacheMu.RUnlock()
		return *u, nil
	}
	a.userCacheMu.RUnlock()

	pool := a.adminPool()
	conn, err := pool.Read.Take(context.Background())
	if err != nil {
		return eventstore.User{}, err
	}
	defer pool.Read.Put(conn)

	var u eventstore.User
	found := false
	err = sqlitex.Execute(conn,
		"SELECT id, name, username, password_hash, roles FROM users WHERE username = ?",
		&sqlitex.ExecOptions{
			Args: []any{username},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				u = eventstore.User{
					Id:             stmt.ColumnText(0),
					Name:           stmt.ColumnText(1),
					Username:       stmt.ColumnText(2),
					HashedPassword: stmt.ColumnText(3),
					Roles:          csvToRoles(stmt.ColumnText(4)),
				}
				found = true
				return nil
			},
		})
	if err != nil {
		return eventstore.User{}, err
	}
	if !found {
		return eventstore.User{}, fmt.Errorf("user not found")
	}
	a.userCacheMu.Lock()
	defer a.userCacheMu.Unlock()
	a.userCache[username] = &u
	return u, nil
}

func (a *SqliteAdminDB) GetUserById(id string) (eventstore.User, error) {
	pool := a.adminPool()
	conn, err := pool.Read.Take(context.Background())
	if err != nil {
		return eventstore.User{}, err
	}
	defer pool.Read.Put(conn)

	var u eventstore.User
	found := false
	err = sqlitex.Execute(conn,
		"SELECT id, name, username, password_hash, roles FROM users WHERE id = ?",
		&sqlitex.ExecOptions{
			Args: []any{id},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				u = eventstore.User{
					Id:             stmt.ColumnText(0),
					Name:           stmt.ColumnText(1),
					Username:       stmt.ColumnText(2),
					HashedPassword: stmt.ColumnText(3),
					Roles:          csvToRoles(stmt.ColumnText(4)),
				}
				found = true
				return nil
			},
		})
	if err != nil {
		return eventstore.User{}, err
	}
	if !found {
		return eventstore.User{}, fmt.Errorf("user not found with id: %s", id)
	}
	return u, nil
}

func (a *SqliteAdminDB) GetUsersCount() (uint32, error) {
	pool := a.adminPool()
	conn, err := pool.Read.Take(context.Background())
	if err != nil {
		return 0, err
	}
	defer pool.Read.Put(conn)

	var count uint32
	err = sqlitex.Execute(conn, "SELECT user_count FROM users_count LIMIT 1", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			count = uint32(stmt.ColumnInt64(0))
			return nil
		},
	})
	return count, err
}

const (
	userCountID = "0195c053-57e7-7a6d-8e17-a2a695f67d1f"
)

func (a *SqliteAdminDB) SaveUsersCount(count uint32) error {
	pool := a.adminPool()
	conn, err := pool.Write.Take(context.Background())
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	return sqlitex.Execute(conn,
		`INSERT INTO users_count (id, user_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET user_count = excluded.user_count, updated_at = excluded.updated_at`,
		&sqlitex.ExecOptions{
			Args: []any{userCountID, int64(count), now, now},
		})
}

func (a *SqliteAdminDB) GetEventsCount(boundary string) (int, error) {
	eventPool, ok := a.pools[boundary]
	if !ok {
		return 0, fmt.Errorf("unknown boundary: %s", boundary)
	}
	cachePool := a.metadataPoolForBoundary(boundary)
	conn, err := cachePool.Read.Take(context.Background())
	if err != nil {
		return 0, err
	}

	var countStr string
	cacheHit := false
	err = sqlitex.Execute(conn, "SELECT event_count FROM events_count WHERE boundary = ?", &sqlitex.ExecOptions{
		Args: []any{boundary},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			countStr = stmt.ColumnText(0)
			cacheHit = true
			return nil
		},
	})
	if err == nil && cacheHit {
		if n, perr := strconv.Atoi(countStr); perr == nil {
			cachePool.Read.Put(conn)
			return n, nil
		}
	}
	cachePool.Read.Put(conn)

	var total int64
	conn, err = eventPool.Read.Take(context.Background())
	if err != nil {
		return 0, err
	}
	defer eventPool.Read.Put(conn)
	err = sqlitex.Execute(conn, "SELECT COUNT(*) FROM orisun_es_event", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			total = stmt.ColumnInt64(0)
			return nil
		},
	})
	if err != nil {
		return 0, err
	}
	return int(total), nil
}

func (a *SqliteAdminDB) SaveEventCount(count int, boundary string) error {
	if _, ok := a.pools[boundary]; !ok {
		return fmt.Errorf("unknown boundary: %s", boundary)
	}
	pool := a.metadataPoolForBoundary(boundary)
	conn, err := pool.Write.Take(context.Background())
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	return sqlitex.Execute(conn,
		`INSERT INTO events_count (boundary, event_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(boundary) DO UPDATE SET event_count = excluded.event_count, updated_at = excluded.updated_at`,
		&sqlitex.ExecOptions{
			Args: []any{boundary, strconv.Itoa(count), now, now},
		})
}

// CreateBoundaryIndex builds a partial expression index over orisun_es_event.data JSON keys.
// Index name = "{name}_idx" inside the boundary's database.
func (a *SqliteAdminDB) CreateBoundaryIndex(
	ctx context.Context,
	boundary, name string,
	fields []eventstore.BoundaryIndexField,
	conditions []eventstore.BoundaryIndexCondition,
	combinator string,
) (err error) {
	pool, ok := a.pools[boundary]
	if !ok {
		return fmt.Errorf("unknown boundary: %s", boundary)
	}
	if err := validateIdentifier(name); err != nil {
		return fmt.Errorf("invalid index name %s: %w", name, err)
	}
	if len(fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}
	if combinator == "" {
		combinator = eventstore.IndexCombinatorAND
	}

	exprs := make([]string, len(fields))
	normalizedFields := make([]eventstore.BoundaryIndexField, len(fields))
	for i, f := range fields {
		valueType := normalizeIndexValueType(f.ValueType)
		normalizedFields[i] = eventstore.BoundaryIndexField{
			JsonKey:   f.JsonKey,
			ValueType: valueType,
		}
		path := jsonPathLiteral(f.JsonKey)
		base := fmt.Sprintf("json_extract(data, %s)", path)
		switch valueType {
		case "numeric":
			exprs[i] = "CAST(" + base + " AS REAL)"
		case "boolean":
			exprs[i] = "CAST(" + base + " AS INTEGER)"
		case "timestamptz":
			exprs[i] = base // ISO8601 strings sort lexicographically
		default:
			exprs[i] = base
		}
	}

	var whereClause string
	if len(conditions) > 0 {
		validOps := map[string]bool{"=": true, ">": true, "<": true, ">=": true, "<=": true}
		validCombinators := map[string]bool{eventstore.IndexCombinatorAND: true, eventstore.IndexCombinatorOR: true}
		if !validCombinators[combinator] {
			return fmt.Errorf("invalid combinator %q: must be AND or OR", combinator)
		}
		typeByKey := make(map[string]string, len(normalizedFields))
		for _, f := range normalizedFields {
			typeByKey[f.JsonKey] = f.ValueType
		}
		predicates := make([]string, len(conditions))
		for i, c := range conditions {
			if !validOps[c.Operator] {
				return fmt.Errorf("invalid operator %q", c.Operator)
			}
			predicate, err := buildIndexConditionPredicate(c, typeByKey, pool.indexes, boundary)
			if err != nil {
				return err
			}
			predicates[i] = predicate
		}
		whereClause = " WHERE " + strings.Join(predicates, " "+combinator+" ")
	}

	conn, err := pool.Write.Take(ctx)
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	endFn, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return err
	}
	defer endFn(&err)

	ddl := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON orisun_es_event (%s)%s",
		quoteIdent(name+"_idx"), strings.Join(exprs, ", "), whereClause)
	if err = sqlitex.Execute(conn, ddl, nil); err != nil {
		return err
	}

	fieldsJSON, err := json.Marshal(normalizedFields)
	if err != nil {
		return err
	}
	conditionsJSON, err := json.Marshal(conditions)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = sqlitex.Execute(conn,
		`INSERT INTO orisun_boundary_index_metadata (name, fields, conditions, combinator, date_created, date_updated)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   fields = excluded.fields,
		   conditions = excluded.conditions,
		   combinator = excluded.combinator,
		   date_updated = excluded.date_updated`,
		&sqlitex.ExecOptions{
			Args: []any{name, string(fieldsJSON), string(conditionsJSON), combinator, now, now},
		})
	if err != nil {
		return err
	}
	if err = loadBoundaryIndexMetadata(conn, boundary, pool.indexes); err != nil {
		return err
	}
	return nil
}

func (a *SqliteAdminDB) DropBoundaryIndex(ctx context.Context, boundary, name string) (err error) {
	pool, ok := a.pools[boundary]
	if !ok {
		return fmt.Errorf("unknown boundary: %s", boundary)
	}
	if err := validateIdentifier(name); err != nil {
		return fmt.Errorf("invalid index name %s: %w", name, err)
	}
	conn, err := pool.Write.Take(ctx)
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	endFn, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return err
	}
	defer endFn(&err)

	err = sqlitex.Execute(conn,
		fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteIdent(name+"_idx")), nil)
	if err != nil {
		return err
	}
	err = sqlitex.Execute(conn,
		"DELETE FROM orisun_boundary_index_metadata WHERE name = ?",
		&sqlitex.ExecOptions{Args: []any{name}})
	if err != nil {
		return err
	}
	if err = loadBoundaryIndexMetadata(conn, boundary, pool.indexes); err != nil {
		return err
	}
	return nil
}

// buildIndexConditionPredicate renders one partial-index condition with the comparison
// typed to the field's declared value type. json_extract returns SQLite numbers for JSON
// numbers, and in SQLite any number sorts before any text — so an untyped text literal
// against a numeric field would make the predicate always-false and the index empty.
// Field types come from this index's own field list first, then the boundary registry.
//
// Shape contract with renderCriterionPredicate: declared-field keys use the raw
// json_extract shape; everything else uses the CASE scalar-text shape, which is what
// queries emit for condition-only keys — and it keeps text conditions correct over
// numeric/boolean JSON values instead of always-false.
func buildIndexConditionPredicate(
	c eventstore.BoundaryIndexCondition,
	typeByKey map[string]string,
	registry *sqliteIndexRegistry,
	boundary string,
) (string, error) {
	valueType, declaredField := typeByKey[c.Key]
	if !declaredField {
		valueType, declaredField, _ = registry.fieldTypeInfo(boundary, c.Key)
	}
	base := fmt.Sprintf("json_extract(data, %s)", jsonPathLiteral(c.Key))
	switch normalizeIndexValueType(valueType) {
	case "numeric":
		f, ok := criterionFiniteFloat(c.Value)
		if !ok {
			return "", fmt.Errorf("condition on numeric field %q requires a finite numeric value, got %q", c.Key, c.Value)
		}
		return fmt.Sprintf("CAST(%s AS REAL) %s %s",
			base, c.Operator, strconv.FormatFloat(f, 'g', -1, 64)), nil
	case "boolean":
		b, ok := sqliteBooleanArg(c.Value).(int64)
		if !ok {
			return "", fmt.Errorf("condition on boolean field %q requires true/false, got %q", c.Key, c.Value)
		}
		return fmt.Sprintf("CAST(%s AS INTEGER) %s %d", base, c.Operator, b), nil
	default: // "text", "timestamptz"
		lit, err := sqlValueLiteral(c.Value)
		if err != nil {
			return "", fmt.Errorf("condition on field %q: %w", c.Key, err)
		}
		if declaredField {
			return fmt.Sprintf("%s %s %s", base, c.Operator, lit), nil
		}
		return fmt.Sprintf("%s %s %s", sqliteJSONScalarTextExpr(c.Key), c.Operator, lit), nil
	}
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// normalizeJSON returns valid JSON for the SQLite TEXT column.
// Mirrors PG's jsonb_typeof check: if v is already a JSON string (e.g. `{"k":"v"}`),
// preserve it as-is; otherwise marshal. Empty/nil → "{}" (matches PG's COALESCE default).
func normalizeJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return "{}", nil
		}
		if !json.Valid([]byte(s)) {
			return "", fmt.Errorf("invalid JSON")
		}
		return s, nil
	}
	if b, ok := v.([]byte); ok {
		if len(b) == 0 {
			return "{}", nil
		}
		if !json.Valid(b) {
			return "", fmt.Errorf("invalid JSON")
		}
		return string(b), nil
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func sqlEscapeLiteral(v string) string {
	return `'` + strings.ReplaceAll(v, `'`, `''`) + `'`
}

// ---------------------------------------------------------------------------
// InitializeSqliteDatabase
// ---------------------------------------------------------------------------

func withMetadataWriteTx(ctx context.Context, pool *BoundaryPools, fn func(*sqlite.Conn) error) (err error) {
	dst, err := pool.Write.Take(ctx)
	if err != nil {
		return err
	}
	defer pool.Write.Put(dst)

	endFn, err := sqlitex.ImmediateTransaction(dst)
	if err != nil {
		return err
	}
	defer endFn(&err)
	return fn(dst)
}

func metadataBoundaryForProjector(projectorName, adminBoundary string, metadataPools map[string]*BoundaryPools) string {
	const eventCountPrefix = "Event_Count_Projection__"
	if boundary, ok := strings.CutPrefix(projectorName, eventCountPrefix); ok {
		if metadataPools[boundary] != nil {
			return boundary
		}
	}
	return adminBoundary
}

func migrateLegacyMetadata(ctx context.Context, metadataPools map[string]*BoundaryPools, pools map[string]*BoundaryPools, adminBoundary string, sqliteCfg config.SqliteConfig) error {
	for boundary, pool := range pools {
		dstPool := metadataPools[boundary]
		if dstPool == nil {
			return fmt.Errorf("missing metadata pool for boundary %s", boundary)
		}
		src, err := pool.Read.Take(ctx)
		if err != nil {
			return err
		}
		err = withMetadataWriteTx(ctx, dstPool, func(dst *sqlite.Conn) error {
			if err := copyLegacyPublisherCheckpoint(src, dst, boundary); err != nil {
				return err
			}
			return copyLegacyEventCount(src, dst, boundary)
		})
		pool.Read.Put(src)
		if err != nil {
			return err
		}
	}

	adminPool := pools[adminBoundary]
	adminMetadataPool := metadataPools[adminBoundary]
	if adminPool != nil && adminMetadataPool != nil {
		src, err := adminPool.Read.Take(ctx)
		if err != nil {
			return err
		}
		err = copyLegacyProjectorCheckpoints(ctx, src, metadataPools, adminBoundary)
		if err == nil {
			err = withMetadataWriteTx(ctx, adminMetadataPool, func(dst *sqlite.Conn) error {
				if err := copyLegacyUsers(src, dst); err != nil {
					return err
				}
				return copyLegacyUsersCount(src, dst)
			})
		}
		adminPool.Read.Put(src)
		if err != nil {
			return err
		}
	}

	return migrateLegacySharedMetadata(ctx, sqliteCfg, metadataPools, adminBoundary)
}

func migrateLegacySharedMetadata(ctx context.Context, sqliteCfg config.SqliteConfig, metadataPools map[string]*BoundaryPools, adminBoundary string) error {
	poolCfg, err := normalizeSqlitePoolConfig(sqliteCfg)
	if err != nil {
		return err
	}
	legacyPath := filepath.Join(poolCfg.dir, legacySharedSqliteMetadataDBName+".db")
	if _, err := os.Stat(legacyPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	legacyPool, err := openSQLitePools(ctx, poolCfg, legacyPath, legacySharedSqliteMetadataDBName, legacySharedSqliteMetadataDBName, applyMetadataMigrations, false)
	if err != nil {
		return err
	}
	defer legacyPool.Close()

	src, err := legacyPool.Read.Take(ctx)
	if err != nil {
		return err
	}
	defer legacyPool.Read.Put(src)

	if err := copySharedPublisherCheckpoints(ctx, src, metadataPools); err != nil {
		return err
	}
	if err := copySharedEventCounts(ctx, src, metadataPools); err != nil {
		return err
	}
	if err := copyLegacyProjectorCheckpoints(ctx, src, metadataPools, adminBoundary); err != nil {
		return err
	}
	adminMetadataPool := metadataPools[adminBoundary]
	if adminMetadataPool == nil {
		return nil
	}
	return withMetadataWriteTx(ctx, adminMetadataPool, func(dst *sqlite.Conn) error {
		if err := copyLegacyUsers(src, dst); err != nil {
			return err
		}
		return copyLegacyUsersCount(src, dst)
	})
}

func copyLegacyPublisherCheckpoint(src, dst *sqlite.Conn, boundary string) error {
	exists, err := tableExists(src, "orisun_last_published_event_position")
	if err != nil || !exists {
		return err
	}
	return sqlitex.Execute(src,
		"SELECT boundary, transaction_id, global_id, date_created, date_updated FROM orisun_last_published_event_position",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				rowBoundary := stmt.ColumnText(0)
				if rowBoundary == "" {
					rowBoundary = boundary
				}
				return sqlitex.Execute(dst,
					`INSERT INTO orisun_last_published_event_position (boundary, transaction_id, global_id, date_created, date_updated)
					 VALUES (?, ?, ?, ?, ?)
					 ON CONFLICT(boundary) DO NOTHING`,
					&sqlitex.ExecOptions{Args: []any{
						rowBoundary,
						stmt.ColumnInt64(1),
						stmt.ColumnInt64(2),
						stmt.ColumnText(3),
						stmt.ColumnText(4),
					}})
			},
		})
}

func copyLegacyEventCount(src, dst *sqlite.Conn, boundary string) error {
	exists, err := tableExists(src, "events_count")
	if err != nil || !exists {
		return err
	}
	return sqlitex.Execute(src,
		"SELECT event_count, created_at, updated_at FROM events_count LIMIT 1",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				return sqlitex.Execute(dst,
					`INSERT INTO events_count (boundary, event_count, created_at, updated_at)
					 VALUES (?, ?, ?, ?)
					 ON CONFLICT(boundary) DO NOTHING`,
					&sqlitex.ExecOptions{Args: []any{
						boundary,
						stmt.ColumnText(0),
						stmt.ColumnText(1),
						stmt.ColumnText(2),
					}})
			},
		})
}

func copySharedPublisherCheckpoints(ctx context.Context, src *sqlite.Conn, metadataPools map[string]*BoundaryPools) error {
	exists, err := tableExists(src, "orisun_last_published_event_position")
	if err != nil || !exists {
		return err
	}
	return sqlitex.Execute(src,
		"SELECT boundary, transaction_id, global_id, date_created, date_updated FROM orisun_last_published_event_position",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				boundary := stmt.ColumnText(0)
				dstPool := metadataPools[boundary]
				if dstPool == nil {
					return nil
				}
				return withMetadataWriteTx(ctx, dstPool, func(dst *sqlite.Conn) error {
					return sqlitex.Execute(dst,
						`INSERT INTO orisun_last_published_event_position (boundary, transaction_id, global_id, date_created, date_updated)
						 VALUES (?, ?, ?, ?, ?)
						 ON CONFLICT(boundary) DO NOTHING`,
						&sqlitex.ExecOptions{Args: []any{
							boundary,
							stmt.ColumnInt64(1),
							stmt.ColumnInt64(2),
							stmt.ColumnText(3),
							stmt.ColumnText(4),
						}})
				})
			},
		})
}

func copySharedEventCounts(ctx context.Context, src *sqlite.Conn, metadataPools map[string]*BoundaryPools) error {
	exists, err := tableExists(src, "events_count")
	if err != nil || !exists {
		return err
	}
	return sqlitex.Execute(src,
		"SELECT boundary, event_count, created_at, updated_at FROM events_count",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				boundary := stmt.ColumnText(0)
				dstPool := metadataPools[boundary]
				if dstPool == nil {
					return nil
				}
				return withMetadataWriteTx(ctx, dstPool, func(dst *sqlite.Conn) error {
					return sqlitex.Execute(dst,
						`INSERT INTO events_count (boundary, event_count, created_at, updated_at)
						 VALUES (?, ?, ?, ?)
						 ON CONFLICT(boundary) DO NOTHING`,
						&sqlitex.ExecOptions{Args: []any{
							boundary,
							stmt.ColumnText(1),
							stmt.ColumnText(2),
							stmt.ColumnText(3),
						}})
				})
			},
		})
}

func copyLegacyProjectorCheckpoints(ctx context.Context, src *sqlite.Conn, metadataPools map[string]*BoundaryPools, adminBoundary string) error {
	exists, err := tableExists(src, "projector_checkpoint")
	if err != nil || !exists {
		return err
	}
	return sqlitex.Execute(src,
		"SELECT id, name, commit_position, prepare_position FROM projector_checkpoint",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				name := stmt.ColumnText(1)
				boundary := metadataBoundaryForProjector(name, adminBoundary, metadataPools)
				dstPool := metadataPools[boundary]
				if dstPool == nil {
					return nil
				}
				return withMetadataWriteTx(ctx, dstPool, func(dst *sqlite.Conn) error {
					return sqlitex.Execute(dst,
						`INSERT INTO projector_checkpoint (id, name, commit_position, prepare_position)
						 VALUES (?, ?, ?, ?)
						 ON CONFLICT(name) DO NOTHING`,
						&sqlitex.ExecOptions{Args: []any{
							stmt.ColumnText(0),
							name,
							stmt.ColumnInt64(2),
							stmt.ColumnInt64(3),
						}})
				})
			},
		})
}

func copyLegacyUsers(src, dst *sqlite.Conn) error {
	exists, err := tableExists(src, "users")
	if err != nil || !exists {
		return err
	}
	return sqlitex.Execute(src,
		"SELECT id, name, username, password_hash, roles, created_at, updated_at FROM users",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				return sqlitex.Execute(dst,
					`INSERT INTO users (id, name, username, password_hash, roles, created_at, updated_at)
					 VALUES (?, ?, ?, ?, ?, ?, ?)
					 ON CONFLICT(id) DO NOTHING`,
					&sqlitex.ExecOptions{Args: []any{
						stmt.ColumnText(0),
						stmt.ColumnText(1),
						stmt.ColumnText(2),
						stmt.ColumnText(3),
						stmt.ColumnText(4),
						stmt.ColumnText(5),
						stmt.ColumnText(6),
					}})
			},
		})
}

func copyLegacyUsersCount(src, dst *sqlite.Conn) error {
	exists, err := tableExists(src, "users_count")
	if err != nil || !exists {
		return err
	}
	return sqlitex.Execute(src,
		"SELECT id, user_count, created_at, updated_at FROM users_count",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				return sqlitex.Execute(dst,
					`INSERT INTO users_count (id, user_count, created_at, updated_at)
					 VALUES (?, ?, ?, ?)
					 ON CONFLICT(id) DO NOTHING`,
					&sqlitex.ExecOptions{Args: []any{
						stmt.ColumnText(0),
						stmt.ColumnInt64(1),
						stmt.ColumnText(2),
						stmt.ColumnText(3),
					}})
			},
		})
}

// InitializeSqliteDatabase opens per-boundary SQLite pools and constructs the
// five backend interfaces. Boundary identity is the file: {dir}/{boundary}.db.
func InitializeSqliteDatabase(
	ctx context.Context,
	sqliteCfg config.SqliteConfig,
	adminCfg config.AdminConfig,
	boundaries []string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, common.DB, eventstore.EventPublishingTracker, func(string) eventstore.EventSignal, error) {
	if sqliteCfg.Dir == "" {
		return nil, nil, nil, nil, nil, nil, errors.New("sqlite dir is empty")
	}
	if err := ensureDir(sqliteCfg.Dir); err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("create sqlite dir: %w", err)
	}

	pools := make(map[string]*BoundaryPools, len(boundaries))
	metadataPools := make(map[string]*BoundaryPools, len(boundaries))
	for _, b := range boundaries {
		if err := validateIdentifier(b); err != nil {
			closeAll(pools)
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("invalid boundary %q: %w", b, err)
		}
		bp, err := OpenBoundaryPoolsWithConfig(ctx, sqliteCfg, b, adminCfg.Boundary)
		if err != nil {
			closeAll(pools)
			return nil, nil, nil, nil, nil, nil, err
		}
		pools[b] = bp

		mp, err := OpenMetadataPoolsWithConfig(ctx, sqliteCfg, b)
		if err != nil {
			closeAll(pools)
			closeAll(metadataPools)
			return nil, nil, nil, nil, nil, nil, err
		}
		metadataPools[b] = mp
	}
	if err := migrateLegacyMetadata(ctx, metadataPools, pools, adminCfg.Boundary, sqliteCfg); err != nil {
		closeAll(pools)
		closeAll(metadataPools)
		return nil, nil, nil, nil, nil, nil, err
	}

	lockProvider, err := eventstore.NewJetStreamLockProvider(ctx, js, logger)
	if err != nil {
		closeAll(pools)
		closeAll(metadataPools)
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("init lock provider: %w", err)
	}

	notifier := NewSqliteEventNotifierWithWakeDelay(time.Second, sqliteCfg.PublisherWakeDelay)
	saver, err := NewSqliteSaveEventsWithConfig(pools, logger, sqliteCfg.GroupCommit)
	if err != nil {
		closeAll(pools)
		closeAll(metadataPools)
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("init sqlite saver: %w", err)
	}
	saver.notifier = notifier
	getter := NewSqliteGetEvents(pools, logger)
	admin := NewSqliteAdminDBWithMetadata(pools, metadataPools, adminCfg.Boundary, logger)
	publishing := NewSqliteEventPublishingWithMetadata(metadataPools, logger)

	go func() {
		<-ctx.Done()
		// Stop the group-commit workers before the pools close so no flush
		// runs against a closed pool.
		saver.close()
		closeAll(pools)
		closeAll(metadataPools)
	}()

	return saver, getter, lockProvider, admin, publishing, notifier.Signal, nil
}

func closeAll(pools map[string]*BoundaryPools) {
	for _, p := range pools {
		_ = p.Close()
	}
}
