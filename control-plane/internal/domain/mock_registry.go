package domain

import "context"

// MockAdapterRegistry is a test double for AdapterRegistry.
type MockAdapterRegistry struct {
	GetAdapterFn       func(rt RuntimeType) (RuntimeAdapter, error)
	GetPortAllocatorFn func(rt RuntimeType) (PortAllocator, error)
}

func (m *MockAdapterRegistry) GetAdapter(rt RuntimeType) (RuntimeAdapter, error) {
	if m.GetAdapterFn != nil {
		return m.GetAdapterFn(rt)
	}
	return &MockRuntimeAdapter{}, nil
}

func (m *MockAdapterRegistry) GetPortAllocator(rt RuntimeType) (PortAllocator, error) {
	if m.GetPortAllocatorFn != nil {
		return m.GetPortAllocatorFn(rt)
	}
	return &MockPortAllocator{}, nil
}

// MockPortAllocator is a test double for PortAllocator.
type MockPortAllocator struct {
	AllocatePortsFn func(ctx context.Context) (*PortSet, error)
}

func (m *MockPortAllocator) AllocatePorts(ctx context.Context) (*PortSet, error) {
	if m.AllocatePortsFn != nil {
		return m.AllocatePortsFn(ctx)
	}
	return &PortSet{KongHTTP: 28081, PostgresPort: 28082, PoolerPort: 28083}, nil
}
