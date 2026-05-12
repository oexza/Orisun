package sqlite

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// BoundaryPools holds the write and read connection pools backing one boundary's SQLite file.
//
// Write pool is sized to 1: SQLite serializes writers anyway via its file lock, so a single
// in-flight writer queues callers in Go and avoids SQLITE_BUSY churn. Read pool is sized to
// runtime.NumCPU() — under WAL, readers run concurrently with the single writer.
type BoundaryPools struct {
	Boundary string
	Write    *sqlitex.Pool
	Read     *sqlitex.Pool
}

// OpenBoundaryPools opens write+read pools for one boundary at {dir}/{boundary}.db.
// Migrations are applied on the first connection drawn from the write pool.
// adminBoundary controls whether admin-only tables (users, users_count) are created.
func OpenBoundaryPools(ctx context.Context, dir, boundary, adminBoundary string) (*BoundaryPools, error) {
	dbPath := filepath.Join(dir, boundary+".db")
	uri := fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=on", url.PathEscape(dbPath))

	prepare := func(conn *sqlite.Conn) error {
		// Belt-and-braces: apply pragmas explicitly in case the URI hints are ignored.
		for _, p := range []string{
			`PRAGMA journal_mode = WAL`,
			`PRAGMA synchronous = NORMAL`,
			`PRAGMA foreign_keys = ON`,
			`PRAGMA busy_timeout = 5000`,
			`PRAGMA temp_store = MEMORY`,
		} {
			if err := sqlitex.ExecuteTransient(conn, p, nil); err != nil {
				return fmt.Errorf("apply pragma %q: %w", p, err)
			}
		}
		return nil
	}

	writePool, err := sqlitex.NewPool(uri, sqlitex.PoolOptions{
		PoolSize:    1,
		PrepareConn: prepare,
	})
	if err != nil {
		return nil, fmt.Errorf("open write pool for %s: %w", boundary, err)
	}

	readPool, err := sqlitex.NewPool(uri, sqlitex.PoolOptions{
		PoolSize:    runtime.NumCPU(),
		PrepareConn: prepare,
	})
	if err != nil {
		writePool.Close()
		return nil, fmt.Errorf("open read pool for %s: %w", boundary, err)
	}

	conn, err := writePool.Take(ctx)
	if err != nil {
		writePool.Close()
		readPool.Close()
		return nil, fmt.Errorf("take migration conn for %s: %w", boundary, err)
	}
	if err := applyMigrations(conn, boundary == adminBoundary); err != nil {
		writePool.Put(conn)
		writePool.Close()
		readPool.Close()
		return nil, fmt.Errorf("migrate %s: %w", boundary, err)
	}
	writePool.Put(conn)

	return &BoundaryPools{Boundary: boundary, Write: writePool, Read: readPool}, nil
}

func (b *BoundaryPools) Close() error {
	var firstErr error
	if err := b.Write.Close(); err != nil {
		firstErr = err
	}
	if err := b.Read.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
