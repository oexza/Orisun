package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OrisunLabs/Orisun/config"
	"github.com/stretchr/testify/require"
)

func TestDiscoverBoundaryNamesUsesEventDatabasesAndBootstrapsAdmin(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"orders.db", "orders_metadata.db", "orisun_metadata.db", "notes.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}

	names, err := DiscoverBoundaryNames(config.SqliteConfig{Dir: dir}, "orisun_admin")
	require.NoError(t, err)
	require.Equal(t, []string{"orders", "orisun_admin"}, names)
}

func TestDiscoverBoundaryNamesFreshDirectoryReturnsAdmin(t *testing.T) {
	names, err := DiscoverBoundaryNames(config.SqliteConfig{Dir: filepath.Join(t.TempDir(), "missing")}, "admin")
	require.NoError(t, err)
	require.Equal(t, []string{"admin"}, names)
}
