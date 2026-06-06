//go:build foundationdb

package foundationdb

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/google/uuid"
	config "github.com/oexza/Orisun/config"
	"github.com/oexza/Orisun/logging"
	eventstore "github.com/oexza/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFoundationDBSaveGetCCCAndIndexes(t *testing.T) {
	backend := newTestBackend(t)
	ctx := context.Background()

	_, _, err := backend.Save(ctx, []eventstore.EventWithMapTags{
		{
			EventId:   "evt-1",
			EventType: "OrderCreated",
			Data: map[string]any{
				"order_id":    "ord-1",
				"customer_id": "cust-1",
			},
			Metadata: map[string]any{},
		},
		{
			EventId:   "evt-2",
			EventType: "OrderPaid",
			Data: map[string]any{
				"order_id":    "ord-1",
				"customer_id": "cust-1",
			},
			Metadata: map[string]any{},
		},
	}, "test", nil, nil)
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	resp, err := backend.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Count:     10,
		Direction: eventstore.Direction_ASC,
	})
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(resp.Events))
	}
	// Positions are FDB versionstamps. Both events come from one Save so they
	// share a commit version and are ordered by consecutive user versions.
	if resp.Events[0].Position.CommitPosition != resp.Events[1].Position.CommitPosition {
		t.Fatalf("events from the same batch should share commit version: %v vs %v", resp.Events[0].Position, resp.Events[1].Position)
	}
	if resp.Events[1].Position.PreparePosition != resp.Events[0].Position.PreparePosition+1 {
		t.Fatalf("expected consecutive prepare positions, got %v then %v", resp.Events[0].Position, resp.Events[1].Position)
	}

	if err := backend.CreateBoundaryIndex(ctx, "test", "customer_id", []eventstore.BoundaryIndexField{
		{JsonKey: "customer_id", ValueType: "text"},
	}, nil, eventstore.IndexCombinatorAND); err != nil {
		t.Fatalf("CreateBoundaryIndex returned error: %v", err)
	}
	indexed, err := backend.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Count:     10,
		Direction: eventstore.Direction_ASC,
		Query: &eventstore.Query{Criteria: []*eventstore.Criterion{
			{Tags: []*eventstore.Tag{{Key: "customer_id", Value: "cust-1"}}},
		}},
	})
	if err != nil {
		t.Fatalf("indexed Get returned error: %v", err)
	}
	if len(indexed.Events) != 2 {
		t.Fatalf("expected 2 indexed events, got %d", len(indexed.Events))
	}

	// A consistency condition must be covered by an index (fail-closed). Index
	// order_id so the condition below can be checked.
	if err := backend.CreateBoundaryIndex(ctx, "test", "order_id", []eventstore.BoundaryIndexField{
		{JsonKey: "order_id", ValueType: "text"},
	}, nil, eventstore.IndexCombinatorAND); err != nil {
		t.Fatalf("CreateBoundaryIndex(order_id) returned error: %v", err)
	}

	notExists := eventstore.NotExistsPosition()
	_, _, err = backend.Save(ctx, []eventstore.EventWithMapTags{
		{
			EventId:   "evt-3",
			EventType: "OrderShipped",
			Data:      map[string]any{"order_id": "ord-1", "customer_id": "cust-1"},
			Metadata:  map[string]any{},
		},
	}, "test", &notExists, &eventstore.Query{Criteria: []*eventstore.Criterion{
		{Tags: []*eventstore.Tag{{Key: "order_id", Value: "ord-1"}}},
	}})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected ALREADY_EXISTS conflict, got %v", err)
	}

	// An unindexed consistency condition must be rejected, not silently scanned.
	_, _, err = backend.Save(ctx, []eventstore.EventWithMapTags{
		{
			EventId:   "evt-4",
			EventType: "OrderNoted",
			Data:      map[string]any{"note": "x"},
			Metadata:  map[string]any{},
		},
	}, "test", &notExists, &eventstore.Query{Criteria: []*eventstore.Criterion{
		{Tags: []*eventstore.Tag{{Key: "uncovered_key", Value: "v"}}},
	}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FAILED_PRECONDITION for unindexed consistency condition, got %v", err)
	}
}

func TestFoundationDBAdminAndPublishingState(t *testing.T) {
	backend := newTestBackend(t)

	user := eventstore.User{
		Id:             "user-1",
		Name:           "Admin",
		Username:       "admin",
		HashedPassword: "hash",
		Roles:          []eventstore.Role{eventstore.RoleAdmin},
	}
	if err := backend.UpsertUser(user); err != nil {
		t.Fatalf("UpsertUser returned error: %v", err)
	}
	got, err := backend.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername returned error: %v", err)
	}
	if got.Id != user.Id {
		t.Fatalf("expected user id %q, got %q", user.Id, got.Id)
	}

	if err := backend.InsertLastPublishedEvent(context.Background(), "test", 4, 4); err != nil {
		t.Fatalf("InsertLastPublishedEvent returned error: %v", err)
	}
	pos, err := backend.GetLastPublishedEventPosition(context.Background(), "test")
	if err != nil {
		t.Fatalf("GetLastPublishedEventPosition returned error: %v", err)
	}
	if pos.CommitPosition != 4 || pos.PreparePosition != 4 {
		t.Fatalf("unexpected published position: %v", pos)
	}
}

func newTestBackend(tb testing.TB) *Backend {
	tb.Helper()
	clusterFile := startFDBCluster(tb)
	apiVersion := defaultAPIVersion
	if raw := os.Getenv("ORISUN_FDB_TEST_API_VERSION"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			tb.Fatalf("invalid ORISUN_FDB_TEST_API_VERSION: %v", err)
		}
		apiVersion = parsed
	}
	if err := ensureAPIVersion(apiVersion); err != nil {
		tb.Fatalf("FoundationDB API version: %v", err)
	}
	db, err := fdb.OpenDatabase(clusterFile)
	if err != nil {
		tb.Fatalf("open FoundationDB: %v", err)
	}
	root := "orisun_test_" + uuid.NewString()
	backend := &Backend{
		db:            db,
		root:          root,
		scanLimit:     1000,
		adminBoundary: "orisun_admin",
		boundaries: map[string]struct{}{
			"test":         {},
			"orisun_admin": {},
		},
		logger: logging.InitializeDefaultLogger(configForTestLogger()),
	}
	tb.Cleanup(func() {
		_, _ = db.Transact(func(tr fdb.Transaction) (interface{}, error) {
			tr.ClearRange(prefixRange(fdb.Key(tuple.Tuple{root}.Pack())))
			return nil, nil
		})
		db.Close()
	})
	return backend
}

func configForTestLogger() config.LoggingConfig {
	return config.LoggingConfig{Level: "ERROR"}
}
