// Package compose implements domain.RuntimeAdapter using Docker Compose v2.
package compose

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

// portBases holds the starting candidate port for each port type.
type portBases struct {
	kongHTTP     int
	postgresPort int
	poolerPort   int
	studioPort   int
	metaPort     int
	imgProxyPort int
}

// defaultPortBases matches the base values documented in domain.PortSet.
var defaultPortBases = portBases{
	kongHTTP:     28081,
	postgresPort: 54320,
	poolerPort:   64300,
	studioPort:   54323,
	metaPort:     54380,
	imgProxyPort: 54381,
}

// ComposePortAllocator implements domain.PortAllocator using the config store and TCP probing.
// The internal mutex serialises concurrent AllocatePorts calls within a single process.
// Cross-process races are handled by the store's UNIQUE constraint on port values.
type ComposePortAllocator struct {
	projectRepo store.ProjectRepository
	configRepo  store.ConfigRepository
	bases       portBases
	mu          sync.Mutex

	// probeFunc is a white-box field used in tests to replace TCP probing.
	// Production code uses probePort.
	probeFunc func(port int) bool
}

// Static interface assertion — fails to compile if ComposePortAllocator no longer
// satisfies domain.PortAllocator.
var _ domain.PortAllocator = (*ComposePortAllocator)(nil)

// NewComposePortAllocator returns a production ComposePortAllocator backed by TCP probing.
func NewComposePortAllocator(projectRepo store.ProjectRepository, configRepo store.ConfigRepository) *ComposePortAllocator {
	return newComposePortAllocatorWithProbe(projectRepo, configRepo, probePort)
}

// newComposePortAllocatorWithProbe constructs an allocator with an injected probe function.
// Used in unit tests to avoid real TCP operations.
func newComposePortAllocatorWithProbe(
	projectRepo store.ProjectRepository,
	configRepo store.ConfigRepository,
	probe func(int) bool,
) *ComposePortAllocator {
	return &ComposePortAllocator{
		projectRepo: projectRepo,
		configRepo:  configRepo,
		bases:       defaultPortBases,
		probeFunc:   probe,
	}
}

// probePort returns true if the given TCP port is available (can be listened on).
func probePort(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// AllocatePorts scans all existing projects, collects their allocated ports,
// then finds the first available port for each type that is not used and passes
// a TCP probe. A flat usedSet prevents cross-portType collisions.
//
// The mutex provides best-effort single-process serialisation; multi-process
// safety relies on the store's UNIQUE constraint.
func (a *ComposePortAllocator) AllocatePorts(ctx context.Context) (*domain.PortSet, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Step 1: list all non-destroyed projects.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	projects, err := a.projectRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("compose port allocator: list projects: %w", err)
	}

	// Step 2: build flat usedSet across all port types.
	usedSet := make(map[int]struct{})
	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cfg, err := a.configRepo.GetConfig(ctx, p.Slug)
		if err != nil {
			if err == store.ErrConfigNotFound {
				continue
			}
			return nil, fmt.Errorf("compose port allocator: get config for %q: %w", p.Slug, err)
		}
		portSet, err := domain.ExtractPortSet(cfg)
		if err != nil {
			// Non-fatal: warn and skip (malformed config should not block new allocations).
			// TODO: replace with structured logger when use-case logging is introduced.
			_ = err
			continue
		}
		usedSet[portSet.KongHTTP] = struct{}{}
		usedSet[portSet.KongHTTP+1] = struct{}{} // KongHTTPS is derived
		usedSet[portSet.PostgresPort] = struct{}{}
		usedSet[portSet.PoolerPort] = struct{}{}
		usedSet[portSet.StudioPort] = struct{}{}
		usedSet[portSet.MetaPort] = struct{}{}
		usedSet[portSet.ImgProxyPort] = struct{}{}
	}

	// Step 3: allocate each port type.
	kongHTTP, err := a.findPort(ctx, a.bases.kongHTTP, usedSet, true)
	if err != nil {
		return nil, err
	}
	// Mark KongHTTPS as used so subsequent types don't collide with it.
	usedSet[kongHTTP] = struct{}{}
	usedSet[kongHTTP+1] = struct{}{}

	postgresPort, err := a.findPort(ctx, a.bases.postgresPort, usedSet, false)
	if err != nil {
		return nil, err
	}
	usedSet[postgresPort] = struct{}{}

	poolerPort, err := a.findPort(ctx, a.bases.poolerPort, usedSet, false)
	if err != nil {
		return nil, err
	}
	usedSet[poolerPort] = struct{}{}

	studioPort, err := a.findPort(ctx, a.bases.studioPort, usedSet, false)
	if err != nil {
		return nil, err
	}
	usedSet[studioPort] = struct{}{}

	metaPort, err := a.findPort(ctx, a.bases.metaPort, usedSet, false)
	if err != nil {
		return nil, err
	}
	usedSet[metaPort] = struct{}{}

	imgProxyPort, err := a.findPort(ctx, a.bases.imgProxyPort, usedSet, false)
	if err != nil {
		return nil, err
	}

	return &domain.PortSet{
		KongHTTP:     kongHTTP,
		PostgresPort: postgresPort,
		PoolerPort:   poolerPort,
		StudioPort:   studioPort,
		MetaPort:     metaPort,
		ImgProxyPort: imgProxyPort,
	}, nil
}

// findPort scans upward from base until a candidate port (and optionally its
// successor for KongHTTP) is available both in usedSet and via TCP probe.
// Returns ErrNoAvailablePort if the scan exceeds the valid port range.
func (a *ComposePortAllocator) findPort(ctx context.Context, base int, usedSet map[int]struct{}, needSuccessor bool) (int, error) {
	for c := base; ; c++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		// Upper bound: for KongHTTP we need c+1 ≤ 65535; for others c ≤ 65535.
		if needSuccessor && c+1 > 65535 {
			return 0, domain.ErrNoAvailablePort
		}
		if !needSuccessor && c > 65535 {
			return 0, domain.ErrNoAvailablePort
		}

		if _, used := usedSet[c]; used {
			continue
		}
		if needSuccessor {
			if _, used := usedSet[c+1]; used {
				continue
			}
		}

		if !a.probeFunc(c) {
			continue
		}
		if needSuccessor && !a.probeFunc(c+1) {
			continue
		}

		return c, nil
	}
}
