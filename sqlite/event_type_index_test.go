package sqlite

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OrisunLabs/Orisun/orisun"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Use the production read/CCC renderer to prove the built-in index remains
// usable for both absent event types and runtime-keyed membership lookups.
func TestBuiltInEventTypeIndexForFreshAndUpgradedStorage(t *testing.T) {
	for _, version := range []int{0, 6, 7} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			conn := openMigrationTestConn(t, filepath.Join(t.TempDir(), "events.db"))
			if version != 0 {
				oldDDL := strings.Replace(eventDDL, eventTypeOrderIndexDDL, "CREATE INDEX idx_event_type_order ON orisun_es_event(json_extract(data, '$.__eventType'), transaction_id DESC, global_id DESC);", 1)
				if err := sqlitex.ExecuteScript(conn, oldDDL+fmt.Sprintf("PRAGMA user_version=%d;", version), nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := applyMigrations(conn); err != nil {
				t.Fatal(err)
			}
			if err := sqlitex.ExecuteScript(conn, `INSERT INTO orisun_es_write VALUES(1, '[]'); INSERT INTO orisun_es_event(data) VALUES('{"__eventId":"id","__eventType":"Posted","__commitPosition":1,"__preparePosition":1,"__writeId":"1:1","__dateCreated":"2026-10-12T00:00:00Z","__metadata":{},"accountBalances.account-a.accountCreatedId":"account-a"}');`, nil); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"Posted", "Absent"} {
				criteria := []orisun.ReadCriterion{{Tags: []orisun.ReadTag{{Key: "__eventType", Value: kind}, {Key: "accountBalances.account-a.accountCreatedId", Value: "account-a"}}}}
				queries, err := latestCriteriaQueries(criteria)
				if err != nil {
					t.Fatal(err)
				}
				for _, query := range queries {
					var plan []string
					err := sqlitex.ExecuteTransient(conn, "EXPLAIN QUERY PLAN "+query, &sqlitex.ExecOptions{ResultFunc: func(stmt *sqlite.Stmt) error { plan = append(plan, stmt.ColumnText(3)); return nil }})
					if err != nil {
						t.Fatal(err)
					}
					detail := strings.Join(plan, " ")
					if !strings.Contains(detail, "SEARCH orisun_es_event USING INDEX idx_event_type_order") || strings.Contains(detail, "TEMP B-TREE") {
						t.Fatalf("unindexed production lookup: %s", detail)
					}
				}
				tx, gid, err := latestCriteriaPosition(conn, criteria)
				if err != nil {
					t.Fatal(err)
				}
				want := int64(-1)
				if kind == "Posted" {
					want = 1
				}
				if tx != want || gid != want {
					t.Fatalf("position = (%d,%d), want (%d,%d)", tx, gid, want, want)
				}
			}
		})
	}
}
