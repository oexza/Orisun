package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oexza/Orisun/logging"
)

//go:embed scripts/*.sql
var sqlFiles embed.FS

// RunDbScripts initializes the SQLite database schema
func RunDbScripts(db *sql.DB, dbPath string, logger logging.Logger) error {
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
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Execute the schema
	if _, err := tx.Exec(string(schemaContent)); err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema: %w", err)
	}

	logger.Infof("SQLite database migrations completed successfully for: %s", dbPath)
	return nil
}

// EnsureDatabaseExists creates the database file and runs migrations if needed
func EnsureDatabaseExists(dbPath string, logger logging.Logger) (*sql.DB, error) {
	// Ensure the database directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open the database (creates it if it doesn't exist)
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Apply performance optimizations
	if err := ApplyPerformanceOptimizations(db, logger); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply performance optimizations: %w", err)
	}

	return db, nil
}

// ApplyPerformanceOptimizations configures SQLite for optimal performance
func ApplyPerformanceOptimizations(db *sql.DB, logger logging.Logger) error {
	optimizations := []struct {
		name string
		sql  string
	}{
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
	}

	for _, opt := range optimizations {
		if _, err := db.Exec(opt.sql); err != nil {
			return fmt.Errorf("failed to set %s: %w", opt.name, err)
		}
		logger.Infof("Applied SQLite optimization: %s", opt.name)
	}

	return nil
}
