//go:build foundationdb

package foundationdb

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/goccy/go-json"
	"github.com/google/uuid"
	eventstore "github.com/oexza/Orisun/orisun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestFoundationDBGeneralLedgerWorkload drives a double-entry ledger — the
// canonical CCC workload — under real write contention:
//
//   - every transfer appends a TransferDebited + TransferCredited pair in ONE
//     batch (atomicity across two accounts),
//   - the consistency context is "either account touched" (two OR'd criteria,
//     so the optimistic lock spans both accounts' index slices),
//   - workers race on a small account set and retry on ALREADY_EXISTS.
//
// Afterwards it audits the full event log for the invariants a real ledger
// cannot violate:
//
//  1. no account's running balance ever goes negative at any point in its
//     history (a dip below zero means two debits raced past the same check —
//     a CCC violation),
//  2. money is conserved: final balances sum to the seeded total,
//  3. every transfer id appears as exactly one debit and one credit of equal
//     amount (batch atomicity),
//  4. per-account index reads return strictly increasing positions.
func TestFoundationDBGeneralLedgerWorkload(t *testing.T) {
	backend := newTestBackend(t)
	ctx := context.Background()

	const (
		accounts           = 10
		initialBalance     = int64(1000)
		workers            = 8
		transfersPerWorker = 20
		maxAttempts        = 200
	)

	accountID := func(i int) string { return fmt.Sprintf("acct-%02d", i) }
	accountCriteria := func(ids ...string) *eventstore.Query {
		criteria := make([]*eventstore.Criterion, len(ids))
		for i, id := range ids {
			criteria[i] = &eventstore.Criterion{Tags: []*eventstore.Tag{{Key: "account_id", Value: id}}}
		}
		return &eventstore.Query{Criteria: criteria}
	}

	// Covering index first: required for both criteria reads and CCC.
	if err := backend.CreateBoundaryIndex(ctx, "test", "account",
		[]eventstore.BoundaryIndexField{{JsonKey: "account_id", ValueType: "text"}},
		nil, eventstore.IndexCombinatorAND); err != nil {
		t.Fatalf("create account index: %v", err)
	}

	// Seed accounts. nil expected position + criteria = "this account's
	// context must still be empty".
	for i := 0; i < accounts; i++ {
		if _, _, err := backend.Save(ctx, []eventstore.EventWithMapTags{{
			EventId:   uuid.NewString(),
			EventType: "AccountOpened",
			Data:      map[string]any{"account_id": accountID(i), "amount": initialBalance},
			Metadata:  map[string]any{},
		}}, "test", nil, accountCriteria(accountID(i))); err != nil {
			t.Fatalf("open account %d: %v", i, err)
		}
	}

	type entry struct {
		eventType string
		accountID string
		amount    int64
		transfer  string
	}
	decodeEntry := func(event *eventstore.Event) entry {
		data := map[string]any{}
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			t.Fatalf("decode event %s: %v", event.EventId, err)
		}
		amount, _ := data["amount"].(float64)
		account, _ := data["account_id"].(string)
		transfer, _ := data["transfer_id"].(string)
		return entry{eventType: event.EventType, accountID: account, amount: int64(amount), transfer: transfer}
	}
	apply := func(balance int64, e entry) int64 {
		switch e.eventType {
		case "AccountOpened", "TransferCredited":
			return balance + e.amount
		case "TransferDebited":
			return balance - e.amount
		default:
			t.Fatalf("unexpected event type %q", e.eventType)
			return 0
		}
	}

	// readContext folds both accounts' balances from one combined criteria
	// read and returns the newest context position for the optimistic lock.
	readContext := func(src, dst string) (srcBal, dstBal int64, last *eventstore.Position, err error) {
		resp, err := backend.Get(ctx, &eventstore.GetEventsRequest{
			Boundary:  "test",
			Count:     maxPageCount,
			Direction: eventstore.Direction_ASC,
			Query:     accountCriteria(src, dst),
		})
		if err != nil {
			return 0, 0, nil, err
		}
		for _, event := range resp.Events {
			e := decodeEntry(event)
			switch e.accountID {
			case src:
				srcBal = apply(srcBal, e)
			case dst:
				dstBal = apply(dstBal, e)
			}
			last = event.Position
		}
		return srcBal, dstBal, last, nil
	}

	var transferred, declined, conflicts int64
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(w) + 42))
			for i := 0; i < transfersPerWorker; i++ {
				src := accountID(rng.Intn(accounts))
				dst := accountID(rng.Intn(accounts))
				for dst == src {
					dst = accountID(rng.Intn(accounts))
				}
				amount := int64(1 + rng.Intn(50))

				attempts := 0
				for {
					attempts++
					if attempts > maxAttempts {
						errs <- fmt.Errorf("worker %d transfer %d: gave up after %d attempts (livelock?)", w, i, maxAttempts)
						return
					}
					srcBal, _, last, err := readContext(src, dst)
					if err != nil {
						errs <- fmt.Errorf("read context: %w", err)
						return
					}
					if srcBal < amount {
						// Business decline: no event appended.
						atomic.AddInt64(&declined, 1)
						break
					}
					transferID := uuid.NewString()
					_, _, err = backend.Save(ctx, []eventstore.EventWithMapTags{
						{
							EventId:   uuid.NewString(),
							EventType: "TransferDebited",
							Data:      map[string]any{"account_id": src, "amount": amount, "transfer_id": transferID},
							Metadata:  map[string]any{},
						},
						{
							EventId:   uuid.NewString(),
							EventType: "TransferCredited",
							Data:      map[string]any{"account_id": dst, "amount": amount, "transfer_id": transferID},
							Metadata:  map[string]any{},
						},
					}, "test", last, accountCriteria(src, dst))
					if status.Code(err) == codes.AlreadyExists {
						// Lost the race on one of the two accounts; re-read and retry.
						atomic.AddInt64(&conflicts, 1)
						continue
					}
					if err != nil {
						errs <- fmt.Errorf("transfer save: %w", err)
						return
					}
					atomic.AddInt64(&transferred, 1)
					break
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	t.Logf("transfers executed=%d declined=%d optimistic conflicts retried=%d", transferred, declined, conflicts)

	// Audit the complete log in position order.
	var all []*eventstore.Event
	var cursor *eventstore.Position
	for {
		req := &eventstore.GetEventsRequest{Boundary: "test", Count: 500, Direction: eventstore.Direction_ASC}
		if cursor != nil {
			req.FromPosition = &eventstore.Position{
				CommitPosition:  cursor.CommitPosition,
				PreparePosition: cursor.PreparePosition + 1,
			}
		}
		resp, err := backend.Get(ctx, req)
		if err != nil {
			t.Fatalf("audit read: %v", err)
		}
		if len(resp.Events) == 0 {
			break
		}
		all = append(all, resp.Events...)
		cursor = resp.Events[len(resp.Events)-1].Position
	}
	wantEvents := accounts + int(transferred)*2
	if len(all) != wantEvents {
		t.Fatalf("expected %d events in log, got %d", wantEvents, len(all))
	}

	// Invariant 1+2: replay running balances in order; never negative, money conserved.
	balances := map[string]int64{}
	type leg struct {
		debits, credits int
		amount          int64
	}
	legs := map[string]*leg{}
	for _, event := range all {
		e := decodeEntry(event)
		balances[e.accountID] = apply(balances[e.accountID], e)
		if balances[e.accountID] < 0 {
			t.Fatalf("account %s running balance went negative at position %v — overdraft slipped past CCC", e.accountID, event.Position)
		}
		if e.transfer != "" {
			l := legs[e.transfer]
			if l == nil {
				l = &leg{amount: e.amount}
				legs[e.transfer] = l
			}
			if e.amount != l.amount {
				t.Fatalf("transfer %s legs disagree on amount: %d vs %d", e.transfer, e.amount, l.amount)
			}
			switch e.eventType {
			case "TransferDebited":
				l.debits++
			case "TransferCredited":
				l.credits++
			}
		}
	}
	var total int64
	for account, balance := range balances {
		if balance < 0 {
			t.Fatalf("account %s final balance negative: %d", account, balance)
		}
		total += balance
	}
	if want := int64(accounts) * initialBalance; total != want {
		t.Fatalf("money not conserved: total %d, want %d", total, want)
	}

	// Invariant 3: every transfer has exactly one debit and one credit.
	if len(legs) != int(transferred) {
		t.Fatalf("expected %d transfer ids, got %d", transferred, len(legs))
	}
	for id, l := range legs {
		if l.debits != 1 || l.credits != 1 {
			t.Fatalf("transfer %s has %d debits / %d credits, want 1/1", id, l.debits, l.credits)
		}
	}

	// Invariant 4: per-account criteria reads come back in strictly increasing
	// position order through the account index.
	for i := 0; i < accounts; i++ {
		resp, err := backend.Get(ctx, &eventstore.GetEventsRequest{
			Boundary:  "test",
			Count:     maxPageCount,
			Direction: eventstore.Direction_ASC,
			Query:     accountCriteria(accountID(i)),
		})
		if err != nil {
			t.Fatalf("account %d history: %v", i, err)
		}
		for j := 1; j < len(resp.Events); j++ {
			if comparePositions(resp.Events[j].Position, resp.Events[j-1].Position) <= 0 {
				t.Fatalf("account %s history out of order at %d", accountID(i), j)
			}
		}
	}
}