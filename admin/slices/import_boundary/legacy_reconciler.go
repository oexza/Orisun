package import_boundary

import (
	"context"
	"fmt"
	"sort"
	"strings"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"github.com/goccy/go-json"
)

type LegacyReconciliationResult struct {
	Imported []string
	Existing []string
}

// ReconcileLegacyBoundaries migrates startup-configured definitions through
// the normal import command. It is orchestration, not a command: each missing
// definition is handled by ImportBoundaryCommandHandler and emits its event.
func ReconcileLegacyBoundaries(
	ctx context.Context,
	definitions []boundarymodel.Definition,
	adminBoundary string,
	appender EventAppender,
	retriever LatestByCriteriaRetriever,
) (LegacyReconciliationResult, error) {
	ordered := append([]boundarymodel.Definition(nil), definitions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	result := LegacyReconciliationResult{
		Imported: make([]string, 0, len(ordered)),
		Existing: make([]string, 0, len(ordered)),
	}
	seen := make(map[string]struct{}, len(ordered))
	for _, definition := range ordered {
		if _, duplicate := seen[definition.Name]; duplicate {
			return LegacyReconciliationResult{}, fmt.Errorf("legacy boundary %q is configured more than once", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		_, err := ImportBoundaryCommandHandler(
			ctx,
			ImportBoundaryCommand{
				Name:        definition.Name,
				Description: definition.Description,
				Placement:   definition.Placement,
				Metadata: CommandMetadata{
					"source":    "legacy_config",
					"operation": "import_boundary",
					"migration": "configured_boundaries_to_catalog",
				},
			},
			adminBoundary,
			appender,
			retriever,
		)
		if statuscode.CodeOf(err) == statuscode.AlreadyExists {
			if err := verifyExistingDefinition(ctx, adminBoundary, definition, retriever); err != nil {
				return result, err
			}
			result.Existing = append(result.Existing, definition.Name)
			continue
		}
		if err != nil {
			return result, fmt.Errorf("reconcile legacy boundary %q: %w", definition.Name, err)
		}
		result.Imported = append(result.Imported, definition.Name)
	}
	return result, nil
}

func verifyExistingDefinition(
	ctx context.Context,
	adminBoundary string,
	expected boundarymodel.Definition,
	retriever LatestByCriteriaRetriever,
) error {
	model, err := loadContext(ctx, adminBoundary, expected.Name, retriever)
	if err != nil {
		return fmt.Errorf("load existing legacy boundary %q: %w", expected.Name, err)
	}
	if model.existing == nil {
		return fmt.Errorf("legacy boundary %q reported as existing but its definition event was not found", expected.Name)
	}
	actual, err := definitionFromEvent(*model.existing)
	if err != nil {
		return fmt.Errorf("decode existing legacy boundary %q: %w", expected.Name, err)
	}
	if !samePlacement(actual.Placement, expected.Placement) {
		return fmt.Errorf(
			"legacy boundary %q placement conflicts with catalog: configured %s/%s, catalog %s/%s",
			expected.Name,
			expected.Placement.Backend,
			expected.Placement.Namespace,
			actual.Placement.Backend,
			actual.Placement.Namespace,
		)
	}
	return nil
}

func definitionFromEvent(event coreeventstore.ReadEvent) (boundarymodel.Definition, error) {
	switch event.EventType {
	case adminevents.EventTypeBoundaryCreated:
		var data adminevents.BoundaryCreated
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return boundarymodel.Definition{}, err
		}
		return boundarymodel.Definition{Name: data.Boundary, Description: data.Description, Placement: data.Placement}, nil
	case adminevents.EventTypeBoundaryImported:
		var data adminevents.BoundaryImported
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			return boundarymodel.Definition{}, err
		}
		return boundarymodel.Definition{Name: data.Boundary, Description: data.Description, Placement: data.Placement}, nil
	default:
		return boundarymodel.Definition{}, fmt.Errorf("unsupported definition event %q", event.EventType)
	}
}

func samePlacement(left, right boundarymodel.Placement) bool {
	return strings.EqualFold(strings.TrimSpace(left.Backend), strings.TrimSpace(right.Backend)) &&
		strings.TrimSpace(left.Namespace) == strings.TrimSpace(right.Namespace)
}
