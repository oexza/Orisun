package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/oexza/Orisun/logging"
	eventstore "github.com/oexza/Orisun/orisun"
)

func newTestPools(t *testing.T) (map[string]*BoundaryPools, func()) {
	t.Helper()
	dir := t.TempDir()
	const boundary = "test"
	logger, _ := logging.ZapLogger("error")
	_ = logger
	bp, err := OpenBoundaryPools(context.Background(), dir, boundary, boundary)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	pools := map[string]*BoundaryPools{boundary: bp}
	return pools, func() { _ = bp.Close() }
}

func mustEvent(t *testing.T, eventType string, data, meta map[string]any) eventstore.EventWithMapTags {
	t.Helper()
	return eventstore.EventWithMapTags{
		EventId:   uuid.NewString(),
		EventType: eventType,
		Data:      data,
		Metadata:  meta,
	}
}

func TestSave_RoundTrip(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")

	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)

	ctx := context.Background()
	events := []eventstore.EventWithMapTags{
		mustEvent(t, "Foo", map[string]any{"k": "v1"}, map[string]any{}),
		mustEvent(t, "Bar", map[string]any{"k": "v2"}, map[string]any{}),
	}

	tx, gid, err := saver.Save(ctx, events, "test", nil, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if tx == "" || gid <= 0 {
		t.Fatalf("expected non-zero tx/gid, got tx=%q gid=%d", tx, gid)
	}

	resp, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Direction: eventstore.Direction_ASC,
		Count:     100,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(resp.Events))
	}
	// Both events in the batch should share transaction_id == max global_id
	if resp.Events[0].Position.CommitPosition != resp.Events[1].Position.CommitPosition {
		t.Errorf("expected shared tx_id within batch, got %d vs %d",
			resp.Events[0].Position.CommitPosition, resp.Events[1].Position.CommitPosition)
	}
	if resp.Events[1].Position.PreparePosition != gid {
		t.Errorf("last event global_id should equal returned gid: got %d, want %d",
			resp.Events[1].Position.PreparePosition, gid)
	}
}

func TestSave_RejectsBatchLargerThanSqliteLimit(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")

	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)
	ctx := context.Background()

	batchSize := sqliteMaxEventsPerInsert + 1
	events := make([]eventstore.EventWithMapTags, batchSize)
	for i := range events {
		events[i] = mustEvent(t, "Chunked", map[string]any{"seq": i}, map[string]any{})
	}

	_, _, err := saver.Save(ctx, events, "test", nil, nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}

	resp, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Direction: eventstore.Direction_ASC,
		Count:     10,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(resp.Events) != 0 {
		t.Fatalf("expected no events after rejected batch, got %d", len(resp.Events))
	}
}

func TestSave_AddsEventTypeToData(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")

	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)
	ctx := context.Background()

	_, _, err := saver.Save(ctx, []eventstore.EventWithMapTags{
		mustEvent(t, "OrderPlaced", map[string]any{
			"order_id":  "order-1",
			"eventType": "stale",
		}, map[string]any{}),
	}, "test", nil, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	resp, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Direction: eventstore.Direction_ASC,
		Count:     100,
		Query: &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{Tags: []*eventstore.Tag{{Key: "eventType", Value: "OrderPlaced"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 matching event, got %d", len(resp.Events))
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(resp.Events[0].Data), &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["eventType"] != "OrderPlaced" {
		t.Fatalf("expected canonical eventType in data, got %v", data["eventType"])
	}
	if data["order_id"] != "order-1" {
		t.Fatalf("expected original data to be preserved, got %v", data["order_id"])
	}
}

func TestSave_RejectsEmpty(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")

	saver := NewSqliteSaveEvents(pools, logger)
	_, _, err := saver.Save(context.Background(), nil, "test", nil, nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestSave_RejectsUnknownBoundary(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")

	saver := NewSqliteSaveEvents(pools, logger)
	_, _, err := saver.Save(context.Background(),
		[]eventstore.EventWithMapTags{mustEvent(t, "X", map[string]any{}, map[string]any{})},
		"missing", nil, nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestSave_CCCViolation(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")
	saver := NewSqliteSaveEvents(pools, logger)
	ctx := context.Background()

	// First write — establishes a position matching criteria {"agg":"a1"}
	criteria := &eventstore.Query{
		Criteria: []*eventstore.Criterion{
			{Tags: []*eventstore.Tag{{Key: "agg", Value: "a1"}}},
		},
	}
	_, gid, err := saver.Save(ctx,
		[]eventstore.EventWithMapTags{mustEvent(t, "Created", map[string]any{"agg": "a1"}, map[string]any{})},
		"test", nil, criteria)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Second write with a stale expectedPosition (-1,-1 / nil) should fail.
	_, _, err = saver.Save(ctx,
		[]eventstore.EventWithMapTags{mustEvent(t, "Updated", map[string]any{"agg": "a1"}, map[string]any{})},
		"test", nil, criteria)
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists, got %v", err)
	}
	if !strings.Contains(err.Error(), "OptimisticConcurrencyException") {
		t.Errorf("expected OptimisticConcurrencyException in error message, got %v", err)
	}

	// Third write with correct expectedPosition (the gid we got back) should succeed.
	expected := &eventstore.Position{CommitPosition: gid, PreparePosition: gid}
	_, _, err = saver.Save(ctx,
		[]eventstore.EventWithMapTags{mustEvent(t, "Updated", map[string]any{"agg": "a1"}, map[string]any{})},
		"test", expected, criteria)
	if err != nil {
		t.Fatalf("expected success with correct expected position, got: %v", err)
	}
}

func TestGet_FilterByCriteria(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")
	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)
	ctx := context.Background()

	_, _, err := saver.Save(ctx, []eventstore.EventWithMapTags{
		mustEvent(t, "A", map[string]any{"agg": "1"}, map[string]any{}),
		mustEvent(t, "A", map[string]any{"agg": "2"}, map[string]any{}),
		mustEvent(t, "A", map[string]any{"agg": "1"}, map[string]any{}),
	}, "test", nil, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	resp, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Direction: eventstore.Direction_ASC,
		Count:     100,
		Query: &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{Tags: []*eventstore.Tag{{Key: "agg", Value: "1"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("expected 2 matching events, got %d", len(resp.Events))
	}
}

func TestGet_OrderAndFromPosition(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")
	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, _, err := saver.Save(ctx, []eventstore.EventWithMapTags{
			mustEvent(t, "Ordered", map[string]any{"index": i}, map[string]any{}),
		}, "test", nil, nil); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	respAsc, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Direction: eventstore.Direction_ASC,
		Count:     10,
	})
	if err != nil {
		t.Fatalf("get asc: %v", err)
	}
	respDesc, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Direction: eventstore.Direction_DESC,
		Count:     10,
	})
	if err != nil {
		t.Fatalf("get desc: %v", err)
	}
	if len(respAsc.Events) != 5 || len(respDesc.Events) != 5 {
		t.Fatalf("expected 5 asc and desc events, got %d and %d", len(respAsc.Events), len(respDesc.Events))
	}
	for i := range respAsc.Events {
		if respAsc.Events[i].EventId != respDesc.Events[4-i].EventId {
			t.Fatalf("desc order mismatch at %d", i)
		}
	}

	from := respAsc.Events[2].Position
	respFromAsc, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:     "test",
		Direction:    eventstore.Direction_ASC,
		Count:        10,
		FromPosition: from,
	})
	if err != nil {
		t.Fatalf("get from asc: %v", err)
	}
	if len(respFromAsc.Events) != 3 || respFromAsc.Events[0].EventId != respAsc.Events[2].EventId {
		t.Fatalf("expected inclusive ASC page from third event, got %d events", len(respFromAsc.Events))
	}

	respFromDesc, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:     "test",
		Direction:    eventstore.Direction_DESC,
		Count:        10,
		FromPosition: from,
	})
	if err != nil {
		t.Fatalf("get from desc: %v", err)
	}
	if len(respFromDesc.Events) != 3 || respFromDesc.Events[0].EventId != respAsc.Events[2].EventId {
		t.Fatalf("expected inclusive DESC page from third event, got %d events", len(respFromDesc.Events))
	}
}

func TestEventPublishing_EmptyCheckpointReturnsNotExists(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")
	tracker := NewSqliteEventPublishing(pools, logger)

	pos, err := tracker.GetLastPublishedEventPosition(context.Background(), "test")
	if err != nil {
		t.Fatalf("get last published position: %v", err)
	}
	want := eventstore.NotExistsPosition()
	if pos.CommitPosition != want.CommitPosition || pos.PreparePosition != want.PreparePosition {
		t.Fatalf("expected not-exists position, got commit=%d prepare=%d", pos.CommitPosition, pos.PreparePosition)
	}
}

func TestCreateDropBoundaryIndex_MetadataAndTypedCriteria(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")
	ctx := context.Background()

	admin := NewSqliteAdminDB(pools, "test", logger)
	saver := NewSqliteSaveEvents(pools, logger)
	getter := NewSqliteGetEvents(pools, logger)
	pool := pools["test"]

	indexExists := func(name string) bool {
		conn, err := pool.Read.Take(ctx)
		if err != nil {
			t.Fatalf("take conn: %v", err)
		}
		defer pool.Read.Put(conn)

		found := false
		err = sqlitex.Execute(conn,
			"SELECT 1 FROM sqlite_master WHERE type = 'index' AND name = ?",
			&sqlitex.ExecOptions{
				Args: []any{name},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					found = true
					return nil
				},
			})
		if err != nil {
			t.Fatalf("check index: %v", err)
		}
		return found
	}

	metadataCount := func(name string) int {
		conn, err := pool.Read.Take(ctx)
		if err != nil {
			t.Fatalf("take conn: %v", err)
		}
		defer pool.Read.Put(conn)

		count := 0
		err = sqlitex.Execute(conn,
			"SELECT COUNT(*) FROM orisun_boundary_index_metadata WHERE name = ?",
			&sqlitex.ExecOptions{
				Args: []any{name},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					count = int(stmt.ColumnInt64(0))
					return nil
				},
			})
		if err != nil {
			t.Fatalf("check metadata: %v", err)
		}
		return count
	}

	err := admin.CreateBoundaryIndex(ctx, "test", "amount",
		[]eventstore.BoundaryIndexField{{JsonKey: "amount", ValueType: "numeric"}},
		nil,
		"")
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	if !indexExists("amount_idx") {
		t.Fatal("expected index to exist")
	}
	if got := metadataCount("amount"); got != 1 {
		t.Fatalf("expected one metadata row, got %d", got)
	}

	sql, args := buildCriteriaSQLForBoundary([]map[string]any{{"amount": "45"}}, pool.indexes, "test")
	if !strings.Contains(sql, "CAST(json_extract(data, '$.\"amount\"') AS REAL)") {
		t.Fatalf("expected numeric cast in criteria SQL, got %q", sql)
	}
	if len(args) != 1 || args[0] != "45" {
		t.Fatalf("unexpected args: %#v", args)
	}

	_, _, err = saver.Save(ctx, []eventstore.EventWithMapTags{
		mustEvent(t, "Priced", map[string]any{"amount": 45}, map[string]any{}),
	}, "test", nil, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	resp, err := getter.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:  "test",
		Direction: eventstore.Direction_ASC,
		Count:     10,
		Query: &eventstore.Query{
			Criteria: []*eventstore.Criterion{
				{Tags: []*eventstore.Tag{{Key: "amount", Value: "45"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Events))
	}

	if err := admin.DropBoundaryIndex(ctx, "test", "amount"); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if indexExists("amount_idx") {
		t.Fatal("expected index to be dropped")
	}
	if got := metadataCount("amount"); got != 0 {
		t.Fatalf("expected no metadata rows, got %d", got)
	}

	sql, _ = buildCriteriaSQLForBoundary([]map[string]any{{"amount": "45"}}, pool.indexes, "test")
	if strings.Contains(sql, "CAST(") {
		t.Fatalf("expected untyped criteria SQL after dropping index, got %q", sql)
	}
}

func TestCreateDropBoundaryIndex_ValidationParity(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")
	admin := NewSqliteAdminDB(pools, "test", logger)
	ctx := context.Background()

	t.Run("composite index", func(t *testing.T) {
		if err := admin.CreateBoundaryIndex(ctx, "test", "cat_prio", []eventstore.BoundaryIndexField{
			{JsonKey: "category", ValueType: "text"},
			{JsonKey: "priority", ValueType: "text"},
		}, nil, ""); err != nil {
			t.Fatalf("create composite index: %v", err)
		}
		if err := admin.DropBoundaryIndex(ctx, "test", "cat_prio"); err != nil {
			t.Fatalf("drop composite index: %v", err)
		}
	})

	t.Run("partial index", func(t *testing.T) {
		if err := admin.CreateBoundaryIndex(ctx, "test", "placed_amount", []eventstore.BoundaryIndexField{
			{JsonKey: "amount", ValueType: "numeric"},
		}, []eventstore.BoundaryIndexCondition{
			{Key: "eventType", Operator: "=", Value: "OrderPlaced"},
		}, eventstore.IndexCombinatorAND); err != nil {
			t.Fatalf("create partial index: %v", err)
		}
		if err := admin.DropBoundaryIndex(ctx, "test", "placed_amount"); err != nil {
			t.Fatalf("drop partial index: %v", err)
		}
	})

	t.Run("unknown boundary", func(t *testing.T) {
		err := admin.CreateBoundaryIndex(ctx, "missing", "idx", []eventstore.BoundaryIndexField{
			{JsonKey: "id", ValueType: "text"},
		}, nil, "")
		if err == nil || !strings.Contains(err.Error(), "unknown boundary") {
			t.Fatalf("expected unknown boundary error, got %v", err)
		}
	})

	t.Run("no fields", func(t *testing.T) {
		err := admin.CreateBoundaryIndex(ctx, "test", "empty", nil, nil, "")
		if err == nil || !strings.Contains(err.Error(), "at least one field") {
			t.Fatalf("expected no fields error, got %v", err)
		}
	})

	t.Run("invalid operator", func(t *testing.T) {
		err := admin.CreateBoundaryIndex(ctx, "test", "bad_op", []eventstore.BoundaryIndexField{
			{JsonKey: "id", ValueType: "text"},
		}, []eventstore.BoundaryIndexCondition{
			{Key: "eventType", Operator: "LIKE", Value: "Order%"},
		}, "")
		if err == nil || !strings.Contains(err.Error(), "invalid operator") {
			t.Fatalf("expected invalid operator error, got %v", err)
		}
	})

	t.Run("invalid combinator", func(t *testing.T) {
		err := admin.CreateBoundaryIndex(ctx, "test", "bad_comb", []eventstore.BoundaryIndexField{
			{JsonKey: "id", ValueType: "text"},
		}, []eventstore.BoundaryIndexCondition{
			{Key: "eventType", Operator: "=", Value: "Placed"},
		}, "XOR")
		if err == nil || !strings.Contains(err.Error(), "invalid combinator") {
			t.Fatalf("expected invalid combinator error, got %v", err)
		}
	})

	t.Run("drop unknown boundary", func(t *testing.T) {
		err := admin.DropBoundaryIndex(ctx, "missing", "idx")
		if err == nil || !strings.Contains(err.Error(), "unknown boundary") {
			t.Fatalf("expected unknown boundary error, got %v", err)
		}
	})
}

func TestAdminUserCacheInvalidatedOnDelete(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")
	admin := NewSqliteAdminDB(pools, "test", logger)

	user := eventstore.User{
		Id:             uuid.NewString(),
		Name:           "Test User",
		Username:       "cache-delete-test",
		HashedPassword: "hash",
		Roles:          []eventstore.Role{eventstore.RoleAdmin},
	}
	if err := admin.UpsertUser(user); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if _, err := admin.GetUserByUsername(user.Username); err != nil {
		t.Fatalf("expected cached user: %v", err)
	}
	if err := admin.DeleteUser(user.Id); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := admin.GetUserByUsername(user.Username); err == nil {
		t.Fatal("expected deleted user lookup to fail")
	}
}

func TestSaveNotifiesSqliteEventSignalAfterCommit(t *testing.T) {
	pools, cleanup := newTestPools(t)
	defer cleanup()
	logger, _ := logging.ZapLogger("error")

	notifier := NewSqliteEventNotifier(time.Hour)
	saver := NewSqliteSaveEvents(pools, logger)
	saver.notifier = notifier
	signal := notifier.Signal("test")
	defer signal.Stop()

	waitCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- signal.Wait(waitCtx)
	}()

	_, _, err := saver.Save(context.Background(), []eventstore.EventWithMapTags{
		mustEvent(t, "Notified", map[string]any{"k": "v"}, map[string]any{}),
	}, "test", nil, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := <-waitCh; err != nil {
		t.Fatalf("expected save notification before polling interval: %v", err)
	}
}

func TestBuildCriteriaSQL(t *testing.T) {
	cases := []struct {
		name     string
		input    []map[string]any
		wantArgs int
	}{
		{"empty", nil, 0},
		{"single criterion", []map[string]any{{"k": "v"}}, 1},
		{"multi-key AND", []map[string]any{{"a": "1", "b": "2"}}, 2},
		{"multi-criterion OR", []map[string]any{{"a": "1"}, {"b": "2"}}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sql, args := buildCriteriaSQL(c.input)
			if len(args) != c.wantArgs {
				t.Errorf("wantArgs=%d gotArgs=%d, sql=%q", c.wantArgs, len(args), sql)
			}
			// Multi-criterion case must contain OR; single-criterion multi-key must contain AND.
			if c.name == "multi-criterion OR" && !strings.Contains(sql, " OR ") {
				t.Errorf("expected OR in %q", sql)
			}
			if c.name == "multi-key AND" && !strings.Contains(sql, " AND ") {
				t.Errorf("expected AND in %q", sql)
			}
		})
	}
}

func TestJSONPathLiteralEscapes(t *testing.T) {
	cases := map[string]string{
		`simple`:       `'$."simple"'`,
		`with"quote`:   `'$."with""quote"'`,
		`with'apos`:    `'$."with''apos"'`,
		`both"and'mix`: `'$."both""and''mix"'`,
	}
	for in, want := range cases {
		got := jsonPathLiteral(in)
		if got != want {
			t.Errorf("jsonPathLiteral(%q) = %q, want %q", in, got, want)
		}
	}
}
