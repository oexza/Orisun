package foundationdb

import (
	"context"
	"sort"

	common "github.com/OrisunLabs/Orisun/admin/slices/common"
	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	"github.com/OrisunLabs/Orisun/orisun"
)

type DatabaseRuntime struct {
	SaveEvents        orisun.EventsSaver
	GetEvents         orisun.EventsRetriever
	LockProvider      orisun.LockProvider
	AdminDB           common.DB
	EventPublishing   orisun.EventPublishingTracker
	SignalProvider    func(string) orisun.EventSignal
	ProvisionBoundary func(context.Context, boundarymodel.Definition) error
	InitialBoundaries []string
	BoundaryNamespace string
	Close             func(context.Context)
}

func LegacyBoundaryDefinitions(names []string, root string) []boundarymodel.Definition {
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	definitions := make([]boundarymodel.Definition, len(ordered))
	for i, name := range ordered {
		definitions[i] = boundarymodel.Definition{
			Name:      name,
			Placement: boundarymodel.Placement{Backend: "foundationdb", Namespace: root},
		}
	}
	return definitions
}
