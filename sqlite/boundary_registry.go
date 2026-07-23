package sqlite

import "sync"

// BoundaryRegistry is the shared, concurrency-safe source of SQLite event and
// metadata pools. Every boundary-aware adapter in one runtime uses the same
// registry so provisioning becomes visible atomically.
type BoundaryRegistry struct {
	mu            sync.RWMutex
	provisionMu   sync.Mutex
	pools         map[string]*BoundaryPools
	metadataPools map[string]*BoundaryPools
}

func NewBoundaryRegistry(pools, metadataPools map[string]*BoundaryPools) *BoundaryRegistry {
	if pools == nil {
		pools = make(map[string]*BoundaryPools)
	}
	if metadataPools == nil {
		metadataPools = make(map[string]*BoundaryPools)
	}
	return &BoundaryRegistry{pools: pools, metadataPools: metadataPools}
}

func (r *BoundaryRegistry) eventPool(boundary string) (*BoundaryPools, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	pool, ok := r.pools[boundary]
	return pool, ok
}

func (r *BoundaryRegistry) metadataPool(boundary string) (*BoundaryPools, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	pool, ok := r.metadataPools[boundary]
	return pool, ok
}

func (r *BoundaryRegistry) register(boundary string, eventPool, metadataPool *BoundaryPools) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.pools[boundary]; exists {
		return false
	}
	r.pools[boundary] = eventPool
	r.metadataPools[boundary] = metadataPool
	return true
}

func (r *BoundaryRegistry) snapshots() (map[string]*BoundaryPools, map[string]*BoundaryPools) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pools := make(map[string]*BoundaryPools, len(r.pools))
	metadataPools := make(map[string]*BoundaryPools, len(r.metadataPools))
	for name, pool := range r.pools {
		pools[name] = pool
	}
	for name, pool := range r.metadataPools {
		metadataPools[name] = pool
	}
	return pools, metadataPools
}
