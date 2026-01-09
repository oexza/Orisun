package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
)

// SQLiteAdminDB handles admin database operations
type SQLiteAdminDB struct {
	db     *sql.DB
	logger logging.Logger
}

// NewSQLiteAdminDB creates a new SQLiteAdminDB instance
func NewSQLiteAdminDB(db *sql.DB, logger logging.Logger) *SQLiteAdminDB {
	return &SQLiteAdminDB{
		db:     db,
		logger: logger,
	}
}

// ListAdminUsers retrieves all admin users
func (s *SQLiteAdminDB) ListAdminUsers() ([]*orisun.User, error) {
	rows, err := s.db.Query("SELECT id, name, username, password_hash, roles FROM users ORDER BY id")
	if err != nil {
		s.logger.Errorf("Failed to list users: %v", err)
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*orisun.User
	for rows.Next() {
		user, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	return users, nil
}

// GetUserByUsername retrieves a user by username
func (s *SQLiteAdminDB) GetUserByUsername(username string) (orisun.User, error) {
	var user orisun.User
	var rolesJSON string

	err := s.db.QueryRow(
		"SELECT id, name, username, password_hash, roles FROM users WHERE username = ?",
		username,
	).Scan(&user.Id, &user.Name, &user.Username, &user.HashedPassword, &rolesJSON)

	if err == sql.ErrNoRows {
		s.logger.Infof("User not found: %s", username)
		return orisun.User{}, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		s.logger.Errorf("Failed to get user by username: %v", err)
		return orisun.User{}, fmt.Errorf("failed to get user: %w", err)
	}

	// Parse roles JSON
	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		s.logger.Errorf("Failed to parse roles JSON: %v", err)
		return orisun.User{}, fmt.Errorf("failed to parse roles: %w", err)
	}

	return user, nil
}

// GetUserById retrieves a user by ID
func (s *SQLiteAdminDB) GetUserById(id string) (orisun.User, error) {
	var user orisun.User
	var rolesJSON string

	err := s.db.QueryRow(
		"SELECT id, name, username, password_hash, roles FROM users WHERE id = ?",
		id,
	).Scan(&user.Id, &user.Name, &user.Username, &user.HashedPassword, &rolesJSON)

	if err == sql.ErrNoRows {
		s.logger.Infof("User not found with ID: %s", id)
		return orisun.User{}, fmt.Errorf("user not found: %s", id)
	}
	if err != nil {
		s.logger.Errorf("Failed to get user by ID: %v", err)
		return orisun.User{}, fmt.Errorf("failed to get user: %w", err)
	}

	// Parse roles JSON
	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		s.logger.Errorf("Failed to parse roles JSON: %v", err)
		return orisun.User{}, fmt.Errorf("failed to parse roles: %w", err)
	}

	return user, nil
}

// UpsertUser creates or updates a user
func (s *SQLiteAdminDB) UpsertUser(user orisun.User) error {
	// Marshal roles to JSON
	rolesJSON, err := json.Marshal(user.Roles)
	if err != nil {
		s.logger.Errorf("Failed to marshal roles: %v", err)
		return fmt.Errorf("failed to marshal roles: %w", err)
	}

	now := time.Now().Unix()

	// Use INSERT OR REPLACE to handle both insert and update
	_, err = s.db.Exec(`
		INSERT INTO users (id, name, username, password_hash, roles, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name,
			username = excluded.username,
			password_hash = excluded.password_hash,
			roles = excluded.roles,
			updated_at = excluded.updated_at
	`,
		user.Id,
		user.Name,
		user.Username,
		user.HashedPassword,
		string(rolesJSON),
		now,
		now,
	)

	if err != nil {
		s.logger.Errorf("Failed to upsert user: %v", err)
		return fmt.Errorf("failed to upsert user: %w", err)
	}

	s.logger.Infof("Successfully upserted user: %s", user.Username)
	return nil
}

// DeleteUser deletes a user by ID
func (s *SQLiteAdminDB) DeleteUser(id string) error {
	_, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		s.logger.Errorf("Failed to delete user: %v", err)
		return fmt.Errorf("failed to delete user: %w", err)
	}

	s.logger.Infof("Successfully deleted user: %s", id)
	return nil
}

// GetUsersCount retrieves the user count
func (s *SQLiteAdminDB) GetUsersCount() (uint32, error) {
	var countStr string

	err := s.db.QueryRow("SELECT user_count FROM users_count LIMIT 1").Scan(&countStr)
	if err == sql.ErrNoRows {
		// Table might be empty, return 0
		return 0, nil
	}
	if err != nil {
		s.logger.Errorf("Failed to get users count: %v", err)
		return 0, fmt.Errorf("failed to get users count: %w", err)
	}

	var count uint32
	_, err = fmt.Sscanf(countStr, "%d", &count)
	if err != nil {
		s.logger.Errorf("Failed to parse users count: %v", err)
		return 0, fmt.Errorf("failed to parse users count: %w", err)
	}

	return count, nil
}

// SaveUsersCount saves the user count
func (s *SQLiteAdminDB) SaveUsersCount(usersCount uint32) error {
	const userCountId = "0195c053-57e7-7a6d-8e17-a2a695f67d1f"
	now := time.Now().Unix()

	countStr := fmt.Sprintf("%d", usersCount)

	_, err := s.db.Exec(`
		INSERT INTO users_count (id, user_count, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			user_count = excluded.user_count,
			updated_at = excluded.updated_at
	`,
		userCountId,
		countStr,
		now,
		now,
	)

	if err != nil {
		s.logger.Errorf("Failed to save users count: %v", err)
		return fmt.Errorf("failed to save users count: %w", err)
	}

	s.logger.Infof("Successfully saved users count: %d", usersCount)
	return nil
}

// GetEventsCount retrieves the event count for a boundary
func (s *SQLiteAdminDB) GetEventsCount(boundary string) (int, error) {
	var countStr string

	err := s.db.QueryRow("SELECT event_count FROM events_count LIMIT 1").Scan(&countStr)
	if err == sql.ErrNoRows {
		// Fall back to counting directly
		var count int
		err := s.db.QueryRow("SELECT COUNT(*) FROM orisun_es_event").Scan(&count)
		if err != nil {
			s.logger.Errorf("Failed to get events count: %v", err)
			return 0, fmt.Errorf("failed to get events count: %w", err)
		}
		return count, nil
	}
	if err != nil {
		s.logger.Errorf("Failed to get events count: %v", err)
		return 0, fmt.Errorf("failed to get events count: %w", err)
	}

	var count int
	_, err = fmt.Sscanf(countStr, "%d", &count)
	if err != nil {
		s.logger.Errorf("Failed to parse events count: %v", err)
		return 0, fmt.Errorf("failed to parse events count: %w", err)
	}

	return count, nil
}

// SaveEventCount saves the event count for a boundary
func (s *SQLiteAdminDB) SaveEventCount(eventCount int, boundary string) error {
	const eventCountId = "0195c053-57e7-7a6d-8e17-a2a695f67d2f"
	now := time.Now().Unix()

	countStr := fmt.Sprintf("%d", eventCount)

	_, err := s.db.Exec(`
		INSERT INTO events_count (id, event_count, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			event_count = excluded.event_count,
			updated_at = excluded.updated_at
	`,
		eventCountId,
		countStr,
		now,
		now,
	)

	if err != nil {
		s.logger.Errorf("Failed to save events count: %v", err)
		return fmt.Errorf("failed to save events count: %w", err)
	}

	s.logger.Infof("Successfully saved events count: %d for boundary: %s", eventCount, boundary)
	return nil
}

// GetProjectorLastPosition retrieves the last position for a projector
func (s *SQLiteAdminDB) GetProjectorLastPosition(projectorName string) (*orisun.Position, error) {
	var commitPos, preparePos int64

	err := s.db.QueryRow(
		"SELECT COALESCE(commit_position, 0), COALESCE(prepare_position, 0) FROM projector_checkpoint WHERE name = ?",
		projectorName,
	).Scan(&commitPos, &preparePos)

	if err == sql.ErrNoRows {
		// Return default position if not found
		return &orisun.Position{
			CommitPosition:  0,
			PreparePosition: 0,
		}, nil
	}
	if err != nil {
		s.logger.Errorf("Failed to get projector position: %v", err)
		return nil, fmt.Errorf("failed to get projector position: %w", err)
	}

	return &orisun.Position{
		CommitPosition:  commitPos,
		PreparePosition: preparePos,
	}, nil
}

// UpdateProjectorPosition updates the position for a projector
func (p *SQLiteAdminDB) UpdateProjectorPosition(name string, position *orisun.Position) error {
	now := time.Now().Unix()

	_, err := p.db.Exec(`
		INSERT INTO projector_checkpoint (name, commit_position, prepare_position, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			commit_position = excluded.commit_position,
			prepare_position = excluded.prepare_position,
			updated_at = excluded.updated_at
	`,
		name,
		position.CommitPosition,
		position.PreparePosition,
		now,
	)

	if err != nil {
		p.logger.Errorf("Failed to update projector position: %v", err)
		return fmt.Errorf("failed to update projector position: %w", err)
	}

	p.logger.Debugf("Successfully updated projector position: %s", name)
	return nil
}

// scanUser scans a user from a database row
func (s *SQLiteAdminDB) scanUser(rows *sql.Rows) (orisun.User, error) {
	var user orisun.User
	var rolesJSON string

	if err := rows.Scan(&user.Id, &user.Name, &user.Username, &user.HashedPassword, &rolesJSON); err != nil {
		s.logger.Errorf("Failed to scan user row: %v", err)
		return orisun.User{}, fmt.Errorf("failed to scan user: %w", err)
	}

	// Parse roles JSON
	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		s.logger.Errorf("Failed to parse roles JSON: %v", err)
		return orisun.User{}, fmt.Errorf("failed to parse roles: %w", err)
	}

	return user, nil
}
