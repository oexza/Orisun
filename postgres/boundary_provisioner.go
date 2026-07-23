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

// ProvisionBoundary validates and creates the physical PostgreSQL boundary.
// Cluster coordination ensures only one server process performs this work.
func (p *PostgresBoundaryProvisioner) ProvisionBoundary(ctx context.Context, definition boundarymodel.Definition) error {
	if p == nil || p.registry == nil || p.migrate == nil {
		return fmt.Errorf("postgres boundary provisioner is not configured")
	}
	schema, err := postgresPlacement(definition)
	if err != nil {
		return err
	}

	p.registry.provisionMu.Lock()
	defer p.registry.provisionMu.Unlock()
	if err := p.migrate(ctx, definition.Name, schema); err != nil {
		return fmt.Errorf("migrate boundary %s in schema %s: %w", definition.Name, schema, err)
	}
	return nil
}

// InstallBoundary registers an already-provisioned boundary in one process's
// boundary-aware SQL adapters.
func (p *PostgresBoundaryProvisioner) InstallBoundary(_ context.Context, definition boundarymodel.Definition) error {
	if p == nil || p.registry == nil {
		return fmt.Errorf("postgres boundary provisioner is not configured")
	}
	schema, err := postgresPlacement(definition)
	if err != nil {
		return err
	}
	if err := p.registry.Register(definition.Name, schema); err != nil {
		return fmt.Errorf("register boundary %s: %w", definition.Name, err)
	}
	return nil
}

func postgresPlacement(definition boundarymodel.Definition) (string, error) {
	if strings.ToLower(strings.TrimSpace(definition.Placement.Backend)) != "postgres" {
		return "", fmt.Errorf("boundary %s uses unsupported backend %q", definition.Name, definition.Placement.Backend)
	}
	if err := validateBoundaryName(definition.Name); err != nil {
		return "", fmt.Errorf("invalid boundary name %s: %w", definition.Name, err)
	}
	schema := definition.Placement.Namespace
	if err := validateBoundaryName(schema); err != nil {
		return "", fmt.Errorf("invalid schema name %s: %w", schema, err)
	}
	return schema, nil
}
