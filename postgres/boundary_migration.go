package postgres

import (
	"sort"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	"github.com/OrisunLabs/Orisun/config"
)

// LegacyBoundaryDefinitions translates existing PostgreSQL schema mappings
// into import commands. ORISUN_PG_SCHEMAS identifies the physical boundaries
// that must be represented in the event-backed catalog.
func LegacyBoundaryDefinitions(
	mappings map[string]config.BoundaryToPostgresSchemaMapping,
) []boundarymodel.Definition {
	names := make([]string, 0, len(mappings))
	for name := range mappings {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]boundarymodel.Definition, len(names))
	for i, name := range names {
		definitions[i] = boundarymodel.Definition{
			Name: name,
			Placement: boundarymodel.Placement{
				Backend:   "postgres",
				Namespace: mappings[name].Schema,
			},
		}
	}
	return definitions
}

func BoundaryNames(mappings map[string]config.BoundaryToPostgresSchemaMapping) []string {
	definitions := LegacyBoundaryDefinitions(mappings)
	names := make([]string, len(definitions))
	for i := range definitions {
		names[i] = definitions[i].Name
	}
	return names
}
