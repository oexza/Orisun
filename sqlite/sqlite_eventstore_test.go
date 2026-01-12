package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oexza/Orisun/logging"
	"github.com/oexza/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Test helper functions

func getTestDB(t *testing.T) *sqlitex.Pool {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	// Create connection pool
	pool, err := sqlitex.NewPool(dbPath+"?_foreign_keys=on&_journal_mode=WAL", sqlitex.PoolOptions{
		PoolSize: 1,
	})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Get connection for initialization
	conn := pool.Get(context.Background())
	defer pool.Put(conn)

	// Run migrations
	logger, _ := logging.ZapLogger("info")
	err = RunDbScripts(conn, dbPath, logger)
	if err != nil {
		pool.Close()
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize locks table
	err = InitializeLocksTable(conn)
	if err != nil {
		pool.Close()
		t.Fatalf("Failed to initialize locks table: %v", err)
	}

	return pool
}

func getBenchmarkDB(b *testing.B) *sqlitex.Pool {
	b.Helper()

	// Create a temporary file-based database for realistic benchmarking
	tmpDir := b.TempDir()
	dbPath := tmpDir + "/benchmark.db"

	// Create connection pool with optimizations
	pool, err := sqlitex.NewPool(dbPath+"?_foreign_keys=on&_journal_mode=WAL", sqlitex.PoolOptions{
		PoolSize: 1,
	})
	if err != nil {
		b.Fatalf("Failed to open benchmark database: %v", err)
	}

	// Get connection for initialization
	conn := pool.Get(context.Background())
	defer pool.Put(conn)

	// Apply production-level performance optimizations for event sourcing
	err = ApplyPerformanceOptimizations(conn, getTestLogger())
	if err != nil {
		pool.Close()
		b.Fatalf("Failed to apply performance optimizations: %v", err)
	}

	// Run migrations
	logger, _ := logging.ZapLogger("info")
	err = RunDbScripts(conn, dbPath, logger)
	if err != nil {
		pool.Close()
		b.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize locks table
	err = InitializeLocksTable(conn)
	if err != nil {
		pool.Close()
		b.Fatalf("Failed to initialize locks table: %v", err)
	}

	return pool
}

// Helper to get a read connection from pool for GetEvents
func getReadConn(pool *sqlitex.Pool) *sqlite.Conn {
	conn := pool.Get(context.Background())
	return conn
}

// Helper to get a connection from pool for admin operations
func getAdminConn(pool *sqlitex.Pool) *sqlite.Conn {
	conn := pool.Get(context.Background())
	return conn
}

// Helper to get a connection from pool for lock provider
func getLockConn(pool *sqlitex.Pool) *sqlite.Conn {
	conn := pool.Get(context.Background())
	return conn
}

func getTestLogger() logging.Logger {
	logger, _ := logging.ZapLogger("info")
	return logger
}

func getTestContext() context.Context {
	return context.Background()
}

// SQLiteSaveEvents Tests

func TestSQLiteSaveEvents_Save(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	events := []orisun.EventWithMapTags{
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent",
			Data: map[string]interface{}{
				"username": "testuser",
				"action":   "login",
			},
			Metadata: map[string]interface{}{
				"timestamp": time.Now().Unix(),
			},
		},
	}

	// Test basic save
	txID, globalID, err := saveEvents.Save(ctx, events, "test_boundary", nil, nil)
	if err != nil {
		t.Fatalf("Failed to save events: %v", err)
	}

	if txID == "" {
		t.Error("Expected non-empty transaction ID")
	}

	if globalID <= 0 {
		t.Errorf("Expected positive global ID, got %d", globalID)
	}
}

func TestSQLiteSaveEvents_SaveMultiple(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	events := []orisun.EventWithMapTags{
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent1",
			Data:      map[string]interface{}{"seq": 1},
		},
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent2",
			Data:      map[string]interface{}{"seq": 2},
		},
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent3",
			Data:      map[string]interface{}{"seq": 3},
		},
	}

	txID, globalID, err := saveEvents.Save(ctx, events, "test_boundary", nil, nil)
	if err != nil {
		t.Fatalf("Failed to save events: %v", err)
	}

	if txID == "" {
		t.Error("Expected non-empty transaction ID")
	}

	if globalID <= 0 {
		t.Errorf("Expected positive global ID, got %d", globalID)
	}

	// Verify all events were saved
	readConn := pool.Get(ctx)
	defer pool.Put(readConn)
	getEvents := NewSQLiteGetEvents(readConn, getTestLogger())
	resp, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Count:    100,
		Boundary: "test_boundary",
	})
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(resp.Events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(resp.Events))
	}
}

func TestSQLiteSaveEvents_SaveEmptyEvents(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	_, _, err := saveEvents.Save(ctx, []orisun.EventWithMapTags{}, "test_boundary", nil, nil)
	if err == nil {
		t.Error("Expected error when saving empty events array")
	}

	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument code, got %v", status.Code(err))
	}
}

func TestSQLiteSaveEvents_OptimisticConcurrency(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	// Save initial event
	events := []orisun.EventWithMapTags{
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent",
			Data: map[string]interface{}{
				"username": "testuser",
				"status":   "active",
			},
		},
	}

	txID, globalID, err := saveEvents.Save(ctx, events, "test_boundary", nil, nil)
	if err != nil {
		t.Fatalf("Failed to save initial events: %v", err)
	}

	// Parse transaction ID
	var txIDInt int64
	_, err = fmt.Sscanf(txID, "%d", &txIDInt)
	if err != nil {
		t.Fatalf("Failed to parse transaction ID: %v", err)
	}

	// Try to save with wrong expected position
	criteria := &orisun.Query{
		Criteria: []*orisun.Criterion{
			{
				Tags: []*orisun.Tag{
					{Key: "username", Value: "testuser"},
				},
			},
		},
	}

	expectedPosition := &orisun.Position{
		CommitPosition:  txIDInt,
		PreparePosition: globalID + 100, // Wrong position
	}

	events2 := []orisun.EventWithMapTags{
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent2",
			Data: map[string]interface{}{
				"username": "testuser",
				"status":   "inactive",
			},
		},
	}

	_, _, err = saveEvents.Save(ctx, events2, "test_boundary", expectedPosition, criteria)
	if err == nil {
		t.Error("Expected optimistic concurrency error")
	}

	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("Expected AlreadyExists code, got %v", status.Code(err))
	}
}

// SQLiteGetEvents Tests

func TestSQLiteGetEvents_Get(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	// Save test events
	events := []orisun.EventWithMapTags{
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent1",
			Data:      map[string]interface{}{"seq": 1},
		},
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent2",
			Data:      map[string]interface{}{"seq": 2},
		},
	}

	_, _, err := saveEvents.Save(ctx, events, "test_boundary", nil, nil)
	if err != nil {
		t.Fatalf("Failed to save events: %v", err)
	}

	// Get events
	readConn := pool.Get(ctx)
	defer pool.Put(readConn)
	getEvents := NewSQLiteGetEvents(readConn, getTestLogger())
	resp, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Count:    10,
		Boundary: "test_boundary",
	})
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(resp.Events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(resp.Events))
	}

	if resp.Events[0].EventType != "TestEvent1" {
		t.Errorf("Expected EventType TestEvent1, got %s", resp.Events[0].EventType)
	}
}

func TestSQLiteGetEvents_GetWithCriteria(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	// Save test events
	events := []orisun.EventWithMapTags{
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent",
			Data: map[string]interface{}{
				"username": "user1",
				"action":   "login",
			},
		},
		{
			EventId:   uuid.New().String(),
			EventType: "TestEvent",
			Data: map[string]interface{}{
				"username": "user2",
				"action":   "logout",
			},
		},
	}

	_, _, err := saveEvents.Save(ctx, events, "test_boundary", nil, nil)
	if err != nil {
		t.Fatalf("Failed to save events: %v", err)
	}

	// Get events with criteria
	readConn := pool.Get(ctx)
	defer pool.Put(readConn)
	getEvents := NewSQLiteGetEvents(readConn, getTestLogger())
	resp, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Count:    10,
		Boundary: "test_boundary",
		Query: &orisun.Query{
			Criteria: []*orisun.Criterion{
				{
					Tags: []*orisun.Tag{
						{Key: "username", Value: "user1"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to get events with criteria: %v", err)
	}

	if len(resp.Events) != 1 {
		t.Errorf("Expected 1 event matching criteria, got %d", len(resp.Events))
	}
}

func TestSQLiteGetEvents_GetWithPagination(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	// Save test events
	events := []orisun.EventWithMapTags{
		{EventId: uuid.New().String(), EventType: "Event1", Data: map[string]interface{}{"seq": 1}},
		{EventId: uuid.New().String(), EventType: "Event2", Data: map[string]interface{}{"seq": 2}},
		{EventId: uuid.New().String(), EventType: "Event3", Data: map[string]interface{}{"seq": 3}},
		{EventId: uuid.New().String(), EventType: "Event4", Data: map[string]interface{}{"seq": 4}},
		{EventId: uuid.New().String(), EventType: "Event5", Data: map[string]interface{}{"seq": 5}},
	}

	_, _, err := saveEvents.Save(ctx, events, "test_boundary", nil, nil)
	if err != nil {
		t.Fatalf("Failed to save events: %v", err)
	}

	// Get first page
	readConn := pool.Get(ctx)
	getEvents := NewSQLiteGetEvents(readConn, getTestLogger())
	resp1, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Count:     2,
		Boundary:  "test_boundary",
		Direction: orisun.Direction_ASC,
	})
	if err != nil {
		t.Fatalf("Failed to get first page: %v", err)
	}

	if len(resp1.Events) != 2 {
		t.Errorf("Expected 2 events in first page, got %d", len(resp1.Events))
	}

	// Get second page using position
	lastEvent := resp1.Events[len(resp1.Events)-1]
	resp2, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Count:        2,
		Boundary:     "test_boundary",
		Direction:    orisun.Direction_ASC,
		FromPosition: lastEvent.Position,
	})
	if err != nil {
		t.Fatalf("Failed to get second page: %v", err)
	}

	if len(resp2.Events) != 2 {
		t.Errorf("Expected 2 events in second page, got %d", len(resp2.Events))
	}

	// Verify we got different events
	if resp1.Events[1].EventId == resp2.Events[0].EventId {
		t.Error("Expected different events on different pages")
	}

	pool.Put(readConn)
}

func TestSQLiteGetEvents_GetDescending(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	// Save test events
	events := []orisun.EventWithMapTags{
		{EventId: uuid.New().String(), EventType: "Event1", Data: map[string]interface{}{"seq": 1}},
		{EventId: uuid.New().String(), EventType: "Event2", Data: map[string]interface{}{"seq": 2}},
		{EventId: uuid.New().String(), EventType: "Event3", Data: map[string]interface{}{"seq": 3}},
	}

	_, _, err := saveEvents.Save(ctx, events, "test_boundary", nil, nil)
	if err != nil {
		t.Fatalf("Failed to save events: %v", err)
	}

	// Get events in descending order
	readConn := pool.Get(ctx)
	defer pool.Put(readConn)
	getEvents := NewSQLiteGetEvents(readConn, getTestLogger())
	resp, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
		Count:     10,
		Boundary:  "test_boundary",
		Direction: orisun.Direction_DESC,
	})
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(resp.Events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(resp.Events))
	}

	// Verify descending order
	if resp.Events[0].Position.PreparePosition < resp.Events[1].Position.PreparePosition {
		t.Error("Expected events in descending order")
	}
}

// SQLiteLockProvider Tests

func TestSQLiteLockProvider_Lock(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	ctx := getTestContext()

	lockConn := pool.Get(ctx)
	defer pool.Put(lockConn)
	lockProvider := NewSQLiteLockProvider(lockConn, getTestLogger())

	lockName := "test_lock"

	// Acquire lock
	err := lockProvider.Lock(ctx, lockName)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Try to acquire same lock again (should fail)
	err = lockProvider.Lock(ctx, lockName)
	if err == nil {
		t.Error("Expected error when trying to acquire already held lock")
	}

	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("Expected AlreadyExists code, got %v", status.Code(err))
	}
}

func TestSQLiteLockProvider_LockTimeout(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	ctx := getTestContext()

	lockName := "test_lock_timeout"

	// Manually insert an expired lock
	lockConn := pool.Get(ctx)
	err := sqlitex.ExecuteTransient(lockConn, `
		INSERT INTO locks (lock_name, locked_at, locked_by)
		VALUES (?, ?, ?)
	`, &sqlitex.ExecOptions{
		Args: []interface{}{lockName, time.Now().Unix() - 35000, "other_process"},
	})
	if err != nil {
		t.Fatalf("Failed to insert expired lock: %v", err)
	}

	// Should be able to acquire lock (old one expired)
	lockProvider := NewSQLiteLockProvider(lockConn, getTestLogger())
	err = lockProvider.Lock(ctx, lockName)
	if err != nil {
		t.Fatalf("Failed to acquire expired lock: %v", err)
	}

	pool.Put(lockConn)
}

func TestSQLiteLockProvider_Unlock(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	ctx := getTestContext()

	lockConn := pool.Get(ctx)
	defer pool.Put(lockConn)
	lockProvider := NewSQLiteLockProvider(lockConn, getTestLogger())

	lockName := "test_unlock"

	// Acquire lock
	err := lockProvider.Lock(ctx, lockName)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}

	// Release lock
	err = lockProvider.Unlock(ctx, lockName)
	if err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}

	// Should be able to acquire lock again
	err = lockProvider.Lock(ctx, lockName)
	if err != nil {
		t.Fatalf("Failed to acquire lock after unlock: %v", err)
	}
}

// SQLiteAdminDB Tests

func TestSQLiteAdminDB_UserCRUD(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	adminConn := pool.Get(context.Background())
	defer pool.Put(adminConn)
	adminDB := NewSQLiteAdminDB(adminConn, getTestLogger())

	// Create user
	user := orisun.User{
		Id:             uuid.New().String(),
		Name:           "Test User",
		Username:       "testuser",
		HashedPassword: "hashed_password",
		Roles:          []orisun.Role{orisun.RoleAdmin},
	}

	err := adminDB.UpsertUser(user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Get user by username
	retrieved, err := adminDB.GetUserByUsername("testuser")
	if err != nil {
		t.Fatalf("Failed to get user by username: %v", err)
	}

	if retrieved.Name != "Test User" {
		t.Errorf("Expected name 'Test User', got '%s'", retrieved.Name)
	}

	// Get user by ID
	retrieved2, err := adminDB.GetUserById(user.Id)
	if err != nil {
		t.Fatalf("Failed to get user by ID: %v", err)
	}

	if retrieved2.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", retrieved2.Username)
	}

	// List users
	users, err := adminDB.ListAdminUsers()
	if err != nil {
		t.Fatalf("Failed to list users: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(users))
	}

	// Delete user
	err = adminDB.DeleteUser(user.Id)
	if err != nil {
		t.Fatalf("Failed to delete user: %v", err)
	}

	// Verify deletion
	_, err = adminDB.GetUserById(user.Id)
	if err == nil {
		t.Error("Expected error when getting deleted user")
	}
}

func TestSQLiteAdminDB_UserCount(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	adminConn := pool.Get(context.Background())
	defer pool.Put(adminConn)
	adminDB := NewSQLiteAdminDB(adminConn, getTestLogger())

	// Save user count
	err := adminDB.SaveUsersCount(42)
	if err != nil {
		t.Fatalf("Failed to save users count: %v", err)
	}

	// Get user count
	count, err := adminDB.GetUsersCount()
	if err != nil {
		t.Fatalf("Failed to get users count: %v", err)
	}

	if count != 42 {
		t.Errorf("Expected count 42, got %d", count)
	}
}

func TestSQLiteAdminDB_EventCount(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	adminConn := pool.Get(context.Background())
	defer pool.Put(adminConn)
	adminDB := NewSQLiteAdminDB(adminConn, getTestLogger())

	// Save event count
	err := adminDB.SaveEventCount(100, "test_boundary")
	if err != nil {
		t.Fatalf("Failed to save events count: %v", err)
	}

	// Get event count
	count, err := adminDB.GetEventsCount("test_boundary")
	if err != nil {
		t.Fatalf("Failed to get events count: %v", err)
	}

	if count != 100 {
		t.Errorf("Expected count 100, got %d", count)
	}
}

func TestSQLiteAdminDB_ProjectorPosition(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	adminConn := pool.Get(context.Background())
	defer pool.Put(adminConn)
	adminDB := NewSQLiteAdminDB(adminConn, getTestLogger())

	projectorName := "test_projector"

	// Update projector position
	position := &orisun.Position{
		CommitPosition:  100,
		PreparePosition: 200,
	}

	err := adminDB.UpdateProjectorPosition(projectorName, position)
	if err != nil {
		t.Fatalf("Failed to update projector position: %v", err)
	}

	// Get projector position
	retrieved, err := adminDB.GetProjectorLastPosition(projectorName)
	if err != nil {
		t.Fatalf("Failed to get projector position: %v", err)
	}

	if retrieved.CommitPosition != 100 {
		t.Errorf("Expected commit position 100, got %d", retrieved.CommitPosition)
	}

	if retrieved.PreparePosition != 200 {
		t.Errorf("Expected prepare position 200, got %d", retrieved.PreparePosition)
	}
}

// SQLiteEventPublishingTracker Tests

func TestSQLiteEventPublishingTracker_PositionTracking(t *testing.T) {
	pool := getTestDB(t)
	defer pool.Close()

	ctx := getTestContext()

	trackerConn := pool.Get(ctx)
	defer pool.Put(trackerConn)
	tracker := NewSQLiteEventPublishingTracker(trackerConn, getTestLogger())

	boundary := "test_boundary"

	// Get initial position (should be 0, 0)
	initialPos, err := tracker.GetLastPublishedEventPosition(ctx, boundary)
	if err != nil {
		t.Fatalf("Failed to get initial position: %v", err)
	}

	if initialPos.CommitPosition != 0 || initialPos.PreparePosition != 0 {
		t.Errorf("Expected initial position (0, 0), got (%d, %d)",
			initialPos.CommitPosition, initialPos.PreparePosition)
	}

	// Update position
	err = tracker.InsertLastPublishedEvent(ctx, boundary, 100, 200)
	if err != nil {
		t.Fatalf("Failed to update position: %v", err)
	}

	// Get updated position
	updatedPos, err := tracker.GetLastPublishedEventPosition(ctx, boundary)
	if err != nil {
		t.Fatalf("Failed to get updated position: %v", err)
	}

	if updatedPos.CommitPosition != 100 {
		t.Errorf("Expected commit position 100, got %d", updatedPos.CommitPosition)
	}

	if updatedPos.PreparePosition != 200 {
		t.Errorf("Expected prepare position 200, got %d", updatedPos.PreparePosition)
	}
}

// Benchmark Tests

func BenchmarkSaveEvents(b *testing.B) {
	pool := getBenchmarkDB(b)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		events := []orisun.EventWithMapTags{
			{
				EventId:   uuid.New().String(),
				EventType: "BenchmarkEvent",
				Data:      map[string]interface{}{"iteration": i},
			},
		}

		_, _, err := saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
		if err != nil {
			b.Fatalf("Failed to save events: %v", err)
		}
	}
}

func BenchmarkGetEvents(b *testing.B) {
	pool := getBenchmarkDB(b)
	defer pool.Close()

	saveEvents := NewSQLiteSaveEvents(pool, getTestLogger())
	ctx := getTestContext()

	// Pre-populate with events
	for i := 0; i < 1000; i++ {
		events := []orisun.EventWithMapTags{
			{
				EventId:   uuid.New().String(),
				EventType: "TestEvent",
				Data:      map[string]interface{}{"seq": i},
			},
		}
		saveEvents.Save(ctx, events, "bench_boundary", nil, nil)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		readConn := pool.Get(ctx)
		getEvents := NewSQLiteGetEvents(readConn, getTestLogger())
		_, err := getEvents.Get(ctx, &orisun.GetEventsRequest{
			Count:    100,
			Boundary: "bench_boundary",
		})
		pool.Put(readConn)
		if err != nil {
			b.Fatalf("Failed to get events: %v", err)
		}
	}
}
