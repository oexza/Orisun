package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oexza/Orisun/logging"
	eventstore "github.com/oexza/Orisun/orisun"
)

// InitializeSQLiteDatabase initializes the SQLite event store
func InitializeSQLiteDatabase(
	ctx context.Context,
	dbPath string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, interface{}, eventstore.EventPublishingTracker) {

	logger.Info("Initializing SQLite Event Store")

	// Create database directory and file
	db, err := EnsureDatabaseExists(dbPath, logger)
	if err != nil {
		logger.Fatalf("Failed to create database: %v", err)
	}

	// Configure connection pool
	// SQLite works best with a single connection for writes, but we can use a pool for reads
	db.SetMaxOpenConns(25)                      // Maximum number of open connections
	db.SetMaxIdleConns(5)                       // Maximum number of idle connections
	db.SetConnMaxIdleTime(5 * 60 * 1000000000)  // 5 minutes
	db.SetConnMaxLifetime(30 * 60 * 1000000000) // 30 minutes

	// Run database migrations
	if err := RunDbScripts(db, dbPath, logger); err != nil {
		logger.Fatalf("Failed to run database migrations: %v", err)
	}

	// Initialize locks table
	if err := InitializeLocksTable(db); err != nil {
		logger.Fatalf("Failed to initialize locks table: %v", err)
	}

	logger.Infof("SQLite database initialized: %s", dbPath)

	// Create save events instance
	saveEvents := NewSQLiteSaveEvents(db, logger)

	// Create get events instance
	getEvents := NewSQLiteGetEvents(db, logger)

	// Create lock provider
	lockProvider := NewSQLiteLockProvider(db, logger)

	// Create admin DB (for user management, projector checkpoints, etc.)
	adminDB := NewSQLiteAdminDB(db, logger)

	// Create event publishing tracker
	eventPublishingTracker := NewSQLiteEventPublishingTracker(db, logger)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down SQLite database connections")
		if err := db.Close(); err != nil {
			logger.Errorf("Error closing database: %v", err)
		}
	}()

	return saveEvents, getEvents, lockProvider, adminDB, eventPublishingTracker
}

// InitializeSQLiteDatabaseWithPools initializes SQLite with separate read/write pools
// Note: SQLite doesn't truly support concurrent writes, so this primarily optimizes read operations
func InitializeSQLiteDatabaseWithPools(
	ctx context.Context,
	dbPath string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, interface{}, eventstore.EventPublishingTracker) {

	logger.Info("Initializing SQLite Event Store with connection pooling")

	// Create database
	db, err := EnsureDatabaseExists(dbPath, logger)
	if err != nil {
		logger.Fatalf("Failed to create database: %v", err)
	}

	// Run migrations
	if err := RunDbScripts(db, dbPath, logger); err != nil {
		logger.Fatalf("Failed to run database migrations: %v", err)
	}

	// Initialize locks table
	if err := InitializeLocksTable(db); err != nil {
		logger.Fatalf("Failed to initialize locks table: %v", err)
	}

	// Configure write pool (single connection to prevent write conflicts)
	// SQLite allows only one writer at a time
	writeDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_timeout=5000")
	if err != nil {
		logger.Fatalf("Failed to open write database: %v", err)
	}
	writeDB.SetMaxOpenConns(1) // Single writer
	writeDB.SetMaxIdleConns(1)

	// Configure read pool (multiple connections for read operations)
	readDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_timeout=5000&_mode=ro")
	if err != nil {
		logger.Fatalf("Failed to open read database: %v", err)
	}
	readDB.SetMaxOpenConns(10)
	readDB.SetMaxIdleConns(5)

	// Configure admin pool
	adminDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		logger.Fatalf("Failed to open admin database: %v", err)
	}
	adminDB.SetMaxOpenConns(5)
	adminDB.SetMaxIdleConns(2)

	logger.Infof("SQLite connection pools configured: write=1, read=10, admin=5")

	// Create instances with appropriate pools
	saveEvents := NewSQLiteSaveEvents(writeDB, logger)
	getEvents := NewSQLiteGetEvents(readDB, logger)
	lockProvider := NewSQLiteLockProvider(adminDB, logger)
	adminDBInstance := NewSQLiteAdminDB(adminDB, logger)
	eventPublishingTracker := NewSQLiteEventPublishingTracker(writeDB, logger)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down SQLite database connections")
		var wg sync.WaitGroup
		dbs := []*sql.DB{writeDB, readDB, adminDB}

		for _, db := range dbs {
			wg.Add(1)
			go func(d *sql.DB) {
				defer wg.Done()
				if err := d.Close(); err != nil {
					logger.Errorf("Error closing database: %v", err)
				}
			}(db)
		}
		wg.Wait()
	}()

	return saveEvents, getEvents, lockProvider, adminDBInstance, eventPublishingTracker
}

// GetSQLiteDBConfig returns the default SQLite database configuration
func GetSQLiteDBConfig(dataDir string) string {
	return fmt.Sprintf("%s/orisun.db", dataDir)
}
