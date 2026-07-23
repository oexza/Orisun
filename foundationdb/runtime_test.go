package foundationdb

import "testing"

func TestLegacyBoundaryDefinitionsUsesFoundationDBRoot(t *testing.T) {
	definitions := LegacyBoundaryDefinitions([]string{"sales", "admin"}, "tenant_root")
	if len(definitions) != 2 || definitions[0].Name != "admin" || definitions[1].Name != "sales" {
		t.Fatalf("definitions = %#v", definitions)
	}
	for _, definition := range definitions {
		if definition.Placement.Backend != "foundationdb" || definition.Placement.Namespace != "tenant_root" {
			t.Fatalf("placement = %#v", definition.Placement)
		}
	}
}
