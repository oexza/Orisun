package sqlite

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oexza/Orisun/logging"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

//go:embed scripts/*.sql
var sqlFiles embed.FS

// RunDbScripts initializes the SQLite database schema
func RunDbScripts(conn *sqlite.Conn, dbPath string, logger logging.Logger) error {
	logger.Infof("Running SQLite database migrations for: %s", dbPath)

	// Ensure the database directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	// Read and execute the SQL schema
	schemaContent, err := sqlFiles.ReadFile("scripts/db_scripts.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	// Execute the schema in a transaction
	err = sqlitex.ExecuteTransient(conn, string(schemaContent), nil)
	if err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	logger.Infof("SQLite database migrations completed successfully for: %s", dbPath)
	return nil
}

// EnsureDatabaseExists creates the database file and runs migrations if needed
func EnsureDatabaseExists(dbPath string, logger logging.Logger) (*sqlite.Conn, error) {
	// Ensure the database directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open the database (creates it if it doesn't exist)
	// URI parameters: https://www.sqlite.org/c3ref/open.html
	conn, err := sqlite.OpenConn(dbPath+"?_foreign_keys=on&_journal_mode=WAL", 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Apply performance optimizations
	if err := ApplyPerformanceOptimizations(conn, logger); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to apply performance optimizations: %w", err)
	}

	return conn, nil
}

// ApplyPerformanceOptimizations configures SQLite for optimal performance
func ApplyPerformanceOptimizations(conn *sqlite.Conn, logger logging.Logger) error {
	optimizations := []struct {
		name string
		sql  string
	}{
		{
			name: "Write-Ahead Logging",
			sql:  "PRAGMA journal_mode = WAL;",
		},
		{
			name: "Synchronous NORMAL",
			sql:  "PRAGMA synchronous = NORMAL;",
		},
		{
			name: "Page cache size (256MB)",
			sql:  "PRAGMA cache_size = -262144;",
		},
		{
			name: "Memory-mapped I/O (256MB)",
			sql:  "PRAGMA mmap_size = 268435456;",
		},
		{
			name: "Temp store in memory",
			sql:  "PRAGMA temp_store = MEMORY;",
		},
		{
			name: "Busy timeout (30 seconds)",
			sql:  "PRAGMA busy_timeout = 30000000000;",
		},
		{
			name: "WAL autocheckpoint (1000 pages)",
			sql:  "PRAGMA wal_autocheckpoint = 1000;",
		},
	}

	for _, opt := range optimizations {
		err := sqlitex.ExecuteTransient(conn, opt.sql, nil)
		if err != nil {
			return fmt.Errorf("failed to set %s: %w", opt.name, err)
		}
		logger.Infof("Applied SQLite optimization: %s", opt.name)
	}

	return nil
}
