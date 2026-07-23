package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
)

type migrateBoundary func(ctx context.Context, boundary, schema string) error

// PostgresBoundaryProvisioner is the PostgreSQL adapter for the
// boundary_provisioning slice's ProvisionBoundary function port.
type PostgresBoundaryProvisioner struct {
	registry *BoundaryRegistry
	migrate  migrateBoundary
}

func NewPostgresBoundaryProvisioner(db *sql.DB, registry *BoundaryRegistry, dialect string) *PostgresBoundaryProvisioner {
	return newPostgresBoundaryProvisioner(registry, func(ctx context.Context, boundary, schema string) error {
		return RunDbScriptsWithDialect(db, boundary, schema, false, dialect, ctx)
	})
}

func newPostgresBoundaryProvisioner(registry *BoundaryRegistry, migrate migrateBoundary) *PostgresBoundaryProvisioner {
	return &PostgresBoundaryProvisioner{registry: registry, migrate: migrate}
}

// ProvisionBoundary validates and creates the physical PostgreSQL boundary,
// then publishes it to every component sharing the registry. Its signature is
// intentionally assignable to boundary_provisioning.ProvisionBoundary.
func (p *PostgresBoundaryProvisioner) ProvisionBoundary(ctx context.Context, definition boundarymodel.Definition) error {
	if p == nil || p.registry == nil || p.migrate == nil {
		return fmt.Errorf("postgres boundary provisioner is not configured")
	}
	if strings.ToLower(strings.TrimSpace(definition.Placement.Backend)) != "postgres" {
		return fmt.Errorf("boundary %s uses unsupported backend %q", definition.Name, definition.Placement.Backend)
	}
	if err := validateBoundaryName(definition.Name); err != nil {
		return fmt.Errorf("invalid boundary name %s: %w", definition.Name, err)
	}
	schema := definition.Placement.Namespace
	if err := validateBoundaryName(schema); err != nil {
		return fmt.Errorf("invalid schema name %s: %w", schema, err)
	}

	// Hold the registry's provisioning lock across the physical migration and
	// registration. Concurrent deliveries of the same definition perform the
	// migration once, while a conflicting placement fails before any DDL runs.
	p.registry.provisionMu.Lock()
	defer p.registry.provisionMu.Unlock()

	if existing, ok := p.registry.lookup(definition.Name); ok {
		if existing.mapping.Schema == schema {
			return nil
		}
		return fmt.Errorf(
			"boundary %s is already registered in schema %s",
			definition.Name,
			existing.mapping.Schema,
		)
	}
	if err := p.migrate(ctx, definition.Name, schema); err != nil {
		return fmt.Errorf("migrate boundary %s in schema %s: %w", definition.Name, schema, err)
	}
	if err := p.registry.Register(definition.Name, schema); err != nil {
		return fmt.Errorf("register boundary %s: %w", definition.Name, err)
	}
	return nil
}
