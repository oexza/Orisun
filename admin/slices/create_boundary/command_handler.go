package create_boundary

import (
	"context"
	"strings"

	boundarymodel "github.com/OrisunLabs/Orisun/boundary"
	adminevents "github.com/OrisunLabs/Orisun/boundary/events"
	coreeventstore "github.com/OrisunLabs/Orisun/eventstore"
	"github.com/OrisunLabs/Orisun/internal/statuscode"
	"github.com/goccy/go-json"
	"github.com/google/uuid"
)

type CommandMetadata map[string]any

type CreateBoundaryCommand struct {
	Name        string
	Description string
	Placement   boundarymodel.Placement
	Metadata    CommandMetadata
}

type CreateBoundaryResult struct {
	Boundary boundarymodel.Boundary
}

type LatestByCriteriaRetriever interface {
	LatestByCriteria(ctx context.Context, request coreeventstore.LatestByCriteriaRequest) (coreeventstore.LatestByCriteriaResult, error)
}

type EventAppender interface {
	Append(ctx context.Context, request coreeventstore.AppendRequest) (coreeventstore.AppendResult, error)
}

func CreateBoundaryCommandHandler(
	ctx context.Context,
	command CreateBoundaryCommand,
	adminBoundary string,
	appender EventAppender,
	retriever LatestByCriteriaRetriever,
) (CreateBoundaryResult, error) {
	definition, err := normalize(command)
	if err != nil {
		return CreateBoundaryResult{}, err
	}
	if adminBoundary == "" || appender == nil || retriever == nil {
		return CreateBoundaryResult{}, statuscode.New(statuscode.Internal, "create boundary handler is not configured")
	}
	model, err := loadContext(ctx, adminBoundary, definition.Name, retriever)
	if err != nil {
		return CreateBoundaryResult{}, err
	}
	if model.exists {
		return CreateBoundaryResult{}, statuscode.Errorf(statuscode.AlreadyExists, "boundary %q already exists", definition.Name)
	}

	eventID, err := uuid.NewV7()
	if err != nil {
		return CreateBoundaryResult{}, statuscode.Errorf(statuscode.Internal, "create boundary event id: %v", err)
	}
	data, err := eventData(adminevents.EventTypeBoundaryCreated, adminevents.BoundaryCreated{
		Boundary:    definition.Name,
		Description: definition.Description,
		Placement:   definition.Placement,
	})
	if err != nil {
		return CreateBoundaryResult{}, statuscode.Errorf(statuscode.Internal, "prepare %s data: %v", adminevents.EventTypeBoundaryCreated, err)
	}
	metadata, err := json.Marshal(metadataWithQuery(command.Metadata, model.query))
	if err != nil {
		return CreateBoundaryResult{}, statuscode.Errorf(statuscode.Internal, "prepare %s metadata: %v", adminevents.EventTypeBoundaryCreated, err)
	}
	appendResult, err := appender.Append(ctx, coreeventstore.AppendRequest{
		Boundary: adminBoundary,
		Events: []coreeventstore.EventToAppend{{
			EventID:   eventID.String(),
			EventType: adminevents.EventTypeBoundaryCreated,
			Data:      data,
			Metadata:  string(metadata),
		}},
		ExpectedPosition: model.position,
		Subset:           model.query,
	})
	if err != nil {
		return CreateBoundaryResult{}, err
	}
	position := appendResult.Position
	return CreateBoundaryResult{Boundary: boundarymodel.Boundary{
		Name:               definition.Name,
		Description:        definition.Description,
		Placement:          definition.Placement,
		Status:             boundarymodel.StatusProvisioning,
		Origin:             boundarymodel.OriginCreated,
		DefinitionPosition: &position,
		StatusPosition:     &coreeventstore.Position{CommitPosition: position.CommitPosition, PreparePosition: position.PreparePosition},
	}}, nil
}

type commandContext struct {
	exists   bool
	position *coreeventstore.Position
	query    coreeventstore.Query
}

func loadContext(ctx context.Context, adminBoundary, name string, retriever LatestByCriteriaRetriever) (*commandContext, error) {
	criteria := definitionCriteria(name)
	latest, err := retriever.LatestByCriteria(ctx, coreeventstore.LatestByCriteriaRequest{
		Boundary: adminBoundary,
		Criteria: criteria,
	})
	if err != nil {
		return nil, err
	}
	if len(latest.Matches) != len(criteria) {
		return nil, statuscode.Errorf(statuscode.Internal, "boundary definition query returned %d matches, expected %d", len(latest.Matches), len(criteria))
	}
	model := &commandContext{
		position: &latest.ContextPosition,
		query:    subsetQuery(criteria),
	}
	for _, match := range latest.Matches {
		if !match.Found {
			continue
		}
		model.exists = true
	}
	return model, nil
}

func normalize(command CreateBoundaryCommand) (boundarymodel.Definition, error) {
	definition := boundarymodel.Definition{
		Name:        strings.TrimSpace(command.Name),
		Description: strings.TrimSpace(command.Description),
		Placement: boundarymodel.Placement{
			Backend:   strings.TrimSpace(command.Placement.Backend),
			Namespace: strings.TrimSpace(command.Placement.Namespace),
		},
	}
	if err := boundarymodel.ValidateName(definition.Name); err != nil {
		return boundarymodel.Definition{}, statuscode.Errorf(statuscode.InvalidArgument, "invalid boundary %q: %v", definition.Name, err)
	}
	if definition.Placement.Backend == "" || definition.Placement.Namespace == "" {
		return boundarymodel.Definition{}, statuscode.New(statuscode.InvalidArgument, "boundary placement backend and namespace are required")
	}
	return definition, nil
}

func definitionCriteria(name string) []coreeventstore.Criterion {
	return []coreeventstore.Criterion{
		{Tags: []coreeventstore.Tag{{Key: "boundary", Value: name}, {Key: "eventType", Value: adminevents.EventTypeBoundaryCreated}}},
		{Tags: []coreeventstore.Tag{{Key: "boundary", Value: name}, {Key: "eventType", Value: adminevents.EventTypeBoundaryImported}}},
	}
}

func subsetQuery(criteria []coreeventstore.Criterion) coreeventstore.Query {
	return coreeventstore.Query{Criteria: criteria}
}

func metadataWithQuery(metadata CommandMetadata, query coreeventstore.Query) map[string]any {
	merged := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		merged[key] = value
	}
	if encoded, err := json.Marshal(query); err == nil {
		merged["query"] = string(encoded)
	}
	return merged
}

func eventData(eventType string, value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return "", err
	}
	object["eventType"] = eventType
	encoded, err = json.Marshal(object)
	return string(encoded), err
}
