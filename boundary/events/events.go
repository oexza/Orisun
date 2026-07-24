package events

import boundarymodel "github.com/OrisunLabs/Orisun/boundary"

const (
	EventTypeBoundaryCreated   = "$BoundaryCreated"
	EventTypeBoundaryActivated = "$BoundaryActivated"
	EventTypeBoundaryFailed    = "$BoundaryProvisioningFailed"
)

// BoundaryCreated records a boundary definition. ExistedBeforeCatalog marks
// storage discovered or attached before its definition entered the catalog.
type BoundaryCreated struct {
	Boundary             string                  `json:"boundary"`
	Description          string                  `json:"description,omitempty"`
	Placement            boundarymodel.Placement `json:"placement"`
	ExistedBeforeCatalog bool                    `json:"existedBeforeCatalog,omitempty"`
}

// BoundaryActivated records successful physical and runtime provisioning.
type BoundaryActivated struct {
	Boundary string `json:"boundary"`
}

// BoundaryProvisioningFailed records the latest provisioning failure.
type BoundaryProvisioningFailed struct {
	Boundary string `json:"boundary"`
	Error    string `json:"error"`
}
