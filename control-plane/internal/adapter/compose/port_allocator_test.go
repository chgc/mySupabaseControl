package compose

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// ── AllocatePorts: empty store ────────────────────────────────────────────────

func TestAllocatePorts_EmptyStore_UsesBasePorts(t *testing.T) {
	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{},
		&mockConfigRepo{},
		func(_ int) bool { return true },
	)

	portSet, err := allocator.AllocatePorts(context.Background())
	require.NoError(t, err)
	assert.Equal(t, defaultPortBases.kongHTTP, portSet.KongHTTP)
	assert.Equal(t, defaultPortBases.postgresPort, portSet.PostgresPort)
	assert.Equal(t, defaultPortBases.poolerPort, portSet.PoolerPort)
	assert.Equal(t, defaultPortBases.studioPort, portSet.StudioPort)
	assert.Equal(t, defaultPortBases.metaPort, portSet.MetaPort)
	assert.Equal(t, defaultPortBases.imgProxyPort, portSet.ImgProxyPort)
}

// ── AllocatePorts: skips occupied ports ───────────────────────────────────────

func TestAllocatePorts_SkipsUsedPorts(t *testing.T) {
	// Project A occupies base ports.
	projectA := &domain.ProjectModel{Slug: "alpha"}
	configA := &domain.ProjectConfig{
		ProjectSlug: "alpha",
		Values: map[string]string{
			"KONG_HTTP_PORT":                "28081",
			"POSTGRES_PORT":                 "54320",
			"POOLER_PROXY_PORT_TRANSACTION": "64300",
			"STUDIO_PORT":                   "54323",
			"PG_META_PORT":                  "54380",
			"IMGPROXY_BIND":                 ":54381",
		},
	}

	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{
			ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
				return []*domain.ProjectModel{projectA}, nil
			},
		},
		&mockConfigRepo{
			GetConfigFn: func(_ context.Context, slug string) (*domain.ProjectConfig, error) {
				if slug == "alpha" {
					return configA, nil
				}
				return nil, store.ErrConfigNotFound
			},
		},
		func(_ int) bool { return true },
	)

	portSet, err := allocator.AllocatePorts(context.Background())
	require.NoError(t, err)
	assert.Greater(t, portSet.KongHTTP, defaultPortBases.kongHTTP)
	assert.Greater(t, portSet.PostgresPort, defaultPortBases.postgresPort)
}

// ── AllocatePorts: skips system-occupied TCP ports ───────────────────────────

func TestAllocatePorts_SkipsOccupiedTCPPorts(t *testing.T) {
	// Probe rejects base ports, accepts base+1 (and base+2 for KongHTTP's successor).
	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{},
		&mockConfigRepo{},
		func(port int) bool {
			// Only allow ports that are not the exact base values.
			bases := []int{
				defaultPortBases.kongHTTP,
				defaultPortBases.kongHTTP + 1,
				defaultPortBases.postgresPort,
				defaultPortBases.poolerPort,
				defaultPortBases.studioPort,
				defaultPortBases.metaPort,
				defaultPortBases.imgProxyPort,
			}
			for _, b := range bases {
				if port == b {
					return false
				}
			}
			return true
		},
	)

	portSet, err := allocator.AllocatePorts(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, defaultPortBases.kongHTTP, portSet.KongHTTP)
	assert.NotEqual(t, defaultPortBases.postgresPort, portSet.PostgresPort)
}

// ── AllocatePorts: flat usedSet prevents cross-type collisions ────────────────

func TestAllocatePorts_FlatUsedSet_PreventsCollision(t *testing.T) {
	// Project A uses MetaPort=54381, which is imgProxyPort base.
	// Without flat usedSet, ImgProxy would get 54381 (colliding with Meta).
	projectA := &domain.ProjectModel{Slug: "beta"}
	configA := &domain.ProjectConfig{
		ProjectSlug: "beta",
		Values: map[string]string{
			"KONG_HTTP_PORT":                "28081",
			"POSTGRES_PORT":                 "54320",
			"POOLER_PROXY_PORT_TRANSACTION": "64300",
			"STUDIO_PORT":                   "54323",
			"PG_META_PORT":                  "54381", // MetaPort = ImgProxy base
			"IMGPROXY_BIND":                 ":54382",
		},
	}

	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{
			ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
				return []*domain.ProjectModel{projectA}, nil
			},
		},
		&mockConfigRepo{
			GetConfigFn: func(_ context.Context, _ string) (*domain.ProjectConfig, error) {
				return configA, nil
			},
		},
		func(_ int) bool { return true },
	)

	portSet, err := allocator.AllocatePorts(context.Background())
	require.NoError(t, err)

	// All allocated ports must be unique.
	seen := make(map[int]bool)
	for _, p := range []int{portSet.KongHTTP, portSet.KongHTTP + 1, portSet.PostgresPort, portSet.PoolerPort, portSet.StudioPort, portSet.MetaPort, portSet.ImgProxyPort} {
		assert.False(t, seen[p], "port %d is duplicated in allocated PortSet", p)
		seen[p] = true
	}
}

// ── AllocatePorts: ErrConfigNotFound skips project ───────────────────────────

func TestAllocatePorts_SkipsProjectWithNoConfig(t *testing.T) {
	project := &domain.ProjectModel{Slug: "noconfig"}
	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{
			ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
				return []*domain.ProjectModel{project}, nil
			},
		},
		&mockConfigRepo{
			GetConfigFn: func(_ context.Context, _ string) (*domain.ProjectConfig, error) {
				return nil, store.ErrConfigNotFound
			},
		},
		func(_ int) bool { return true },
	)

	portSet, err := allocator.AllocatePorts(context.Background())
	require.NoError(t, err)
	assert.Equal(t, defaultPortBases.kongHTTP, portSet.KongHTTP)
}

// ── AllocatePorts: list error propagates ──────────────────────────────────────

func TestAllocatePorts_ListError_ReturnsError(t *testing.T) {
	listErr := errors.New("db down")
	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{
			ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
				return nil, listErr
			},
		},
		&mockConfigRepo{},
		func(_ int) bool { return true },
	)

	_, err := allocator.AllocatePorts(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
}

// ── AllocatePorts: GetConfig error propagates ─────────────────────────────────

func TestAllocatePorts_GetConfigError_ReturnsError(t *testing.T) {
	project := &domain.ProjectModel{Slug: "broken"}
	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{
			ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
				return []*domain.ProjectModel{project}, nil
			},
		},
		&mockConfigRepo{
			GetConfigFn: func(_ context.Context, _ string) (*domain.ProjectConfig, error) {
				return nil, errors.New("store failure")
			},
		},
		func(_ int) bool { return true },
	)

	_, err := allocator.AllocatePorts(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "store failure")
}

// ── AllocatePorts: port boundary ─────────────────────────────────────────────

func TestAllocatePorts_ExceedsPortRange_ReturnsErrNoAvailablePort(t *testing.T) {
	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{},
		&mockConfigRepo{},
		func(_ int) bool { return false }, // all ports "occupied"
	)
	allocator.bases.kongHTTP = 65534 // Only 65534+65535 available but probe rejects all

	_, err := allocator.AllocatePorts(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrNoAvailablePort)
}

// ── AllocatePorts: context cancellation ──────────────────────────────────────

func TestAllocatePorts_ContextCanceled_ReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{},
		&mockConfigRepo{},
		func(_ int) bool { return true },
	)

	_, err := allocator.AllocatePorts(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ── AllocatePorts: ExtractPortSet failure skips project ───────────────────────

func TestAllocatePorts_InvalidPortSet_SkipsProject(t *testing.T) {
	project := &domain.ProjectModel{Slug: "malformed"}
	configMalformed := &domain.ProjectConfig{
		ProjectSlug: "malformed",
		Values:      map[string]string{"KONG_HTTP_PORT": "not-a-number"},
	}

	called := false
	allocator := newComposePortAllocatorWithProbe(
		&mockProjectRepo{
			ListFn: func(_ context.Context, _ ...store.ListFilter) ([]*domain.ProjectModel, error) {
				called = true
				return []*domain.ProjectModel{project}, nil
			},
		},
		&mockConfigRepo{
			GetConfigFn: func(_ context.Context, _ string) (*domain.ProjectConfig, error) {
				return configMalformed, nil
			},
		},
		func(_ int) bool { return true },
	)

	portSet, err := allocator.AllocatePorts(context.Background())
	require.NoError(t, err)
	assert.True(t, called)
	// Should still allocate base ports since malformed project was skipped.
	assert.Equal(t, defaultPortBases.kongHTTP, portSet.KongHTTP)
}
