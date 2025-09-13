package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"orisun/logging"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"

	eventstore "orisun/eventstore"

	config "orisun/config"

	globalCommon "orisun/common"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const insertEventsWithConsistency = `
SELECT * FROM insert_events_with_consistency($1::text, $2::jsonb, $3::jsonb)
`

const selectMatchingEvents = `
SELECT * FROM get_matching_events($1::text, $2, $3::BIGINT, $4::jsonb, $5::jsonb, $6, $7::INT)
`

const setSearchPath = `
set search_path to '%s'
`

type PostgresSaveEvents struct {
	db                     *sql.DB
	logger                 logging.Logger
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping
}

func NewPostgresSaveEvents(
	ctx context.Context,
	db *sql.DB,
	logger logging.Logger,
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresSaveEvents {
	return &PostgresSaveEvents{db: db, logger: logger, boundarySchemaMappings: boundarySchemaMappings}
}

func (s *PostgresSaveEvents) Schema(boundary string) (string, error) {
	schema := s.boundarySchemaMappings[boundary]
	if (schema == config.BoundaryToPostgresSchemaMapping{}) {
		return "", errors.New("No schema found for Boundary " + boundary)
	}
	return schema.Schema, nil
}

type PostgresGetEvents struct {
	db                     *sql.DB
	logger                 logging.Logger
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping
}

func (s *PostgresGetEvents) Schema(boundary string) (string, error) {
	schema := s.boundarySchemaMappings[boundary]
	if (schema == config.BoundaryToPostgresSchemaMapping{}) {
		return "", errors.New("No schema found for Boundary " + boundary)
	}
	return schema.Schema, nil
}

func NewPostgresGetEvents(db *sql.DB, logger logging.Logger,
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresGetEvents {
	return &PostgresGetEvents{db: db, logger: logger, boundarySchemaMappings: boundarySchemaMappings}
}

func (s *PostgresSaveEvents) Save(
	ctx context.Context,
	events []eventstore.EventWithMapTags,
	boundary string,
	streamName string,
	expectedVersion int64,
	streamConsistencyCondition *eventstore.Query) (transactionID string, globalID int64, newStreamPosition int64, err error) {
	s.logger.Debug("Postgres: Saving events from request: %v", events)

	if len(events) == 0 {
		return "", 0, 0, status.Errorf(codes.InvalidArgument, "events cannot be empty")
	}

	schema, err := s.Schema(boundary)
	if err != nil {
		return "", 0, -1, status.Errorf(codes.Internal, "failed to get schema: %v", err)
	}

	streamSubsetAsBytes, err := json.Marshal(getStreamSectionAsMap(streamName, expectedVersion, streamConsistencyCondition))
	if err != nil {
		return "", 0, -1, status.Errorf(codes.Internal, "failed to marshal consistency condition: %v", err)
	}
	s.logger.Debugf("streamSubsetAsJsonString: %v", string(streamSubsetAsBytes))

	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return "", 0, -1, status.Errorf(codes.Internal, "failed to marshal events: %v", err)
	}
	// s.logger.Infof("eventsJSON: %v", string(eventsJSON))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger.Errorf("Error beginning transaction: %v", err)
		return "", 0, -1, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// _, errr := tx.Exec("SET log_statement = 'all';")
	// if errr != nil {
	// 	s.logger.Errorf("Error setting log_statement: %v", err)
	// 	return "", 0, -1, status.Errorf(codes.Internal, "failed to set log_statement: %v", err)
	// }

	row := tx.QueryRowContext(
		ctx,
		insertEventsWithConsistency,
		schema,
		streamSubsetAsBytes,
		eventsJSON,
	)

	if row.Err() != nil {
		s.logger.Errorf("Error inserting events: %v", row.Err())
		return "", 0, -1, status.Errorf(codes.Internal, "failed to insert events: %v", row.Err())
	}

	// Scan the result
	err = error(nil)

	var tranID string
	var globID int64
	var newStreamVersion int64
	err = row.Scan(&newStreamVersion, &tranID, &globID)
	err = tx.Commit()

	if err != nil {
		return "", 0, -1, status.Errorf(codes.Internal, "failed to commit transaction: %v", err)
	}

	s.logger.Infof("PG save events::::: Transaction ID: %v, Global ID: %v", tranID, globID)

	if err != nil {
		if strings.Contains(err.Error(), "OptimisticConcurrencyException") {
			return "", 0, -1, status.Error(codes.AlreadyExists, err.Error())
		}
		s.logger.Errorf("Error saving events to database: %v", err)
		return "", 0, -1, status.Errorf(codes.Internal, "Error saving events to database")
	}

	return tranID, globID, newStreamVersion, nil
}

func (s *PostgresGetEvents) Get(ctx context.Context, req *eventstore.GetEventsRequest) (*eventstore.GetEventsResponse, error) {
	s.logger.Debugf("Getting events from request: %v", req)

	schema, err := s.Schema(req.Boundary)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "no schema found for boundary: %s", req.Boundary)
	}

	var fromPositionMarshaled *[]byte = nil

	if req.FromPosition != nil {
		fromPosition := map[string]int64{
			"transaction_id": req.FromPosition.CommitPosition,
			"global_id":      req.FromPosition.PreparePosition,
		}
		fromPositionJson, err := json.Marshal(fromPosition)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to marshal from position: %v", err)
		}
		fromPositionMarshaled = &fromPositionJson
	}

	var paramsJSON *[]byte = nil
	if req.Query != nil && len(req.Query.Criteria) > 0 {
		criteriaList := getCriteriaAsList(req.Query)
		if len(criteriaList) > 0 {
			globalQuery := map[string]interface{}{
				"criteria": criteriaList,
			}
			paramsBytes, err := json.Marshal(globalQuery)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to marshal params: %v", err)
			}
			paramsJSON = &paramsBytes
		}
	}

	var streamName *string = nil
	var fromStreamVersion *int64 = nil

	if req.Stream != nil {
		streamName = &req.Stream.Name
		fromStreamVersion = &req.Stream.FromVersion
	}

	// s.logger.Debugf("params: %v", paramsJSON)
	// s.logger.Debugf("direction: %v", req.Direction)
	// s.logger.Debugf("count: %v", req.Count)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// _, errr := tx.Exec("SET log_statement = 'all';")
	// if errr != nil {
	// 	return nil, status.Errorf(codes.Internal, "failed to set log_statement: %v", err)
	// }

	// Prepare the query once
	// query := fmt.Sprintf(selectMatchingEvents)
	rows, err := tx.Query(
		selectMatchingEvents,
		schema,
		streamName,
		fromStreamVersion,
		paramsJSON,
		fromPositionMarshaled,
		req.Direction.String(),
		req.Count,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to execute query: %v", err)
	}
	defer rows.Close()

	// Pre-allocate events slice with exact capacity
	events := make([]*eventstore.Event, 0, req.Count)

	columns, err := rows.Columns()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get column names: %v", err)
	}

	// Create a slice of pointers to scan into
	scanArgs := make([]any, len(columns))

	// Reuse these variables for each row to reduce allocations
	var event eventstore.Event
	// var tagsBytes []byte
	var transactionID, globalID int64
	var dateCreated time.Time

	// Create a map of pointers to hold our row data - only once
	rowData := map[string]any{
		"event_id":       &event.EventId,
		"event_type":     &event.EventType,
		"data":           &event.Data,
		"metadata":       &event.Metadata,
		"transaction_id": &transactionID,
		"global_id":      &globalID,
		"date_created":   &dateCreated,
		"stream_name":    &event.StreamId,
		"stream_version": &event.Version,
	}

	for i, col := range columns {
		if ptr, ok := rowData[col]; ok {
			scanArgs[i] = ptr
		} else {
			return nil, status.Errorf(codes.Internal, "unexpected column: %s", col)
		}
	}

	for rows.Next() {
		// Reset event for reuse
		event = eventstore.Event{}

		// Scan the row into our map
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to scan row: %v", err)
		}

		// Set the Position
		event.Position = &eventstore.Position{
			CommitPosition:  transactionID,
			PreparePosition: globalID,
		}

		// Set the DateCreated
		event.DateCreated = timestamppb.New(dateCreated)

		// Create a new event pointer for each row
		eventCopy := eventstore.Event{
			EventId:     event.EventId,
			EventType:   event.EventType,
			Data:        event.Data,
			Metadata:    event.Metadata,
			StreamId:    event.StreamId,
			Version:     event.Version,
			Position:    event.Position,
			DateCreated: event.DateCreated,
		}
		events = append(events, &eventCopy)
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "error iterating rows: %v", err)
	}

	return &eventstore.GetEventsResponse{Events: events}, nil
}

func getStreamSectionAsMap(streamName string, expectedVersion int64, consistencyCondition *eventstore.Query) map[string]interface{} {
	lastRetrievedPositions := make(map[string]interface{})
	lastRetrievedPositions["stream_name"] = streamName
	lastRetrievedPositions["expected_version"] = expectedVersion

	if conditions := consistencyCondition; conditions != nil {
		lastRetrievedPositions["criteria"] = getCriteriaAsList(consistencyCondition)
	}

	return lastRetrievedPositions
}

func getCriteriaAsList(query *eventstore.Query) []map[string]any {
	result := make([]map[string]any, 0, len(query.Criteria))
	for _, criterion := range query.Criteria {
		anded := make(map[string]any, len(criterion.Tags))
		for _, tag := range criterion.Tags {
			anded[tag.Key] = tag.Value
		}
		result = append(result, anded)
	}
	return result
}

// type PGLockProvider struct {
// 	db     *sql.DB
// 	logger logging.Logger
// }

// func NewPGLockProvider(db *sql.DB, logger logging.Logger) *PGLockProvider {
// 	return &PGLockProvider{
// 		db:     db,
// 		logger: logger,
// 	}
// }

// func (m *PGLockProvider) Lock(ctx context.Context, lockName string) (eventstore.UnlockFunc, error) {
// 	m.logger.Debug("Lock called for: %v", lockName)
// 	conn, err := m.db.Conn(ctx)

// 	if err != nil {
// 		return nil, err
// 	}

// 	tx, err := conn.BeginTx(ctx, &sql.TxOptions{})

// 	if err != nil {
// 		return nil, err
// 	}

// 	hash := sha256.Sum256([]byte(lockName))
// 	lockID := int64(binary.BigEndian.Uint64(hash[:]))

// 	var acquired bool

// 	_, err = tx.ExecContext(ctx, fmt.Sprintf(setSearchPath, lockName))
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to set search path: %v", err)
// 	}
// 	err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock($1)", int32(lockID)).Scan(&acquired)

// 	if err != nil {
// 		m.logger.Errorf("Failed to acquire lock: %v, will retry", err)
// 		return nil, err
// 	}

// 	if !acquired {
// 		m.logger.Warnf("Failed to acquire lock within timeout")
// 		return nil, errors.New("lock acquisition timed out")
// 	}

// 	unlockFunc := func() error {
// 		fmt.Printf("Unlock called for: %s", lockName)
// 		defer conn.Close()
// 		defer tx.Rollback()

// 		return nil
// 	}

// 	return unlockFunc, nil
// }

type PostgresAdminDB struct {
	db                     *sql.DB
	logger                 logging.Logger
	adminSchema            string
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping
}

func NewPostgresAdminDB(db *sql.DB, logger logging.Logger, schema string, boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresAdminDB {
	return &PostgresAdminDB{
		db:                     db,
		logger:                 logger,
		adminSchema:            schema,
		boundarySchemaMappings: boundarySchemaMappings,
	}
}

var userCache = map[string]*globalCommon.User{}

func (s *PostgresAdminDB) ListAdminUsers() ([]*globalCommon.User, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT id, name, username, password_hash, roles FROM %s.users ORDER BY id", s.adminSchema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*globalCommon.User
	for rows.Next() {
		var user globalCommon.User
		user, err = s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	return users, nil
}

func (s *PostgresAdminDB) GetProjectorLastPosition(projectorName string) (*eventstore.Position, error) {
	var commitPos, preparePos int64
	err := s.db.QueryRow(
		fmt.Sprintf("SELECT COALESCE(commit_position, 0), COALESCE(prepare_position, 0) FROM %s.projector_checkpoint where name = $1", s.adminSchema),
		projectorName,
	).Scan(&commitPos, &preparePos)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return &eventstore.Position{
		CommitPosition:  commitPos,
		PreparePosition: preparePos,
	}, nil
}

func (p *PostgresAdminDB) UpdateProjectorPosition(name string, position *eventstore.Position) error {

	id, err := uuid.NewV7()
	if err != nil {
		p.logger.Error("Error generating UUID: %v", err)
		return err
	}

	if _, err := p.db.Exec(
		fmt.Sprintf("INSERT INTO %s.projector_checkpoint (id, name, commit_position, prepare_position) VALUES ($1, $2, $3, $4) ON CONFLICT (name) DO UPDATE SET commit_position = $3, prepare_position = $4", p.adminSchema),
		id.String(),
		name,
		position.CommitPosition,
		position.PreparePosition,
	); err != nil {
		p.logger.Error("Error updating checkpoint: %v", err)
		return err
	}
	return nil
}

func (p *PostgresAdminDB) CreateNewUser(id string, username string, password_hash string, name string, roles []globalCommon.Role) error {
	roleStrings := make([]string, len(roles))
	for i, role := range roles {
		roleStrings[i] = string(role)
	}
	rolesStr := "{" + strings.Join(roleStrings, ",") + "}"

	_, err := p.db.Exec(
		fmt.Sprintf("INSERT INTO %s.users (id, name, username, password_hash, roles) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (username) DO UPDATE SET name = $2, password_hash = $4, roles = $5, updated_at = $6", p.adminSchema),
		id,
		name,
		username,
		password_hash,
		rolesStr,
		time.Now().UTC(),
	)

	if err != nil {
		p.logger.Error("Error creating user: %v", err)
		return err
	}

	userCache[username] = &globalCommon.User{
		Id:             id,
		Username:       username,
		HashedPassword: password_hash,
		Roles:          roles,
	}
	return nil
}

func (p *PostgresAdminDB) DeleteUser(id string) error {
	_, err := p.db.Exec(
		fmt.Sprintf("DELETE FROM %s.users WHERE id = $1", p.adminSchema),
		id,
	)

	if err != nil {
		p.logger.Error("Error updating checkpoint: %v", err)
		return err
	}
	return nil
}

func (s *PostgresAdminDB) scanUser(rows *sql.Rows) (globalCommon.User, error) {
	var user globalCommon.User
	var roles []string
	if err := rows.Scan(&user.Id, &user.Name, &user.Username, &user.HashedPassword, pq.Array(&roles)); err != nil {
		s.logger.Error("Failed to scan user row: %v", err)
		return globalCommon.User{}, err
	}

	for _, role := range roles {
		user.Roles = append(user.Roles, globalCommon.Role(role))
	}
	return user, nil
}

func (s *PostgresAdminDB) GetUserByUsername(username string) (globalCommon.User, error) {
	user := userCache[username]
	if user != nil {
		s.logger.Debug("Fetched from cache")
		return *user, nil
	}

	rows, err := s.db.Query(fmt.Sprintf("SELECT id, name, username, password_hash, roles FROM %s.users where username = $1", s.adminSchema), username)
	if err != nil {
		s.logger.Debugf("Userrrrr: %v", err)
		return globalCommon.User{}, err
	}
	defer rows.Close()

	var userResponse globalCommon.User
	if rows.Next() {
		userResponse, err = s.scanUser(rows)
		if err != nil {
			return globalCommon.User{}, err
		}
	}

	if userResponse.Id != "" {
		userCache[username] = &userResponse
		return userResponse, nil
	}
	return globalCommon.User{}, fmt.Errorf("user not found")
}

func (s *PostgresAdminDB) GetUsersCount() (uint32, error) {
	rows, err := s.db.Query(fmt.Sprintf("SELECT user_count FROM %s.users_count limit 1", s.adminSchema))
	if err != nil {
		s.logger.Debugf("User count: %v", err)
		return 0, err
	}
	defer rows.Close()

	var count uint32 = 0
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			s.logger.Error("Failed to scan user count: %v", err)
			return 0, err
		}
	}
	return count, nil
}

func (s *PostgresAdminDB) GetEventsCount(boundary string) (int, error) {
	schemaMapping, ok := s.boundarySchemaMappings[boundary]
	// First try to get the count from the events_count table
	rows, err := s.db.Query(fmt.Sprintf("SELECT event_count FROM %s.events_count limit 1", schemaMapping.Schema))
	if err != nil {
		s.logger.Debugf("Event count table query error: %v", err)
		// If the table doesn't exist yet or there's another error, fall back to counting directly
		if !ok {
			return 0, fmt.Errorf("no schema mapping found for boundary: %s", boundary)
		}

		query := fmt.Sprintf("SELECT COUNT(*) FROM %s.orisun_es_event", schemaMapping.Schema)
		var count int
		err := s.db.QueryRow(query).Scan(&count)
		if err != nil {
			s.logger.Errorf("Error getting events count for boundary %s: %v", boundary, err)
			return 0, err
		}

		return count, nil
	}
	defer rows.Close()

	var count int = 0
	if rows.Next() {
		var countStr string
		if err := rows.Scan(&countStr); err != nil {
			s.logger.Error("Failed to scan event count: %v", err)
			return 0, err
		}
		count, err = strconv.Atoi(countStr)
		if err != nil {
			s.logger.Error("Failed to convert event count to int: %v", err)
			return 0, err
		}
	}
	return count, nil
}

const (
	userCountId  = "0195c053-57e7-7a6d-8e17-a2a695f67d1f"
	eventCountId = "0195c053-57e7-7a6d-8e17-a2a695f67d2f"
)

func (s *PostgresAdminDB) SaveUsersCount(users_count uint32) error {
	_, err := s.db.Exec(
		fmt.Sprintf("INSERT INTO %s.users_count (id, user_count, created_at, updated_at) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET user_count = $2, updated_at = $4", s.adminSchema),
		userCountId,
		users_count,
		time.Now().UTC(),
		time.Now().UTC(),
	)

	if err != nil {
		s.logger.Error("Error creating user count: %v", err)
		return err
	}

	return nil
}

func (s *PostgresAdminDB) SaveEventCount(event_count int, boundary string) error {
	// Get the schema mapping for the boundary
	schemaMapping, ok := s.boundarySchemaMappings[boundary]
	if !ok {
		return fmt.Errorf("no schema mapping found for boundary: %s", boundary)
	}

	_, err := s.db.Exec(
		fmt.Sprintf("INSERT INTO %s.events_count (id, event_count, created_at, updated_at) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET event_count = $2, updated_at = $4", schemaMapping.Schema),
		eventCountId,
		strconv.Itoa(event_count),
		time.Now().UTC(),
		time.Now().UTC(),
	)

	if err != nil {
		s.logger.Error("Error creating event count: %v", err)
		return err
	}

	return nil
}
