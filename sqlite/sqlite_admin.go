package sqlite

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// SQLiteAdminDB handles admin database operations
type SQLiteAdminDB struct {
	conn   *sqlite.Conn
	logger logging.Logger
}

// NewSQLiteAdminDB creates a new SQLiteAdminDB instance
func NewSQLiteAdminDB(conn *sqlite.Conn, logger logging.Logger) *SQLiteAdminDB {
	return &SQLiteAdminDB{
		conn:   conn,
		logger: logger,
	}
}

// ListAdminUsers retrieves all admin users
func (s *SQLiteAdminDB) ListAdminUsers() ([]*orisun.User, error) {
	var users []*orisun.User

	err := sqlitex.ExecuteTransient(s.conn, "SELECT id, name, username, password_hash, roles FROM users ORDER BY id", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			for {
				hasRow, err := stmt.Step()
				if err != nil {
					return err
				}
				if !hasRow {
					break
				}

				rolesJSON := stmt.ColumnText(4)
				user := orisun.User{
					Id:             stmt.ColumnText(0),
					Name:           stmt.ColumnText(1),
					Username:       stmt.ColumnText(2),
					HashedPassword: stmt.ColumnText(3),
				}

				// Parse roles JSON
				if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
					s.logger.Errorf("Failed to parse roles JSON: %v", err)
					return fmt.Errorf("failed to parse roles JSON: %w", err)
				}

				users = append(users, &user)
			}
			return nil
		},
	})

	if err != nil {
		s.logger.Errorf("Failed to list users: %v", err)
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	return users, nil
}

// GetUserByUsername retrieves a user by username
func (s *SQLiteAdminDB) GetUserByUsername(username string) (orisun.User, error) {
	var user orisun.User
	var found bool

	err := sqlitex.ExecuteTransient(s.conn, "SELECT id, name, username, password_hash, roles FROM users WHERE username = ?", &sqlitex.ExecOptions{
		Args: []interface{}{username},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			hasRow, err := stmt.Step()
			if err != nil {
				return err
			}
			if !hasRow {
				return nil // No user found
			}

			found = true
			rolesJSON := stmt.ColumnText(4)
			user.Id = stmt.ColumnText(0)
			user.Name = stmt.ColumnText(1)
			user.Username = stmt.ColumnText(2)
			user.HashedPassword = stmt.ColumnText(3)

			// Parse roles JSON
			if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
				s.logger.Errorf("Failed to parse roles JSON: %v", err)
				return fmt.Errorf("failed to parse roles JSON: %w", err)
			}

			return nil
		},
	})

	if err != nil {
		s.logger.Errorf("Failed to get user by username: %v", err)
		return orisun.User{}, fmt.Errorf("failed to get user: %w", err)
	}

	if !found {
		s.logger.Infof("User not found: %s", username)
		return orisun.User{}, fmt.Errorf("user not found: %s", username)
	}

	return user, nil
}

// GetUserById retrieves a user by ID
func (s *SQLiteAdminDB) GetUserById(id string) (orisun.User, error) {
	var user orisun.User
	var found bool

	err := sqlitex.ExecuteTransient(s.conn, "SELECT id, name, username, password_hash, roles FROM users WHERE id = ?", &sqlitex.ExecOptions{
		Args: []interface{}{id},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			hasRow, err := stmt.Step()
			if err != nil {
				return err
			}
			if !hasRow {
				return nil // No user found
			}

			found = true
			rolesJSON := stmt.ColumnText(4)
			user.Id = stmt.ColumnText(0)
			user.Name = stmt.ColumnText(1)
			user.Username = stmt.ColumnText(2)
			user.HashedPassword = stmt.ColumnText(3)

			// Parse roles JSON
			if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
				s.logger.Errorf("Failed to parse roles JSON: %v", err)
				return fmt.Errorf("failed to parse roles JSON: %w", err)
			}

			return nil
		},
	})

	if err != nil {
		s.logger.Errorf("Failed to get user by ID: %v", err)
		return orisun.User{}, fmt.Errorf("failed to get user: %w", err)
	}

	if !found {
		s.logger.Infof("User not found with ID: %s", id)
		return orisun.User{}, fmt.Errorf("user not found: %s", id)
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
	err = sqlitex.ExecuteTransient(s.conn, `
		INSERT INTO users (id, name, username, password_hash, roles, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name,
			username = excluded.username,
			password_hash = excluded.password_hash,
			roles = excluded.roles,
			updated_at = excluded.updated_at
	`, &sqlitex.ExecOptions{
		Args: []interface{}{
			user.Id,
			user.Name,
			user.Username,
			user.HashedPassword,
			string(rolesJSON),
			now,
			now,
		},
	})

	if err != nil {
		s.logger.Errorf("Failed to upsert user: %v", err)
		return fmt.Errorf("failed to upsert user: %w", err)
	}

	s.logger.Infof("Successfully upserted user: %s", user.Username)
	return nil
}

// DeleteUser deletes a user by ID
func (s *SQLiteAdminDB) DeleteUser(id string) error {
	err := sqlitex.ExecuteTransient(s.conn, "DELETE FROM users WHERE id = ?", &sqlitex.ExecOptions{
		Args: []interface{}{id},
	})

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
	var found bool

	err := sqlitex.ExecuteTransient(s.conn, "SELECT user_count FROM users_count LIMIT 1", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			hasRow, err := stmt.Step()
			if err != nil {
				return err
			}
			if hasRow {
				found = true
				countStr = stmt.ColumnText(0)
			}
			return nil
		},
	})

	if err != nil {
		s.logger.Errorf("Failed to get users count: %v", err)
		return 0, fmt.Errorf("failed to get users count: %w", err)
	}

	if !found {
		// Table might be empty, return 0
		return 0, nil
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

	err := sqlitex.ExecuteTransient(s.conn, `
		INSERT INTO users_count (id, user_count, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			user_count = excluded.user_count,
			updated_at = excluded.updated_at
	`, &sqlitex.ExecOptions{
		Args: []interface{}{
			userCountId,
			countStr,
			now,
			now,
		},
	})

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
	var found bool

	err := sqlitex.ExecuteTransient(s.conn, "SELECT event_count FROM events_count LIMIT 1", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			hasRow, err := stmt.Step()
			if err != nil {
				return err
			}
			if hasRow {
				found = true
				countStr = stmt.ColumnText(0)
			}
			return nil
		},
	})

	if err != nil {
		s.logger.Errorf("Failed to get events count: %v", err)
		return 0, fmt.Errorf("failed to get events count: %w", err)
	}

	if !found {
		// Fall back to counting directly
		var count int
		err := sqlitex.ExecuteTransient(s.conn, "SELECT COUNT(*) FROM orisun_es_event", &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				hasRow, err := stmt.Step()
				if err != nil {
					return err
				}
				if hasRow {
					count = int(stmt.ColumnInt64(0))
				}
				return nil
			},
		})
		if err != nil {
			s.logger.Errorf("Failed to get events count: %v", err)
			return 0, fmt.Errorf("failed to get events count: %w", err)
		}
		return count, nil
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

	err := sqlitex.ExecuteTransient(s.conn, `
		INSERT INTO events_count (id, event_count, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			event_count = excluded.event_count,
			updated_at = excluded.updated_at
	`, &sqlitex.ExecOptions{
		Args: []interface{}{
			eventCountId,
			countStr,
			now,
			now,
		},
	})

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
	var found bool

	err := sqlitex.ExecuteTransient(s.conn, "SELECT COALESCE(commit_position, 0), COALESCE(prepare_position, 0) FROM projector_checkpoint WHERE name = ?", &sqlitex.ExecOptions{
		Args: []interface{}{projectorName},
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
		s.logger.Errorf("Failed to get projector position: %v", err)
		return nil, fmt.Errorf("failed to get projector position: %w", err)
	}

	if !found {
		// Return default position if not found
		return &orisun.Position{
			CommitPosition:  0,
			PreparePosition: 0,
		}, nil
	}

	return &orisun.Position{
		CommitPosition:  commitPos,
		PreparePosition: preparePos,
	}, nil
}

// UpdateProjectorPosition updates the position for a projector
func (p *SQLiteAdminDB) UpdateProjectorPosition(name string, position *orisun.Position) error {
	now := time.Now().Unix()

	err := sqlitex.ExecuteTransient(p.conn, `
		INSERT INTO projector_checkpoint (name, commit_position, prepare_position, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			commit_position = excluded.commit_position,
			prepare_position = excluded.prepare_position,
			updated_at = excluded.updated_at
	`, &sqlitex.ExecOptions{
		Args: []interface{}{
			name,
			position.CommitPosition,
			position.PreparePosition,
			now,
		},
	})

	if err != nil {
		p.logger.Errorf("Failed to update projector position: %v", err)
		return fmt.Errorf("failed to update projector position: %w", err)
	}

	p.logger.Debugf("Successfully updated projector position: %s", name)
	return nil
}
