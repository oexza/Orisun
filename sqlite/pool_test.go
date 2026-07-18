package sqlite

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/OrisunLabs/Orisun/config"
)

func TestNormalizeSqlitePoolConfig_Defaults(t *testing.T) {
	cfg, err := normalizeSqlitePoolConfig(config.SqliteConfig{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("normalize default config: %v", err)
	}
	if cfg.synchronous != "FULL" {
		t.Fatalf("expected FULL synchronous, got %q", cfg.synchronous)
	}
	if cfg.busyTimeoutMs != 5000 {
		t.Fatalf("expected 5000ms busy timeout, got %d", cfg.busyTimeoutMs)
	}
	if cfg.readPoolSize != runtime.NumCPU() {
		t.Fatalf("expected runtime.NumCPU read pool, got %d", cfg.readPoolSize)
	}
	if cfg.tempStore != "MEMORY" {
		t.Fatalf("expected MEMORY temp store, got %q", cfg.tempStore)
	}
}

func TestNormalizeSqlitePoolConfig_CustomPragmas(t *testing.T) {
	cfg, err := normalizeSqlitePoolConfig(config.SqliteConfig{
		Dir:               t.TempDir(),
		Synchronous:       "full",
		BusyTimeoutMs:     250,
		ReadPoolSize:      2,
		CacheSize:         -4096,
		MmapSize:          1 << 20,
		WalAutoCheckpoint: 128,
		TempStore:         "file",
	})
	if err != nil {
		t.Fatalf("normalize custom config: %v", err)
	}
	if cfg.synchronous != "FULL" || cfg.busyTimeoutMs != 250 || cfg.readPoolSize != 2 || cfg.tempStore != "FILE" {
		t.Fatalf("unexpected normalized config: %#v", cfg)
	}

	pragmas := sqlitePragmas(cfg)
	for _, want := range []string{
		"PRAGMA synchronous = FULL",
		"PRAGMA busy_timeout = 250",
		"PRAGMA temp_store = FILE",
		"PRAGMA cache_size = -4096",
		"PRAGMA mmap_size = 1048576",
		"PRAGMA wal_autocheckpoint = 128",
	} {
		if !slices.Contains(pragmas, want) {
			t.Fatalf("expected pragma %q in %v", want, pragmas)
		}
	}

	uri := sqliteURI("/tmp/example.db", cfg)
	if !strings.Contains(uri, "_synchronous=FULL") || !strings.Contains(uri, "_busy_timeout=250") {
		t.Fatalf("expected custom pragma URI params, got %q", uri)
	}
}

func TestNormalizeSqlitePoolConfig_InvalidValues(t *testing.T) {
	cases := []config.SqliteConfig{
		{Dir: t.TempDir(), Synchronous: "sometimes"},
		{Dir: t.TempDir(), BusyTimeoutMs: -1},
		{Dir: t.TempDir(), MmapSize: -1},
		{Dir: t.TempDir(), WalAutoCheckpoint: -1},
		{Dir: t.TempDir(), TempStore: "ram"},
	}
	for _, cfg := range cases {
		if _, err := normalizeSqlitePoolConfig(cfg); err == nil {
			t.Fatalf("expected invalid config %#v to fail", cfg)
		}
	}
}
