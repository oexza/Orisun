package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

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
func buildCriteriaSQL(criteria []map[string]any) (string, []any) {
	args := make([]any, 0)
	if len(criteria) == 0 {
		return "1", args
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
			andParts = append(andParts, fmt.Sprintf("json_extract(data, %s) = ?", jsonPathLiteral(k)))
			args = append(args, c[k])
		}
		if len(andParts) > 0 {
			orParts = append(orParts, "("+strings.Join(andParts, " AND ")+")")
		}
	}
	if len(orParts) == 0 {
		return "1", args
	}
	return strings.Join(orParts, " OR "), args
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
	pools  map[string]*BoundaryPools
	logger logging.Logger
}

func NewSqliteSaveEvents(pools map[string]*BoundaryPools, logger logging.Logger) *SqliteSaveEvents {
	return &SqliteSaveEvents{pools: pools, logger: logger}
}

func (s *SqliteSaveEvents) Save(
	ctx context.Context,
	events []eventstore.EventWithMapTags,
	boundary string,
	expectedPosition *eventstore.Position,
	streamConsistencyCondition *eventstore.Query,
) (transactionID string, globalID int64, err error) {
	if len(events) == 0 {
		return "", 0, status.Errorf(codes.InvalidArgument, "events cannot be empty")
	}
	pool, ok := s.pools[boundary]
	if !ok {
		return "", 0, status.Errorf(codes.InvalidArgument, "unknown boundary: %s", boundary)
	}
	events, err = eventstore.NormalizeEventsForSave(events)
	if err != nil {
		return "", 0, status.Errorf(codes.InvalidArgument, "invalid event data: %v", err)
	}

	conn, takeErr := pool.Write.Take(ctx)
	if takeErr != nil {
		return "", 0, status.Errorf(codes.Internal, "take write conn: %v", takeErr)
	}
	defer pool.Write.Put(conn)

	endFn, beginErr := sqlitex.ImmediateTransaction(conn)
	if beginErr != nil {
		return "", 0, status.Errorf(codes.Internal, "begin tx: %v", beginErr)
	}
	defer endFn(&err)

	// CCC check
	if streamConsistencyCondition != nil && len(streamConsistencyCondition.Criteria) > 0 {
		criteria := criteriaAsList(streamConsistencyCondition)
		if len(criteria) > 0 {
			where, whereArgs := buildCriteriaSQL(criteria)
			checkSQL := "SELECT transaction_id, global_id FROM orisun_es_event WHERE " + where +
				" ORDER BY transaction_id DESC, global_id DESC LIMIT 1"

			latestTx, latestGid := int64(-1), int64(-1)
			if err = sqlitex.Execute(conn, checkSQL, &sqlitex.ExecOptions{
				Args: whereArgs,
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
				err = status.Errorf(codes.AlreadyExists,
					"OptimisticConcurrencyException:StreamVersionConflict: Expected (%d, %d), Actual (%d, %d)",
					expectedTx, expectedGid, latestTx, latestGid)
				return "", 0, err
			}
		}
	}

	// Allocate N global_ids atomically.
	n := int64(len(events))
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

	// Multi-row INSERT — all events share transaction_id = lastID.
	var sb strings.Builder
	sb.Grow(64 + len(events)*48)
	sb.WriteString("INSERT INTO orisun_es_event (transaction_id, global_id, event_id, event_type, data, metadata) VALUES ")
	insertArgs := make([]any, 0, len(events)*6)
	for i, e := range events {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?)")
		gid := firstID + int64(i)

		dataJSON, mErr := normalizeJSON(e.Data)
		if mErr != nil {
			err = status.Errorf(codes.Internal, "marshal event data: %v", mErr)
			return "", 0, err
		}
		metaJSON, mErr := normalizeJSON(e.Metadata)
		if mErr != nil {
			err = status.Errorf(codes.Internal, "marshal event metadata: %v", mErr)
			return "", 0, err
		}
		insertArgs = append(insertArgs,
			lastID, gid, e.EventId, e.EventType, dataJSON, metaJSON,
		)
	}

	if err = sqlitex.Execute(conn, sb.String(), &sqlitex.ExecOptions{Args: insertArgs}); err != nil {
		return "", 0, status.Errorf(codes.Internal, "insert events: %v", err)
	}

	return strconv.FormatInt(lastID, 10), lastID, nil
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

func (s *SqliteGetEvents) Get(ctx context.Context, req *eventstore.GetEventsRequest) (*eventstore.GetEventsResponse, error) {
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
			where, wargs := buildCriteriaSQL(critList)
			whereParts = append(whereParts, "("+where+")")
			args = append(args, wargs...)
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
		count = 1000
	}
	if count > 10000 {
		count = 10000
	}

	q := fmt.Sprintf(
		"SELECT transaction_id, global_id, event_id, event_type, data, metadata, date_created "+
			"FROM orisun_es_event WHERE %s ORDER BY transaction_id %s, global_id %s LIMIT %d",
		whereSQL, dirSQL, dirSQL, count,
	)

	events := make([]*eventstore.Event, 0, count)
	if err := sqlitex.Execute(conn, q, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			tx := stmt.ColumnInt64(0)
			gid := stmt.ColumnInt64(1)
			eid := stmt.ColumnText(2)
			etype := stmt.ColumnText(3)
			data := stmt.ColumnText(4)
			meta := stmt.ColumnText(5)
			created := stmt.ColumnText(6)

			t, parseErr := time.Parse(time.RFC3339Nano, created)
			if parseErr != nil {
				if t2, e2 := time.Parse("2006-01-02T15:04:05.000Z", created); e2 == nil {
					t = t2
				} else {
					t = time.Now().UTC()
				}
			}
			events = append(events, &eventstore.Event{
				EventId:   eid,
				EventType: etype,
				Data:      data,
				Metadata:  meta,
				Position: &eventstore.Position{
					CommitPosition:  tx,
					PreparePosition: gid,
				},
				DateCreated: timestamppb.New(t),
			})
			return nil
		},
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "query events: %v", err)
	}

	return &eventstore.GetEventsResponse{Events: events}, nil
}

// ---------------------------------------------------------------------------
// SqliteAdminDB
// ---------------------------------------------------------------------------

type SqliteAdminDB struct {
	pools         map[string]*BoundaryPools
	adminBoundary string
	logger        logging.Logger
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

func (a *SqliteAdminDB) adminPool() *BoundaryPools { return a.pools[a.adminBoundary] }

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
	pool := a.adminPool()
	conn, err := pool.Read.Take(context.Background())
	if err != nil {
		return nil, err
	}
	defer pool.Read.Put(conn)

	var commit, prepare int64
	err = sqlitex.Execute(conn,
		"SELECT commit_position, prepare_position FROM projector_checkpoint WHERE name = ?",
		&sqlitex.ExecOptions{
			Args: []any{projectorName},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				commit = stmt.ColumnInt64(0)
				prepare = stmt.ColumnInt64(1)
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	return &eventstore.Position{CommitPosition: commit, PreparePosition: prepare}, nil
}

func (a *SqliteAdminDB) UpdateProjectorPosition(name string, position *eventstore.Position) error {
	pool := a.adminPool()
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
	return sqlitex.Execute(conn, "DELETE FROM users WHERE id = ?", &sqlitex.ExecOptions{Args: []any{id}})
}

func (a *SqliteAdminDB) GetUserByUsername(username string) (eventstore.User, error) {
	if u, ok := a.userCache[username]; ok && u != nil {
		return *u, nil
	}
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
	userCountID  = "0195c053-57e7-7a6d-8e17-a2a695f67d1f"
	eventCountID = "0195c053-57e7-7a6d-8e17-a2a695f67d2f"
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
	pool, ok := a.pools[boundary]
	if !ok {
		return 0, fmt.Errorf("unknown boundary: %s", boundary)
	}
	conn, err := pool.Read.Take(context.Background())
	if err != nil {
		return 0, err
	}
	defer pool.Read.Put(conn)

	var countStr string
	cacheHit := false
	err = sqlitex.Execute(conn, "SELECT event_count FROM events_count LIMIT 1", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			countStr = stmt.ColumnText(0)
			cacheHit = true
			return nil
		},
	})
	if err == nil && cacheHit {
		if n, perr := strconv.Atoi(countStr); perr == nil {
			return n, nil
		}
	}

	var total int64
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
	pool, ok := a.pools[boundary]
	if !ok {
		return fmt.Errorf("unknown boundary: %s", boundary)
	}
	conn, err := pool.Write.Take(context.Background())
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	return sqlitex.Execute(conn,
		`INSERT INTO events_count (id, event_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET event_count = excluded.event_count, updated_at = excluded.updated_at`,
		&sqlitex.ExecOptions{
			Args: []any{eventCountID, strconv.Itoa(count), now, now},
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
) error {
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

	exprs := make([]string, len(fields))
	for i, f := range fields {
		path := jsonPathLiteral(f.JsonKey)
		base := fmt.Sprintf("json_extract(data, %s)", path)
		switch f.ValueType {
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
		if combinator == "" {
			combinator = eventstore.IndexCombinatorAND
		}
		if !validCombinators[combinator] {
			return fmt.Errorf("invalid combinator %q: must be AND or OR", combinator)
		}
		predicates := make([]string, len(conditions))
		for i, c := range conditions {
			if !validOps[c.Operator] {
				return fmt.Errorf("invalid operator %q", c.Operator)
			}
			predicates[i] = fmt.Sprintf("json_extract(data, %s) %s %s",
				jsonPathLiteral(c.Key), c.Operator, sqlEscapeLiteral(c.Value))
		}
		whereClause = " WHERE " + strings.Join(predicates, " "+combinator+" ")
	}

	conn, err := pool.Write.Take(ctx)
	if err != nil {
		return err
	}
	defer pool.Write.Put(conn)

	ddl := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON orisun_es_event (%s)%s",
		quoteIdent(name+"_idx"), strings.Join(exprs, ", "), whereClause)
	return sqlitex.Execute(conn, ddl, nil)
}

func (a *SqliteAdminDB) DropBoundaryIndex(ctx context.Context, boundary, name string) error {
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

	return sqlitex.Execute(conn,
		fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteIdent(name+"_idx")), nil)
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
		return s, nil
	}
	if b, ok := v.([]byte); ok {
		if len(b) == 0 {
			return "{}", nil
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

// InitializeSqliteDatabase opens per-boundary SQLite pools and constructs the
// five backend interfaces. Boundary identity is the file: {dir}/{boundary}.db.
func InitializeSqliteDatabase(
	ctx context.Context,
	sqliteCfg config.SqliteConfig,
	adminCfg config.AdminConfig,
	boundaries []string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, common.DB, eventstore.EventPublishingTracker, error) {
	if sqliteCfg.Dir == "" {
		return nil, nil, nil, nil, nil, errors.New("sqlite dir is empty")
	}
	if err := ensureDir(sqliteCfg.Dir); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("create sqlite dir: %w", err)
	}

	pools := make(map[string]*BoundaryPools, len(boundaries))
	for _, b := range boundaries {
		if err := validateIdentifier(b); err != nil {
			closeAll(pools)
			return nil, nil, nil, nil, nil, fmt.Errorf("invalid boundary %q: %w", b, err)
		}
		bp, err := OpenBoundaryPools(ctx, sqliteCfg.Dir, b, adminCfg.Boundary)
		if err != nil {
			closeAll(pools)
			return nil, nil, nil, nil, nil, err
		}
		pools[b] = bp
	}

	lockProvider, err := eventstore.NewJetStreamLockProvider(ctx, js, logger)
	if err != nil {
		closeAll(pools)
		return nil, nil, nil, nil, nil, fmt.Errorf("init lock provider: %w", err)
	}

	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)
	admin := NewSqliteAdminDB(pools, adminCfg.Boundary, logger)
	publishing := NewSqliteEventPublishing(pools, logger)

	go func() {
		<-ctx.Done()
		closeAll(pools)
	}()

	return saver, getter, lockProvider, admin, publishing, nil
}

func closeAll(pools map[string]*BoundaryPools) {
	for _, p := range pools {
		_ = p.Close()
	}
}
