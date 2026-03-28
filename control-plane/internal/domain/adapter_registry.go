package domain

import (
	"errors"
	"fmt"
)

// ErrUnsupportedRuntime is returned when a requested runtime type has no registered adapter.
var ErrUnsupportedRuntime = errors.New("unsupported runtime type")

// AdapterRegistry provides runtime-specific component lookup.
// Implementations must be safe for concurrent use.
type AdapterRegistry interface {
	// GetAdapter returns the RuntimeAdapter for the given runtime type.
	// Returns ErrUnsupportedRuntime if the runtime type is not registered.
	GetAdapter(rt RuntimeType) (RuntimeAdapter, error)

	// GetPortAllocator returns the PortAllocator for the given runtime type.
	// Returns ErrUnsupportedRuntime if the runtime type is not registered.
	GetPortAllocator(rt RuntimeType) (PortAllocator, error)
}

// AdapterRegistryConfig holds the components to register for each runtime type.
type AdapterRegistryConfig struct {
	RuntimeType   RuntimeType
	Adapter       RuntimeAdapter
	PortAllocator PortAllocator
}

// defaultAdapterRegistry is the production implementation of AdapterRegistry.
// It holds a fixed set of adapters and allocators, configured at startup.
type defaultAdapterRegistry struct {
	adapters   map[RuntimeType]RuntimeAdapter
	allocators map[RuntimeType]PortAllocator
}

// NewAdapterRegistry creates a new AdapterRegistry from the given configurations.
// At least one configuration must be provided.
func NewAdapterRegistry(configs ...AdapterRegistryConfig) (AdapterRegistry, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one adapter configuration is required")
	}
	reg := &defaultAdapterRegistry{
		adapters:   make(map[RuntimeType]RuntimeAdapter, len(configs)),
		allocators: make(map[RuntimeType]PortAllocator, len(configs)),
	}
	for _, c := range configs {
		reg.adapters[c.RuntimeType] = c.Adapter
		reg.allocators[c.RuntimeType] = c.PortAllocator
	}
	return reg, nil
}

func (r *defaultAdapterRegistry) GetAdapter(rt RuntimeType) (RuntimeAdapter, error) {
	a, ok := r.adapters[rt]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedRuntime, rt)
	}
	return a, nil
}

func (r *defaultAdapterRegistry) GetPortAllocator(rt RuntimeType) (PortAllocator, error) {
	a, ok := r.allocators[rt]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedRuntime, rt)
	}
	return a, nil
}
