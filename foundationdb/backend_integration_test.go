//go:build foundationdb

package foundationdb

import (
	"context"
	"os"
	"strconv"
	"sync"
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
		t.Fatalf("unexpected published position: %v", &pos)
	}
}

// TestFoundationDBPagingFromPosition walks a boundary in pages using the
// publisher's cursor convention (commit, prepare+1). Regression test for the
// scan that filtered by position AFTER applying the range limit: page 2 would
// come back empty because the limit was consumed by pre-cursor keys.
func TestFoundationDBPagingFromPosition(t *testing.T) {
	backend := newTestBackend(t)
	ctx := context.Background()

	const batches = 3
	const perBatch = 10
	for i := 0; i < batches; i++ {
		events := make([]eventstore.EventWithMapTags, perBatch)
		for j := 0; j < perBatch; j++ {
			events[j] = eventstore.EventWithMapTags{
				EventId:   uuid.NewString(),
				EventType: "Paged",
				Data:      map[string]any{"batch": strconv.Itoa(i), "n": strconv.Itoa(j)},
				Metadata:  map[string]any{},
			}
		}
		if _, _, err := backend.Save(ctx, events, "test", nil, nil); err != nil {
			t.Fatalf("Save batch %d: %v", i, err)
		}
	}

	seen := map[string]struct{}{}
	var cursor *eventstore.Position
	pages := 0
	for {
		req := &eventstore.GetEventsRequest{
			Boundary:  "test",
			Count:     perBatch,
			Direction: eventstore.Direction_ASC,
		}
		if cursor != nil {
			req.FromPosition = &eventstore.Position{
				CommitPosition:  cursor.CommitPosition,
				PreparePosition: cursor.PreparePosition + 1,
			}
		}
		resp, err := backend.Get(ctx, req)
		if err != nil {
			t.Fatalf("Get page %d: %v", pages, err)
		}
		if len(resp.Events) == 0 {
			break
		}
		pages++
		if pages > batches+1 {
			t.Fatalf("paging did not terminate")
		}
		prev := cursor
		for _, event := range resp.Events {
			if prev != nil && comparePositions(event.Position, prev) <= 0 {
				t.Fatalf("event %s position %v not after %v", event.EventId, event.Position, prev)
			}
			prev = event.Position
			if _, dup := seen[event.EventId]; dup {
				t.Fatalf("event %s returned twice", event.EventId)
			}
			seen[event.EventId] = struct{}{}
		}
		cursor = resp.Events[len(resp.Events)-1].Position
	}
	if len(seen) != batches*perBatch {
		t.Fatalf("expected %d events across pages, got %d in %d pages", batches*perBatch, len(seen), pages)
	}

	// DESC from the newest cursor must page backwards without duplicates.
	desc, err := backend.Get(ctx, &eventstore.GetEventsRequest{
		Boundary:     "test",
		Count:        perBatch,
		Direction:    eventstore.Direction_DESC,
		FromPosition: cursor,
	})
	if err != nil {
		t.Fatalf("Get DESC: %v", err)
	}
	if len(desc.Events) != perBatch {
		t.Fatalf("expected %d DESC events, got %d", perBatch, len(desc.Events))
	}
	if comparePositions(desc.Events[0].Position, cursor) != 0 {
		t.Fatalf("DESC page should start at the inclusive cursor")
	}
}

// TestFoundationDBCCCSuccessAndStaleExpected drives a single aggregate through
// the optimistic-lock cycle: first write against an empty context, follow-up
// write with the returned position, then a stale write that must conflict.
func TestFoundationDBCCCSuccessAndStaleExpected(t *testing.T) {
	backend := newTestBackend(t)
	ctx := context.Background()

	if err := backend.CreateBoundaryIndex(ctx, "test", "agg", []eventstore.BoundaryIndexField{
		{JsonKey: "agg_id", ValueType: "text"},
	}, nil, eventstore.IndexCombinatorAND); err != nil {
		t.Fatalf("CreateBoundaryIndex: %v", err)
	}
	criteria := &eventstore.Query{Criteria: []*eventstore.Criterion{
		{Tags: []*eventstore.Tag{{Key: "agg_id", Value: "agg-1"}}},
	}}

	notExists := eventstore.NotExistsPosition()
	txID, gid, err := backend.Save(ctx, []eventstore.EventWithMapTags{{
		EventId:   uuid.NewString(),
		EventType: "Created",
		Data:      map[string]any{"agg_id": "agg-1"},
		Metadata:  map[string]any{},
	}}, "test", &notExists, criteria)
	if err != nil {
		t.Fatalf("first Save: %v", err)
	}
	commit, err := strconv.ParseInt(txID, 10, 64)
	if err != nil {
		t.Fatalf("parse commit position: %v", err)
	}
	current := eventstore.Position{CommitPosition: commit, PreparePosition: gid}

	if _, _, err := backend.Save(ctx, []eventstore.EventWithMapTags{{
		EventId:   uuid.NewString(),
		EventType: "Updated",
		Data:      map[string]any{"agg_id": "agg-1"},
		Metadata:  map[string]any{},
	}}, "test", &current, criteria); err != nil {
		t.Fatalf("Save with correct expected position: %v", err)
	}

	if _, _, err := backend.Save(ctx, []eventstore.EventWithMapTags{{
		EventId:   uuid.NewString(),
		EventType: "Updated",
		Data:      map[string]any{"agg_id": "agg-1"},
		Metadata:  map[string]any{},
	}}, "test", &current, criteria); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected ALREADY_EXISTS for stale expected position, got %v", err)
	}
}

// TestFoundationDBUserRename: renaming a user must stop the old username from
// resolving — both in FoundationDB and in the auth cache.
func TestFoundationDBUserRename(t *testing.T) {
	backend := newTestBackend(t)

	user := eventstore.User{Id: "u-1", Name: "Ada", Username: "ada", HashedPassword: "h1", Roles: []eventstore.Role{eventstore.RoleAdmin}}
	if err := backend.UpsertUser(user); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if _, err := backend.GetUserByUsername("ada"); err != nil {
		t.Fatalf("GetUserByUsername(ada): %v", err)
	}

	user.Username = "ada2"
	if err := backend.UpsertUser(user); err != nil {
		t.Fatalf("UpsertUser rename: %v", err)
	}
	if _, err := backend.GetUserByUsername("ada"); err == nil {
		t.Fatalf("old username must not resolve after rename")
	}
	got, err := backend.GetUserByUsername("ada2")
	if err != nil {
		t.Fatalf("GetUserByUsername(ada2): %v", err)
	}
	if got.Id != "u-1" {
		t.Fatalf("expected user u-1, got %q", got.Id)
	}
}

func TestFoundationDBGetEventsCountPaged(t *testing.T) {
	backend := newTestBackend(t)
	ctx := context.Background()

	const total = 25
	for i := 0; i < total; i++ {
		if _, _, err := backend.Save(ctx, []eventstore.EventWithMapTags{{
			EventId:   uuid.NewString(),
			EventType: "Counted",
			Data:      map[string]any{"n": strconv.Itoa(i)},
			Metadata:  map[string]any{},
		}}, "test", nil, nil); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}
	count, err := backend.GetEventsCount("test")
	if err != nil {
		t.Fatalf("GetEventsCount: %v", err)
	}
	if count != total {
		t.Fatalf("expected %d events, got %d", total, count)
	}
}

// TestFoundationDBTotalOrderUnderConcurrency: boundary-wide total order is a
// core Orisun guarantee. Many goroutines append in parallel (no consistency
// condition, so nothing serialises them app-side); afterwards a paged read of
// the boundary must yield every event exactly once in strictly increasing
// position order, and batches must never interleave.
func TestFoundationDBTotalOrderUnderConcurrency(t *testing.T) {
	backend := newTestBackend(t)
	ctx := context.Background()

	const writers = 8
	const savesPerWriter = 20
	const eventsPerSave = 3

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < savesPerWriter; i++ {
				events := make([]eventstore.EventWithMapTags, eventsPerSave)
				for j := range events {
					events[j] = eventstore.EventWithMapTags{
						EventId:   uuid.NewString(),
						EventType: "Ordered",
						Data:      map[string]any{"writer": strconv.Itoa(w), "save": strconv.Itoa(i), "j": strconv.Itoa(j)},
						Metadata:  map[string]any{},
					}
				}
				if _, _, err := backend.Save(ctx, events, "test", nil, nil); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Save: %v", err)
	}

	const total = writers * savesPerWriter * eventsPerSave
	seen := map[string]struct{}{}
	var prev *eventstore.Position
	var prevCommit int64 = -1
	var cursor *eventstore.Position
	for {
		req := &eventstore.GetEventsRequest{Boundary: "test", Count: 50, Direction: eventstore.Direction_ASC}
		if cursor != nil {
			req.FromPosition = &eventstore.Position{
				CommitPosition:  cursor.CommitPosition,
				PreparePosition: cursor.PreparePosition + 1,
			}
		}
		resp, err := backend.Get(ctx, req)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if len(resp.Events) == 0 {
			break
		}
		for _, event := range resp.Events {
			if prev != nil && comparePositions(event.Position, prev) <= 0 {
				t.Fatalf("total order violated: %v after %v", event.Position, prev)
			}
			// A batch shares one commit position; once the commit position
			// advances it must never come back (batches cannot interleave).
			if event.Position.CommitPosition < prevCommit {
				t.Fatalf("batch interleaving: commit %d after %d", event.Position.CommitPosition, prevCommit)
			}
			prevCommit = event.Position.CommitPosition
			prev = event.Position
			if _, dup := seen[event.EventId]; dup {
				t.Fatalf("event %s seen twice", event.EventId)
			}
			seen[event.EventId] = struct{}{}
		}
		cursor = resp.Events[len(resp.Events)-1].Position
	}
	if len(seen) != total {
		t.Fatalf("expected %d events in total order, got %d", total, len(seen))
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
		logger:    logging.InitializeDefaultLogger(configForTestLogger()),
		userCache: make(map[string]*eventstore.User),
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
