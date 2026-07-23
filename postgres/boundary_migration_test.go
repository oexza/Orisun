package postgres

import (
	"testing"

	"github.com/OrisunLabs/Orisun/config"
)

func TestLegacyBoundaryDefinitionsUsesMappingsAsAuthority(t *testing.T) {
	definitions := LegacyBoundaryDefinitions(
		map[string]config.BoundaryToPostgresSchemaMapping{
			"sales": {Boundary: "sales", Schema: "tenant_data"},
			"admin": {Boundary: "admin", Schema: "public"},
		},
	)
	if len(definitions) != 2 {
		t.Fatalf("definitions = %d", len(definitions))
	}
	if definitions[0].Name != "admin" || definitions[0].Description != "" || definitions[0].Placement.Namespace != "public" {
		t.Fatalf("admin definition = %#v", definitions[0])
	}
	if definitions[1].Name != "sales" || definitions[1].Description != "" || definitions[1].Placement.Namespace != "tenant_data" {
		t.Fatalf("sales definition = %#v", definitions[1])
	}
}
