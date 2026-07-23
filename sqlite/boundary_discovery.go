package sqlite

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	"github.com/OrisunLabs/Orisun/config"
)

// DiscoverBoundaryNames replaces the removed startup boundary list for SQLite.
// Existing event databases are authoritative; the admin boundary is included
// on a fresh installation so its catalog can bootstrap itself.
func DiscoverBoundaryNames(sqliteCfg config.SqliteConfig, adminBoundary string) ([]string, error) {
	if err := validateIdentifier(adminBoundary); err != nil {
		return nil, fmt.Errorf("invalid admin boundary %q: %w", adminBoundary, err)
	}
	entries, err := os.ReadDir(sqliteCfg.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read sqlite directory: %w", err)
	}

	names := map[string]struct{}{adminBoundary: {}}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		if !strings.HasSuffix(filename, ".db") || strings.HasSuffix(filename, "_metadata.db") || filename == "orisun_metadata.db" {
			continue
		}
		name := strings.TrimSuffix(filename, ".db")
		if err := validateIdentifier(name); err != nil {
			return nil, fmt.Errorf("invalid SQLite boundary file %q: %w", filename, err)
		}
		names[name] = struct{}{}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func LegacyBoundaryDefinitions(names []string) []boundarymodel.Definition {
	definitions := make([]boundarymodel.Definition, len(names))
	for i, name := range names {
		definitions[i] = boundarymodel.Definition{
			Name:      name,
			Placement: boundarymodel.Placement{Backend: "sqlite", Namespace: name},
		}
	}
	return definitions
}
