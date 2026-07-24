package config

import "testing"

func TestPostgresSchemaMappingsTrimAndValidateMigrationSource(t *testing.T) {
	mappings, err := parsePostgresSchemaMappings(" orders : tenant_data , orisun_admin : admin ")
	if err != nil {
		t.Fatalf("parsePostgresSchemaMappings() error = %v", err)
	}
	if mappings["orders"].Schema != "tenant_data" || mappings["orisun_admin"].Schema != "admin" {
		t.Fatalf("mappings = %#v", mappings)
	}
}

func TestPostgresSchemaMappingsRejectDuplicateBoundary(t *testing.T) {
	if _, err := parsePostgresSchemaMappings("orders:public, orders:tenant_data"); err == nil {
		t.Fatal("parsePostgresSchemaMappings() accepted duplicate boundary")
	}
}

func TestPostgresSchemaMappingsRejectMalformedEntry(t *testing.T) {
	if _, err := parsePostgresSchemaMappings("orders"); err == nil {
		t.Fatal("parsePostgresSchemaMappings() accepted malformed entry")
	}
}
