package domain_test

import (
	"testing"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAdapterRegistry(t *testing.T) {
	t.Run("creates registry with one config", func(t *testing.T) {
		reg, err := domain.NewAdapterRegistry(domain.AdapterRegistryConfig{
			RuntimeType:   domain.RuntimeDockerCompose,
			Adapter:       &domain.MockRuntimeAdapter{},
			PortAllocator: &domain.MockPortAllocator{},
		})
		require.NoError(t, err)
		assert.NotNil(t, reg)
	})

	t.Run("creates registry with multiple configs", func(t *testing.T) {
		reg, err := domain.NewAdapterRegistry(
			domain.AdapterRegistryConfig{
				RuntimeType:   domain.RuntimeDockerCompose,
				Adapter:       &domain.MockRuntimeAdapter{},
				PortAllocator: &domain.MockPortAllocator{},
			},
			domain.AdapterRegistryConfig{
				RuntimeType:   domain.RuntimeKubernetes,
				Adapter:       &domain.MockRuntimeAdapter{},
				PortAllocator: &domain.MockPortAllocator{},
			},
		)
		require.NoError(t, err)
		assert.NotNil(t, reg)
	})

	t.Run("returns error with no configs", func(t *testing.T) {
		_, err := domain.NewAdapterRegistry()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one adapter configuration")
	})

	t.Run("returns error with nil adapter", func(t *testing.T) {
		_, err := domain.NewAdapterRegistry(domain.AdapterRegistryConfig{
			RuntimeType:   domain.RuntimeDockerCompose,
			Adapter:       nil,
			PortAllocator: &domain.MockPortAllocator{},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be nil")
	})

	t.Run("returns error with nil port allocator", func(t *testing.T) {
		_, err := domain.NewAdapterRegistry(domain.AdapterRegistryConfig{
			RuntimeType:   domain.RuntimeDockerCompose,
			Adapter:       &domain.MockRuntimeAdapter{},
			PortAllocator: nil,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be nil")
	})
}

func TestAdapterRegistry_GetAdapter(t *testing.T) {
	adapter := &domain.MockRuntimeAdapter{}
	reg, err := domain.NewAdapterRegistry(domain.AdapterRegistryConfig{
		RuntimeType:   domain.RuntimeDockerCompose,
		Adapter:       adapter,
		PortAllocator: &domain.MockPortAllocator{},
	})
	require.NoError(t, err)

	t.Run("returns registered adapter", func(t *testing.T) {
		got, err := reg.GetAdapter(domain.RuntimeDockerCompose)
		require.NoError(t, err)
		assert.Same(t, adapter, got)
	})

	t.Run("returns ErrUnsupportedRuntime for unregistered type", func(t *testing.T) {
		_, err := reg.GetAdapter(domain.RuntimeKubernetes)
		require.ErrorIs(t, err, domain.ErrUnsupportedRuntime)
	})

	t.Run("returns ErrUnsupportedRuntime for unknown type", func(t *testing.T) {
		_, err := reg.GetAdapter(domain.RuntimeType("podman"))
		require.ErrorIs(t, err, domain.ErrUnsupportedRuntime)
	})
}

func TestAdapterRegistry_GetPortAllocator(t *testing.T) {
	allocator := &domain.MockPortAllocator{}
	reg, err := domain.NewAdapterRegistry(domain.AdapterRegistryConfig{
		RuntimeType:   domain.RuntimeDockerCompose,
		Adapter:       &domain.MockRuntimeAdapter{},
		PortAllocator: allocator,
	})
	require.NoError(t, err)

	t.Run("returns registered allocator", func(t *testing.T) {
		got, err := reg.GetPortAllocator(domain.RuntimeDockerCompose)
		require.NoError(t, err)
		assert.Same(t, allocator, got)
	})

	t.Run("returns ErrUnsupportedRuntime for unregistered type", func(t *testing.T) {
		_, err := reg.GetPortAllocator(domain.RuntimeKubernetes)
		require.ErrorIs(t, err, domain.ErrUnsupportedRuntime)
	})
}
