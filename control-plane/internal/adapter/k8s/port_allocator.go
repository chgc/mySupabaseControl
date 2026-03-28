// Package k8s implements domain.RuntimeAdapter using Kubernetes (Helm).
package k8s

import (
	"context"
	"fmt"
	"sync"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/store"
)

const (
	nodePortMin      = 30000 // Kubernetes NodePort minimum
	nodePortMax      = 32767 // Kubernetes NodePort maximum
	baseKongHTTP     = 30080 // KongHTTP NodePort base
	basePostgresPort = 30432 // PostgresPort NodePort base
)

// K8sPortAllocator implements domain.PortAllocator for Kubernetes NodePort allocation.
// The internal mutex serialises concurrent AllocatePorts calls within a single process.
type K8sPortAllocator struct {
	projectRepo store.ProjectRepository
	configRepo  store.ConfigRepository
	mu          sync.Mutex
}

// Static interface assertion.
var _ domain.PortAllocator = (*K8sPortAllocator)(nil)

// NewK8sPortAllocator returns a K8sPortAllocator backed by the given repositories.
func NewK8sPortAllocator(
	projectRepo store.ProjectRepository,
	configRepo store.ConfigRepository,
) *K8sPortAllocator {
	return &K8sPortAllocator{
		projectRepo: projectRepo,
		configRepo:  configRepo,
	}
}

// AllocatePorts scans existing Kubernetes projects, collects their allocated ports,
// then finds the first available NodePort for KongHTTP and PostgresPort.
// PoolerPort is always 0 (K8s Helm chart does not include supavisor).
func (a *K8sPortAllocator) AllocatePorts(ctx context.Context) (*domain.PortSet, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	projects, err := a.projectRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("k8s port allocator: list projects: %w", err)
	}

	usedSet := make(map[int]struct{})
	for _, p := range projects {
		if p.RuntimeType != domain.RuntimeKubernetes {
			continue
		}
		cfg, err := a.configRepo.GetConfig(ctx, p.Slug)
		if err != nil {
			if err == store.ErrConfigNotFound {
				continue
			}
			// Non-fatal: skip project whose config cannot be loaded.
			// TODO: replace with structured logger when available.
			_ = err
			continue
		}
		portSet, err := domain.ExtractPortSet(cfg)
		if err != nil {
			// Non-fatal: malformed config should not block new allocations.
			_ = err
			continue
		}
		usedSet[portSet.KongHTTP] = struct{}{}
		usedSet[portSet.PostgresPort] = struct{}{}
	}

	kongHTTP, err := a.findPort(baseKongHTTP, usedSet)
	if err != nil {
		return nil, err
	}
	usedSet[kongHTTP] = struct{}{}

	postgresPort, err := a.findPort(basePostgresPort, usedSet)
	if err != nil {
		return nil, err
	}

	return &domain.PortSet{
		KongHTTP:     kongHTTP,
		PostgresPort: postgresPort,
		PoolerPort:   0,
	}, nil
}

// findPort scans upward from base until it finds a port not in usedSet.
// Returns ErrNoAvailablePort if the NodePort range is exhausted.
func (a *K8sPortAllocator) findPort(base int, usedSet map[int]struct{}) (int, error) {
	for port := base; port <= nodePortMax; port++ {
		if _, used := usedSet[port]; !used {
			return port, nil
		}
	}
	return 0, fmt.Errorf("k8s port allocator: no available NodePort in range %d-%d: %w",
		base, nodePortMax, domain.ErrNoAvailablePort)
}
