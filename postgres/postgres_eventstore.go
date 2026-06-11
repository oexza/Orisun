package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go/jetstream"
	common "github.com/oexza/Orisun/admin/slices/common"
	"github.com/oexza/Orisun/logging"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/lib/pq"
	eventstore "github.com/oexza/Orisun/orisun"

	config "github.com/oexza/Orisun/config"

	"github.com/oexza/Orisun/orisun"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const insertEventsWithConsistency = `
SELECT * FROM %s.insert_events_with_consistency_v3($1::text, $2::text, $3::jsonb, $4::jsonb)
`

const selectMatchingEvents = `
SELECT * FROM %s.get_matching_events_v3($1::text, $2::text, $3::jsonb, $4::jsonb, $5, $6::INT)
`

const selectLatestByCriteria = `
SELECT * FROM %s.get_latest_by_criteria_v1($1::text, $2::text, $3::jsonb)
`

const setSearchPath = `
set search_path to '%s'
`

type PostgresSaveEvents struct {
	db                     *sql.DB
	logger                 logging.Logger
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping
	insertQueries          map[string]string // boundary -> pre-formatted insert SQL
}

func NewPostgresSaveEvents(
	ctx context.Context,
	db *sql.DB,
	logger logging.Logger,
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresSaveEvents {
	queries := make(map[string]string, len(boundarySchemaMappings))
	for boundary, m := range boundarySchemaMappings {
		queries[boundary] = fmt.Sprintf(insertEventsWithConsistency, m.Schema)
	}
	return &PostgresSaveEvents{
		db:                     db,
		logger:                 logger,
		boundarySchemaMappings: boundarySchemaMappings,
		insertQueries:          queries,
	}
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
	selectQueries          map[string]string // boundary -> pre-formatted select SQL
	latestQueries          map[string]string // boundary -> pre-formatted latest-by-criteria SQL
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
	queries := make(map[string]string, len(boundarySchemaMappings))
	latestQueries := make(map[string]string, len(boundarySchemaMappings))
	for boundary, m := range boundarySchemaMappings {
		queries[boundary] = fmt.Sprintf(selectMatchingEvents, m.Schema)
		latestQueries[boundary] = fmt.Sprintf(selectLatestByCriteria, m.Schema)
	}
	return &PostgresGetEvents{
		db:                     db,
		logger:                 logger,
		boundarySchemaMappings: boundarySchemaMappings,
		selectQueries:          queries,
		latestQueries:          latestQueries,
	}
}

func (s *PostgresSaveEvents) Save(
	ctx context.Context,
	events []eventstore.EventWithMapTags,
	boundary string,
	expectedPosition *eventstore.Position,
	streamConsistencyCondition *eventstore.Query) (transactionID string, globalID int64, err error) {
	s.logger.Debug("Postgres: Saving events from request: %v", events)

	if len(events) == 0 {
		return "", 0, status.Errorf(codes.InvalidArgument, "events cannot be empty")
	}

	schema, err := s.Schema(boundary)
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to get schema: %v", err)
	}
	events, err = eventstore.NormalizeEventsForSave(events)
	if err != nil {
		return "", 0, status.Errorf(codes.InvalidArgument, "invalid event data: %v", err)
	}

	streamSubsetAsBytes, err := json.Marshal(getStreamSectionAsMap(expectedPosition, streamConsistencyCondition))
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to marshal consistency condition: %v", err)
	}
	s.logger.Debugf("streamSubsetAsJsonString: %v", string(streamSubsetAsBytes))

	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to marshal events: %v", err)
	}
	// s.logger.Infof("eventsJSON: %v", string(eventsJSON))

	// Use a shorter-lived transaction with immediate execution and commit
	var tranID string
	var globID int64
	var newGlobalID int64

	// Execute the operation in a single atomic transaction
	err = func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			s.logger.Errorf("Error beginning transaction: %v", err)
			return status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
		}
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// intruct postgres to log sql statements
		// _, errr := tx.Exec("SET log_statement = 'all';")
		// if errr != nil {
		// 	return status.Errorf(codes.Internal, "failed to set log_statement: %v", err)
		// }

		row := tx.QueryRowContext(
			ctx,
			s.insertQueries[boundary],
			boundary,
			schema,
			streamSubsetAsBytes,
			eventsJSON,
		)

		if row.Err() != nil {
			s.logger.Errorf("Error inserting events: %v", row.Err())
			return status.Errorf(codes.Internal, "failed to insert events: %v", row.Err())
		}

		// Scan the result
		if err := row.Scan(&newGlobalID, &tranID, &globID); err != nil {
			s.logger.Errorf("Error scanning result: %v", err)
			return status.Errorf(codes.Internal, "failed to scan result: %v", err)
		}

		// Commit immediately to release the connection
		if err := tx.Commit(); err != nil {
			s.logger.Errorf("Error committing transaction: %v", err)
			return status.Errorf(codes.Internal, "failed to commit transaction: %v", err)
		}

		return nil
	}()

	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to commit transaction: %v", err)
	}

	s.logger.Debugf("PG save events::::: Transaction ID: %v, Global ID: %v", tranID, globID)

	if err != nil {
		if strings.Contains(err.Error(), "OptimisticConcurrencyException") {
			return "", 0, status.Error(codes.AlreadyExists, err.Error())
		}
		s.logger.Errorf("Error saving events to database: %v", err)
		return "", 0, status.Errorf(codes.Internal, "Error saving events to database")
	}

	return tranID, globID, nil
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
			globalQuery := map[string]any{
				"criteria": criteriaList,
			}
			paramsBytes, err := json.Marshal(globalQuery)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to marshal params: %v", err)
			}
			paramsJSON = &paramsBytes
		}
	}

	// s.logger.Debugf("params: %v", paramsJSON)
	// s.logger.Debugf("direction: %v", req.Direction)
	// s.logger.Debugf("count: %v", req.Count)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// _, errr := tx.Exec("SET log_statement = 'all';")
	// if errr != nil {
	// 	return nil, status.Errorf(codes.Internal, "failed to set log_statement: %v", err)
	// }

	rows, err := tx.Query(
		s.selectQueries[req.Boundary],
		req.Boundary,
		schema,
		paramsJSON,
		fromPositionMarshaled,
		req.Direction.String(),
		req.Count,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to execute query: %v", err)
	}
	defer rows.Close()

	// Pre-allocate events slice with expected capacity
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

// GetLatestByCriteria returns the latest event per criterion plus the max
// observed position. The per-criterion lookups run inside one SQL statement
// (get_latest_by_criteria_v1 builds a UNION ALL of LIMIT-1 subqueries), so the
// whole context comes from one snapshot — assembling it from independent
// queries would let an event commit in between with a position below the
// observed maximum, invisible to a scalar expected-position check.
func (s *PostgresGetEvents) GetLatestByCriteria(ctx context.Context, req *eventstore.GetLatestByCriteriaRequest) (*eventstore.GetLatestByCriteriaResponse, error) {
	schema, err := s.Schema(req.Boundary)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "no schema found for boundary: %s", req.Boundary)
	}
	if len(req.Criteria) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "at least one criterion is required")
	}

	criteriaList := getCriteriaAsList(&eventstore.Query{Criteria: req.Criteria})
	if len(criteriaList) != len(req.Criteria) {
		return nil, status.Errorf(codes.InvalidArgument, "every criterion needs at least one tag")
	}
	paramsBytes, err := json.Marshal(map[string]any{"criteria": criteriaList})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal criteria: %v", err)
	}

	rows, err := s.db.QueryContext(ctx, s.latestQueries[req.Boundary], req.Boundary, schema, paramsBytes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to execute query: %v", err)
	}
	defer rows.Close()

	eventsByIdx := make(map[int]*eventstore.Event, len(req.Criteria))
	for rows.Next() {
		var (
			idx                     int
			transactionID, globalID int64
			eventID, eventType      string
			dataJSON, metadataJSON  string
			dateCreated             time.Time
		)
		if err := rows.Scan(&idx, &transactionID, &globalID, &eventID, &eventType, &dataJSON, &metadataJSON, &dateCreated); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to scan row: %v", err)
		}
		eventsByIdx[idx] = &eventstore.Event{
			EventId:   eventID,
			EventType: eventType,
			Data:      dataJSON,
			Metadata:  metadataJSON,
			Position: &eventstore.Position{
				CommitPosition:  transactionID,
				PreparePosition: globalID,
			},
			DateCreated: timestamppb.New(dateCreated),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "error iterating rows: %v", err)
	}

	resp := &eventstore.GetLatestByCriteriaResponse{}
	contextPos := eventstore.NotExistsPosition()
	found := false
	for i, criterion := range req.Criteria {
		result := &eventstore.LatestCriterionResult{Criterion: criterion}
		if event, ok := eventsByIdx[i]; ok {
			result.Event = event
			if !found || event.Position.CommitPosition > contextPos.CommitPosition ||
				(event.Position.CommitPosition == contextPos.CommitPosition &&
					event.Position.PreparePosition > contextPos.PreparePosition) {
				contextPos.CommitPosition = event.Position.CommitPosition
				contextPos.PreparePosition = event.Position.PreparePosition
				found = true
			}
		}
		resp.Results = append(resp.Results, result)
	}
	resp.ContextPosition = &eventstore.Position{
		CommitPosition:  contextPos.CommitPosition,
		PreparePosition: contextPos.PreparePosition,
	}
	return resp, nil
}

func getStreamSectionAsMap(expectedPosition *eventstore.Position, consistencyCondition *eventstore.Query) map[string]any {
	lastRetrievedPositions := make(map[string]any)

	if expectedPosition != nil {
		lastRetrievedPositions["expected_position"] = map[string]int64{
			"transaction_id": expectedPosition.CommitPosition,
			"global_id":      expectedPosition.PreparePosition,
		}
	}

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

type PostgresAdminDB struct {
	db                     *sql.DB
	logger                 logging.Logger
	adminSchema            string
	adminBoundary          string
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping

	// Pre-formatted admin queries (fixed schema/boundary)
	qListUsers          string
	qGetProjectorPos    string
	qUpsertProjectorPos string
	qDeleteUser         string
	qGetUserByUsername  string
	qGetUserById        string
	qUpsertUser         string
	qGetUsersCount      string
	qSaveUsersCount     string

	// Per-boundary event count queries
	qGetEventCount      map[string]string
	qFallbackEventCount map[string]string
	qSaveEventCount     map[string]string
}

func NewPostgresAdminDB(db *sql.DB, logger logging.Logger, schema string, boundary string, boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresAdminDB {
	getEventCount := make(map[string]string, len(boundarySchemaMappings))
	fallbackEventCount := make(map[string]string, len(boundarySchemaMappings))
	saveEventCount := make(map[string]string, len(boundarySchemaMappings))
	for b, m := range boundarySchemaMappings {
		getEventCount[b] = fmt.Sprintf("SELECT event_count FROM %s.%s_events_count limit 1", m.Schema, b)
		fallbackEventCount[b] = fmt.Sprintf("SELECT COUNT(*) FROM %s.%s_orisun_es_event", m.Schema, b)
		saveEventCount[b] = fmt.Sprintf("INSERT INTO %s.%s_events_count (id, event_count, created_at, updated_at) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET event_count = $2, updated_at = $4", m.Schema, b)
	}

	return &PostgresAdminDB{
		db:                     db,
		logger:                 logger,
		adminSchema:            schema,
		adminBoundary:          boundary,
		boundarySchemaMappings: boundarySchemaMappings,

		qListUsers:          fmt.Sprintf("SELECT id, name, username, password_hash, roles FROM %s.%s_users ORDER BY id", schema, boundary),
		qGetProjectorPos:    fmt.Sprintf("SELECT COALESCE(commit_position, 0), COALESCE(prepare_position, 0) FROM %s.%s_projector_checkpoint where name = $1", schema, boundary),
		qUpsertProjectorPos: fmt.Sprintf("INSERT INTO %s.%s_projector_checkpoint (id, name, commit_position, prepare_position) VALUES ($1, $2, $3, $4) ON CONFLICT (name) DO UPDATE SET commit_position = $3, prepare_position = $4", schema, boundary),
		qDeleteUser:         fmt.Sprintf("DELETE FROM %s.%s_users WHERE id = $1", schema, boundary),
		qGetUserByUsername:  fmt.Sprintf("SELECT id, name, username, password_hash, roles FROM %s.%s_users where username = $1", schema, boundary),
		qGetUserById:        fmt.Sprintf("SELECT id, name, username, password_hash, roles FROM %s.%s_users where id = $1", schema, boundary),
		qUpsertUser:         fmt.Sprintf("INSERT INTO %s.%s_users (id, name, username, password_hash, roles) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO UPDATE SET name = $2, username = $3, password_hash = $4, roles = $5, updated_at = $6", schema, boundary),
		qGetUsersCount:      fmt.Sprintf("SELECT user_count FROM %s.%s_users_count limit 1", schema, boundary),
		qSaveUsersCount:     fmt.Sprintf("INSERT INTO %s.%s_users_count (id, user_count, created_at, updated_at) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET user_count = $2, updated_at = $4", schema, boundary),

		qGetEventCount:      getEventCount,
		qFallbackEventCount: fallbackEventCount,
		qSaveEventCount:     saveEventCount,
	}
}

var userCache = map[string]*orisun.User{}

func (s *PostgresAdminDB) ListAdminUsers() ([]*orisun.User, error) {
	rows, err := s.db.Query(s.qListUsers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*orisun.User
	for rows.Next() {
		var user orisun.User
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
		s.qGetProjectorPos,
		projectorName,
	).Scan(&commitPos, &preparePos)
	if err == sql.ErrNoRows {
		pos := eventstore.NotExistsPosition()
		return &pos, nil
	}
	if err != nil {
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
		p.qUpsertProjectorPos,
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

func (p *PostgresAdminDB) DeleteUser(id string) error {
	_, err := p.db.Exec(
		p.qDeleteUser,
		id,
	)

	if err != nil {
		p.logger.Error("Error updating checkpoint: %v", err)
		return err
	}
	return nil
}

func (s *PostgresAdminDB) scanUser(rows *sql.Rows) (orisun.User, error) {
	var user orisun.User
	var roles []string
	if err := rows.Scan(&user.Id, &user.Name, &user.Username, &user.HashedPassword, pq.Array(&roles)); err != nil {
		s.logger.Error("Failed to scan user row: %v", err)
		return orisun.User{}, err
	}

	for _, role := range roles {
		user.Roles = append(user.Roles, orisun.Role(role))
	}
	return user, nil
}

func (s *PostgresAdminDB) GetUserByUsername(username string) (orisun.User, error) {
	user := userCache[username]
	if user != nil {
		s.logger.Debug("Fetched from cache")
		return *user, nil
	}

	rows, err := s.db.Query(s.qGetUserByUsername, username)
	if err != nil {
		s.logger.Infof("User: %v", err)
		return orisun.User{}, err
	}
	defer rows.Close()

	var userResponse orisun.User
	if rows.Next() {
		userResponse, err = s.scanUser(rows)
		if err != nil {
			return orisun.User{}, err
		}
	}

	if userResponse.Id != "" {
		userCache[username] = &userResponse
		return userResponse, nil
	}
	return orisun.User{}, fmt.Errorf("user not found")
}

func (s *PostgresAdminDB) GetUserById(id string) (orisun.User, error) {
	rows, err := s.db.Query(s.qGetUserById, id)
	if err != nil {
		s.logger.Debugf("User by ID: %v", err)
		return orisun.User{}, err
	}
	defer rows.Close()

	var userResponse orisun.User
	if rows.Next() {
		userResponse, err = s.scanUser(rows)
		if err != nil {
			return orisun.User{}, err
		}
	}

	if userResponse.Id != "" {
		return userResponse, nil
	}
	return orisun.User{}, fmt.Errorf("user not found with id: %s", id)
}

func (s *PostgresAdminDB) UpsertUser(user orisun.User) error {
	roleStrings := make([]string, len(user.Roles))
	for i, role := range user.Roles {
		roleStrings[i] = string(role)
	}
	rolesStr := "{" + strings.Join(roleStrings, ",") + "}"

	_, err := s.db.Exec(
		s.qUpsertUser,
		user.Id,
		user.Name,
		user.Username,
		user.HashedPassword,
		rolesStr,
		time.Now().UTC(),
	)

	if err != nil {
		s.logger.Error("Error upserting user: %v", err)
		return err
	}

	// Update cache
	userCache[user.Username] = &user
	return nil
}

func (s *PostgresAdminDB) GetUsersCount() (uint32, error) {
	rows, err := s.db.Query(s.qGetUsersCount)
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
	q, ok := s.qGetEventCount[boundary]
	if !ok {
		return 0, fmt.Errorf("no schema mapping found for boundary: %s", boundary)
	}

	rows, err := s.db.Query(q)
	if err != nil {
		s.logger.Debugf("Event count table query error: %v", err)
		var count int
		err := s.db.QueryRow(s.qFallbackEventCount[boundary]).Scan(&count)
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
		s.qSaveUsersCount,
		userCountId,
		strconv.FormatUint(uint64(users_count), 10),
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
	q, ok := s.qSaveEventCount[boundary]
	if !ok {
		return fmt.Errorf("no schema mapping found for boundary: %s", boundary)
	}
	_, err := s.db.Exec(
		q,
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

func (db *PostgresAdminDB) CreateBoundaryIndex(
	ctx context.Context,
	boundary, name string,
	fields []eventstore.BoundaryIndexField,
	conditions []eventstore.BoundaryIndexCondition,
	combinator string,
) error {
	mapping, ok := db.boundarySchemaMappings[boundary]
	if !ok {
		return fmt.Errorf("unknown boundary: %s", boundary)
	}
	schema := mapping.Schema

	if err := validateBoundaryName(name); err != nil {
		return fmt.Errorf("invalid index name %s: %w", name, err)
	}
	if len(fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}

	// Build index expression list
	exprs := make([]string, len(fields))
	for i, f := range fields {
		key := pq.QuoteLiteral(f.JsonKey)
		switch f.ValueType {
		case "numeric":
			exprs[i] = "((data->>" + key + ")::numeric)"
		case "boolean":
			exprs[i] = "((data->>" + key + ")::boolean)"
		case "timestamptz":
			exprs[i] = "((data->>" + key + ")::timestamptz)"
		default: // "text"
			exprs[i] = "(data->>" + key + ")"
		}
	}

	// Build WHERE clause
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
				return fmt.Errorf("invalid operator %q: must be one of =, >, <, >=, <=", c.Operator)
			}
			predicates[i] = "(data->>" + pq.QuoteLiteral(c.Key) + ") " + c.Operator + " " + pq.QuoteLiteral(c.Value)
		}
		whereClause = " WHERE " + strings.Join(predicates, " "+combinator+" ")
	}

	indexName := pq.QuoteIdentifier(boundary + "_" + name + "_idx")
	tableName := pq.QuoteIdentifier(boundary + "_orisun_es_event")
	schemaName := pq.QuoteIdentifier(schema)

	ddl := fmt.Sprintf(
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON %s.%s USING btree (%s)%s",
		indexName,
		schemaName,
		tableName,
		strings.Join(exprs, ", "),
		whereClause,
	)

	_, err := db.db.ExecContext(ctx, ddl)
	return err
}

func (db *PostgresAdminDB) DropBoundaryIndex(
	ctx context.Context,
	boundary, name string,
) error {
	mapping, ok := db.boundarySchemaMappings[boundary]
	if !ok {
		return fmt.Errorf("unknown boundary: %s", boundary)
	}
	schema := mapping.Schema

	if err := validateBoundaryName(name); err != nil {
		return fmt.Errorf("invalid index name %s: %w", name, err)
	}

	indexName := pq.QuoteIdentifier(boundary + "_" + name + "_idx")
	schemaName := pq.QuoteIdentifier(schema)

	ddl := fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s.%s", schemaName, indexName)
	_, err := db.db.ExecContext(ctx, ddl)
	return err
}

func InitializePostgresDatabase(
	ctx context.Context,
	postgresDBConfig config.PostgresDBConfig,
	adminConfig config.AdminConfig,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, common.DB, eventstore.EventPublishingTracker, *PGNotifyListener) {
	// Create database connection string
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		postgresDBConfig.Host,
		postgresDBConfig.Port,
		postgresDBConfig.User,
		postgresDBConfig.Password,
		postgresDBConfig.Name,
		postgresDBConfig.SSLMode,
	)

	// Create write database pool (optimized for write operations)
	writeDB, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.Fatalf("Failed to connect to write database: %v", err)
	}

	// Configure write pool - fewer connections, shorter lifetimes for consistency
	writeDB.SetMaxOpenConns(postgresDBConfig.WriteMaxOpenConns)
	writeDB.SetMaxIdleConns(postgresDBConfig.WriteMaxIdleConns)
	writeDB.SetConnMaxIdleTime(postgresDBConfig.WriteConnMaxIdleTime)
	writeDB.SetConnMaxLifetime(postgresDBConfig.WriteConnMaxLifetime)

	// Create read database pool (optimized for read operations)
	readDB, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.Fatalf("Failed to connect to read database: %v", err)
	}

	// Configure read pool - more connections, longer lifetimes for performance
	readDB.SetMaxOpenConns(postgresDBConfig.ReadMaxOpenConns)
	readDB.SetMaxIdleConns(postgresDBConfig.ReadMaxIdleConns)
	readDB.SetConnMaxIdleTime(postgresDBConfig.ReadConnMaxIdleTime)
	readDB.SetConnMaxLifetime(postgresDBConfig.ReadConnMaxLifetime)

	// Create admin database pool (optimized for admin operations)
	adminDBPool, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.Fatalf("Failed to connect to admin database: %v", err)
	}

	// Configure admin pool - moderate connections, longer lifetimes for admin tasks
	adminDBPool.SetMaxOpenConns(postgresDBConfig.AdminMaxOpenConns)
	adminDBPool.SetMaxIdleConns(postgresDBConfig.AdminMaxIdleConns)
	adminDBPool.SetConnMaxIdleTime(postgresDBConfig.AdminConnMaxIdleTime)
	adminDBPool.SetConnMaxLifetime(postgresDBConfig.AdminConnMaxLifetime)

	// Test all connections
	if err = writeDB.PingContext(ctx); err != nil {
		logger.Fatalf("Failed to ping write database: %v", err)
	}
	if err = readDB.PingContext(ctx); err != nil {
		logger.Fatalf("Failed to ping read database: %v", err)
	}
	if err = adminDBPool.PingContext(ctx); err != nil {
		logger.Fatalf("Failed to ping admin database: %v", err)
	}

	logger.Info("Database connections established with separate read/write/admin pools")
	logger.Infof("Write pool: %d max open, %d max idle connections",
		postgresDBConfig.WriteMaxOpenConns, postgresDBConfig.WriteMaxIdleConns)
	logger.Infof("Read pool: %d max open, %d max idle connections",
		postgresDBConfig.ReadMaxOpenConns, postgresDBConfig.ReadMaxIdleConns)
	logger.Infof("AdminConfig pool: %d max open, %d max idle connections",
		postgresDBConfig.AdminMaxOpenConns, postgresDBConfig.AdminMaxIdleConns)

	go func() {
		<-ctx.Done()
		logger.Info("Shutting down database connections")
		writeDB.Close()
		readDB.Close()
		adminDBPool.Close()
	}()

	postgesBoundarySchemaMappings := postgresDBConfig.GetSchemaMapping()

	// Create PG LISTEN/NOTIFY listener if enabled.
	var pgListener *PGNotifyListener
	if postgresDBConfig.ListenEnabled {
		var listenErr error
		pgListener, listenErr = NewPGNotifyListener(ctx, connStr, postgesBoundarySchemaMappings, logger)
		if listenErr != nil {
			logger.Warnf("PG LISTEN disabled (connection failed): %v — falling back to polling", listenErr)
			pgListener = nil
		} else {
			logger.Info("PG LISTEN/NOTIFY listener created")
		}
	} else {
		logger.Info("PG LISTEN disabled by configuration — using polling fallback")
	}

	// First, create all unique schemas
	uniqueSchemas := make(map[string]struct{})
	for _, mapping := range postgesBoundarySchemaMappings {
		uniqueSchemas[mapping.Schema] = struct{}{}
	}
	for schema := range uniqueSchemas {
		if schema != "public" {
			if err := createSchemaIfNotExists(writeDB, schema, ctx); err != nil {
				logger.Fatalf("Failed to create schema %s: %v", schema, err)
			}
		}
	}

	// Initialize tables for each boundary
	for boundary, mapping := range postgesBoundarySchemaMappings {
		isAdminBoundary := boundary == adminConfig.Boundary
		// Use write pool for database migrations (schema changes)
		if err = RunDbScripts(writeDB, boundary, mapping.Schema, isAdminBoundary, ctx); err != nil {
			logger.Fatalf("Failed to run database migrations for boundary %s in schema %s: %v", boundary, mapping.Schema, err)
		}

		logger.Infof("Database migrations for boundary %s in schema %s completed successfully", boundary, mapping.Schema)
	}

	// Use write pool for save operations and read pool for get operations
	saveEvents := NewPostgresSaveEvents(ctx, writeDB, logger, postgesBoundarySchemaMappings)
	getEvents := NewPostgresGetEvents(readDB, logger, postgesBoundarySchemaMappings)
	lockProvider, err := eventstore.NewJetStreamLockProvider(ctx, js, logger)
	if err != nil {
		logger.Fatalf("Failed to create lock provider: %v", err)
	}
	// lockProvider := postgres.NewPGLockProvider(db, AppLogger)
	adminSchema := postgesBoundarySchemaMappings[adminConfig.Boundary]
	if (adminSchema == config.BoundaryToPostgresSchemaMapping{}) {
		logger.Fatalf("No schema specified for admin boundary %v", err)
	}

	// Use admin pool for admin operations (user management)
	adminDB := NewPostgresAdminDB(
		adminDBPool,
		logger,
		adminSchema.Schema,
		adminConfig.Boundary,
		postgesBoundarySchemaMappings,
	)

	// Use write pool for event publishing operations
	eventPublishing := NewPostgresEventPublishing(
		writeDB,
		logger,
		postgesBoundarySchemaMappings,
	)

	return saveEvents, getEvents, lockProvider, adminDB, eventPublishing, pgListener
}
