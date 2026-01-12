package sqlite

import (
	"context"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oexza/Orisun/logging"
	eventstore "github.com/oexza/Orisun/orisun"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// InitializeSQLiteDatabase initializes the SQLite event store
func InitializeSQLiteDatabase(
	ctx context.Context,
	dbPath string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, interface{}, eventstore.EventPublishingTracker) {

	logger.Info("Initializing SQLite Event Store")

	// Create a pool of connections
	pool, err := sqlitex.NewPool(dbPath+"?_foreign_keys=on&_journal_mode=WAL", sqlitex.PoolOptions{
		PoolSize: 4,
	})
	if err != nil {
		logger.Fatalf("Failed to create database pool: %v", err)
	}

	// Get a connection from the pool for initialization
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	// Run database migrations
	if err := RunDbScripts(conn, dbPath, logger); err != nil {
		logger.Fatalf("Failed to run database migrations: %v", err)
	}

	// Initialize locks table
	if err := InitializeLocksTable(conn); err != nil {
		logger.Fatalf("Failed to initialize locks table: %v", err)
	}

	logger.Infof("SQLite database initialized: %s", dbPath)

	// Create save events instance with pool
	saveEvents := NewSQLiteSaveEvents(pool, logger)

	// Create get events instance (uses a separate connection directly)
	getConn, err := sqlite.OpenConn(dbPath+"?mode=ro", 0)
	if err != nil {
		logger.Fatalf("Failed to open read connection: %v", err)
	}
	getEvents := NewSQLiteGetEvents(getConn, logger)

	// Create lock provider (uses a separate connection directly)
	lockConn, err := sqlite.OpenConn(dbPath, 0)
	if err != nil {
		logger.Fatalf("Failed to open lock connection: %v", err)
	}
	lockProvider := NewSQLiteLockProvider(lockConn, logger)

	// Create admin DB (for user management, projector checkpoints, etc.)
	adminConn, err := sqlite.OpenConn(dbPath, 0)
	if err != nil {
		logger.Fatalf("Failed to open admin connection: %v", err)
	}
	adminDB := NewSQLiteAdminDB(adminConn, logger)

	// Create event publishing tracker
	trackerConn, err := sqlite.OpenConn(dbPath, 0)
	if err != nil {
		logger.Fatalf("Failed to open tracker connection: %v", err)
	}
	eventPublishingTracker := NewSQLiteEventPublishingTracker(trackerConn, logger)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down SQLite database connections")
		if err := pool.Close(); err != nil {
			logger.Errorf("Error closing pool: %v", err)
		}
		if err := getConn.Close(); err != nil {
			logger.Errorf("Error closing get connection: %v", err)
		}
		if err := lockConn.Close(); err != nil {
			logger.Errorf("Error closing lock connection: %v", err)
		}
		if err := adminConn.Close(); err != nil {
			logger.Errorf("Error closing admin connection: %v", err)
		}
		if err := trackerConn.Close(); err != nil {
			logger.Errorf("Error closing tracker connection: %v", err)
		}
	}()

	return saveEvents, getEvents, lockProvider, adminDB, eventPublishingTracker
}

// InitializeSQLiteDatabaseWithPools initializes SQLite with separate read/write pools
// Note: With zombiezen/go-sqlite, we manage connections directly instead of using database/sql pools
func InitializeSQLiteDatabaseWithPools(
	ctx context.Context,
	dbPath string,
	js jetstream.JetStream,
	logger logging.Logger,
) (eventstore.EventsSaver, eventstore.EventsRetriever, eventstore.LockProvider, interface{}, eventstore.EventPublishingTracker) {

	logger.Info("Initializing SQLite Event Store with separate connections")

	// Create write pool (single writer for SQLite)
	writePool, err := sqlitex.NewPool(dbPath+"?_foreign_keys=on&_journal_mode=WAL", sqlitex.PoolOptions{
		PoolSize: 1,
	})
	if err != nil {
		logger.Fatalf("Failed to create write database pool: %v", err)
	}

	// Get a connection from the write pool for migrations
	writeConn := writePool.Get(ctx)
	defer writePool.Put(writeConn)

	// Run migrations on write connection
	if err := RunDbScripts(writeConn, dbPath, logger); err != nil {
		logger.Fatalf("Failed to run database migrations: %v", err)
	}

	// Initialize locks table
	if err := InitializeLocksTable(writeConn); err != nil {
		logger.Fatalf("Failed to initialize locks table: %v", err)
	}

	// Create read connection (for read operations)
	// Open in read-only mode
	readConn, err := sqlite.OpenConn(dbPath+"?mode=ro", 0)
	if err != nil {
		logger.Fatalf("Failed to open read database: %v", err)
	}

	// Create admin connection
	adminConn, err := sqlite.OpenConn(dbPath, 0)
	if err != nil {
		logger.Fatalf("Failed to open admin database: %v", err)
	}

	logger.Infof("SQLite connections configured: write pool=1, read=1, admin=1")

	// Create instances with appropriate connections/pools
	saveEvents := NewSQLiteSaveEvents(writePool, logger)
	getEvents := NewSQLiteGetEvents(readConn, logger)
	lockProvider := NewSQLiteLockProvider(adminConn, logger)
	adminDBInstance := NewSQLiteAdminDB(adminConn, logger)

	// Create tracker connection
	trackerConn, err := sqlite.OpenConn(dbPath, 0)
	if err != nil {
		logger.Fatalf("Failed to open tracker connection: %v", err)
	}
	eventPublishingTracker := NewSQLiteEventPublishingTracker(trackerConn, logger)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		logger.Info("Shutting down SQLite database connections")
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writePool.Close(); err != nil {
				logger.Errorf("Error closing write pool: %v", err)
			}
		}()

		conns := []*sqlite.Conn{readConn, adminConn, trackerConn}
		for _, conn := range conns {
			wg.Add(1)
			go func(c *sqlite.Conn) {
				defer wg.Done()
				if err := c.Close(); err != nil {
					logger.Errorf("Error closing database: %v", err)
				}
			}(conn)
		}
		wg.Wait()
	}()

	return saveEvents, getEvents, lockProvider, adminDBInstance, eventPublishingTracker
}

// GetSQLiteDBConfig returns the default SQLite database configuration
func GetSQLiteDBConfig(dataDir string) string {
	return fmt.Sprintf("%s/orisun.db", dataDir)
}
