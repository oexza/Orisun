package sqlite

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	config "github.com/oexza/Orisun/config"
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
	indexes  *sqliteIndexRegistry
}

// OpenBoundaryPools opens write+read pools for one boundary at {dir}/{boundary}.db.
// Migrations are applied on the first connection drawn from the write pool.
// adminBoundary controls whether admin-only tables (users, users_count) are created.
func OpenBoundaryPools(ctx context.Context, dir, boundary, adminBoundary string) (*BoundaryPools, error) {
	return OpenBoundaryPoolsWithConfig(ctx, config.SqliteConfig{Dir: dir}, boundary, adminBoundary)
}

func OpenBoundaryPoolsWithConfig(ctx context.Context, sqliteCfg config.SqliteConfig, boundary, adminBoundary string) (*BoundaryPools, error) {
	poolCfg, err := normalizeSqlitePoolConfig(sqliteCfg)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(poolCfg.dir, boundary+".db")
	uri := sqliteURI(dbPath, poolCfg)

	prepare := func(conn *sqlite.Conn) error {
		// Belt-and-braces: apply pragmas explicitly in case the URI hints are ignored.
		for _, p := range sqlitePragmas(poolCfg) {
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
		PoolSize:    poolCfg.readPoolSize,
		PrepareConn: prepare,
	})
	if err != nil {
		writePool.Close()
		return nil, fmt.Errorf("open read pool for %s: %w", boundary, err)
	}

	indexes := newSqliteIndexRegistry()

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
	if err := loadBoundaryIndexMetadata(conn, boundary, indexes); err != nil {
		writePool.Put(conn)
		writePool.Close()
		readPool.Close()
		return nil, fmt.Errorf("load index metadata %s: %w", boundary, err)
	}
	writePool.Put(conn)

	return &BoundaryPools{Boundary: boundary, Write: writePool, Read: readPool, indexes: indexes}, nil
}

type sqlitePoolConfig struct {
	dir               string
	synchronous       string
	busyTimeoutMs     int
	readPoolSize      int
	cacheSize         int
	mmapSize          int64
	walAutoCheckpoint int
	tempStore         string
}

func normalizeSqlitePoolConfig(sqliteCfg config.SqliteConfig) (sqlitePoolConfig, error) {
	cfg := sqlitePoolConfig{
		dir:           sqliteCfg.Dir,
		synchronous:   strings.ToUpper(strings.TrimSpace(sqliteCfg.Synchronous)),
		busyTimeoutMs: sqliteCfg.BusyTimeoutMs,
		readPoolSize:  sqliteCfg.ReadPoolSize,
		cacheSize:     sqliteCfg.CacheSize,
		mmapSize:      sqliteCfg.MmapSize,
		tempStore:     strings.ToUpper(strings.TrimSpace(sqliteCfg.TempStore)),
	}
	if cfg.synchronous == "" {
		cfg.synchronous = "NORMAL"
	}
	if cfg.busyTimeoutMs == 0 {
		cfg.busyTimeoutMs = 5000
	}
	if cfg.readPoolSize <= 0 {
		cfg.readPoolSize = runtime.NumCPU()
	}
	if cfg.tempStore == "" {
		cfg.tempStore = "MEMORY"
	}
	cfg.walAutoCheckpoint = sqliteCfg.WalAutoCheckpoint

	switch cfg.synchronous {
	case "OFF", "NORMAL", "FULL", "EXTRA":
	default:
		return sqlitePoolConfig{}, fmt.Errorf("invalid sqlite synchronous %q", sqliteCfg.Synchronous)
	}
	if cfg.busyTimeoutMs < 0 {
		return sqlitePoolConfig{}, fmt.Errorf("sqlite busy timeout must be >= 0, got %d", sqliteCfg.BusyTimeoutMs)
	}
	if cfg.mmapSize < 0 {
		return sqlitePoolConfig{}, fmt.Errorf("sqlite mmap size must be >= 0, got %d", sqliteCfg.MmapSize)
	}
	if cfg.walAutoCheckpoint < 0 {
		return sqlitePoolConfig{}, fmt.Errorf("sqlite wal auto checkpoint must be >= 0, got %d", sqliteCfg.WalAutoCheckpoint)
	}
	switch cfg.tempStore {
	case "DEFAULT", "FILE", "MEMORY":
	default:
		return sqlitePoolConfig{}, fmt.Errorf("invalid sqlite temp store %q", sqliteCfg.TempStore)
	}

	return cfg, nil
}

func sqliteURI(dbPath string, cfg sqlitePoolConfig) string {
	params := url.Values{}
	params.Set("_journal_mode", "WAL")
	params.Set("_synchronous", cfg.synchronous)
	params.Set("_busy_timeout", strconv.Itoa(cfg.busyTimeoutMs))
	params.Set("_foreign_keys", "on")
	return "file:" + url.PathEscape(dbPath) + "?" + params.Encode()
}

func sqlitePragmas(cfg sqlitePoolConfig) []string {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = " + cfg.synchronous,
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = " + strconv.Itoa(cfg.busyTimeoutMs),
		"PRAGMA temp_store = " + cfg.tempStore,
	}
	if cfg.cacheSize != 0 {
		pragmas = append(pragmas, "PRAGMA cache_size = "+strconv.Itoa(cfg.cacheSize))
	}
	if cfg.mmapSize > 0 {
		pragmas = append(pragmas, "PRAGMA mmap_size = "+strconv.FormatInt(cfg.mmapSize, 10))
	}
	if cfg.walAutoCheckpoint > 0 {
		pragmas = append(pragmas, "PRAGMA wal_autocheckpoint = "+strconv.Itoa(cfg.walAutoCheckpoint))
	}
	return pragmas
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
