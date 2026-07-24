package orisun

import boundarymodel "github.com/OrisunLabs/Orisun/boundary"

// Deprecated: use boundary.Status.
type BoundaryStatus = boundarymodel.Status

const (
	// Deprecated: use boundary.StatusProvisioning.
	BoundaryStatusProvisioning = boundarymodel.StatusProvisioning
	// Deprecated: use boundary.StatusActive.
	BoundaryStatusActive = boundarymodel.StatusActive
	// Deprecated: use boundary.StatusFailed.
	BoundaryStatusFailed = boundarymodel.StatusFailed
)

// Deprecated: use boundary.Placement.
type BoundaryPlacement = boundarymodel.Placement

// Deprecated: use boundary.Definition.
type BoundaryDefinition = boundarymodel.Definition

// Deprecated: use boundary.Boundary.
type Boundary = boundarymodel.Boundary

// ValidateBoundaryName is retained for compatibility.
// Deprecated: use boundary.ValidateName.
func ValidateBoundaryName(name string) error {
	return boundarymodel.ValidateName(name)
}
