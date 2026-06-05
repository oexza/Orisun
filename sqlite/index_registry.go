package sqlite

import (
	"fmt"
	"strings"
	"sync"

	"github.com/goccy/go-json"
	eventstore "github.com/oexza/Orisun/orisun"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

type sqliteIndexRegistry struct {
	mu                   sync.RWMutex
	fieldTypesByBoundary map[string]map[string]string
}

func newSqliteIndexRegistry() *sqliteIndexRegistry {
	return &sqliteIndexRegistry{
		fieldTypesByBoundary: make(map[string]map[string]string),
	}
}

func (r *sqliteIndexRegistry) fieldType(boundary, key string) string {
	if r == nil {
		return "text"
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if fields := r.fieldTypesByBoundary[boundary]; fields != nil {
		if valueType := fields[key]; valueType != "" {
			return valueType
		}
	}
	return "text"
}

func (r *sqliteIndexRegistry) replaceBoundaryFields(boundary string, fields map[string]string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(fields) == 0 {
		delete(r.fieldTypesByBoundary, boundary)
		return
	}
	r.fieldTypesByBoundary[boundary] = fields
}

func normalizeIndexValueType(valueType string) string {
	switch strings.ToLower(strings.TrimSpace(valueType)) {
	case "numeric":
		return "numeric"
	case "boolean":
		return "boolean"
	case "timestamptz":
		return "timestamptz"
	default:
		return "text"
	}
}

func loadBoundaryIndexMetadata(conn *sqlite.Conn, boundary string, registry *sqliteIndexRegistry) error {
	fieldsByKey := make(map[string]string)
	err := sqlitex.Execute(conn,
		"SELECT fields FROM orisun_boundary_index_metadata ORDER BY name",
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				var fields []eventstore.BoundaryIndexField
				if err := json.Unmarshal([]byte(stmt.ColumnText(0)), &fields); err != nil {
					return fmt.Errorf("decode index metadata fields: %w", err)
				}
				for _, field := range fields {
					if field.JsonKey == "" {
						continue
					}
					fieldsByKey[field.JsonKey] = normalizeIndexValueType(field.ValueType)
				}
				return nil
			},
		})
	if err != nil {
		return err
	}
	registry.replaceBoundaryFields(boundary, fieldsByKey)
	return nil
}
