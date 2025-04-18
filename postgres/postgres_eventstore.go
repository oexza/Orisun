package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"orisun/logging"
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
SELECT * FROM %s.insert_events_with_consistency($1::text, $2::jsonb, $3::jsonb, $4::jsonb)
`

const selectMatchingEvents = `
SELECT * FROM %s.get_matching_events($1::text, $2, $3::INT, $4::jsonb, $5::jsonb, $6, $7::INT)
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
	db *sql.DB,
	logger *logging.Logger,
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresSaveEvents {
	return &PostgresSaveEvents{db: db, logger: *logger, boundarySchemaMappings: boundarySchemaMappings}
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

func NewPostgresGetEvents(db *sql.DB, logger *logging.Logger,
	boundarySchemaMappings map[string]config.BoundaryToPostgresSchemaMapping) *PostgresGetEvents {
	return &PostgresGetEvents{db: db, logger: *logger, boundarySchemaMappings: boundarySchemaMappings}
}

func (s *PostgresSaveEvents) Save(
	ctx context.Context,
	events []eventstore.EventWithMapTags,
	consistencyCondition *eventstore.IndexLockCondition,
	boundary string,
	streamName string,
	expectedVersion int32,
	streamConsistencyCondition *eventstore.Query) (transactionID string, globalID uint64, err error) {
	s.logger.Debugf("Postgres: Saving events from request: %v", consistencyCondition)

	schema, err := s.Schema(boundary)
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to get schema: %v", err)
	}

	streamSubsetAsBytes, err := json.Marshal(getStreamSectionAsMap(streamName, expectedVersion, streamConsistencyCondition))
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to marshal consistency condition: %v", err)
	}
	// s.logger.Debugf("streamSubsetAsJsonString: %v", jsonStr)

	var consistencyConditionJSONString []byte = nil
	if consistencyCondition != nil {
		consistencyConditionJSON, err := json.Marshal(getConsistencyConditionAsMap(consistencyCondition))
		if err != nil {
			return "", 0, status.Errorf(codes.Internal, "failed to marshal consistency condition: %v", err)
		}
		consistencyConditionJSONString = consistencyConditionJSON
	}

	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to marshal events: %v", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// _, err = tx.ExecContext(ctx, fmt.Sprintf(setSearchPath, schema))
	// if err != nil {
	// 	return "", 0, status.Errorf(codes.Internal, "failed to set search path: %v", err)
	// }

	// s.logger.Debugf("Postgres: Consistency Condition: %v", *consistencyConditionJSONString)
	row := tx.QueryRowContext(
		ctx,
		fmt.Sprintf(insertEventsWithConsistency, schema),
		schema,
		streamSubsetAsBytes,
		consistencyConditionJSONString,
		string(eventsJSON),
	)

	if row.Err() != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to insert events: %v", row.Err())
	}
	// Scan the result
	noop := false
	err = error(nil)

	var tranID string
	var globID uint64
	err = row.Scan(&tranID, &globID, &noop)
	err = tx.Commit()

	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "failed to commit transaction: %v", err)
	}

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
		fromPosition := &map[string]uint64{
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
			// s.logger.Debugf("paramsJson: %v", jsonStr)
		}
	}

	var streamName *string = nil
	var fromStreamVersion *int32 = nil

	if req.Stream != nil {
		streamName = &req.Stream.Name
		// if req.Stream.FromVersion != 0 {
		fromStreamVersion = &req.Stream.FromVersion
		// }
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

	// _, err = tx.Exec(fmt.Sprintf(setSearchPath, schema))
	// if err != nil {
	// 	return nil, status.Errorf(codes.Internal, "failed to set search path: %v", err)
	// }

	rows, err := tx.Query(
		fmt.Sprintf(selectMatchingEvents, schema),
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

	// Pre-allocate events slice with a reasonable capacity
	events := make([]*eventstore.Event, 0, req.Count)

	columns, err := rows.Columns()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get column names: %v", err)
	}

	// Create a reusable map for column mapping
	columnMap := make(map[string]int, len(columns))
	for i, col := range columns {
		columnMap[col] = i
	}

	for rows.Next() {
		var event eventstore.Event
		var tagsBytes []byte
		var transactionID, globalID uint64
		var dateCreated time.Time

		// Create a map of pointers to hold our row data
		rowData := map[string]interface{}{
			"event_id":       &event.EventId,
			"event_type":     &event.EventType,
			"data":           &event.Data,
			"metadata":       &event.Metadata,
			"tags":           &tagsBytes,
			"transaction_id": &transactionID,
			"global_id":      &globalID,
			"date_created":   &dateCreated,
			"stream_name":    &event.StreamId,
			"stream_version": &event.Version,
		}

		// Create a slice of pointers to scan into
		scanArgs := make([]interface{}, len(columns))
		for i, col := range columns {
			if ptr, ok := rowData[col]; ok {
				scanArgs[i] = ptr
			} else {
				return nil, status.Errorf(codes.Internal, "unexpected column: %s", col)
			}
		}

		// Scan the row into our map
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to scan row: %v", err)
		}

		// Process tags - pre-allocate tags slice
		var tagsMap map[string]string
		if err := json.Unmarshal(tagsBytes, &tagsMap); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to unmarshal tags: %v", err)
		}

		event.Tags = make([]*eventstore.Tag, 0, len(tagsMap))
		for key, value := range tagsMap {
			event.Tags = append(event.Tags, &eventstore.Tag{Key: key, Value: value})
		}

		// Set the Position
		event.Position = &eventstore.Position{
			CommitPosition:  transactionID,
			PreparePosition: globalID,
		}

		// Set the DateCreated
		event.DateCreated = timestamppb.New(dateCreated)

		events = append(events, &event)
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "error iterating rows: %v", err)
	}

	return &eventstore.GetEventsResponse{Events: events}, nil
}

func getStreamSectionAsMap(streamName string, expectedVersion int32, consistencyCondition *eventstore.Query) map[string]interface{} {
	lastRetrievedPositions := make(map[string]interface{})
	lastRetrievedPositions["stream_name"] = streamName
	lastRetrievedPositions["expected_version"] = expectedVersion

	if conditions := consistencyCondition; conditions != nil {
		lastRetrievedPositions["criteria"] = getCriteriaAsList(consistencyCondition)
	}

	return lastRetrievedPositions
}

func getConsistencyConditionAsMap(consistencyCondition *eventstore.IndexLockCondition) map[string]interface{} {
	lastRetrievedPositions := make(map[string]uint64)
	if consistencyCondition.ConsistencyMarker != nil {
		lastRetrievedPositions["transaction_id"] = consistencyCondition.ConsistencyMarker.CommitPosition
		lastRetrievedPositions["global_id"] = consistencyCondition.ConsistencyMarker.PreparePosition
	}

	criteriaList := getCriteriaAsList(consistencyCondition.Query)

	return map[string]interface{}{
		"last_retrieved_position": lastRetrievedPositions,
		"criteria":                criteriaList,
	}
}

func getCriteriaAsList(query *eventstore.Query) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(query.Criteria))
	for _, criterion := range query.Criteria {
		anded := make(map[string]interface{}, len(criterion.Tags))
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
	db          *sql.DB
	logger      logging.Logger
	adminSchema string
}

func NewPostgresAdminDB(db *sql.DB, logger logging.Logger, schema string) *PostgresAdminDB {
	return &PostgresAdminDB{
		db:          db,
		logger:      logger,
		adminSchema: schema,
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
		users = append(users, &user)
	}

	return users, nil
}

func (s *PostgresAdminDB) GetProjectorLastPosition(projectorName string) (*eventstore.Position, error) {
	var commitPos, preparePos uint64
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

const id = "0195c053-57e7-7a6d-8e17-a2a695f67d1f"

func (s *PostgresAdminDB) SaveUsersCount(users_count uint32) error {
	_, err := s.db.Exec(
		fmt.Sprintf("INSERT INTO %s.users_count (id, user_count, created_at, updated_at) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET user_count = $2, updated_at = $4", s.adminSchema),
		id,
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
